package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/TejGandham/roundtable/internal/roundtable"
	"github.com/TejGandham/roundtable/internal/stdiomcp"
)

// version is the release string injected via -ldflags "-X main.version=..."
// at build time. Defaults to "dev" for ad-hoc `go build` / `go run`.
var version = "dev"

const usage = `roundtable — multi-model consensus MCP server

Usage:
  roundtable stdio          Run the MCP server on stdin/stdout (used by Claude Code)
  roundtable version        Print version and exit
  roundtable help           Print this help

Environment:
  ROUNDTABLE_GEMINI_PATH        Absolute path to gemini binary (optional)
  ROUNDTABLE_CODEX_PATH         Absolute path to codex binary (optional)
  ROUNDTABLE_CLAUDE_PATH        Absolute path to claude binary (optional)
  ROUNDTABLE_ROLES_DIR          Override global roles directory
  ROUNDTABLE_PROJECT_ROLES_DIR  Per-project role overrides
  ROUNDTABLE_PROVIDERS          JSON array of OpenAI-compatible HTTP providers
`

func main() {
	// MUST be first. See internal/stdiomcp.InitStdioDiscipline docs.
	logger := stdiomcp.InitStdioDiscipline()

	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	// Global --help / -h / help short-circuit, so `roundtable stdio --help`
	// prints usage instead of entering the MCP loop and hanging on stdin.
	for _, a := range args {
		if a == "--help" || a == "-h" || a == "help" {
			fmt.Fprint(os.Stdout, usage)
			return
		}
	}

	switch args[0] {
	case "stdio":
		if len(args) > 1 {
			fmt.Fprintf(os.Stderr, "stdio: unexpected arguments %q\n\n%s", args[1:], usage)
			os.Exit(2)
		}
		runStdio(logger)
	case "version", "--version":
		if len(args) > 1 {
			fmt.Fprintf(os.Stderr, "version: unexpected arguments %q\n", args[1:])
			os.Exit(2)
		}
		fmt.Fprintln(os.Stdout, "roundtable", version)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n%s", args[0], usage)
		os.Exit(2)
	}
}

func runStdio(logger *slog.Logger) {
	backends, _ := buildBackends(logger, nil)
	defer stopBackends(backends, logger)

	cfg := stdiomcp.Config{
		RolesDir:        rolesDirEnv(logger, "ROUNDTABLE_ROLES_DIR", "ROUNDTABLE_HTTP_ROLES_DIR"),
		ProjectRolesDir: rolesDirEnv(logger, "ROUNDTABLE_PROJECT_ROLES_DIR", "ROUNDTABLE_HTTP_PROJECT_ROLES_DIR"),
		ServerName:      "roundtable",
		ServerVersion:   version,
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

// buildBackends constructs the model backends. Subprocess backends
// (gemini/codex/claude) are always registered. HTTP providers come from
// ROUNDTABLE_PROVIDERS (see internal/roundtable/providers.go). Providers
// whose api_key_env is empty are silently skipped.
func buildBackends(logger *slog.Logger, observe roundtable.ObserveFunc) (map[string]roundtable.Backend, []roundtable.ProviderInfo) {
	var codexBackend roundtable.Backend
	codexPath := roundtable.ResolveExecutable("codex")
	if codexPath != "" {
		codexBackend = roundtable.NewCodexBackend(codexPath, "", version)
		logger.Info("codex backend configured (lazy start)", "path", codexPath)
	} else {
		logger.Warn("codex binary not found, using CodexFallback")
		codexBackend = roundtable.NewCodexFallbackBackend("", "")
	}

	backends := map[string]roundtable.Backend{
		"gemini": roundtable.NewGeminiBackend(""),
		"codex":  codexBackend,
		"claude": roundtable.NewClaudeBackend(""),
	}

	var infos []roundtable.ProviderInfo

	configs, err := roundtable.LoadProviderRegistry(os.Getenv)
	if err != nil {
		logger.Error("ROUNDTABLE_PROVIDERS parse failed; no HTTP providers registered", "error", err)
		return backends, infos
	}

	for _, c := range configs {
		if os.Getenv(c.APIKeyEnv) == "" {
			logger.Warn("provider skipped — credential env var unset",
				"id", c.ID, "api_key_env", c.APIKeyEnv)
			continue
		}
		backends[c.ID] = roundtable.NewOpenAIHTTPBackend(c, observe)
		infos = append(infos, roundtable.ProviderInfo{
			ID: c.ID, BaseURL: c.BaseURL, DefaultModel: c.DefaultModel,
		})
		logger.Info("provider registered",
			"id", c.ID,
			"base_url", c.BaseURL,
			"default_model", c.DefaultModel,
			"max_concurrent", c.MaxConcurrent)
	}

	return backends, infos
}

// rolesDirEnv reads the new env var, falling back to the legacy HTTP-era
// name with a one-shot deprecation log. Remove the fallback in v0.9.
func rolesDirEnv(logger *slog.Logger, name, legacy string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	if v := os.Getenv(legacy); v != "" {
		logger.Warn("deprecated env var; rename before v0.9",
			"deprecated", legacy, "use", name)
		return v
	}
	return ""
}

func stopBackends(backends map[string]roundtable.Backend, logger *slog.Logger) {
	for name, b := range backends {
		if err := b.Stop(); err != nil {
			logger.Error("failed to stop backend", "backend", name, "error", err)
		}
	}
}

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
