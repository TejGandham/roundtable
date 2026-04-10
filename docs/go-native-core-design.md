# Roundtable Phase 2: Go Native Core — Design Spec

> **Branch:** `go-http-phase1` (phase 2 work landed on the same branch)
> **Design date:** 2026-04-10
> **Status:** **AS BUILT** — implementation complete, all phases landed.

This document is the original phase 2 design spec, annotated with an "AS BUILT" trail where the implementation matched, deviated from, or extended the plan. Use it as the historical record of how the native Go core came to be; use `docs/ARCHITECTURE.md` for the canonical description of the current system.

---

## Overview

Phase 2 replaces the Elixir `roundtable-cli` subprocess with a native Go dispatch core. The Go HTTP MCP server (phase 1) stops shelling out and handles everything natively.

**Before (phase 1):**

```
Claude Code -> Go HTTP MCP -> roundtable-cli (Elixir) -> CLIs
```

**After (phase 2):**

```
Claude Code -> Go HTTP MCP -> Go native dispatcher -> {
    Codex:  app-server (long-lived stdio JSON-RPC)
    Gemini: subprocess (per-request)
    Claude: subprocess (per-request)
}
```

**AS BUILT:** matches the plan. `main.go` runtime-probes the codex app-server at startup and falls back to `CodexFallbackBackend` (subprocess-per-request JSONL) if `initialize` fails, so there are effectively four backend implementations in the tree. See `internal/roundtable/codex_fallback.go`.

---

## Observed Response Times

Data from the design session (2026-04-10), 4 roundtable calls:

```
           Short prompts          Production prompts
           (design session)       (with files, real usage)
           ----------------       ------------------------
Claude:    1m 33s - 2m 10s        8 - 11 min
Codex:     1m 07s - 2m 40s        8 - 11 min
Gemini:    1m 11s - 3m 59s        8 - 11 min
Wall clock: 2m 08s - 5m 00s       up to 11+ min
```

**AS BUILT:** informed the 900s default tool timeout and the 30s run grace. Both values shipped unchanged.

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

**AS BUILT:** all values match. Constants live in `internal/roundtable/dispatcher.go` (`ProbeTimeout = 5 * time.Second`, `RunGrace = 30 * time.Second`) and `internal/roundtable/request.go` (`DefaultTimeout = 900`). The tool schema still enforces `1 <= timeout <= 900` in `httpmcp/server.go`.

---

## Key Decisions Made

- **Mixed backend model** — Codex uses `codex app-server` (structured JSON-RPC), Gemini/Claude stay as subprocess-per-request. **AS BUILT.**
- **Unified Backend interface** with lifecycle methods (`Start/Stop/Healthy/Run`). **AS BUILT** in `internal/roundtable/backend.go`.
- **stdio transport** for codex app-server (stable, default — not experimental websocket). **AS BUILT** — `codex app-server --listen stdio://`.
- **No serialization needed** — app-server handles concurrent threads natively. **AS BUILT** — concurrent `Run()` calls each start their own `thread/start` and get their own notification channel.
- **Custom minimal JSON-RPC client** — ~250 lines, referencing `pmenglund/codex-sdk-go` and `C7A6/codex-app-server-sdk-go` as examples. **AS BUILT** — `codex_rpc.go` is ~420 lines of Go (larger than the 250 line estimate because of notification routing and interrupt handling).
- **2-package architecture** — `httpmcp` (transport) + `roundtable` (domain). **AS BUILT** — `internal/httpmcp` depends on `internal/roundtable` but not vice versa.

---

## Package Structure

```
cmd/roundtable-http-mcp/
  main.go                    <- wires deps, starts server

internal/
  httpmcp/                   <- HTTP/MCP transport boundary (existing)
    server.go                  tool registration, HTTP handlers
    backend.go                 CLI-mode backend wrapper (kept for fallback)
    config.go                  env vars
    metrics.go                 atomic counters

  roundtable/                <- domain logic (~2,000 lines incl. tests)
    backend.go                 Backend interface
    run.go                     Run() entry point — parallel probe + run per agent
    dispatcher.go              Dispatcher struct (legacy single-role path)
    request.go                 transport-neutral request type
    result.go                  Result / Meta / DispatchResult types
    output.go                  BuildResult, BuildMeta normalization
    prompt.go                  prompt assembly (role + request + files)
    roles.go                   role file loader + go:embed defaults
    roles/                     embedded default role prompts
    runner.go                  subprocess lifecycle, timeout, cleanup
    runner_unix.go             process group kill (build-tagged)
    runner_windows.go          Windows process kill (build-tagged)
    gemini.go                  Gemini CLI arg builder + JSON parser
    claude.go                  Claude CLI arg builder + JSON parser
    codex_rpc.go               Codex app-server stdio JSON-RPC client
    codex_fallback.go          Codex JSONL parser (subprocess fallback)
```

**AS BUILT:** matches the plan with these additions:

- A standalone `backend.go` (interface) separate from `dispatcher.go` (impl).
- A dedicated `run.go` that implements the per-agent dispatch path used by the tool handler. The legacy `Dispatcher.Dispatch` from `dispatcher.go` is retained but is not on the production hot path — it dispatches the same role to all backends, while `Run` resolves roles per-agent.
- `internal/httpmcp/backend.go` was renamed to `backend.go` (not `handler.go` as the plan guessed); it now represents the CLI-mode fallback, not the primary backend.
- `internal/httpmcp/metrics.go` was added (not enumerated in the plan but implied by the phase 1 migration).

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

| Method | Subprocess (Gemini/Claude/CodexFallback) | Codex App-Server |
|-|-|-|
| `Start()` | no-op | Launch child process, send `initialize` |
| `Stop()` | no-op | Close stdin, kill process, wait for readLoop |
| `Healthy()` | Resolve binary, run `--version` | Check process alive + readLoop running |
| `Run()` | Spawn process, capture, parse | `thread/start` -> `turn/start` -> collect -> return |

**AS BUILT:** interface matches exactly. CodexFallback was added as a fourth subprocess implementation.

---

## Request and Result Types

**Request** — transport-neutral, no MCP awareness:

- `Prompt` — the assembled full prompt (role + request + files)
- `Files` — file paths to reference
- `Timeout` — seconds, 1-900
- `Role` — resolved role name (carried for observability)
- `Model` — optional model override
- `Resume` — optional session resume ID
- `RolesDir` / `ProjectRolesDir` — role file search paths

**Result** — exact match of the Elixir JSON contract:

- `response`, `model`, `status`, `exit_code`, `exit_signal`
- `elapsed_ms`, `parse_error`, `truncated`, `session_id`
- `stderr`, `stderr_truncated`

**AS BUILT:** the `metadata` field present in the Elixir contract is not currently a top-level field on `Result`; per-backend metadata (`model_used`, `tokens`, `usage`) flows through `ParsedOutput.Metadata` into `BuildResult` but `BuildResult` only propagates `model_used` into the result's `Model` field, not the raw metadata map. This is a small deviation from byte-for-byte parity: the Elixir version emitted a `metadata` key in the per-backend JSON; the Go version does not. The golden-file tests pass because they compare the fields the tool consumers actually use.

`DispatchResult` uses a custom `MarshalJSON` to inline each backend's result at the top level of the object along with a `meta` key, matching the Elixir shape.

---

## Dispatcher Flow

```
     Probe Phase (5s timeout per backend)
     |
     |  goroutine per agent:
     |    gemini.Healthy(ctx)  --> ok / probe_failed
     |    codex.Healthy(ctx)   --> ok / probe_failed
     |    claude.Healthy(ctx)  --> ok / probe_failed
     |
     v
     Run Phase (timeout + 30s grace)
     |
     |  goroutine per healthy agent:
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

**Error taxonomy (original plan):**

- `ErrTransport` — connection drop, process crash
- `ErrProtocol` — invalid JSON-RPC, parse failure
- `ErrApplication` — backend returned structured error
- `ErrTimeout` — deadline exceeded
- `ErrNotFound` — executable not found

**AS BUILT:** errors are expressed as `Result.Status` values rather than typed error sentinels: `ok`, `error`, `timeout`, `terminated`, `rate_limited`, `probe_failed`, `not_found`. `Backend.Run` may also return a plain Go `error` for the dispatcher to convert, but the primary channel is the status string on the result. This matches the Elixir output contract verbatim and avoided churn on the tool consumer side.

`Run` in `run.go` uses buffered channels + index-keyed slices instead of the originally-planned `errgroup`, to gracefully handle mixed healthy/unhealthy agents without polluting the goroutine group with pre-failed entries.

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
   |<--- item/completed (agentMessage) ---|  (streaming, 8-11 min)
   |<--- item/completed (agentMessage) ---|  (streaming)
   |<--- threadTokenUsage/updated --------|
   |<--- turn/completed { threadId } ----|
   |                                       |
```

**Key implementation details (as built):**

- Launch: `codex app-server --listen stdio://`. **AS BUILT.**
- Auth: inherits from `codex login` or `OPENAI_API_KEY`. **AS BUILT.**
- Concurrency: each `Run()` starts its own thread. **AS BUILT** — `collectTurn` registers a per-`threadID` notification channel in `notifs` and `readLoop` routes by `params.threadId`.
- Cancellation: `ctx.Done()` sends `turn/interrupt` (best effort, 5s fresh context) then returns a `timeout` `Result`. **AS BUILT.**
- Mutex on stdin writes, monotonic request IDs via `atomic.Int64`. **AS BUILT.**
- Stderr from child process: currently not captured into a structured log sink — it's just let go to the parent process's stderr. **DEVIATION from plan** — deemed acceptable because `slog` is attached to the parent.
- Backpressure / `-32001` handling: not explicitly coded; any RPC error surfaces as a Go error through `c.call` and becomes an `errorResult`. **DEVIATION from plan** — good enough in practice.
- Deadlock avoidance: `Start` releases `c.mu` before calling `c.call("initialize", ...)` and `Stop` grabs handles under lock then releases before closing. **ADDITION** not in the original plan but needed to avoid self-deadlock on the shared mutex.

Notifications observed in practice that the client handles or ignores:

| Notification | Handling |
|-|-|
| `item/completed` with `item.type == "agentMessage"` | text appended to response |
| `item/completed` with any other item type | ignored (reasoning traces, tool calls) |
| `threadTokenUsage/updated` | TODO marker, currently ignored |
| `turn/completed` | terminates the turn, returns the result |

---

## Subprocess Backend (Gemini / Claude / CodexFallback)

**Execution flow per `Run()` call:**

```
  resolve executable path (cached after first Healthy())
         |
         v
  exec.CommandContext(ctx, path, args...)
         |
         +-- SysProcAttr: Setpgid=true (Unix)
         +-- Stdin: nil (/dev/null)
         +-- Stdout: LimitedWriter(1 MB)
         +-- Stderr: LimitedWriter(512 KB)
         +-- Env: parent + ROUNDTABLE_ACTIVE=1 + prepended ROUNDTABLE_EXTRA_PATH
         +-- WaitDelay: 2s
         |
         v
  cmd.Start(); defer killProcessGroup(pid); cmd.Wait()
         |
         +-- exit 0 -> parse stdout -> Result
         +-- exit non-zero -> parse error -> Result
         +-- ctx cancelled -> cmd.Cancel (kill -SIGKILL -PGID) -> timeout Result
         +-- signaled -> extract signal name -> terminated Result
         |
         v
  return RawRunOutput -> BuildResult(raw, parsed, fallbackModel) -> *Result
```

**Path resolution order:**

1. Env var override (`ROUNDTABLE_GEMINI_PATH`, etc.)
2. `ROUNDTABLE_EXTRA_PATH` directories (colon-separated)
3. `exec.LookPath`

**AS BUILT:** matches. Lives in `internal/roundtable/runner.go::ResolveExecutable`. The `Candidates` list step from the Elixir era was not ported — we rely on explicit env var + system PATH only.

**Parser notes:**

- **Gemini** — JSON on stdout first, stderr fallback, then raw text. Rate-limit substring match on `429`, `rate limit`, `too many requests`, `resource_exhausted`, `quota`. Metadata from `stats.models.<first_key>.tokens`. **AS BUILT.**
- **Claude** — JSON on stdout only, `is_error` flag, ANSI-strip the first `modelUsage` key for the model name. **AS BUILT.**
- **Codex fallback** — JSONL line-by-line: `item.completed` with `agent_message`, `thread.started` for session ID, `turn.completed` for usage, `error` for errors. Resume modes: empty / `last` / session id. **AS BUILT.**

---

## Construction and Wiring

Original plan:

```
main.go
  |
  +-- LoadConfig()
  +-- Build backends
  +-- Build dispatcher
  +-- Wire to HTTP: app = httpmcp.NewApp(cfg, disp)
```

**AS BUILT:** the wiring ended up slightly different. `main.go` builds the backends map directly and passes a `DispatchFunc` closure (not a `Dispatcher` struct) plus a `BackendProbe` map into `httpmcp.NewAppWithDispatcherAndBackends`. This keeps `httpmcp` from having to know about the `Backend` interface at all — it only needs the probe interface and a function pointer.

```
main.go
  |
  +-- httpmcp.LoadConfig(logger)
  |
  +-- resolve codex path, attempt NewCodexBackend + Start
  |     success -> codexBackend = codexRPC
  |     failure -> codexBackend = NewCodexFallbackBackend("", "")
  |
  +-- backends = {
  |     "gemini": NewGeminiBackend(""),
  |     "codex":  codexBackend,
  |     "claude": NewClaudeBackend(""),
  |   }
  |
  +-- startBackends(ctx, backends, logger)
  +-- defer stopBackends(backends, logger)
  |
  +-- probes = backends as map[string]httpmcp.BackendProbe
  +-- dispatch = buildDispatchFunc(backends, config, logger)
  +-- app = httpmcp.NewAppWithDispatcherAndBackends(config, dispatch, probes)
  |
  +-- signal.NotifyContext(SIGINT, SIGTERM)
  +-- http.Server{...}.ListenAndServe in goroutine
  +-- server.Shutdown with 30s context on signal
```

`httpmcp` only depends on `roundtable.ToolRequest`, the `DispatchFunc` closure, and a `BackendProbe` interface with a single `Healthy` method. It never imports subprocess or codex-specific types. **AS DESIGNED.**

---

## Testing Strategy

**Unit tests (per file in `internal/roundtable/`):**

| Test file | Coverage | AS BUILT? |
|-|-|-|
| `run_test.go` | `Run()` with mock backends, agent resolution | yes (replaces the planned `dispatcher_test.go` for the production path) |
| `dispatcher_test.go` | Legacy `Dispatcher`, probe failure, panic recovery | yes |
| `output_test.go` | `BuildResult` status normalization | yes |
| `result_test.go` | Result / Meta / DispatchResult JSON marshal | yes (added, not in original plan) |
| `domain_test.go` | Cross-cutting sanity checks | yes (added) |
| `prompt_test.go` | Assembly with roles, files, edge cases | yes |
| `roles_test.go` | Filesystem fallback, embedded defaults | yes |
| `runner_test.go` | Fake CLI scripts, timeout kill, truncation | yes |
| `gemini_test.go` | Arg building, JSON parsing, rate limits | yes |
| `claude_test.go` | Arg building, ANSI strip, usage extraction | yes |
| `codex_rpc_test.go` | Fake app-server pipe, handshake, connection drop | yes |
| `codex_fallback_test.go` | JSONL events, resume modes | yes |

Plus `internal/httpmcp/{server,backend,e2e}_test.go` for the transport layer.

**Fake CLI scripts** — reuse pattern from `test/support/`. **AS BUILT** under `internal/roundtable/runner_test.go` using inline shell scripts.

**Comparison harness** (`//go:build comparison`):

- Run same fixtures through Go dispatcher and `roundtable-cli`.
- Compare decoded JSON field by field.
- Tolerance-band `elapsed_ms`.

**DEVIATION from plan:** the formal comparison harness was not built as a separate build-tagged package. Instead, byte-for-byte parity was validated ad-hoc during phase 2B by hand-comparing Elixir and Go output on representative fixtures, and the golden-file style lives inside the individual `*_test.go` files (especially `output_test.go` and `result_test.go`). The phase 2B + 2D gates were met without the harness, and the Elixir backend is still available via `cli` mode if regression hunting is ever needed.

---

## Execution Phases

### Phase 2A — Codex App-Server PoC

- [x] Create `internal/roundtable/` with types (`request.go`, `result.go`)
- [x] Define `Backend` interface in `backend.go`
- [x] Build `codex_rpc.go` — minimal stdio JSON-RPC client
- [x] Build `codex_rpc_test.go` with fake app-server pipe
- [x] Wire hybrid: Codex via app-server, Gemini/Claude still via `roundtable-cli`
- [x] Validate one real Codex call end-to-end

**Milestone:** Codex works through native Go. **ACHIEVED.**
**Risk gate:** If unreliable -> fall back to `codex_fallback.go`. Fallback was built anyway in phase 2C and is selected at startup if `initialize` fails.

### Phase 2B — Comparison Harness + Pure Domain

- [x] Build `prompt.go` + tests
- [x] Build `roles.go` + tests + `go:embed`
- [x] Build `output.go` normalization + tests
- [~] Set up comparison harness with 3-5 fixtures — replaced by inline golden-file tests
- [x] Validate parity with Elixir (spot-checked, not build-tagged harness)

**Milestone:** parity on pure-function modules. **ACHIEVED** (less formally than planned).

### Phase 2C — Subprocess Backends + Dispatcher

- [x] Build `runner.go` + `runner_unix.go` + `runner_windows.go`
- [x] Build `gemini.go` + tests
- [x] Build `claude.go` + tests
- [x] Build `codex_fallback.go` + tests
- [x] Build `Dispatch()` logic — landed as `Run()` in `run.go`, with the legacy `Dispatcher` kept separately
- [x] Build `dispatcher_test.go` with mock backends

**Milestone:** Full dispatch loop runs natively in Go. **ACHIEVED.**

### Phase 2D — Swap + Validate + Burn-in

- [x] Modify `httpmcp` wiring — `NewAppWithDispatcherAndBackends` added
- [x] Update `main.go` — construct backends, inject
- [x] Full Go test suite: `go test ./...`
- [x] Manual smoke test through HTTP MCP
- [~] Burn-in: 50+ real tool calls — in progress / ongoing in production

**Milestone:** `roundtable-cli` removed from runtime path. **ACHIEVED** — the escript now only runs when `ROUNDTABLE_HTTP_BACKEND_MODE=cli`.

---

## Risks

| Risk | Likelihood | Impact | Mitigation | Outcome |
|-|-|-|-|-|
| Codex app-server protocol changes | Medium | High | Backend interface makes swap to fallback trivial | Runtime startup probe + automatic fallback to `CodexFallbackBackend`. |
| App-server orphan child processes | Medium | Medium | Process group kill in `Stop()`, integration test | Not observed in practice; `Stop` releases lock before Kill to avoid deadlock. |
| Output JSON contract drift | Low | High | Golden-file tests, field comparison | One known deviation: `metadata` map not emitted at the top level. Consumers unaffected. |
| Platform subprocess differences | Low | Medium | Build-tagged files, Linux is primary target | `runner_unix.go` + `runner_windows.go` in place. Linux is the tested path. |
| Scope creep during port | Medium | Medium | Match Elixir exactly first, improve later | Scope held. Behavioral parity first. |
| Concurrent stdio write corruption | Low | Medium | Mutex on writes, monotonic request IDs | `CodexBackend.mu` guards stdin writes, `nextID.Add(1)` for IDs. |
| Long-running subprocess OOM | Low | Medium | 1 MB stdout cap, 512 KB stderr cap, truncation flags | `LimitedWriter` in `runner.go` with `Truncated()` propagated to result. |

---

## Modules Ported

Final line counts (as built) vs the original estimates. These are non-test `.go` files; test files roughly double the totals.

| Go file | Est. lines | Actual lines | Elixir source | Elixir lines |
|-|-|-|-|-|
| `backend.go` | -- | 40 | (new) | -- |
| `run.go` | -- | 390 | (new) | -- |
| `dispatcher.go` | ~150 | 130 | `dispatcher.ex` | 135 |
| `output.go` | ~120 | 120 | `output.ex` | 114 |
| `result.go` | -- | 90 | (split out) | -- |
| `request.go` | ~30 | 15 | (new) | -- |
| `prompt.go` | ~50 | 65 | `assembler.ex` | 39 |
| `roles.go` | ~50 | 70 | `roles.ex` | 39 |
| `runner.go` | ~200 | 235 | `runner.ex` | 287 |
| `runner_unix.go` | ~20 | 45 | `platform.ex` | 110 |
| `runner_windows.go` | ~20 | 20 | `platform.ex` | -- |
| `gemini.go` | ~130 | 245 | `gemini.ex` | 125 |
| `claude.go` | ~80 | 175 | `claude.ex` | 72 |
| `codex_rpc.go` | ~250 | 420 | (new) | -- |
| `codex_fallback.go` | ~130 | 215 | `codex.ex` | 131 |
| **Total** | **~1,230** | **~2,275** | | **~1,052** |

The Go tree is roughly twice the size of the original estimate and twice the size of the Elixir source. Cost drivers:

- Explicit type definitions and JSON marshalers (vs. Elixir maps).
- `codex_rpc.go` grew from 250 to 420 lines to cover notification routing, interrupt handling, and deadlock-avoiding Stop semantics.
- The `gemini.go` parser grew to handle rate-limit detection across multiple error shapes and the stderr JSON fallback.
- `runner.go` grew to add the `LimitedWriter` abstraction and environment manipulation.

---

## References

- Migration plan: `docs/go-http-migration-plan.md`
- Current architecture: `docs/ARCHITECTURE.md`
- Codex app-server docs: `developers.openai.com/codex/app-server`
- Codex Go SDK (reference): `github.com/pmenglund/codex-sdk-go`
- Codex Go SDK (reference): `github.com/C7A6/codex-app-server-sdk-go`
