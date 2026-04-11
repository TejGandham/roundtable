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
	"github.com/TejGandham/roundtable/internal/stdiomcp"
)

func main() {
	// MUST be first. See internal/stdiomcp.InitStdioDiscipline docs.
	logger := stdiomcp.InitStdioDiscipline()

	args := os.Args[1:]
	if len(args) > 0 && args[0] == "stdio" {
		runStdio(logger)
		return
	}
	runHTTP(logger)
}

func runStdio(logger *slog.Logger) {
	backends := buildBackends(logger)
	defer stopBackends(backends, logger)

	cfg := stdiomcp.Config{
		RolesDir:        os.Getenv("ROUNDTABLE_HTTP_ROLES_DIR"),
		ProjectRolesDir: os.Getenv("ROUNDTABLE_HTTP_PROJECT_ROLES_DIR"),
		ServerName:      "roundtable",
		ServerVersion:   "0.8.0-dev",
	}
	dispatch := buildStdioDispatch(backends, cfg)
	srv := stdiomcp.NewServer(cfg, dispatch, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	logger.Info("roundtable stdio MCP server starting")
	if err := stdiomcp.Serve(ctx, srv); err != nil {
		logger.Error("stdio serve error", "error", err)
		os.Exit(1)
	}
}

func runHTTP(logger *slog.Logger) {
	config := httpmcp.LoadConfig(logger)
	logger.Info("starting roundtable MCP server (HTTP, legacy — will be removed in Phase C)")

	backends := buildBackends(logger)
	defer stopBackends(backends, logger)

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

// buildBackends constructs the three model backends. Shared between the
// stdio and HTTP entry points.
func buildBackends(logger *slog.Logger) map[string]roundtable.Backend {
	var codexBackend roundtable.Backend
	codexPath := roundtable.ResolveExecutable("codex")
	if codexPath != "" {
		codexBackend = roundtable.NewCodexBackend(codexPath, "")
		logger.Info("codex backend configured (lazy start)", "path", codexPath)
	} else {
		logger.Warn("codex binary not found, using CodexFallback")
		codexBackend = roundtable.NewCodexFallbackBackend("", "")
	}
	return map[string]roundtable.Backend{
		"gemini": roundtable.NewGeminiBackend(""),
		"codex":  codexBackend,
		"claude": roundtable.NewClaudeBackend(""),
	}
}

func stopBackends(backends map[string]roundtable.Backend, logger *slog.Logger) {
	for name, b := range backends {
		if err := b.Stop(); err != nil {
			logger.Error("failed to stop backend", "backend", name, "error", err)
		}
	}
}

// buildDispatchFunc is the HTTP-path dispatch adapter. Unchanged from
// before except it was lifted out of main(). Removed in Phase C1.
func buildDispatchFunc(
	backends map[string]roundtable.Backend,
	config httpmcp.Config,
) httpmcp.DispatchFunc {
	return func(ctx context.Context, spec httpmcp.ToolSpec, input httpmcp.ToolInput) ([]byte, error) {
		var agents []roundtable.AgentSpec
		if input.Agents != "" {
			parsed, err := roundtable.ParseAgents(input.Agents)
			if err != nil {
				return nil, err
			}
			agents = parsed
		}
		var files []string
		if input.Files != "" {
			for _, f := range strings.Split(input.Files, ",") {
				f = strings.TrimSpace(f)
				if f != "" {
					files = append(files, f)
				}
			}
		}
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

// buildStdioDispatch is the stdio-path dispatch adapter. Nearly identical
// to buildDispatchFunc but uses stdiomcp.ToolSpec / ToolInput instead of
// the httpmcp types. Phase C1 collapses both into one.
func buildStdioDispatch(
	backends map[string]roundtable.Backend,
	cfg stdiomcp.Config,
) stdiomcp.DispatchFunc {
	return func(ctx context.Context, spec stdiomcp.ToolSpec, input stdiomcp.ToolInput) ([]byte, error) {
		var agents []roundtable.AgentSpec
		if input.Agents != "" {
			parsed, err := roundtable.ParseAgents(input.Agents)
			if err != nil {
				return nil, err
			}
			agents = parsed
		}
		var files []string
		if input.Files != "" {
			for _, f := range strings.Split(input.Files, ",") {
				f = strings.TrimSpace(f)
				if f != "" {
					files = append(files, f)
				}
			}
		}
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
			RolesDir:        cfg.RolesDir,
			ProjectRolesDir: cfg.ProjectRolesDir,
		}
		return roundtable.Run(ctx, req, backends)
	}
}
