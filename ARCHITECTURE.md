# Roundtable Architecture

Roundtable is a single static Go binary (`roundtable`) that serves the Model Context Protocol over **stdio** and dispatches each prompt to Claude, Gemini, and Codex CLIs — plus any configured OpenAI-compatible HTTP providers — in parallel, returning a structured JSON document with every response and per-agent metadata. It supports selective agent dispatch: invoke any subset of backends, run the same backend with different models, and assign per-agent roles.

## Overview

`roundtable` is a single Go binary that:

1. Speaks MCP over stdin/stdout (the `modelcontextprotocol/go-sdk` stdio transport). There is no listening port and no HTTP endpoint.
2. Registers five tools: `roundtable-canvass`, `roundtable-deliberate`, `roundtable-blueprint`, `roundtable-critique`, `roundtable-crosscheck`.
3. On each tool call, assembles a role-scoped prompt and dispatches it to the resolved set of backends in parallel.
4. Normalizes each backend's output into a uniform JSON contract (`result.go::DispatchResult`) and returns the aggregate to the caller.

Claude Code (or any other MCP client) fork/execs `roundtable stdio` once per session and the two processes exchange JSON-RPC frames over the pipes until the client disconnects. No daemon survives between sessions, so there is nothing to babysit and no cross-session state to worry about.

## Architecture diagram

```
+----------------+                       +---------------------------+
|  Claude Code   |  fork/exec + stdio    |   roundtable (Go binary)  |
|  (MCP client)  +---------------------->+                           |
+----------------+                       |  +---------------------+  |
                                         |  | stdiomcp (transport)|  |
                                         |  |  - tool registration|  |
                                         |  |  - stdio framing    |  |
                                         |  |  - 5s keepalive     |  |
                                         |  |  - panic recovery   |  |
                                         |  +----------+----------+  |
                                         |             |             |
                                         |             v             |
                                         |  +---------------------+  |
                                         |  | roundtable (domain) |  |
                                         |  |  - Run() dispatcher |  |
                                         |  |  - prompt assembly  |  |
                                         |  |  - role loader      |  |
                                         |  |  - result normalize |  |
                                         |  |  - provider registry|  |
                                         |  +----------+----------+  |
                                         |             |             |
                                         |   +----+----+----+----+   |
                                         |   |    |    |    |    |   |
                                         |   v    v    v    v    v   |
                                         | gemini codex claude  HTTP |
                                         | Back.  Back. Back. providers
                                         +---|------|------|------|--+
                                             |      |      |      |
                                        subprocess stdio subprocess POST
                                            |   JSON-RPC   |      /v1/chat
                                            v       |      v      /completions
                                       +---------+  |  +---------+
                                       | gemini  |  |  | claude  |
                                       |   CLI   |  |  |   CLI   |
                                       +---------+  |  +---------+
                                                    v
                                              +-----------+
                                              | codex     |
                                              | app-server|
                                              +-----------+
```

The Codex app-server is launched lazily under a `sync.Once` on the first tool call that needs it, not at process start. The subprocess backends (Gemini, Claude) are spawned per-request. OpenAI-compatible providers speak HTTP to their configured `base_url` with `stream: false` chat completions.

## Components

### Top-level

| Package / file | Responsibility |
|-|-|
| `cmd/roundtable/main.go` | Entry point. Parses `stdio` / `version` / `help` subcommands, builds backends, wires dispatch into `stdiomcp`, runs the stdio server with signal-driven shutdown. |

### `internal/stdiomcp` — transport

| File | Responsibility |
|-|-|
| `server.go` | MCP tool registration against `mcp.Server`, 5s keepalive progress notifications, tool-handler panic recovery, server startup logging; schema fast-fail parse before dispatch closure (invalid `input.Schema` → `IsError: true`, no backend invoked). |
| `types.go` | `ToolSpec`, `ToolInput`, `ToolRequest`, `DispatchFunc` — transport-neutral contracts shared with the domain layer. |
| `discipline.go` | Structured-output discipline helpers (prompt suffix handling). |

### `internal/roundtable` — domain + backends

| File | Responsibility |
|-|-|
| `run.go` | `Run()` — the native dispatch entry point. Resolves agents, loads roles, assembles prompts, runs two-phase parallel probe/run, returns `DispatchResult` JSON. Holds `ProbeTimeout` (5s) and `RunGrace` (30s) constants. |
| `backend.go` | `Backend` interface (`Name`, `Start`, `Stop`, `Healthy`, `Run`). |
| `runner.go` | `SubprocessRunner` with bounded `LimitedWriter` stdout/stderr capture, process-group cleanup, `ROUNDTABLE_ACTIVE=1` env injection, `ROUNDTABLE_EXTRA_PATH` prepend. |
| `runner_unix.go` | Unix `Setpgid` + `kill -SIGKILL -PGID` atomic process-group kill. |
| `runner_windows.go` | Windows process kill stub. |
| `gemini.go` | Gemini subprocess backend. Arg builder, stdout/stderr JSON parser, rate-limit detection, metadata extraction. |
| `claude.go` | Claude subprocess backend. Arg builder, JSON parser, ANSI stripping from `modelUsage` keys. |
| `codex_rpc.go` | **Primary Codex path.** Long-lived `codex app-server` subprocess with JSON-RPC 2.0 over stdio. Lazy-started under `sync.Once` on first use. Handles initialize handshake, per-turn threads, notification routing, interrupt on cancel. |
| `codex_rpc_pdeathsig_linux.go` | Linux-only: `PR_SET_PDEATHSIG = SIGKILL` so the kernel reaps the app-server if `roundtable` exits without calling `Stop`. |
| `codex_rpc_pdeathsig_other.go` / `codex_rpc_pdeathsig_windows.go` | No-op implementations for non-Linux. |
| `codex_fallback.go` | Fallback Codex backend: subprocess-per-request `codex exec --json` JSONL parser. Used only when `NewCodexBackend.Start` fails (e.g., a Codex build without `app-server`). |
| `openai_http.go` | Generic OpenAI-compatible HTTP backend. Posts `stream: false` chat completions, handles 429/503 as `rate_limited`, parses string-or-array `content`, enforces per-provider concurrency via a semaphore. |
| `providers.go` | `ProviderConfig` + `LoadProviderRegistry` — parses `ROUNDTABLE_PROVIDERS`, validates `base_url`, registers one `OpenAIHTTPBackend` per entry. |
| `observe.go` | Lightweight observability hooks for per-provider/model outcome counting. |
| `files.go` | `InlineFileContents` — reads referenced files under caps (size and total), emits `<file path="...">...</file>` boundaries. |
| `prompt.go` | `AssemblePrompt` — joins role + `=== REQUEST ===` + inlined files. |
| `roles.go` | `LoadRolePrompt` with three-level fallback: project dir → global dir → `go:embed roles/*.txt` defaults. |
| `roles/` | Embedded defaults: `default.txt`, `planner.txt`, `codereviewer.txt`. |
| `request.go` | `Request` — transport-neutral input to a backend. |
| `result.go` | `Result`, `Meta`, `DispatchResult` types and JSON marshaling. `NotFoundResult`, `ProbeFailedResult`, `ConfigErrorResult` constructors. Carries `Structured json.RawMessage` (omitempty — parsed validated payload from `dispatchschema.Validate`) and `StructuredError *dispatchschema.ValidationError` (omitempty — per-panelist validation failure). When no schema is supplied, both fields are nil and elided from marshaled JSON — wire format unchanged. |
| `output.go` | `BuildResult` status normalization (timeout / terminated / error / rate_limited / ok) and `BuildMeta` aggregation. |

### `internal/roundtable/dispatchschema` — JSON-Schema-lite parser + prompt suffix + response validator

| File | Responsibility |
|-|-|
| `schema.go` | `SafeParse(raw json.RawMessage) (*Schema, error)` — sanctioned entry for untrusted bytes. Enforces `MaxSchemaBytes=65536` on `len(raw)` BEFORE `bytes.TrimSpace` (rejects whitespace-flood DoS pre-trim), short-circuits empty/`null`/whitespace-only to `(nil, nil)`, then delegates to `Parse`. Untrusted input MUST go through `SafeParse`. `Parse(raw json.RawMessage) (*Schema, error)` — JSON-Schema-lite subset parser; expects trimmed, non-empty bytes from a trusted in-process source. Accepts a top-level object with typed scalar fields (string/number/boolean), optional `enum` on string fields, and optional `required: [...]`. Rejects nested objects, arrays, `anyOf`/`oneOf`/`allOf`, `$ref`, `format`, `additionalProperties: true`, and any other keyword outside the supported subset. Enforces `MaxProperties=256`, `MaxEnumEntries=256`, `MaxRequiredEntries=256` count caps; over-cap returns `*ParseError{Kind: KindBoundExceeded}`. All other rejections return `*ParseError{Kind: KindMalformed, Cause: <inner>}` preserving message strings verbatim. `ParseError` exposes `Kind`/`Message` plus `Unwrap()` so callers can `errors.As` through to `*json.SyntaxError`. Stdlib-only; preserves field order via `json.Decoder` token stream. |
| `prompt.go` | `BuildPromptSuffix(schema *Schema) string` — deterministic prompt-suffix builder. Enumerates schema fields with type; lists enum values verbatim for string-enum fields; emits `(string, free-text)` for free-text strings and `(number)` / `(boolean)` for typed scalars. Instructs panelists to wrap structured response in a single fenced ` ```json ` block (last block is canonical payload). Sanitizes field names and enum values against prompt-injection vectors (LF, triple-backticks, control chars). Stdlib-only (`fmt`, `strings`). |
| `validate.go` | `Validate(response string, schema *Schema) (parsed json.RawMessage, vErr *ValidationError)` — per-panelist response validator. Extracts the **last** fenced ` ```json ` block from `response`, JSON-decodes, and validates against `schema` (typed-scalar conformance, string-enum membership, required-field presence). Fails closed on closed-then-unclosed fence sequences (no stale-block fallback). Rejects literal `null` for scalars (no zero-value coercion). Returns `ValidationError{Kind, Field, Message, Excerpt}` with `Kind ∈ {missing_fence, json_parse, schema_violation}` exposed as exported untyped string constants. `Excerpt` is rune-aware capped at 200 runes. Stdlib-only; no panics on hostile input; no retry. Package is a leaf — must not import `internal/roundtable` (enforced by `imports_test.go`). |

## Request flow

Step-by-step for a single `roundtable-canvass` tool call:

1. **MCP frame arrives** on stdin. The go-sdk stdio transport parses it as an `mcp.CallToolRequest`.
2. **Tool handler fires** in `stdiomcp/server.go` (registered by `registerTool`). It captures the progress token and launches a worker goroutine; the main goroutine selects on the worker's done channel, `ctx.Done()`, and a 5s ticker emitting keepalive progress notifications. An immediate `notify(0)` on tool-call start saves clients that default to a short deadline.
3. **DispatchFunc is invoked** (`buildStdioDispatch` in `main.go`). It parses the `agents` JSON, splits `files` on commas, resolves the timeout (default 900s, capped at 900s); calls `dispatchschema.SafeParse(input.Schema)` — `SafeParse` owns the byte cap (rejecting `>65536`-byte payloads pre-trim), the trim, and the empty/`null` short-circuit (returns `(nil, nil)` for "no schema"); on parse failure returns immediately with `fmt.Errorf("invalid schema parameter: %w", err)` (surfaces as `IsError: true` before any backend is invoked); builds a `roundtable.ToolRequest` (carrying the parsed `*Schema` when present), and calls `roundtable.Run`.
4. **`Run` resolves agents** — explicit `Agents` > `ROUNDTABLE_DEFAULT_AGENTS` env > the default three (`gemini`, `codex`, `claude`).
5. **Per-agent config** — for each agent, `Run` resolves role (agent spec > per-CLI > tool default > `"default"`), model, and resume ID; loads the role prompt via `LoadRolePrompt`; calls `AssemblePrompt(rolePrompt, basePrompt+suffix, files)`; finds the matching backend by provider id.
6. **Probe phase** — one goroutine per agent calls `backend.Healthy(ctx)` with `ProbeTimeout=5s`. Results collect from a buffered channel into a fixed-index slice. `Healthy` is cheap for all backends (Codex uses a lock-free liveness check against the cached start state; subprocess backends cache `execPath` via `ResolveExecutable`).
7. **Run phase** — for each agent that passed the probe, a goroutine calls `backend.Run(runCtx, req)` where `runCtx` has a `timeout + 30s` grace deadline. Panics are recovered into error `Result`s. Unhealthy agents get `ProbeFailedResult` or `NotFoundResult`.
8. **Result aggregation** — `Run` assembles a `map[string]*Result` keyed by agent name. When `req.Schema != nil`, iterates results and calls `dispatchschema.Validate(result.Response, req.Schema)` for each result with `Status == "ok"`, populating `result.Structured` (parsed payload) on success or `result.StructuredError` (`Kind/Field/Message/Excerpt`) on failure. Non-`ok` statuses skip validation; both fields stay nil. Then calls `BuildMeta` and JSON-marshals a `DispatchResult`.
9. **Response** — the bytes are sent back through the worker's done channel to the tool handler, wrapped in an `mcp.CallToolResult` with a single `TextContent`, and written out over stdio.

If the context is cancelled (client disconnect or deadline), the handler returns an error result. Each backend's `Run` is expected to honor `ctx.Done()` and return a `timeout` or `error` `Result`.

## Backend implementations

### GeminiBackend (subprocess-per-request)

- `Healthy`: `ResolveExecutable("gemini")` then `gemini --version` via `SubprocessRunner.Probe`.
- `Run`: builds args `[-p <prompt> -o json --yolo (-m <model>)? (--resume <id>)?]`, executes, parses stdout-or-stderr JSON.
- Parser detects rate limits via case-insensitive substring match on `429`, `rate limit`, `too many requests`, `resource_exhausted`, `quota` and emits `status: "rate_limited"` with a canned message.
- Metadata comes from `stats.models.<first_key>.tokens`.

### ClaudeBackend (subprocess-per-request)

- `Healthy`: `claude --version` probe.
- `Run`: builds args `[-p --output-format json --dangerously-skip-permissions (--model <m>)? (-r <id>)? <prompt>]`. The prompt is positional, last.
- Parser reads stdout JSON only. Checks `is_error` for the error path. Strips ANSI escape codes from the first `modelUsage` key to extract the model name.

### CodexBackend (primary — lazy long-lived app-server)

- **Lazy start**: `codex app-server --listen stdio://` is **not** launched at `roundtable` startup. It is started under a `sync.Once` inside `ensureStarted`, called from the first `Run` invocation that dispatches to Codex. This keeps process start cheap for sessions that never call Codex and means `Healthy(ctx)` is cheap (pre-start it returns nil without launching anything).
- **Orphan prevention**: on Linux, `applyPdeathsig` sets `Setpgid=true` and `PR_SET_PDEATHSIG=SIGKILL` so the kernel delivers SIGKILL to the app-server if `roundtable` exits without calling `Stop`. On macOS / Windows the process-group flag alone is used; orphan cleanup in those environments relies on happy-path `Stop`.
- **Handshake**: once started, the backend pipes stdin/stdout, spawns a `readLoop` goroutine, then sends an `initialize` request with `clientInfo` populated from `config.ServerName` / `config.ServerVersion`.
- **`Healthy`** checks that `cmd.Process != nil` and the `done` channel (closed by `readLoop` on EOF) is not closed.
- **`Run`** implements a three-step protocol per request:
  1. `thread/start` with empty params → receives `{thread: {id}}`.
  2. `turn/start` with `{threadId, input: [{type: "text", text: prompt}], (model)?}`.
  3. Block on the per-thread notification channel until `turn/completed`, accumulating `item/completed` events where `item.type == "agentMessage"`.
- **`Stop`** grabs references under lock, releases the lock (to avoid deadlock with `readLoop`), closes stdin, kills the process, waits, and waits for `done`.
- On context cancel during a turn, fires a best-effort `turn/interrupt` with a fresh 5s context and returns a `timeout` result. The app-server stays alive for subsequent turns.
- `readLoop` reads newline-delimited JSON. Messages with an `id` field are responses (routed to `pending[id]`); messages with only `method` are notifications (routed to `notifs[threadId]` by parsing `params.threadId`).
- Concurrency: `pending` and `notifs` use separate mutexes. The top-level `mu` protects the stdin `Write` so concurrent turns can interleave messages safely.

### CodexFallbackBackend (subprocess-per-request JSONL)

Used only when `NewCodexBackend.Start` fails at startup (e.g., a codex binary that does not support `app-server`).

- `Healthy`: `codex --version` probe.
- `Run`: builds args `[exec --json --dangerously-bypass-approvals-and-sandbox (-c model=<m>)? (-c reasoning_effort=<r>)? <resume...> <prompt>]`. Resume modes: empty, `last` (`resume --last <prompt>`), or session id (`resume <id> <prompt>`).
- Parser walks stdout line by line, decoding each `{` JSON event:
  - `item.completed` with `item.type == "agent_message"` → append text to messages.
  - `thread.started` → capture `thread_id` as session ID.
  - `turn.completed` → capture `usage` into metadata.
  - `error` → append to errors.
- Success path joins messages with `\n\n`. Error-only path joins errors with `\n`. Empty output returns a synthetic error with `parse_error`.

### OpenAIHTTPBackend (outbound HTTP providers)

- Registered from `ROUNDTABLE_PROVIDERS` at startup (see Configuration below).
- `Healthy`: verifies the `api_key_env` env var is non-empty. Does not make a network call.
- `Run`: POSTs `{"model": ..., "messages": [{"role": "system", "content": <role>}, {"role": "user", "content": <inlined files>+<prompt>}], "stream": false}` to `${base_url}/chat/completions`.
- The per-provider semaphore (`max_concurrent`, default 3) gates concurrent requests. Waits above `gate_slow_log_threshold` (default 100 ms) emit a debug log for tuning.
- Response parsing handles `choices[0].message.content` as both string and array-of-parts; extracts text parts, fails closed on empty assistant output.
- 429 / 503 map to `status: "rate_limited"` with `metadata.retry_after` populated when the server sends `Retry-After`. No auto-retry.
- `finish_reason == "length"` sets `metadata.output_truncated = true` so callers can detect cutoff without knowing provider conventions.

## Codex app-server protocol

The app-server speaks JSON-RPC 2.0 over stdio. Every message is one JSON object terminated by `\n`. All methods return `-32600 "Not initialized"` until `initialize` completes.

```
    roundtable                          codex app-server
       |                                        |
       |  (first Codex-bound tool call arrives — sync.Once fires)
       |                                        |
       |  -->  initialize                       |
       |       { clientInfo: { name, version }} |
       |  <--  { result: {...} }                |
       |                                        |
       |  -->  thread/start                     |
       |       { }                              |
       |  <--  { result:                        |
       |          { thread:                     |
       |            { id: "thr_.." }            |
       |          }                             |
       |        }                               |
       |                                        |
       |  -->  turn/start                       |
       |       { threadId: "thr_..",            |
       |         input: [{type:                 |
       |         "text", text: ...}]            |
       |       }                                |
       |  <--  { result: {...} }                |
       |                                        |
       |  <-- notify: item/completed (agentMessage)
       |  <-- notify: item/completed (reasoning, ignored)
       |  <-- notify: threadTokenUsage/updated (ignored)
       |  <-- notify: turn/completed { threadId }
       |                                        |
       |     (on client cancel)                 |
       |  -->  turn/interrupt                   |
       |       { threadId }                     |
```

The Go implementation only extracts text from `agentMessage` items; other item types (reasoning traces, tool calls) are silently dropped.

## Configuration

All configuration is via environment variables.

### Server

|Variable|Default|Purpose|
|-|-|-|
|`ROUNDTABLE_ROLES_DIR`|(embedded)|Global roles directory. Overrides embedded defaults. `ROUNDTABLE_HTTP_ROLES_DIR` accepted as a deprecated fallback.|
|`ROUNDTABLE_PROJECT_ROLES_DIR`|(none)|Project-local roles directory, searched before global. `ROUNDTABLE_HTTP_PROJECT_ROLES_DIR` accepted as a deprecated fallback.|

### Dispatch

|Variable|Default|Purpose|
|-|-|-|
|`ROUNDTABLE_DEFAULT_AGENTS`|(all three subprocess backends)|JSON-encoded array string of default agents when the tool call omits `agents`. HTTP providers are never in the default set — adding one is a compile-time test failure (`TestDefaultAgents_ExcludesAllHTTPProviders`).|
|`ROUNDTABLE_PROVIDERS`|(none)|JSON array registering one or more OpenAI-compatible HTTP providers. See `INSTALL.md` §7–15 for the full schema and examples.|

### Executable resolution

For each CLI name (`gemini`, `codex`, `claude`), `ResolveExecutable` tries:

1. `ROUNDTABLE_<NAME>_PATH` — explicit override. Must exist on disk.
2. `ROUNDTABLE_EXTRA_PATH` — colon-separated directories, searched before system PATH.
3. `exec.LookPath(name)` — system PATH lookup.

Returns empty string if not found. The backend records `status: "not_found"` in that case.

The child process environment always includes `ROUNDTABLE_ACTIVE=1` (set by `subprocessEnv`) so the inner CLIs can detect they are running under Roundtable.

## Health and observability

`roundtable` is a stdio MCP server, so there are no HTTP endpoints and no `/healthz`, `/readyz`, or `/metricsz`. Liveness is implicit: if Claude Code (or any MCP client) can exchange `initialize` frames with the binary over stdin/stdout, the server is alive. A stalled server manifests as stdio EOF or the client's own deadline firing.

Per-backend health is observed lazily at tool-call time via the probe phase in `Run()` — each call runs a fresh health probe with the 5s `ProbeTimeout`, and probe failures are surfaced per-agent in the response JSON rather than as a server-level ready/not-ready signal.

Provider-level observability is handled by the `observe.go` hooks: outcome counters are recorded per `(provider, model, status)` tuple for optional external collection. There is no built-in Prometheus / JSON endpoint in the current stdio-only build.

## Concurrency model

| Component | Concurrency mechanism |
|-|-|
| stdio server | The go-sdk stdio transport reads framed JSON-RPC messages from stdin. Each MCP call runs on its own goroutine. |
| Tool handler | Spawns a worker goroutine; the main goroutine selects on `done`, `ctx.Done()`, and a 5s ticker emitting keepalive progress notifications (plus an immediate progress-on-start). |
| Native `Run()` | Two sequential phases, each using goroutines + a buffered channel for fan-in. Probe phase collects into a fixed-index slice; run phase collects into a counted loop. |
| GeminiBackend / ClaudeBackend / CodexFallbackBackend | `sync.Mutex` protects the cached `execPath`. `Run` is otherwise stateless — each call spawns its own subprocess. |
| CodexBackend | `sync.Once` gates app-server start; three mutexes: `mu` (subprocess handles + stdin writes), `pendingMu` (request id → response channel), `notifMu` (thread id → notification channel). A single `readLoop` goroutine fans incoming messages out to the right channel. `done` is closed on EOF so callers can unblock. |
| OpenAIHTTPBackend | Per-provider `semaphore.Weighted` gates concurrent requests at `max_concurrent`. Shared `http.Client` with per-transport tuning (`ResponseHeaderTimeout`, idle pool). |
| SubprocessRunner | Each process gets its own process group (`Setpgid=true`). `exec.CommandContext` + `cmd.Cancel` issue `kill -SIGKILL -PGID` on context cancel. A deferred `killProcessGroup` after `Wait` reaps orphan grandchildren. `cmd.WaitDelay = 2s` ensures deterministic cleanup. |

## Error handling

- **Panic recovery** — the tool handler and every `Run` goroutine in `run.go` wrap their work in a `recover()` block that emits an error `Result`.
- **Timeouts** — the Go context is the single source of truth. Each backend's `Run` must honor `ctx.Done()`; `SubprocessRunner` converts `ctx.Err() != nil` into `TimedOut=true` and `BuildResult` maps that to `status: "timeout"` with a synthetic response message. `OpenAIHTTPBackend` maps `context.DeadlineExceeded` during both the `sem.Acquire` wait and the `http.Do` / body-read phases to `timeout`.
- **Process orphans** — atomic process-group kill on Unix (`kill -SIGKILL -PGID`) plus a deferred cleanup after `Wait` to catch grandchildren that escaped the parent PID. The Codex app-server additionally sets `PR_SET_PDEATHSIG=SIGKILL` on Linux so the kernel cleans it up if `roundtable` exits abnormally. Windows uses `cmd.Process.Kill()`.
- **Output limits** — stdout capped at 1 MB, stderr at 512 KB, probe output at 64 KB for subprocess backends. HTTP response bodies are capped at 8 MiB via `io.LimitReader`. `LimitedWriter` reports full consumption to avoid broken-pipe errors in the child, then sets a `truncated` flag that propagates to the `Result`.
- **Graceful shutdown** — `main.go` installs a `signal.NotifyContext` on SIGINT / SIGTERM. On signal, `stdiomcp.Serve` returns, `stopBackends` releases the Codex app-server, and the process exits cleanly. Client disconnect (stdin EOF) reaches the same shutdown path.

## Development

### Prerequisites

- Go 1.26+ (via `mise`).
- `gemini`, `codex`, and `claude` CLIs on PATH (or configured via env vars).

### Build

```bash
make build
```

Manually:

```bash
mise exec go@1.26.2 -- env GOTOOLCHAIN=local GOMODCACHE=/tmp/gomodcache GOCACHE=/tmp/gocache \
  go build -ldflags "-s -w -X main.version=$(git describe --tags --abbrev=0)" \
  -o roundtable ./cmd/roundtable
```

### Test

```bash
make test
```

Go test layout:

|File|Coverage|
|-|-|
|`internal/stdiomcp/server_test.go`|Tool registration, handler dispatch wiring, panic recovery.|
|`internal/stdiomcp/server_schema_test.go`|F04 schema parameter integration: all five tools accept optional `schema`, malformed-schema fast-fail (`IsError: true`, `callCount == 0`), absent/null/whitespace detection, omitted-schema byte-equivalence regression.|
|`internal/stdiomcp/discipline_test.go`|Structured-output discipline.|
|`internal/stdiomcp/e2e_test.go`|End-to-end stdio MCP flow with mock dispatch.|
|`internal/roundtable/run_test.go`|`Run()` with mock backends, agent resolution, per-agent roles, probe failure.|
|`internal/roundtable/run_schema_test.go`|F04 Schema threading inside `Run`: suffix append with `\n\n` separator, per-panelist Validate gate (`req.Schema != nil && Status == "ok"`), non-ok statuses skip validation, omitted-schema byte-equivalence + no-`structured`-keys regression, race-detector-clean concurrent shared `*Schema`.|
|`internal/roundtable/runner_test.go`|Fake CLI scripts, timeout kill, truncation.|
|`internal/roundtable/gemini_test.go`|Arg ordering, JSON + stderr fallback, rate-limit detection.|
|`internal/roundtable/claude_test.go`|Arg ordering, ANSI stripping, `is_error`.|
|`internal/roundtable/codex_rpc_test.go`|Fake app-server pipe, handshake, notification routing, interrupt, lazy-start semantics.|
|`internal/roundtable/codex_rpc_orphan_linux_test.go`|Linux `PR_SET_PDEATHSIG` orphan-cleanup behavior.|
|`internal/roundtable/codex_fallback_test.go`|JSONL event parsing, resume modes.|
|`internal/roundtable/openai_http_test.go`|HTTP provider dispatch, semaphore gate, 429/503 mapping, string/array content parsing.|
|`internal/roundtable/providers_test.go`|`LoadProviderRegistry` parsing, `base_url` validation, per-entry skip semantics.|
|`internal/roundtable/files_test.go`|File inlining, size caps, `<file>` boundary formatting.|
|`internal/roundtable/prompt_test.go`, `roles_test.go`, `output_test.go`, `result_test.go`, `domain_test.go`|Pure-function unit tests.|
|`internal/roundtable/mock_backend_test.go`|Shared `mockBackend` test double used by `run_test.go`.|

### Run locally

```bash
./roundtable stdio
```

Or via `make run` (builds + runs). The binary reads MCP frames from stdin and writes responses to stdout; stderr is reserved for structured logs.

Register with Claude Code:

```bash
claude mcp add -s user roundtable -- ~/.local/share/roundtable/roundtable stdio
```

See `INSTALL.md` for the full one-line install + registration flow.
