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
	logger *slog.Logger,
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

func startBackends(ctx context.Context, backends map[string]roundtable.Backend, logger *slog.Logger) {
	for name, b := range backends {
		if err := b.Start(ctx); err != nil {
			logger.Error("failed to start backend", "backend", name, "error", err)
		}
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

	mode := strings.TrimSpace(os.Getenv("ROUNDTABLE_HTTP_BACKEND_MODE"))
	if mode == "" {
		mode = "native"
	}

	var app *httpmcp.App
	var backends map[string]roundtable.Backend

	switch mode {
	case "native":
		logger.Info("starting in native dispatch mode")

		// CodexRPC (app-server RPC from Phase 2A) is the primary Codex path.
		// If it fails to start, degrade to CodexFallback (CLI wrapper).
		var codexBackend roundtable.Backend
		codexRPC := roundtable.NewCodexBackend("", "")
		if err := codexRPC.Start(context.Background()); err != nil {
			logger.Warn("CodexRPC failed to start, falling back to CodexFallback", "error", err)
			codexBackend = roundtable.NewCodexFallbackBackend("", "")
		} else {
			codexBackend = codexRPC
		}

		backends = map[string]roundtable.Backend{
			"gemini": roundtable.NewGeminiBackend(""),
			"codex":  codexBackend,
			"claude": roundtable.NewClaudeBackend(""),
		}

		ctx := context.Background()
		startBackends(ctx, backends, logger)
		defer stopBackends(backends, logger)

		// Build BackendProbe map for readyz health probes.
		probes := make(map[string]httpmcp.BackendProbe, len(backends))
		for name, b := range backends {
			probes[name] = b
		}

		dispatch := buildDispatchFunc(backends, config, logger)
		app = httpmcp.NewAppWithDispatcherAndBackends(config, dispatch, probes)

	case "cli":
		logger.Info("starting in CLI backend mode (legacy)")

		if path, err := config.ResolvedBackendPath(); err == nil {
			logger.Info("resolved backend", "path", path)
		} else {
			logger.Warn("backend not ready at startup", "error", err)
		}

		app = httpmcp.NewApp(config, nil)

	default:
		logger.Error("unknown ROUNDTABLE_HTTP_BACKEND_MODE", "mode", mode)
		os.Exit(1)
	}

	logger.Info("starting roundtable HTTP MCP server",
		"addr", config.Addr,
		"mcp_path", config.MCPPath,
		"mode", mode,
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	server := &http.Server{
		Addr:              config.Addr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: config.ProbeTimeout,
	}

	// Start server in background, shut down gracefully on signal
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
