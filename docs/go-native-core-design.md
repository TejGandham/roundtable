# Roundtable Phase 2: Go Native Core — Design Spec

> **Branch:** `go-http-phase1` → phase 2 work
> **Date:** 2026-04-10
> **Status:** Design approved, pending implementation plan

---

## Overview

Phase 2 replaces the Elixir `roundtable-cli` subprocess with a native Go dispatch core. The Go HTTP MCP server (phase 1) stops shelling out and handles everything natively.

**Before (phase 1):**

```
Claude Code → Go HTTP MCP → roundtable-cli (Elixir) → CLIs
```

**After (phase 2):**

```
Claude Code → Go HTTP MCP → Go native dispatcher → {
    Codex:  app-server (long-lived stdio JSON-RPC)
    Gemini: subprocess (per-request)
    Claude: subprocess (per-request)
}
```

---

## Observed Response Times

Data from this design session (2026-04-10), 4 roundtable calls:

```
           Short prompts          Production prompts
           (this session)         (with files, real usage)
           ─────────────          ────────────────────────
Claude:    1m 33s — 2m 10s       8 — 11 min
Codex:     1m 07s — 2m 40s       8 — 11 min
Gemini:    1m 11s — 3m 59s       8 — 11 min
Wall clock: 2m 08s — 5m 00s      up to 11+ min
```

---

## Timeout Budget

```
User-specified tool timeout (default: 900s = 15 min)
  |
  +---> Backend deadline = tool timeout + 30s grace
        |
        +---> Go context cancellation = hard stop
```

| Setting | Value | Rationale |
|-|-|-|
| Default timeout | 900s (15 min) | Accommodates 8-11 min CLI response times with margin |
| Min timeout | 1s | User override for fast-fail scenarios |
| Max timeout | 900s | Hard ceiling |
| Grace period | 30s | Extra time for CLI to flush output + exit after timeout |
| Backend deadline | timeout + 30s | Go kills the process if CLI doesn't finish in time |
| Probe timeout | 5s | `--version` is fast regardless of prompt complexity |
| Comparison harness timeout | 900s | Golden-file tests with real CLIs need the full budget |

---

## Key Decisions Made

- **Mixed backend model** — Codex uses `codex app-server` (structured JSON-RPC), Gemini/Claude stay as subprocess-per-request
- **Unified Backend interface** with lifecycle methods (`Start/Stop/Healthy/Run`)
- **stdio transport** for codex app-server (stable, default — not experimental websocket)
- **No serialization needed** — app-server handles concurrent threads natively
- **Custom minimal JSON-RPC client** — ~250 lines, referencing `pmenglund/codex-sdk-go` and `C7A6/codex-app-server-sdk-go` as examples
- **2-package architecture** — `httpmcp` (transport) + `roundtable` (domain) — validated by 3-model roundtable challenge

---

## Package Structure

```
cmd/roundtable-http-mcp/
  main.go                    <- wires deps, starts server

internal/
  httpmcp/                   <- HTTP/MCP transport boundary (existing)
    server.go                  tool registration, HTTP handlers
    handler.go                 renamed from backend.go, delegates to dispatcher
    config.go                  env vars

  roundtable/                <- NEW: unified domain logic (~1,230 lines)
    dispatcher.go              Backend interface + parallel fan-out
    output.go                  result normalization, JSON contract
    request.go                 transport-neutral request type
    prompt.go                  prompt assembly (role + request + files)
    roles.go                   role file loader + go:embed defaults
    runner.go                  subprocess lifecycle, timeout, cleanup
    runner_unix.go             process group kill (build-tagged)
    runner_windows.go          Windows process kill (build-tagged)
    gemini.go                  Gemini CLI arg builder + JSON parser
    claude.go                  Claude CLI arg builder + JSON parser
    codex_rpc.go               Codex app-server stdio JSON-RPC client
    codex_fallback.go          Codex JSONL parser (subprocess fallback)
```

---

## Backend Interface

```go
type Backend interface {
    Name() string
    Start(ctx context.Context) error    // no-op for subprocess
    Stop() error                        // no-op for subprocess
    Healthy(ctx context.Context) error  // probe check
    Run(ctx context.Context, req Request) (*Result, error)
}
```

**Implementation matrix:**

| Method | Subprocess (Gemini/Claude) | Codex App-Server |
|-|-|-|
| `Start()` | no-op | Launch child process, send `initialize` |
| `Stop()` | no-op | Close stdin, kill process group |
| `Healthy()` | Resolve binary, run `--version` | Check process alive + initialized |
| `Run()` | Spawn process, capture, parse | `thread/start` -> `turn/start` -> collect -> return |

---

## Request and Result Types

**Request** — transport-neutral, no MCP awareness:

- `Prompt` — the user's prompt text
- `Files` — file paths to reference
- `Timeout` — seconds, 1-900
- `Role` — resolved role name
- `Model` — optional model override
- `Resume` — optional session resume ID
- `RolesDir` / `ProjectRolesDir` — role file search paths

**Result** — exact match of current Elixir JSON contract:

- `response`, `model`, `status`, `exit_code`, `exit_signal`
- `elapsed_ms`, `parse_error`, `truncated`, `session_id`
- `stderr`, `stderr_truncated`, `metadata`

---

## Dispatcher Flow

```
     Probe Phase (5s timeout)
     |
     |  errgroup fan-out:
     |    gemini.Healthy(ctx)  --> ok / probe_failed
     |    codex.Healthy(ctx)   --> ok / probe_failed
     |    claude.Healthy(ctx)  --> ok / probe_failed
     |
     v
     Run Phase (timeout + 30s grace)
     |
     |  errgroup fan-out (healthy backends only):
     |    gemini.Run(ctx, req)  --> *Result
     |    codex.Run(ctx, req)   --> *Result
     |    claude.Run(ctx, req)  --> *Result
     |
     |  Typical: 8-11 min per backend
     |  Wall clock: bounded by slowest backend
     |
     v
     Collect
     |
     |  map[string]*Result {
     |    "gemini": { status: "ok", response: "..." }
     |    "codex":  { status: "ok", response: "..." }
     |    "claude": { status: "timeout" }
     |  }
```

**Error taxonomy:**

- `ErrTransport` — connection drop, process crash
- `ErrProtocol` — invalid JSON-RPC, parse failure
- `ErrApplication` — backend returned structured error
- `ErrTimeout` — deadline exceeded
- `ErrNotFound` — executable not found

---

## Codex App-Server Integration

**Protocol flow for each `Run()` call:**

```
Go client                           codex app-server
   |                                       |
   |---- thread/start ------------------->|
   |<--- { thread: { id: "thr_abc" } } ---|
   |                                       |
   |---- turn/start (threadId, prompt) -->|
   |                                       |
   |<--- item/agentMessage/delta ---------|  (streaming, 8-11 min)
   |<--- item/agentMessage/delta ---------|  (streaming)
   |<--- thread/tokenUsage/updated -------|
   |<--- turn/completed { status } -------|
   |                                       |
```

**Key implementation details:**

- Launch: `codex app-server --listen stdio://`
- Auth: inherits from `codex login` or `OPENAI_API_KEY` — no separate key needed
- Concurrency: each `Run()` starts its own thread — app-server isolates them
- Cancellation: `ctx.Done()` -> send `turn/interrupt` -> return timeout result (server stays alive)
- Backpressure: `-32001` from server -> surface as `ErrTransport`
- Mutex on stdin writes, monotonic request IDs
- Stderr from child process -> structured log sink

---

## Subprocess Backend (Gemini / Claude)

**Execution flow per `Run()` call:**

```
  resolve executable path
         |
         v
  exec.CommandContext(ctx, path, args...)
         |
         +-- SysProcAttr: Setpgid=true (Unix)
         +-- stdout pipe (max 1MB)
         +-- stderr pipe (max 512KB)
         |
         v
  cmd.Wait()                <- blocks 8-11 min typically
         |
         +-- exit 0 -> parse stdout -> Result
         +-- exit non-zero -> parse error -> Result
         +-- ctx cancelled -> kill process group -> timeout Result
         |
         v
  return *Result with elapsed_ms
```

**Path resolution order:**

1. Env var override (`ROUNDTABLE_GEMINI_PATH`, etc.)
2. Candidates list
3. `exec.LookPath`

**Parser notes:**

- **Gemini** — JSON output, rate limit detection (429 / RESOURCE_EXHAUSTED / quota), stderr fallback
- **Claude** — JSON output, ANSI escape stripping from model names, usage from `modelUsage`
- **Codex fallback** — JSONL events (`item.completed`, `thread.started`, `turn.completed`, `error`), resume modes

---

## Construction and Wiring

`main.go` builds everything and injects into `httpmcp`:

```
main.go
  |
  +-- LoadConfig()
  |
  +-- Build backends:
  |     gemini  = NewSubprocessBackend("gemini", geminiSpec)
  |     claude  = NewSubprocessBackend("claude", claudeSpec)
  |     codex   = NewCodexRPCBackend(codexPath, opts)
  |
  +-- Build dispatcher:
  |     disp = NewDispatcher([gemini, claude, codex], timeout, grace=30s)
  |
  +-- Wire to HTTP:
        app = httpmcp.NewApp(cfg, disp)
        app.ListenAndServe()
```

`httpmcp` only depends on `roundtable.Dispatcher`, `roundtable.Request`, `roundtable.Result`. It never imports subprocess or codex-specific types.

---

## Testing Strategy

**Unit tests** (per file in `internal/roundtable/`):

- `dispatcher_test.go` — mock backends, timeout, probe failure, agent selection
- `output_test.go` — golden-file JSON comparison with Elixir output
- `prompt_test.go` — assembly with roles, files, edge cases
- `roles_test.go` — filesystem fallback, embedded defaults
- `runner_test.go` — fake CLI scripts, timeout kill, truncation
- `gemini_test.go` — arg building, JSON parsing, rate limits
- `claude_test.go` — arg building, ANSI strip, usage extraction
- `codex_rpc_test.go` — fake app-server pipe, handshake, connection drop
- `codex_fallback_test.go` — JSONL events, resume modes

**Fake CLI scripts** — reuse pattern from `test/support/`:

- `fake_gemini_success.sh` — valid JSON response
- `fake_gemini_timeout.sh` — hangs forever (tests kill)
- `fake_gemini_error.sh` — rate limit error

**Comparison harness** (build-tagged `//go:build comparison`):

- Run same fixtures through Go dispatcher and `roundtable-cli`
- Compare decoded JSON field-by-field
- Tolerance-band `elapsed_ms`
- Explicit opt-in: `go test -tags comparison -timeout 900s ./...`

---

## Execution Phases

### Phase 2A — Codex App-Server PoC (de-risk first)

- [ ] Create `internal/roundtable/` with types (`request.go`, `output.go`)
- [ ] Define `Backend` interface in `dispatcher.go`
- [ ] Build `codex_rpc.go` — minimal stdio JSON-RPC client
- [ ] Build `codex_rpc_test.go` with fake app-server pipe
- [ ] Wire hybrid: Codex via app-server, Gemini/Claude still via `roundtable-cli`
- [ ] Validate one real Codex call end-to-end (expect 8-11 min)

**Milestone:** Codex works through native Go.
**Risk gate:** If unreliable -> fall back to `codex_fallback.go`.

### Phase 2B — Comparison Harness + Pure Domain

- [ ] Build `prompt.go` + tests
- [ ] Build `roles.go` + tests + `go:embed`
- [ ] Build `output.go` normalization + golden-file tests
- [ ] Set up comparison harness with 3-5 fixtures
- [ ] Validate byte-for-byte parity with Elixir

**Milestone:** 100% parity on pure-function modules.

### Phase 2C — Subprocess Backends + Dispatcher

- [ ] Build `runner.go` + `runner_unix.go` — process lifecycle
- [ ] Build `gemini.go` + tests
- [ ] Build `claude.go` + tests
- [ ] Build `codex_fallback.go` + tests
- [ ] Build `Dispatch()` logic with `errgroup`
- [ ] Build `dispatcher_test.go` with mock backends

**Milestone:** Full dispatch loop runs natively in Go.

### Phase 2D — Swap + Validate + Burn-in

- [ ] Modify `httpmcp/handler.go` — use `roundtable.Dispatcher`
- [ ] Update `main.go` — construct backends, inject
- [ ] Run comparison tests for all 5 tools (budget 15 min per test)
- [ ] Full test suite: `go test ./...`
- [ ] Manual smoke test through HTTP MCP
- [ ] Burn-in: 50+ real tool calls across all 5 tools (expect ~8-11 min each)

**Milestone:** `roundtable-cli` removed from runtime path.

---

## Risks

| Risk | Likelihood | Impact | Mitigation |
|-|-|-|-|
| Codex app-server protocol changes | Medium | High | Backend interface makes swap to fallback trivial |
| App-server orphan child processes | Medium | Medium | Process group kill in `Stop()`, integration test |
| Output JSON contract drift | Low | High | Golden-file tests, field-by-field comparison |
| Platform subprocess differences | Low | Medium | Build-tagged files, Linux is primary target |
| Scope creep during port | Medium | Medium | Match Elixir exactly first, improve later |
| Concurrent stdio write corruption | Low | Medium | Mutex on writes, monotonic request IDs |
| Long-running subprocess OOM | Low | Medium | Stdout 1MB cap, stderr 512KB cap, truncation flags |

---

## Modules Ported

| Go file | Est. lines | Elixir source | Lines |
|-|-|-|-|
| `dispatcher.go` | ~150 | `dispatcher.ex` | 135 |
| `output.go` | ~120 | `output.ex` | 114 |
| `request.go` | ~30 | (new) | -- |
| `prompt.go` | ~50 | `assembler.ex` | 39 |
| `roles.go` | ~50 | `roles.ex` | 39 |
| `runner.go` | ~200 | `runner.ex` | 287 |
| `runner_unix.go` | ~20 | `platform.ex` | 110 |
| `runner_windows.go` | ~20 | `platform.ex` | -- |
| `gemini.go` | ~130 | `gemini.ex` | 125 |
| `claude.go` | ~80 | `claude.ex` | 72 |
| `codex_rpc.go` | ~250 | (new) | -- |
| `codex_fallback.go` | ~130 | `codex.ex` | 131 |
| **Total** | **~1,230** | | **~1,052** |

---

## References

- Migration plan: `docs/go-http-migration-plan.md`
- Codex app-server docs: `developers.openai.com/codex/app-server`
- Codex Go SDK (reference): `github.com/pmenglund/codex-sdk-go`
- Codex Go SDK (reference): `github.com/C7A6/codex-app-server-sdk-go`
