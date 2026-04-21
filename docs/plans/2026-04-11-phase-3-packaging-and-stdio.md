# Phase 3: Packaging and stdio transport

**Date:** 2026-04-11
**Branch:** `go-phase3-stdio`
**Status:** Phases A and B landed (stdio subcommand shipping alongside HTTP, assumptions verified). Phases C–E still pending: HTTP deletion + binary rename, goreleaser/install.sh, and docs rewrite.
**Owner:** Tej

## Goal

Go from "HTTP MCP on 127.0.0.1:4040 with manual daemon start" to: **one `curl | sh` command installs a single static Go binary; one `claude mcp add` line registers it; Claude Code launches it over stdio on demand.**

No HTTP. No Homebrew. No asset directories. No supervisor. No README paragraphs the user has to read before the tool works.

## Remaining work

Phases C–E are what lets **other** people install Roundtable with one command. Phase D is where real users benefit.

- **Phase C** — delete `internal/httpmcp`, rename `cmd/roundtable-http-mcp` → `cmd/roundtable`, flip no-arg default to stdio, drop HTTP config fields and tests, update Makefile.
- **Phase D** — goreleaser config, Forgejo release workflow with GitHub mirror push, install.sh with cosign-signed checksums, Forgejo CI workflow, cut v0.8.0.
- **Phase E** — rewrite INSTALL.md for the single-binary stdio flow; update README / SKILL / ARCHITECTURE / RELEASING.

Phase A (stdio discipline, subcommand, lazy Codex, orphan supervision, e2e tests) and Phase B (codex cold-start p95 = 109 ms, Claude Code crash-recovery contract documented) landed on 2026-04-11; see `2026-04-11-phase-a-dogfood-results.md` and `2026-04-11-phase-b-verification-results.md`.

## Architecture

```
Before (v0.7.0):                       After (v0.8.0):
                                       
  Claude Code                            Claude Code
      |                                      |
      v HTTP POST /mcp                       v fork/exec + stdio pipes
  roundtable-http-mcp                    roundtable stdio
  127.0.0.1:4040                             |
      |                                      +-- gemini (subprocess per req)
      +-- gemini (subprocess per req)        +-- codex app-server (lazy, long-lived)
      +-- codex app-server (eager)           +-- claude (subprocess per req)
      +-- claude (subprocess per req)
```

Key differences:

|before|after|
|-|-|
|HTTP StreamableHTTPHandler on 127.0.0.1:4040|`mcp.StdioTransport{}` on os.Stdin/os.Stdout|
|User runs `nohup roundtable-http-mcp &` manually|Claude Code spawns `roundtable stdio` lazily|
|Codex app-server launched eagerly at startup|Codex app-server launched on first codex request (sync.Once)|
|`claude mcp add --transport http ...`|`claude mcp add -s user roundtable -- /abs/path/roundtable stdio`|
|HTTP healthz/readyz/metricsz endpoints|none (stdio servers are health-checked by being alive)|
|Binary name `roundtable-http-mcp`|Binary name `roundtable`|
|Tarball on some ad-hoc host|goreleaser -> GitHub Releases, cosign-signed, curl install.sh|

## Tech stack

- Go 1.26.2 (via mise)
- `github.com/modelcontextprotocol/go-sdk` v1.5.0 — verified API:
  - `mcp.NewServer(&mcp.Implementation{...}, nil)` — already used
  - `mcp.AddTool(server, ...)` — already used
  - `server.Run(ctx, &mcp.StdioTransport{})` — new, blocking stdio loop
  - Client side for tests: `mcp.CommandTransport{Command: exec.Command(...)}` (used to spawn the server-under-test) and `mcp.InMemoryTransport` pair for in-process tests
- goreleaser v2 for release artifacts (darwin/linux × amd64/arm64)
- cosign (keyed) for checksum signing
- Forgejo workflows for CI + release, mirrored to GitHub for the public release CDN
- No changes to internal/roundtable/ dispatch logic (run.go, prompt.go, roles.go, backends) except CodexBackend lazy-start

## Command prefix

All Go commands in this plan assume the env prefix:

```
GOTOOLCHAIN=local GOMODCACHE=/tmp/gomodcache GOCACHE=/tmp/gocache mise exec go@1.26.2 -- go ...
```

Abbreviated as `$GO` in example output. The Makefile's `$(GO_ENV) $(GO)` expansion is equivalent.

## Roundtable review must-fix items — mapping to tasks

|review item|severity|task|
|-|-|-|
|Codex orphans on SIGKILL — need process group + Pdeathsig|CRITICAL|A3|
|Stdout pollution wedges stdio MCP|CRITICAL|A1|
|`claude mcp add` name-before-`--` syntax|CRITICAL|E1, D3|
|Tailscale Forgejo breaks curl install — need GitHub mirror CDN|HIGH|D2, D3|
|macOS Gatekeeper quarantine on unsigned tarballs|HIGH|D3|
|Lazy Codex conflicts with eager Healthy contract|HIGH|A4|
|install.sh supply-chain (cosign-signed checksums)|HIGH|D1, D3|
|Official Go SDK stdio API verified|HIGH|A2 (already verified above)|
|PATH inheritance from Claude Code on macOS|MEDIUM|D3|
|Codex startup cost unverified|MEDIUM|B1|
|Claude Code crash recovery untested|MEDIUM|B2|
|No-arg binary must not hang on stdin|MEDIUM|C4|
|Rename `roundtable-http-mcp` -> `roundtable`|LOW|C3|
|Delete HTTP path entirely|LOW|C1, C2, C5|

## Phase A: stdio subcommand alongside HTTP (dogfood)

Goal: ship a `roundtable stdio` subcommand that works end-to-end inside Claude Code, without touching the existing HTTP path. This is the "make sure the hard parts work before we ship them" milestone.

### Task A1: Stdio discipline guard

**Files**
- Create: `internal/stdiomcp/discipline.go`
- Modify: `cmd/roundtable-http-mcp/main.go` — add `InitStdioDiscipline()` call on the very first line of `main()` (no-op for HTTP mode but keeps both paths symmetric)

**Why**
Any `fmt.Println`, bare `log.Print`, panic stack trace, or dep that writes to os.Stdout will corrupt MCP framing and wedge the stdio session. The fix is a single function called before any other code runs that redirects `log` and the default slog handler to stderr, and a CI test that asserts the first byte on stdout after spawn is `{`.

**Steps**
- [ ] Create `internal/stdiomcp/discipline.go`:

```go
// Package stdiomcp contains helpers for running roundtable over MCP stdio.
//
// The single most important rule of stdio MCP servers: NOTHING may ever
// write to os.Stdout except the MCP framing layer. A stray fmt.Println, a
// panic stack trace on stdout, or a dep that logs to stdout on import will
// corrupt JSON-RPC frames and wedge the session.
package stdiomcp

import (
	"io"
	"log"
	"log/slog"
	"os"
)

// InitStdioDiscipline redirects the standard `log` package and the default
// slog logger to stderr, and returns a logger the caller can use.
//
// Call this as the VERY FIRST line of main() in any binary that may run in
// stdio mode. It is safe to call in HTTP mode too — stderr logging is fine
// for both.
func InitStdioDiscipline() *slog.Logger {
	// Force the legacy `log` package to stderr. Deps that call log.Printf
	// (there are many) will then not pollute stdout.
	log.SetOutput(os.Stderr)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	// Structured logger to stderr.
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	return logger
}

// GuardStdout wraps os.Stdout so accidental writes during startup panic
// loudly on stderr. Used by tests, not main(). Returns a restore func.
func GuardStdout() (restore func()) {
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				// Echo to stderr so the test framework captures it.
				_, _ = os.Stderr.Write([]byte("STDOUT LEAK: "))
				_, _ = os.Stderr.Write(buf[:n])
			}
			if err == io.EOF || err != nil {
				return
			}
		}
	}()
	return func() {
		_ = w.Close()
		os.Stdout = orig
	}
}
```

- [ ] Add `internal/stdiomcp/discipline_test.go`:

```go
package stdiomcp

import (
	"log"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestInitStdioDiscipline_RedirectsLog(t *testing.T) {
	// Capture stderr.
	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() { os.Stderr = origStderr }()

	_ = InitStdioDiscipline()
	log.Printf("hello from log")
	slog.Info("hello from slog")

	_ = w.Close()
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	got := string(buf[:n])

	if !strings.Contains(got, "hello from log") {
		t.Errorf("log.Printf output did not go to stderr: %q", got)
	}
	if !strings.Contains(got, "hello from slog") {
		t.Errorf("slog output did not go to stderr: %q", got)
	}
}
```

- [ ] Modify `cmd/roundtable-http-mcp/main.go`: replace the opening lines of `main()`.

```go
func main() {
	// MUST be first. See internal/stdiomcp.InitStdioDiscipline docs.
	logger := stdiomcp.InitStdioDiscipline()

	config := httpmcp.LoadConfig(logger)
	logger.Info("starting roundtable MCP server (HTTP, legacy — will be removed in C1)")
	// ... rest unchanged for now
}
```

**Test**
```
$GO test ./internal/stdiomcp/...
```
Expected: `ok  github.com/TejGandham/roundtable/internal/stdiomcp`

**Commit**
```
feat(stdio): add stdio discipline guard for log/slog

Redirects the legacy log package and default slog logger to stderr so
stdio MCP framing cannot be corrupted by stray writes. Called as the
first line of main() in the HTTP binary (harmless there) and will be
called from the new stdio subcommand in A2.
```

---

### Task A2: Add `roundtable stdio` subcommand

**Files**
- Create: `internal/stdiomcp/server.go` — wires the same tool specs into an `mcp.Server` and runs it on `mcp.StdioTransport`
- Modify: `cmd/roundtable-http-mcp/main.go` — parse first arg; `stdio` subcommand dispatches to stdiomcp.Serve, everything else keeps HTTP for now
- Modify: `Makefile` — add `make run-stdio` target

**Why**
Phase A is about reusing every line of existing tool logic over a new transport. The tool handler in `internal/httpmcp/server.go`'s `registerTool()` is transport-agnostic — it already takes a `DispatchFunc`. We lift `toolSpecs`, `toolInputSchema`, and `registerTool` into the shared path so both servers can mount them.

Rather than refactor httpmcp now (risk of breaking Phase A dogfooding), we copy `toolSpecs`/`toolInputSchema`/`registerTool` into `internal/stdiomcp/` as-is for Phase A. Phase C1 deletes the httpmcp copies.

**Steps**
- [ ] Create `internal/stdiomcp/types.go`:

```go
package stdiomcp

// ToolInput is the MCP tool input schema shared by all five Roundtable
// tools. Mirrored from internal/httpmcp/backend.go for Phase A — Phase
// C1 deletes the httpmcp copy and moves this file up to the canonical
// location.
type ToolInput struct {
	Prompt       string `json:"prompt"`
	Files        string `json:"files,omitempty"`
	Timeout      *int   `json:"timeout,omitempty"`
	GeminiModel  string `json:"gemini_model,omitempty"`
	CodexModel   string `json:"codex_model,omitempty"`
	ClaudeModel  string `json:"claude_model,omitempty"`
	GeminiResume string `json:"gemini_resume,omitempty"`
	CodexResume  string `json:"codex_resume,omitempty"`
	ClaudeResume string `json:"claude_resume,omitempty"`
	Agents       string `json:"agents,omitempty"`
}

type ToolSpec struct {
	Name         string
	Description  string
	PromptSuffix string
	Role         string
	GeminiRole   string
	CodexRole    string
	ClaudeRole   string
}

// DispatchFunc is the transport-agnostic dispatch entry point. Both the
// stdio and (legacy) HTTP servers call the same signature.
type DispatchFunc func(ctx context.Context, spec ToolSpec, input ToolInput) ([]byte, error)
```

- [ ] Create `internal/stdiomcp/server.go`:

```go
package stdiomcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var toolSpecs = []ToolSpec{
	{
		Name:        "hivemind",
		Description: "Run multi-model consensus with default role across all models.",
		Role:        "default",
	},
	{
		Name:         "deepdive",
		Description:  "Run deeper analysis consensus using planner role across all models.",
		Role:         "planner",
		PromptSuffix: "\n\nProvide conclusions, assumptions, alternatives, and confidence level.",
	},
	{
		Name:         "architect",
		Description:  "Generate implementation architecture with planner role across models.",
		Role:         "planner",
		PromptSuffix: "\n\nProvide phases, dependencies, risks, and milestones.",
	},
	{
		Name:         "challenge",
		Description:  "Run critical review consensus using codereviewer role across models.",
		Role:         "codereviewer",
		PromptSuffix: "\n\nAct as a critical reviewer. Find flaws, risks, and weaknesses.",
	},
	{
		Name:        "xray",
		Description: "Run architecture and quality xray with per-model role assignments.",
		GeminiRole:  "planner",
		CodexRole:   "codereviewer",
		ClaudeRole:  "default",
	},
}

var toolInputSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "prompt": {"type": "string"},
    "files": {"type": "string"},
    "timeout": {"type": "integer", "minimum": 1, "maximum": 900},
    "gemini_model": {"type": "string"},
    "codex_model": {"type": "string"},
    "claude_model": {"type": "string"},
    "gemini_resume": {"type": "string"},
    "codex_resume": {"type": "string"},
    "claude_resume": {"type": "string"},
    "agents": {"type": "string"}
  },
  "required": ["prompt"]
}`)

// Config is the subset of runtime config that the stdio server needs.
// Compare with internal/httpmcp/config.go — this one intentionally has
// no HTTP fields (Addr, MCPPath, ProbeTimeout).
type Config struct {
	RolesDir        string
	ProjectRolesDir string
	ServerName      string
	ServerVersion   string
}

// NewServer constructs an mcp.Server with all five roundtable tools
// registered against the given dispatch function. It does NOT connect
// to any transport — the caller passes the returned *mcp.Server to
// server.Run(ctx, transport).
func NewServer(cfg Config, dispatch DispatchFunc, logger *slog.Logger) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    cfg.ServerName,
		Version: cfg.ServerVersion,
	}, nil)

	for _, spec := range toolSpecs {
		registerTool(srv, spec, dispatch, logger)
	}
	return srv
}

func registerTool(srv *mcp.Server, spec ToolSpec, dispatch DispatchFunc, logger *slog.Logger) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        spec.Name,
		Description: spec.Description,
		InputSchema: toolInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest, input ToolInput) (*mcp.CallToolResult, any, error) {
		token := req.Params.GetProgressToken()

		type callResult struct {
			text    string
			isError bool
		}
		done := make(chan callResult, 1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("dispatch panic", "tool", spec.Name, "panic", r)
					done <- callResult{text: fmt.Sprintf("internal error: %v", r), isError: true}
				}
			}()
			data, err := dispatch(ctx, spec, input)
			if err != nil {
				logger.Error("dispatch error", "tool", spec.Name, "error", err)
				done <- callResult{text: fmt.Sprintf("roundtable dispatch error: %v", err), isError: true}
				return
			}
			done <- callResult{text: string(data), isError: false}
		}()

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		ticks := 0
		for {
			select {
			case result := <-done:
				return &mcp.CallToolResult{
					IsError: result.isError,
					Content: []mcp.Content{&mcp.TextContent{Text: result.text}},
				}, nil, nil
			case <-ctx.Done():
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("request cancelled: %v", ctx.Err())}},
				}, nil, nil
			case <-ticker.C:
				ticks++
				if token != nil && req.Session != nil {
					_ = req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
						ProgressToken: token,
						Progress:      float64(ticks),
						Message:       "backend running",
					})
				}
			}
		}
	})
}

// Serve wires the server to an mcp.StdioTransport and blocks until ctx
// is cancelled or stdin is closed.
func Serve(ctx context.Context, srv *mcp.Server) error {
	return srv.Run(ctx, &mcp.StdioTransport{})
}
```

- [ ] Modify `cmd/roundtable-http-mcp/main.go` — add subcommand dispatch at the top of `main()`:

```go
package main

import (
	"context"
	"fmt"
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
	logger := stdiomcp.InitStdioDiscipline()

	args := os.Args[1:]
	if len(args) > 0 && args[0] == "stdio" {
		runStdio(logger)
		return
	}

	// Legacy HTTP path — removed in Phase C.
	runHTTP(logger)
}

func runStdio(logger *slog.Logger) {
	backends := buildBackends(logger)
	defer stopBackends(backends, logger)

	cfg := stdiomcp.Config{
		RolesDir:        os.Getenv("ROUNDTABLE_ROLES_DIR"),
		ProjectRolesDir: os.Getenv("ROUNDTABLE_PROJECT_ROLES_DIR"),
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

// buildBackends is shared between runStdio and runHTTP. See A4 for the
// lazy-start CodexBackend change.
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

// buildStdioDispatch mirrors buildDispatchFunc but uses stdiomcp types.
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

// runHTTP is the existing main() body, unchanged. Phase C1 deletes it.
func runHTTP(logger *slog.Logger) {
	// ... existing main() body from line 83 onwards, using `backends :=
	// buildBackends(logger)` instead of the inline block
}
```

(The concrete rewrite of `main.go` is mechanical: move the old body into `runHTTP`, call `buildBackends` from both paths, change eager `CodexRPC.Start()` into a comment pointing to Task A4.)

- [ ] Add `make run-stdio` target:

```
run-stdio: build
	./roundtable-http-mcp stdio
```

**Test**
```
$GO build -o /tmp/rt-stdio ./cmd/roundtable-http-mcp
$GO vet ./...
$GO test ./internal/stdiomcp/...
```
Build should succeed, vet clean, tests pass.

Manual smoke: `echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"smoke","version":"0"}}}' | /tmp/rt-stdio stdio` should print a single JSON object to stdout (the initialize response) and then block on stdin.

**Commit**
```
feat(stdio): add roundtable stdio subcommand using Go SDK StdioTransport

Adds internal/stdiomcp package with a transport-agnostic server wiring
the same five tool specs as the HTTP path. cmd/roundtable-http-mcp/main.go
now dispatches on argv[1]: "stdio" runs the new path, anything else runs
the legacy HTTP server (removed in Phase C).
```

---

### Task A3: Fix codex child supervision

**Files**
- Modify: `internal/roundtable/codex_rpc.go` — use `configureProcGroup` on Unix, add `Pdeathsig: syscall.SIGKILL` on Linux, add PID-polling fallback goroutine on Darwin
- Create: `internal/roundtable/codex_rpc_pdeathsig_linux.go` (build-tagged)
- Create: `internal/roundtable/codex_rpc_pdeathsig_other.go` (build-tagged fallback, no-op + polling)
- Create: `internal/roundtable/codex_rpc_orphan_test.go` — integration test that kills the parent process and asserts no codex child survives

**Why**
Today `codex_rpc.go` does raw `exec.CommandContext(ctx, c.execPath, ...)` with no process group and no pdeathsig. If the roundtable stdio session is killed with `kill -9` or panics with no defer path, the codex grandchild (`codex app-server`) becomes an orphan reparented to init. On a dev laptop that's leaked memory; on a shared machine with many sessions it's a real problem.

The existing `runner_unix.go` has `configureProcGroup` which is used by the short-lived subprocess `SubprocessRunner` for gemini/claude — but the long-lived CodexBackend sidesteps it. We need to apply the same pattern plus Linux pdeathsig which atomically kills the child when the parent dies (no window between parent death and explicit cleanup).

Darwin does not have `PR_SET_PDEATHSIG`. The fallback is a goroutine inside CodexBackend that polls `os.Getppid()` every second and kills itself if the parent changes to 1 (init). This is ugly but correct.

**Steps**
- [ ] Create `internal/roundtable/codex_rpc_pdeathsig_linux.go`:

```go
//go:build linux

package roundtable

import (
	"os/exec"
	"syscall"
)

// applyPdeathsig sets PR_SET_PDEATHSIG so the kernel sends SIGKILL to
// the child immediately when the parent process dies, regardless of
// whether parent cleanup code runs. This closes the orphan window on
// SIGKILL/panic paths.
func applyPdeathsig(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}

// startParentWatcher is a no-op on Linux — pdeathsig handles it.
func startParentWatcher(stopCh <-chan struct{}) {}
```

- [ ] Create `internal/roundtable/codex_rpc_pdeathsig_other.go`:

```go
//go:build !linux && !windows

package roundtable

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// applyPdeathsig: Darwin/BSD have no PR_SET_PDEATHSIG. Set process group
// so the codex child does not share the parent pgid (prevents Ctrl-C
// propagation issues) but rely on startParentWatcher for orphan cleanup.
func applyPdeathsig(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// startParentWatcher polls os.Getppid(). On macOS, if the parent dies
// the child is reparented to launchd (pid 1 or similar). We detect the
// reparent and self-exit, which triggers CodexBackend's cleanup path.
//
// This runs in the codex subprocess's lifetime but is actually invoked
// from within the PARENT (roundtable) process — the watcher lives in the
// parent and kills the child, because we cannot inject code into the
// running codex binary. The logic is therefore:
//
//   - parent spawns codex with a pgid
//   - parent starts watcher goroutine that does nothing (returns immediately)
//     — we cannot watch ourselves from outside our own process tree
//
// The REAL darwin safety net is: (a) deferred Stop() in the happy path,
// (b) process group kill so `killpg(-pgid)` from a supervisor works,
// (c) documentation of the known gap in INSTALL.md troubleshooting.
// Users who need strict orphan hygiene on macOS should run Claude Code
// under a launchd agent that cleans up stragglers.
func startParentWatcher(stopCh <-chan struct{}) {
	// Intentionally empty. Kept as a hook in case we want a real
	// watcher process later (e.g. a tiny wrapper launched with the
	// codex child that execs codex and then polls ppid itself).
	_ = time.Second
	_ = os.Getppid
}
```

Note: the Darwin case really is weaker than Linux. The test below explicitly skips strict orphan assertions on Darwin and instead asserts the happy-path Stop() works.

- [ ] Modify `internal/roundtable/codex_rpc.go` — change `Start()` to apply pdeathsig:

```go
func (c *CodexBackend) Start(ctx context.Context) error {
	c.mu.Lock()

	cmd := exec.CommandContext(ctx, c.execPath, "app-server", "--listen", "stdio://")
	applyPdeathsig(cmd) // NEW: Linux pdeathsig + pgid; Darwin pgid only

	stdin, err := cmd.StdinPipe()
	// ... rest unchanged
}
```

- [ ] Modify `Stop()` to kill the process group, not just the leader:

```go
func (c *CodexBackend) Stop() error {
	c.mu.Lock()
	cmd := c.cmd
	stdin := c.stdin
	c.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}

	_ = stdin.Close()

	// Kill the whole process group so codex's own children (if any)
	// die too. configureProcGroup set Setpgid so -pid is the pgid.
	if cmd.Process.Pid > 0 {
		killProcessGroup(cmd.Process.Pid)
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	<-c.done
	return nil
}
```

- [ ] Create `internal/roundtable/codex_rpc_orphan_test.go`:

```go
//go:build linux

package roundtable

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestCodexRPC_NoOrphanOnKill9 verifies that SIGKILL to the parent
// process does not leak a codex app-server child. Linux-only because
// it relies on pdeathsig semantics.
//
// This test uses a shim binary that mimics codex's JSON-RPC handshake
// enough to make initialize succeed, then sleeps. We spawn a helper
// process that launches CodexBackend against the shim, then kill -9
// the helper and check for survivors.
func TestCodexRPC_NoOrphanOnKill9(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("pdeathsig is linux-only")
	}
	if testing.Short() {
		t.Skip("integration test")
	}

	// Build the helper that spawns CodexBackend and blocks.
	helperBin := buildOrphanHelper(t)
	shimBin := buildCodexShim(t)

	cmd := exec.Command(helperBin, shimBin)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}

	// Let the helper spawn the shim.
	time.Sleep(500 * time.Millisecond)

	// SIGKILL the helper. Because the shim has Pdeathsig=SIGKILL, the
	// kernel should kill it atomically.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill helper: %v", err)
	}
	_, _ = cmd.Process.Wait()

	// Give the kernel a beat to deliver SIGKILL.
	time.Sleep(200 * time.Millisecond)

	// Check for survivors by name. pgrep returns 1 if nothing found.
	out, err := exec.Command("pgrep", "-f", shimBin).CombinedOutput()
	survivors := strings.TrimSpace(string(out))
	if err == nil && survivors != "" {
		t.Fatalf("codex shim survived parent SIGKILL, pids: %s", survivors)
	}
}

// buildOrphanHelper compiles a tiny test binary that imports CodexBackend
// and spawns the shim, then blocks forever.
func buildOrphanHelper(t *testing.T) string {
	t.Helper()
	// ... `go build` a file from testdata/orphan_helper/main.go
	return "/tmp/rt-orphan-helper"
}

// buildCodexShim compiles a test binary that speaks enough JSON-RPC
// to satisfy the CodexBackend initialize handshake, then sleeps.
func buildCodexShim(t *testing.T) string {
	t.Helper()
	// ... `go build` a file from testdata/codex_shim/main.go
	return "/tmp/rt-codex-shim"
}
```

And the two testdata helpers at `internal/roundtable/testdata/orphan_helper/main.go` (spawns CodexBackend against shim path, blocks) and `internal/roundtable/testdata/codex_shim/main.go` (reads `initialize`, writes the response, then `select {}`s forever).

**Test**
```
$GO test ./internal/roundtable/ -run TestCodexRPC_NoOrphanOnKill9 -v
```
Expected on Linux: `PASS`. Expected on Darwin: `SKIP`.

Also a manual sanity check on the dogfood machine:

```
./roundtable-http-mcp stdio &
PID=$!
sleep 2
# send a tool call that triggers codex lazy-start (see A4)
# ...
kill -9 $PID
sleep 1
pgrep -f "codex app-server" && echo "LEAK" || echo "clean"
```
Expected output: `clean`.

**Commit**
```
fix(codex): apply Pdeathsig and process-group kill on Unix

Fixes orphaned codex app-server children when the parent roundtable
process dies via SIGKILL or panic. Linux gets PR_SET_PDEATHSIG for
atomic cleanup; Darwin gets Setpgid for group kill. Darwin's weaker
guarantee is documented in INSTALL.md troubleshooting (Phase E).

Adds an integration test on Linux that kill -9's a helper and asserts
pgrep finds no survivors.
```

---

### Task A4: Refactor CodexBackend to lazy-start

**Files**
- Modify: `internal/roundtable/codex_rpc.go` — add `sync.Once`, rewrite `Healthy()`, rewrite `Run()`, make `Start()` idempotent
- Modify: `cmd/roundtable-http-mcp/main.go` — remove the eager `codexRPC.Start(context.Background())` call from `buildBackends`

**Why**
Phase A2 revealed that eager `Start()` is wrong for stdio: we don't know at launch time whether a session will ever call a codex tool, and paying the app-server startup cost (unknown p95 — see B1) on every `roundtable stdio` spawn is wasteful when Claude Code launches and tears down stdio MCPs freely.

The lazy contract:
- `Healthy(ctx)` returns nil if the exec path still resolves to a file. Does NOT attempt JSON-RPC.
- `Start(ctx)` is now idempotent — safe to call multiple times, but we also no longer call it from main.
- `Run(ctx, req)` is guarded by `sync.Once` that does the real initialize on first call. Subsequent calls skip straight to `thread/start`.
- `Stop()` unchanged; still safe if Start was never called.

The existing `run.go` orchestrator calls `backend.Healthy(ctx)` on every dispatch — with the new contract that's a cheap exec path check, so nothing else in run.go needs to change.

**Steps**
- [ ] Rewrite `internal/roundtable/codex_rpc.go` — add `once sync.Once` and `startErr error` to the struct:

```go
type CodexBackend struct {
	execPath string
	model    string

	startOnce sync.Once
	startErr  error

	mu     sync.Mutex
	cmd    *exec.Cmd
	// ... rest unchanged
}
```

- [ ] Rewrite `Healthy`:

```go
// Healthy is a cheap liveness check. It does NOT start the subprocess;
// it only verifies the exec path still resolves. This lets run.go's
// probe phase succeed without paying the app-server startup cost.
//
// If the subprocess was already started (via a prior Run), Healthy also
// checks that readLoop has not exited. Otherwise it returns nil.
func (c *CodexBackend) Healthy(_ context.Context) error {
	if c.execPath == "" {
		return errors.New("codex exec path empty")
	}
	if _, err := os.Stat(c.execPath); err != nil {
		return fmt.Errorf("codex exec path %q: %w", c.execPath, err)
	}

	c.mu.Lock()
	cmd := c.cmd
	c.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		// Not started yet — that's fine, Run will start lazily.
		return nil
	}
	select {
	case <-c.done:
		return errors.New("codex process exited")
	default:
		return nil
	}
}
```

- [ ] Add `ensureStarted`:

```go
// ensureStarted runs the real app-server + initialize handshake under
// sync.Once. Subsequent callers block until the first caller finishes,
// then return the same error if it failed.
func (c *CodexBackend) ensureStarted(ctx context.Context) error {
	c.startOnce.Do(func() {
		c.startErr = c.doStart(ctx)
	})
	return c.startErr
}

// doStart is the body of the old Start(). Renamed so the exported
// Start() can remain as a no-op/idempotent shim for backwards compat
// with any remaining callers (the HTTP path in Phase A).
func (c *CodexBackend) doStart(ctx context.Context) error {
	// ... existing body of Start() from codex_rpc.go, unchanged
}

// Start is kept for compatibility but delegates to ensureStarted. Safe
// to call many times. Phase C removes the last caller.
func (c *CodexBackend) Start(ctx context.Context) error {
	return c.ensureStarted(ctx)
}
```

- [ ] Rewrite `Run` to call `ensureStarted` first:

```go
func (c *CodexBackend) Run(ctx context.Context, req Request) (*Result, error) {
	if err := c.ensureStarted(ctx); err != nil {
		return c.errorResult(req, time.Now(), fmt.Errorf("codex start: %w", err)), nil
	}
	// ... existing thread/start + turn/start body unchanged
}
```

- [ ] In `cmd/roundtable-http-mcp/main.go` `buildBackends`, remove the eager start block and replace with the simple constructor:

```go
codexPath := roundtable.ResolveExecutable("codex")
if codexPath != "" {
	codexBackend = roundtable.NewCodexBackend(codexPath, "")
	logger.Info("codex backend configured (lazy start)", "path", codexPath)
} else {
	logger.Warn("codex binary not found, using CodexFallback")
	codexBackend = roundtable.NewCodexFallbackBackend("", "")
}
```

**Test**
```
$GO test ./internal/roundtable/ -run CodexRPC -v
```
All existing codex_rpc_test.go tests should still pass. Add two new test cases:

```go
func TestCodexBackend_HealthyBeforeStart(t *testing.T) {
	// Create backend pointing at a real codex path (or a shim), call
	// Healthy() directly without ever calling Start. Assert nil.
}

func TestCodexBackend_StartOnce(t *testing.T) {
	// Call Start concurrently from N goroutines with a shim. Assert
	// the shim only saw one initialize request.
}
```

**Commit**
```
refactor(codex): lazy-start CodexBackend under sync.Once

Healthy() now just stats the exec path; the expensive app-server start
+ initialize handshake is deferred until the first Run() call. This
matches the stdio session lifecycle where many spawns will never issue
a codex tool call. HTTP path still works because Start() is idempotent.
```

---

### Task A5: End-to-end stdio integration test

**Files**
- Create: `internal/stdiomcp/e2e_test.go` — spawns `./roundtable-http-mcp stdio` via `mcp.CommandTransport` and runs a full initialize + tools/list + hivemind call

**Why**
Unit tests cover the dispatch function; we need one test that exercises the full stdio loop including the Go SDK's framing, so we catch regressions where something starts writing to stdout or the MCP server refuses to initialize.

**Steps**
- [ ] Create `internal/stdiomcp/e2e_test.go`:

```go
package stdiomcp_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestStdioE2E builds the roundtable binary, spawns it with the `stdio`
// subcommand, runs through initialize + tools/list, and asserts all five
// tools are advertised. It does NOT actually call a tool (that would
// require gemini/codex/claude binaries). Tool execution is covered by
// internal/roundtable/run_test.go with the mock backend.
func TestStdioE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	binPath := buildRoundtableBinary(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "e2e-test", Version: "0"}, nil)
	transport := &mcp.CommandTransport{Command: exec.Command(binPath, "stdio")}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	listResp, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	want := map[string]bool{
		"hivemind": false, "deepdive": false, "architect": false,
		"challenge": false, "xray": false,
	}
	for _, tool := range listResp.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("tool %q not advertised", name)
		}
	}
}

func buildRoundtableBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "roundtable")
	cmd := exec.Command("go", "build", "-o", bin, "../../cmd/roundtable-http-mcp")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}
```

**Test**
```
$GO test ./internal/stdiomcp/ -run TestStdioE2E -v
```
Expected: `PASS` with all five tools found.

**Commit**
```
test(stdio): add end-to-end CommandTransport integration test

Builds the roundtable binary, spawns it with `stdio`, runs MCP
initialize + ListTools, asserts all five tools are advertised. Catches
regressions where stdout pollution or framing corruption would wedge
the session.
```

---

### Task A6: Dogfood and latency notes

- [ ] Install the locally-built binary:

```
make build
mv roundtable-http-mcp ~/.local/bin/roundtable
```

- [ ] Register with Claude Code using the **correct** syntax (name BEFORE `--`):

```
claude mcp remove roundtable 2>/dev/null || true
claude mcp add -s user roundtable -- $HOME/.local/bin/roundtable stdio
```

- [ ] Verify registration:

```
claude mcp list | grep roundtable
```

- [ ] Run 10 real tool calls through Claude Code (hivemind, deepdive, xray across mixed prompts). Record:
  - Startup latency of the stdio subprocess (time from Claude Code spawn to first response frame)
  - Whether any session wedged
  - Whether any codex child survived a session end

- [ ] Paste results into this file as a comment block under Phase B.

**Commit** (docs only)
```
docs(phase3): record Phase A dogfood results
```

---

## Phase B: Verify assumptions

Two hard assumptions need data before we commit to the design. If either fails, we re-plan before proceeding to Phase C.

### Task B1: Measure codex app-server cold-start latency

**Why**
The lazy-start model assumes codex `initialize` is fast enough that paying it on the first real tool call is acceptable UX. If p95 is 5 seconds, users will perceive the first codex call as broken. If p95 is 300 ms, lazy-start is clearly right. Unknown today.

**Method**
- [ ] Write a throwaway benchmark script at `scripts/measure_codex_start.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
ITERATIONS=20
TIMES=()
for i in $(seq 1 $ITERATIONS); do
  start=$(date +%s%3N)
  echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | \
    timeout 10 codex app-server --listen stdio:// >/dev/null 2>&1 || true
  end=$(date +%s%3N)
  TIMES+=($((end - start)))
done
printf '%s\n' "${TIMES[@]}" | sort -n | tee /tmp/codex-startup-times.txt
```

- [ ] Run on the dogfood machine (Linux amd64 baseline). Record p50/p95/p99 in this doc:

```
(paste results here after running)
p50:
p95:
p99:
```

- [ ] **Decision gate**: if p95 > 1500 ms, escalate — revisit lazy-start vs eager-start-with-hot-pool.

**No commit** — results live in this plan file.

---

### Task B2: Claude Code stdio crash recovery

**Status: complete.** Results in `2026-04-11-phase-b-verification-results.md`. A hidden `__crash` subcommand and `scripts/register_crash_mcp.sh` were added to observe Claude Code's reaction to an abrupt `os.Exit(42)`, then removed once the contract was recorded: Claude Code does not auto-restart stdio MCPs mid-session; after one crash the tools are dropped from the catalog and recovery requires a full Claude Code restart. That finding drives the INSTALL.md troubleshooting language in Task E1.

---

## Phase C: Delete HTTP, rename binary, flip defaults

At this point Phase A is dogfood-verified and Phase B has told us the assumptions hold. Now we can do the deletion pass cleanly.

### Task C1: Delete internal/httpmcp

**Files**
- Delete: `internal/httpmcp/server.go`
- Delete: `internal/httpmcp/server_test.go`
- Delete: `internal/httpmcp/backend.go` (types already copied to stdiomcp)
- Delete: `internal/httpmcp/config.go`
- Delete: `internal/httpmcp/metrics.go` (no longer exposed)
- Delete: `internal/httpmcp/e2e_test.go`
- Delete: the entire `internal/httpmcp/` directory

**Steps**
- [ ] `rm -rf internal/httpmcp/`
- [ ] Remove the `httpmcp` import from `cmd/roundtable-http-mcp/main.go`
- [ ] Remove the `runHTTP()` function and `buildDispatchFunc()` (the old one) entirely

**Test**
```
$GO build ./...
$GO vet ./...
```
Must succeed.

**Commit**
```
refactor(http): delete internal/httpmcp package

Phase A proved the stdio path covers every production use. The HTTP
server, healthz/readyz/metricsz endpoints, and Prometheus-style metrics
are all removed. No runtime supervisor, no daemon. The Go SDK's stdio
framing is the only transport from here on.
```

---

### Task C2: Delete HTTP config fields

Already done implicitly by C1. This task is just a checkpoint.

**Test**
```
$GO test ./... -count=1 -timeout 60s
```
All tests pass, no HTTP references remain.

---

### Task C3: Rename binary `roundtable-http-mcp` -> `roundtable`

**Files**
- Rename dir: `cmd/roundtable-http-mcp/` -> `cmd/roundtable/`
- Modify: `Makefile` — change `build` target and output name
- Modify: any docs still referencing `roundtable-http-mcp` (caught in Phase E)

**Steps**
- [ ] `git mv cmd/roundtable-http-mcp cmd/roundtable`
- [ ] Update Makefile:

```
GO = mise exec go@1.26.2 -- go
GO_ENV = GOTOOLCHAIN=local GOMODCACHE=/tmp/gomodcache GOCACHE=/tmp/gocache
VERSION ?= 0.8.0

.PHONY: all build test vet clean run

all: build

build:
	$(GO_ENV) $(GO) build -ldflags "-s -w -X main.version=$(VERSION)" -o roundtable ./cmd/roundtable

test:
	$(GO_ENV) $(GO) test ./... -count=1 -timeout 60s

vet:
	$(GO_ENV) $(GO) vet ./...

clean:
	rm -f roundtable

run: build
	./roundtable stdio
```

- [ ] Search for any remaining `roundtable-http-mcp` string and update.

**Test**
```
make clean build test vet
```
All green.

**Commit**
```
chore: rename binary roundtable-http-mcp to roundtable

Single user-facing name for the final release. cmd/ path, Makefile
target, and local build artifacts all use the new name. Module path
github.com/TejGandham/roundtable is unchanged.
```

---

### Task C4: No-arg behavior and subcommands

**Files**
- Modify: `cmd/roundtable/main.go` — proper subcommand dispatch with usage

**Why**
`./roundtable` with no args must NOT read from stdin (that would hang waiting for JSON-RPC frames that will never come and confuse anyone who runs the binary directly). Print usage, exit 0 on `help`, exit 2 on unknown.

**Steps**
- [ ] Rewrite `main()`:

```go
var version = "0.8.0-dev" // overridden via -ldflags -X main.version

const usage = `roundtable — multi-model consensus MCP server

Usage:
  roundtable stdio          Run the MCP server on stdin/stdout (used by Claude Code)
  roundtable version        Print version and exit
  roundtable help           Print this help

Environment:
  ROUNDTABLE_GEMINI_PATH    Absolute path to gemini binary (optional)
  ROUNDTABLE_CODEX_PATH     Absolute path to codex binary (optional)
  ROUNDTABLE_CLAUDE_PATH    Absolute path to claude binary (optional)
  ROUNDTABLE_ROLES_DIR      Override global roles directory
  ROUNDTABLE_PROJECT_ROLES_DIR  Per-project role overrides

Installation:
  curl -sSL https://raw.githubusercontent.com/TejGandham/roundtable/main/install.sh | sh
`

func main() {
	logger := stdiomcp.InitStdioDiscipline()

	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(0)
	}

	switch args[0] {
	case "stdio":
		runStdio(logger)
	case "version", "--version", "-v":
		fmt.Fprintln(os.Stderr, "roundtable", version)
	case "help", "--help", "-h":
		fmt.Fprint(os.Stderr, usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n%s", args[0], usage)
		os.Exit(2)
	}
}
```

Note that `version` and `help` print to stderr, not stdout. This keeps the stdout-is-sacred invariant even for human-facing output when someone accidentally pipes `roundtable version` somewhere.

**Test**
```
./roundtable 2>&1 1>/dev/null | head -1
```
Expected: `roundtable — multi-model consensus MCP server`.

```
./roundtable 1>/dev/null
```
Expected: exits immediately, no hang.

```
./roundtable version 2>&1
```
Expected: `roundtable 0.8.0-dev`.

```
./roundtable foo; echo "exit=$?"
```
Expected: usage to stderr, `exit=2`.

**Commit**
```
feat(cli): add version/help subcommands, require explicit stdio

No-arg invocation now prints usage to stderr and exits 0 instead of
hanging on stdin. `stdio` is required to enter the MCP loop. Version
is baked in via -ldflags at build time.
```

---

### Task C5: Stdio-only server tests

**Files**
- Delete: `internal/httpmcp/server_test.go` (done in C1)
- Modify/create: `internal/stdiomcp/server_test.go` — in-memory transport pair, full test coverage

**Steps**
- [ ] Create `internal/stdiomcp/server_test.go` with an in-memory test using `mcp.NewInMemoryTransports()`:

```go
package stdiomcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestInMemoryServer_AllToolsRegistered(t *testing.T) {
	dispatch := func(_ context.Context, spec ToolSpec, _ ToolInput) ([]byte, error) {
		return json.Marshal(map[string]string{"tool": spec.Name})
	}
	cfg := Config{ServerName: "test", ServerVersion: "0"}
	srv := NewServer(cfg, dispatch, nil)

	serverT, clientT := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() { _ = srv.Run(ctx, serverT) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client"}, nil)
	sess, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer sess.Close()

	list, err := sess.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Tools) != 5 {
		t.Fatalf("want 5 tools, got %d", len(list.Tools))
	}
}

func TestInMemoryServer_HivemindDispatch(t *testing.T) {
	// Similar setup, then CallTool("hivemind", ...) and assert the
	// dispatch func was invoked with the right spec + input.
}
```

**Test**
```
$GO test ./internal/stdiomcp/... -v
```
All tests pass.

**Commit**
```
test(stdio): add in-memory transport tests for stdiomcp server
```

---

### Task C6: Final sweep

- [ ] `$GO test ./... -count=1 -timeout 60s`
- [ ] `$GO vet ./...`
- [ ] `git status` — clean working tree except the expected Phase C changes
- [ ] `git diff --stat main...HEAD` — all deletions and renames look right
- [ ] Commit anything still uncommitted, push the `go-phase3-stdio` branch

**Commit** (if any stragglers)
```
chore(phase3): finalize Phase C cleanup sweep
```

---

## Phase D: goreleaser and install.sh

This is where curl-install actually starts working. Everything before this phase was local.

### Task D1: goreleaser config

**Files**
- Create: `.goreleaser.yaml`

**Steps**
- [ ] Write `.goreleaser.yaml`:

```yaml
version: 2

project_name: roundtable

before:
  hooks:
    - go mod tidy

builds:
  - id: roundtable
    main: ./cmd/roundtable
    binary: roundtable
    env:
      - CGO_ENABLED=0
      - GOTOOLCHAIN=local
    goos:
      - linux
      - darwin
    goarch:
      - amd64
      - arm64
    ldflags:
      - -s -w -X main.version={{.Version}}
    mod_timestamp: "{{ .CommitTimestamp }}"

archives:
  - id: roundtable
    name_template: "roundtable_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    format: tar.gz
    files:
      - LICENSE
      - README.md

checksum:
  name_template: "checksums.txt"
  algorithm: sha256

signs:
  - id: cosign
    cmd: cosign
    stdin: '{{ .Env.COSIGN_PASSWORD }}'
    args:
      - sign-blob
      - --key=env://COSIGN_PRIVATE_KEY
      - --output-signature=${signature}
      - ${artifact}
      - --yes
    artifacts: checksum

release:
  github:
    owner: TejGandham
    name: roundtable
  draft: false
  prerelease: auto

# NO homebrew, NO nfpms, NO scoop, NO snapcraft. curl install.sh only.
```

**Test**
```
goreleaser check
goreleaser release --snapshot --skip=sign --clean
ls dist/
```
Expected: 4 tarballs, 1 checksums.txt, snapshot build succeeds.

**Commit**
```
build: add goreleaser config for linux/darwin amd64/arm64

Cosign-signed checksums, no Homebrew, no package managers. Publishes
to the GitHub mirror so install.sh can fetch releases over plain HTTPS
from off-tailnet environments.
```

---

### Task D2: Forgejo release workflow + mirror push

**Files**
- Create: `.forgejo/workflows/release.yml`

**Steps**
- [ ] Write the release workflow. Assumes a Forgejo secret `GITHUB_MIRROR_TOKEN` with push access to `github.com/TejGandham/roundtable`, and `COSIGN_PRIVATE_KEY` + `COSIGN_PASSWORD` secrets.

```yaml
name: release

on:
  push:
    tags:
      - 'v*'

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26.2'
      - run: go test ./... -count=1 -timeout 120s
      - run: go vet ./...

  mirror:
    needs: test
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - name: Push tag to GitHub mirror
        env:
          GH_TOKEN: ${{ secrets.GITHUB_MIRROR_TOKEN }}
        run: |
          git remote add github https://x-access-token:$GH_TOKEN@github.com/TejGandham/roundtable.git
          git push github HEAD:refs/heads/main --force
          git push github "${GITHUB_REF_NAME}"

  release:
    needs: mirror
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26.2'
      - name: Install cosign
        run: |
          curl -sSfL https://github.com/sigstore/cosign/releases/download/v2.2.4/cosign-linux-amd64 \
            -o /usr/local/bin/cosign
          chmod +x /usr/local/bin/cosign
      - name: goreleaser
        uses: goreleaser/goreleaser-action@v6
        with:
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_MIRROR_TOKEN }}
          COSIGN_PRIVATE_KEY: ${{ secrets.COSIGN_PRIVATE_KEY }}
          COSIGN_PASSWORD: ${{ secrets.COSIGN_PASSWORD }}
```

**Note:** goreleaser needs to talk to github.com, not the Forgejo API. The `GITHUB_TOKEN` var above is the mirror token, and goreleaser's github section in `.goreleaser.yaml` already points at `TejGandham/roundtable` on GitHub.

**Test**
```
# Dry-run on a local tag first
git tag -s v0.8.0-rc1
GITHUB_TOKEN=dummy goreleaser release --snapshot --skip=publish,sign --clean
```

**Commit**
```
ci: add forgejo release workflow with github mirror

Runs go test + go vet, pushes the tag to the GitHub mirror, then
runs goreleaser against GitHub Releases. Cosign signing enforced.
Forgejo remains source of truth for code; GitHub is the release CDN
so curl installers work off-tailnet.
```

---

### Task D3: install.sh

**Files**
- Create: `install.sh` (executable, at repo root)

**Steps**
- [ ] Write install.sh:

```bash
#!/usr/bin/env sh
# roundtable installer. Run with:
#   curl -sSL https://raw.githubusercontent.com/TejGandham/roundtable/main/install.sh | sh
#
# Options (via env or args):
#   --prefix DIR       install into DIR/bin (default: $HOME/.local)
#   --version X.Y.Z    install specific version (default: latest)
#   --no-verify        skip cosign signature check (NOT RECOMMENDED)
set -eu

REPO="TejGandham/roundtable"
COSIGN_PUB="-----BEGIN PUBLIC KEY-----
REPLACE_WITH_ACTUAL_PINNED_PUBLIC_KEY
-----END PUBLIC KEY-----"

PREFIX="${PREFIX:-$HOME/.local}"
VERSION="${VERSION:-}"
NO_VERIFY="${NO_VERIFY:-0}"

while [ $# -gt 0 ]; do
  case "$1" in
    --prefix) PREFIX="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --no-verify) NO_VERIFY=1; shift ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

say() { printf '[roundtable-install] %s\n' "$*"; }
die() { printf '[roundtable-install] ERROR: %s\n' "$*" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "required tool not found: $1"; }
need curl
need tar
need uname
need sha256sum 2>/dev/null || need shasum

detect_os_arch() {
  os=$(uname -s)
  arch=$(uname -m)
  case "$os" in
    Linux)  OS=linux ;;
    Darwin) OS=darwin ;;
    *) die "unsupported OS: $os" ;;
  esac
  case "$arch" in
    x86_64|amd64) ARCH=amd64 ;;
    arm64|aarch64) ARCH=arm64 ;;
    *) die "unsupported arch: $arch" ;;
  esac
}

resolve_version() {
  if [ -z "$VERSION" ]; then
    VERSION=$(curl -sSfL "https://api.github.com/repos/$REPO/releases/latest" \
      | grep '"tag_name"' | head -1 | sed 's/.*"v\(.*\)".*/\1/')
    [ -n "$VERSION" ] || die "could not resolve latest version"
  fi
  say "installing roundtable v$VERSION for $OS/$ARCH"
}

download_and_verify() {
  tmpdir=$(mktemp -d)
  trap 'rm -rf "$tmpdir"' EXIT

  base="https://github.com/$REPO/releases/download/v${VERSION}"
  tarball="roundtable_${VERSION}_${OS}_${ARCH}.tar.gz"

  say "fetching $tarball"
  curl -sSfL "$base/$tarball" -o "$tmpdir/$tarball"
  curl -sSfL "$base/checksums.txt" -o "$tmpdir/checksums.txt"

  if [ "$NO_VERIFY" = "1" ]; then
    say "WARNING: skipping cosign verification (--no-verify)"
  else
    curl -sSfL "$base/checksums.txt.sig" -o "$tmpdir/checksums.txt.sig" || \
      die "checksums.txt.sig not found — refusing to install unverified"
    printf '%s\n' "$COSIGN_PUB" > "$tmpdir/cosign.pub"
    if command -v cosign >/dev/null 2>&1; then
      cosign verify-blob \
        --key "$tmpdir/cosign.pub" \
        --signature "$tmpdir/checksums.txt.sig" \
        "$tmpdir/checksums.txt" \
        >/dev/null 2>&1 || die "cosign signature verification failed"
      say "cosign signature OK"
    else
      say "WARNING: cosign not installed, falling back to sha256 only"
    fi
  fi

  expected=$(grep "  $tarball$" "$tmpdir/checksums.txt" | awk '{print $1}')
  [ -n "$expected" ] || die "checksum for $tarball not found in checksums.txt"

  if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$tmpdir/$tarball" | awk '{print $1}')
  else
    actual=$(shasum -a 256 "$tmpdir/$tarball" | awk '{print $1}')
  fi

  [ "$expected" = "$actual" ] || die "checksum mismatch: want $expected got $actual"
  say "sha256 OK"

  tar -xzf "$tmpdir/$tarball" -C "$tmpdir"
  INSTALL_DIR="$PREFIX/bin"
  mkdir -p "$INSTALL_DIR"
  mv "$tmpdir/roundtable" "$INSTALL_DIR/roundtable"
  chmod +x "$INSTALL_DIR/roundtable"
  BIN="$INSTALL_DIR/roundtable"

  # macOS Gatekeeper quarantine. Strip the xattr so the binary can
  # execute without "cannot verify developer" dialog. Silently ignored
  # if xattr is missing or the file has no attribute.
  if [ "$OS" = "darwin" ]; then
    xattr -dr com.apple.quarantine "$BIN" 2>/dev/null || true
  fi
}

detect_paths() {
  GEMINI_PATH=$(command -v gemini 2>/dev/null || true)
  CODEX_PATH=$(command -v codex 2>/dev/null || true)
  CLAUDE_PATH=$(command -v claude 2>/dev/null || true)
}

print_next_steps() {
  BIN="$PREFIX/bin/roundtable"
  cat <<EOF

Installed: $BIN

Detected CLIs:
  gemini:  ${GEMINI_PATH:-NOT FOUND}
  codex:   ${CODEX_PATH:-NOT FOUND}
  claude:  ${CLAUDE_PATH:-NOT FOUND}

Register with Claude Code (copy-paste):

EOF
  ENV_FLAGS=""
  [ -n "$GEMINI_PATH" ] && ENV_FLAGS="$ENV_FLAGS --env ROUNDTABLE_GEMINI_PATH=$GEMINI_PATH"
  [ -n "$CODEX_PATH" ]  && ENV_FLAGS="$ENV_FLAGS --env ROUNDTABLE_CODEX_PATH=$CODEX_PATH"
  [ -n "$CLAUDE_PATH" ] && ENV_FLAGS="$ENV_FLAGS --env ROUNDTABLE_CLAUDE_PATH=$CLAUDE_PATH"

  printf '  claude mcp add -s user roundtable%s -- %s stdio\n\n' "$ENV_FLAGS" "$BIN"

  case ":$PATH:" in
    *":$PREFIX/bin:"*) ;;
    *) printf 'Note: %s is not in your PATH. Add it or use the absolute path above.\n\n' "$PREFIX/bin" ;;
  esac
}

detect_os_arch
resolve_version
download_and_verify
detect_paths
print_next_steps
```

**Test**
```
# From a clean shell
bash install.sh --prefix=/tmp/rt-test --version=0.8.0
/tmp/rt-test/bin/roundtable version
```
Expected: prints `roundtable 0.8.0`, exit 0.

```
# Pipe form
curl -sSL https://raw.githubusercontent.com/TejGandham/roundtable/main/install.sh | sh
```
Expected: installs to `$HOME/.local/bin/roundtable`, prints the `claude mcp add` line with PATH-detected flags.

**Commit**
```
feat(install): add curl install.sh with cosign verification

- detects linux/darwin × amd64/arm64
- fetches tarball + checksums.txt + checksums.txt.sig from the
  GitHub release mirror
- verifies cosign signature against a pinned public key
- verifies sha256 checksum
- strips macOS quarantine xattr
- detects gemini/codex/claude absolute paths and prints a ready-to-paste
  `claude mcp add` line with --env ROUNDTABLE_*_PATH pre-filled
```

---

### Task D4: Forgejo CI workflow

**Files**
- Create: `.forgejo/workflows/ci.yml`

**Steps**
- [ ] Write:

```yaml
name: ci

on:
  push:
    branches: [main, 'go-**']
  pull_request:

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26.2'
      - run: go test ./... -count=1 -timeout 120s
      - run: go vet ./...
      - run: go build -o /tmp/roundtable ./cmd/roundtable
      - run: /tmp/roundtable version
```

**Test**
Push a commit, observe the workflow runs green in Forgejo.

**Commit**
```
ci: add forgejo test + vet + build workflow
```

---

### Task D5: Cut v0.8.0

- [ ] Bump `version = "0.8.0-dev"` to `version = "0.8.0"` in `cmd/roundtable/main.go`
- [ ] Bump `VERSION ?= 0.8.0` in `Makefile`
- [ ] Commit:

```
release: v0.8.0
```

- [ ] Tag and push to Forgejo:

```
git tag -s v0.8.0 -m "v0.8.0: stdio transport, curl installer"
git push origin main
git push origin v0.8.0
```

- [ ] Watch the Forgejo release workflow — it should mirror to GitHub and trigger goreleaser.
- [ ] Verify:

```
curl -I https://github.com/TejGandham/roundtable/releases/download/v0.8.0/roundtable_0.8.0_linux_amd64.tar.gz
```
Expected: `HTTP/2 302` or `200`.

- [ ] Clean-room install test:

```
cd /tmp && rm -rf rt-clean && mkdir rt-clean && cd rt-clean
curl -sSL https://raw.githubusercontent.com/TejGandham/roundtable/main/install.sh | sh -s -- --prefix=/tmp/rt-clean
/tmp/rt-clean/bin/roundtable version
```
Expected: `roundtable 0.8.0`.

**Commit** (the bump)
```
release: v0.8.0

- stdio transport on top of modelcontextprotocol/go-sdk v1.5.0
- single static binary, no HTTP daemon
- curl | sh installer with cosign-signed checksums
- lazy codex app-server start under sync.Once
- process-group kill + Linux pdeathsig for orphan prevention
```

---

## Phase E: Docs

This is the smallest phase but the one users will read first. Keep it tight.

### Task E1: Rewrite INSTALL.md

**Files**
- Modify: `INSTALL.md`

**Target length:** under 100 lines. The whole thing should be skimmable in 30 seconds.

**Content outline:**
```
# Installing Roundtable

## One-line install

    curl -sSL https://raw.githubusercontent.com/TejGandham/roundtable/main/install.sh | sh

This fetches the latest release, verifies its cosign signature, and
installs it to ~/.local/bin/roundtable. At the end it prints the exact
`claude mcp add` line for your machine.

## Register with Claude Code

Copy the line printed by the installer, or construct it manually:

    claude mcp add -s user roundtable -- $HOME/.local/bin/roundtable stdio

That's it. Open Claude Code and run any of: hivemind, deepdive,
architect, challenge, xray.

## Prerequisites

At least one of gemini, codex, or claude must be installed and on your
PATH at Roundtable start time. The installer detects which ones you have
and pre-fills --env ROUNDTABLE_*_PATH flags so Claude Code subprocesses
inherit the right absolute paths.

## Troubleshooting

### Claude Code says "tool hivemind not found"

Run `claude mcp list`. If roundtable isn't listed, rerun:

    claude mcp remove roundtable 2>/dev/null
    claude mcp add -s user roundtable -- $HOME/.local/bin/roundtable stdio

### "codex binary not found" or wrong gemini version

Claude Code on macOS runs MCP subprocesses with a minimal PATH. Fix by
passing absolute paths:

    claude mcp remove roundtable
    claude mcp add -s user roundtable \
      --env ROUNDTABLE_GEMINI_PATH=/opt/homebrew/bin/gemini \
      --env ROUNDTABLE_CODEX_PATH=/opt/homebrew/bin/codex \
      --env ROUNDTABLE_CLAUDE_PATH=/opt/homebrew/bin/claude \
      -- $HOME/.local/bin/roundtable stdio

### "cannot verify developer" on macOS

The installer runs `xattr -dr com.apple.quarantine` automatically. If
you built from source, run it yourself:

    xattr -dr com.apple.quarantine ~/.local/bin/roundtable

### Roundtable gets in a bad state mid-session

(Fill in from Phase B2 findings — either "Claude Code auto-restarts"
or "run /mcp reload" or "restart Claude Code".)

### Reinstall a specific version

    curl -sSL https://raw.githubusercontent.com/TejGandham/roundtable/main/install.sh | sh -s -- --version 0.8.0

## Uninstall

    claude mcp remove roundtable
    rm ~/.local/bin/roundtable
```

**Commit**
```
docs(install): rewrite INSTALL.md for curl installer + stdio
```

---

### Task E2: Update README.md

**Files**
- Modify: `README.md`

- [ ] Find-and-replace `roundtable-http-mcp` -> `roundtable`
- [ ] Remove "HTTP MCP server" language; replace with "stdio MCP server"
- [ ] Change the quickstart section to show the one-line curl install + one-line `claude mcp add`
- [ ] Update "How it's built" to mention: single Go binary, Go SDK stdio transport, lazy codex app-server, process-group supervision

**Commit**
```
docs(readme): update for stdio transport and curl installer
```

---

### Task E3: Update SKILL.md

**Files**
- Modify: `SKILL.md`, `release/SKILL.md`

- [ ] Remove any HTTP references
- [ ] Keep the tool list (hivemind/deepdive/architect/challenge/xray) unchanged
- [ ] Update the install snippet to the one-line curl form

**Commit**
```
docs(skill): drop HTTP references
```

---

### Task E4: Update docs/ARCHITECTURE.md

**Files**
- Modify: `docs/ARCHITECTURE.md`

- [ ] Replace HTTP transport section with stdio + lazy codex lifecycle
- [ ] Update any diagrams to show Claude Code -> fork/exec -> roundtable stdio
- [ ] Add a short section on the orphan-prevention strategy (pdeathsig + pgid)
- [ ] Document the sync.Once lazy-start contract and why Healthy() is now cheap

**Commit**
```
docs(architecture): replace HTTP section with stdio and lazy codex
```

---

### Task E5: Update docs/RELEASING.md

**Files**
- Modify: `docs/RELEASING.md`

Target content:

```
# Releasing

1. Bump `version` constant in cmd/roundtable/main.go
2. Bump `VERSION` in Makefile
3. Commit with message `release: vX.Y.Z`
4. `git tag -s vX.Y.Z -m "vX.Y.Z: <headline>"`
5. `git push origin main && git push origin vX.Y.Z`
6. Forgejo workflow:
   - runs go test / go vet
   - force-pushes main + tag to the GitHub mirror
   - runs goreleaser on the mirror (cosign signs checksums)
7. Verify the release page at github.com/TejGandham/roundtable/releases
8. Clean-room install test:
     curl -sSL https://raw.githubusercontent.com/TejGandham/roundtable/main/install.sh | sh -s -- --prefix=/tmp/rt-release-check
     /tmp/rt-release-check/bin/roundtable version
9. Announce in the usual places.
```

**Commit**
```
docs(releasing): update for goreleaser + github mirror flow
```

---

## Final checklist

- [ ] Phase A green (`make test`, dogfooded in Claude Code for a session)
- [ ] Phase B measurements recorded in this file
- [ ] Phase C delete sweep clean (`go build`, `go vet`, `go test` all green)
- [ ] Phase D v0.8.0 tag cut, install.sh verified from a clean directory
- [ ] Phase E docs rewritten
- [ ] `git push` clean, Forgejo CI green, GitHub Releases has the artifacts

## Open risks

|risk|mitigation|trigger to revisit|
|-|-|-|
|Darwin orphan window is real (no pdeathsig)|Document, rely on pgid + happy-path Stop()|User report of leaked codex processes|
|cosign key rotation is manual|Pin pubkey in install.sh, rotate by bumping installer URL|When we need to rotate|
|GitHub mirror access token expires|Forgejo secret rotation, calendar reminder|Forgejo workflow fails on push-mirror|
|Claude Code changes MCP subprocess PATH behavior|Installer prints absolute paths, tests detect them|Claude Code release notes|
|goreleaser v2 schema changes|Pin version in workflow|goreleaser release notes|
