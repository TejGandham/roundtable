package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/TejGandham/roundtable/internal/httpmcp"
	"github.com/TejGandham/roundtable/internal/roundtable"
)

func buildDispatchFunc(
	backends map[string]roundtable.Backend,
	config httpmcp.Config,
) httpmcp.DispatchFunc {
	return func(ctx context.Context, spec httpmcp.ToolSpec, input httpmcp.ToolInput) ([]byte, error) {
		// Parse agents from JSON string
		var agents []roundtable.AgentSpec
		if input.Agents != "" {
			parsed, err := roundtable.ParseAgents(input.Agents)
			if err != nil {
				return nil, err
			}
			agents = parsed
		}

		// Parse files from comma-separated string
		var files []string
		if input.Files != "" {
			for _, f := range strings.Split(input.Files, ",") {
				f = strings.TrimSpace(f)
				if f != "" {
					files = append(files, f)
				}
			}
		}

		// Resolve timeout
		timeout := 900
		if input.Timeout != nil && *input.Timeout > 0 {
			timeout = *input.Timeout
		}

		req := roundtable.ToolRequest{
			Prompt:          input.Prompt,
			PromptSuffix:    spec.PromptSuffix,
			Files:           files,
			Timeout:         timeout,
			Role:            spec.Role,
			GeminiRole:      spec.GeminiRole,
			CodexRole:       spec.CodexRole,
			ClaudeRole:      spec.ClaudeRole,
			GeminiModel:     input.GeminiModel,
			CodexModel:      input.CodexModel,
			ClaudeModel:     input.ClaudeModel,
			GeminiResume:    input.GeminiResume,
			CodexResume:     input.CodexResume,
			ClaudeResume:    input.ClaudeResume,
			Agents:          agents,
			RolesDir:        config.RolesDir,
			ProjectRolesDir: config.ProjectRolesDir,
		}

		return roundtable.Run(ctx, req, backends)
	}
}

func stopBackends(backends map[string]roundtable.Backend, logger *slog.Logger) {
	for name, b := range backends {
		if err := b.Stop(); err != nil {
			logger.Error("failed to stop backend", "backend", name, "error", err)
		}
	}
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	config := httpmcp.LoadConfig(logger)

	logger.Info("starting roundtable HTTP MCP server (native Go dispatch)")

	// Construct native backends. CodexRPC (app-server) is the primary Codex
	// path; if it fails to start, degrade to CodexFallback (subprocess-per-request).
	var codexBackend roundtable.Backend
	codexPath := roundtable.ResolveExecutable("codex")
	if codexPath != "" {
		codexRPC := roundtable.NewCodexBackend(codexPath, "")
		if err := codexRPC.Start(context.Background()); err != nil {
			logger.Warn("CodexRPC failed to start, falling back to CodexFallback", "error", err)
			codexBackend = roundtable.NewCodexFallbackBackend("", "")
		} else {
			logger.Info("CodexRPC app-server started", "path", codexPath)
			codexBackend = codexRPC
		}
	} else {
		logger.Warn("codex binary not found, using CodexFallback")
		codexBackend = roundtable.NewCodexFallbackBackend("", "")
	}

	backends := map[string]roundtable.Backend{
		"gemini": roundtable.NewGeminiBackend(""),
		"codex":  codexBackend,
		"claude": roundtable.NewClaudeBackend(""),
	}
	defer stopBackends(backends, logger)

	// Build BackendProbe map for readyz health checks.
	probes := make(map[string]httpmcp.BackendProbe, len(backends))
	for name, b := range backends {
		probes[name] = b
	}

	dispatch := buildDispatchFunc(backends, config)
	app := httpmcp.NewApp(config, dispatch, probes)

	logger.Info("roundtable HTTP MCP server listening",
		"addr", config.Addr,
		"mcp_path", config.MCPPath,
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	server := &http.Server{
		Addr:              config.Addr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 30 * time.Second,
	}

	serverErrCh := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrCh <- err
		}
		close(serverErrCh)
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutting down gracefully")
	case err := <-serverErrCh:
		if err != nil {
			logger.Error("server exited", "error", err)
			os.Exit(1)
		}
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
	}
}
