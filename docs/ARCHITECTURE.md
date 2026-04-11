# Roundtable Architecture

Roundtable is a single static Go binary (`roundtable-http-mcp`) that serves MCP over HTTP and dispatches prompts to Claude, Gemini, and Codex CLIs in parallel, returning structured JSON with all responses and metadata. It supports selective agent dispatch — invoke any subset of CLIs, run the same CLI with different models, and assign per-agent roles.

## Overview

Roundtable is a single Go binary (`roundtable-http-mcp`) that:

1. Serves MCP over HTTP on `127.0.0.1:4040/mcp` (stateless streamable HTTP transport).
2. Exposes five tools: `hivemind`, `deepdive`, `architect`, `challenge`, `xray`.
3. On each tool call, assembles a role-scoped prompt and dispatches it to three model backends in parallel.
4. Normalizes each backend's output into a uniform JSON contract and returns the aggregate to the caller.

The Go binary is the only process Claude Code talks to. All dispatch, prompt assembly, role loading, output parsing, and JSON construction happen in Go. There is no Elixir or Erlang runtime involved.

## Architecture diagram

```
+----------------+            +----------------------------+
|  Claude Code   |  HTTP/MCP  |   roundtable-http-mcp      |
|  (MCP client)  +----------->+   (Go single binary)       |
+----------------+            |                            |
                              |  +----------------------+  |
                              |  | httpmcp (transport)  |  |
                              |  |  - tool registration |  |
                              |  |  - streamable HTTP   |  |
                              |  |  - health endpoints  |  |
                              |  |  - metrics counters  |  |
                              |  +----------+-----------+  |
                              |             |              |
                              |             v              |
                              |  +----------------------+  |
                              |  | roundtable (domain)  |  |
                              |  |  - Run() dispatcher  |  |
                              |  |  - prompt assembly   |  |
                              |  |  - role loader       |  |
                              |  |  - result normalize  |  |
                              |  +----------+-----------+  |
                              |             |              |
                              |    +--------+--------+     |
                              |    |        |        |     |
                              |    v        v        v     |
                              |  gemini   codex    claude  |
                              |  Backend  Backend  Backend |
                              +----|--------|--------|-----+
                                   |        |        |
                              subprocess  stdio    subprocess
                                  |    JSON-RPC      |
                                  v        |         v
                             +---------+   |    +---------+
                             | gemini  |   |    | claude  |
                             |   CLI   |   |    |   CLI   |
                             +---------+   |    +---------+
                                           v
                                     +------------+
                                     | codex      |
                                     | app-server |
                                     +------------+
```

Dashed line for Codex: the long-lived `codex app-server` process is launched once at startup and reused across all requests. Each request creates a fresh thread inside the app-server.

## Components

| Package / file | Responsibility |
|-|-|
| `cmd/roundtable-http-mcp/main.go` | Entry point. Builds backends, starts the Codex app-server, wires the dispatch function into httpmcp, runs HTTP server with signal-driven graceful shutdown. |
| `internal/httpmcp/server.go` | MCP tool registration, HTTP handler mux, streamable HTTP transport, progress notifications, request panic recovery, cached backend health probes. |
| `internal/httpmcp/backend.go` | `ToolInput` and `ToolSpec` types shared between the tool handler and the dispatch bridge. |
| `internal/httpmcp/config.go` | Environment variable loader. |
| `internal/httpmcp/metrics.go` | Atomic counters exposed via `/metricsz`. |
|`internal/roundtable/run.go`|`Run()` — the native dispatch entry point. Resolves agents, loads roles, assembles prompts, runs two-phase parallel probe/run, returns `DispatchResult` JSON. Also holds `ProbeTimeout` (5s) and `RunGrace` (30s) constants.|
|`internal/roundtable/backend.go`|`Backend` interface (`Name`, `Start`, `Stop`, `Healthy`, `Run`).|
| `internal/roundtable/runner.go` | `SubprocessRunner` with bounded `LimitedWriter` stdout/stderr capture, process group cleanup, `ROUNDTABLE_ACTIVE=1` env injection, `ROUNDTABLE_EXTRA_PATH` prepend. |
| `internal/roundtable/runner_unix.go` | Unix `Setpgid` + `kill -SIGKILL -PGID` atomic process group kill. |
| `internal/roundtable/runner_windows.go` | Windows process kill stub. |
| `internal/roundtable/gemini.go` | Gemini subprocess backend. Arg builder, stdout/stderr JSON parser, rate-limit detection, metadata extraction. |
| `internal/roundtable/claude.go` | Claude subprocess backend. Arg builder, JSON parser, ANSI stripping from `modelUsage` keys. |
| `internal/roundtable/codex_rpc.go` | **Primary Codex path.** Long-lived `codex app-server` process with JSON-RPC 2.0 over stdio. Handles initialize handshake, per-turn threads, notification routing, interrupt on cancel. |
| `internal/roundtable/codex_fallback.go` | Fallback Codex backend. Subprocess-per-request `codex exec --json` JSONL parser. Used when the app-server fails to start. |
| `internal/roundtable/prompt.go` | `AssemblePrompt` — joins role + `=== REQUEST ===` + `=== FILES ===` sections. `FormatFileReferences` stats each file. |
| `internal/roundtable/roles.go` | `LoadRolePrompt` with three-level fallback: project dir -> global dir -> `go:embed roles/*.txt` defaults. |
| `internal/roundtable/roles/` | Embedded defaults: `default.txt`, `planner.txt`, `codereviewer.txt`. |
| `internal/roundtable/request.go` | `Request` — transport-neutral input to a backend. |
| `internal/roundtable/result.go` | `Result`, `Meta`, `DispatchResult` types and JSON marshaling. `NotFoundResult`, `ProbeFailedResult` constructors. |
| `internal/roundtable/output.go` | `BuildResult` status normalization (timeout / terminated / error / ok) and `BuildMeta` aggregation. |

## Request flow

Step-by-step for a single `hivemind` tool call:

1. **HTTP request arrives** at `POST /mcp`. The `mcp.StreamableHTTPHandler` (from `modelcontextprotocol/go-sdk`) parses it as an MCP `CallToolRequest`.
2. **Tool handler fires** in `server.go::registerTool`. It increments `TotalRequests`, extracts the progress token, and launches a goroutine to do the actual work so the main goroutine can push 30-second progress notifications back to the client.
3. **DispatchFunc is invoked** (`buildDispatchFunc` in `main.go`). It parses the `agents` JSON, splits `files` on commas, resolves the timeout (default 900s), builds a `roundtable.ToolRequest`, and calls `roundtable.Run`.
4. **`Run` resolves agents** — explicit `Agents` > `ROUNDTABLE_DEFAULT_AGENTS` env > the default three (`gemini`, `codex`, `claude`).
5. **Per-agent config** — for each agent, `Run` resolves role (agent spec > per-CLI > tool default > `"default"`), model, and resume ID; loads the role prompt via `LoadRolePrompt`; calls `AssemblePrompt(rolePrompt, basePrompt+suffix, files)`; finds the matching backend by CLI name.
6. **Probe phase** — a goroutine per agent calls `backend.Healthy(ctx)` with a 5s timeout. Results are collected from a buffered channel into a fixed-index slice.
7. **Run phase** — for each agent that passed the probe, a goroutine calls `backend.Run(runCtx, req)` where `runCtx` has a `timeout + 30s grace` deadline. Panics are recovered into error `Result`s. Unhealthy agents get `ProbeFailedResult` or `NotFoundResult`.
8. **Result aggregation** — `Run` assembles a `map[string]*Result` keyed by agent name, calls `BuildMeta` to compute max elapsed, files referenced, and per-agent role names, then JSON-marshals a `DispatchResult`.
9. **Response** — the bytes are sent back through the done channel to the HTTP handler, wrapped in an `mcp.CallToolResult` with a single `TextContent`, and serialized to the HTTP client.

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

### CodexBackend (primary — long-lived app-server)

- `Start` launches `codex app-server --listen stdio://` once at server startup. Pipes stdin/stdout, spawns a `readLoop` goroutine, then sends an `initialize` request with `clientInfo` populated from `config.ServerName` and `config.ServerVersion`.
- `Healthy` checks that `cmd.Process != nil` and the `done` channel (closed by `readLoop` on EOF) is not closed.
- `Run` implements a three-step protocol per request:
  1. `thread/start` with empty params -> receives `{thread: {id}}`.
  2. `turn/start` with `{threadId, input: [{type: "text", text: prompt}], (model)?}`.
  3. Block on the per-thread notification channel until `turn/completed`, accumulating `item/completed` events where `item.type == "agentMessage"`.
- `Stop` grabs references under lock, releases the lock (to avoid deadlock with `readLoop`), closes stdin, kills the process, waits, and waits for `done`.
- On context cancel during a turn, fires a best-effort `turn/interrupt` with a fresh 5s context and returns a `timeout` result. The app-server stays alive for subsequent turns.
- `readLoop` reads newline-delimited JSON. Messages with an `id` field are responses (routed to `pending[id]`); messages with only `method` are notifications (routed to `notifs[threadId]` by parsing `params.threadId`).
- Concurrency: `pending` and `notifs` use separate mutexes. The top-level `mu` protects the stdin `Write` so concurrent turns can interleave messages safely.

### CodexFallbackBackend (subprocess-per-request JSONL)

Used only when `NewCodexBackend.Start` fails at startup (e.g., the codex binary version does not support `app-server`).

- `Healthy`: `codex --version` probe.
- `Run`: builds args `[exec --json --dangerously-bypass-approvals-and-sandbox (-c model=<m>)? (-c reasoning_effort=<r>)? <resume...> <prompt>]`. Resume modes: empty, `last` (`resume --last <prompt>`), or session id (`resume <id> <prompt>`).
- Parser walks stdout line by line, decoding each `{` JSON event:
  - `item.completed` with `item.type == "agent_message"` -> append text to messages.
  - `thread.started` -> capture `thread_id` as session ID.
  - `turn.completed` -> capture `usage` into metadata.
  - `error` -> append to errors.
- Success path joins messages with `\n\n`. Error-only path joins errors with `\n`. Empty output returns a synthetic error with `parse_error`.

## Codex app-server protocol

The app-server speaks JSON-RPC 2.0 over stdio. Every message is one JSON object terminated by `\n`. All methods return `-32600 "Not initialized"` until `initialize` completes.

```
roundtable-http-mcp          codex app-server
       |                            |
       |  -->  initialize           |
       |       { clientInfo: {...} }|
       |  <--  { result: {...} }    |
       |                            |
       |        (idle until a tool call arrives)
       |                            |
       |  -->  thread/start         |
       |       { }                  |
       |  <--  { result:            |
       |          { thread:         |
       |            { id: "thr_.." }|
       |          }                 |
       |        }                   |
       |                            |
       |  -->  turn/start           |
       |       { threadId: "thr_..",|
       |         input: [{type:     |
       |         "text", text: ...}]|
       |       }                    |
       |  <--  { result: {...} }    |
       |                            |
       |  <-- notify: item/completed (agentMessage) ...
       |  <-- notify: item/completed (reasoning, ignored)
       |  <-- notify: threadTokenUsage/updated (ignored)
       |  <-- notify: turn/completed { threadId }
       |                            |
       |     (on client cancel)     |
       |  -->  turn/interrupt       |
       |       { threadId }         |
```

The Go implementation only extracts text from `agentMessage` items; other item types (reasoning traces, tool calls) are silently dropped. Token usage is ignored in the current build — the hook exists in `collectTurn` but is a no-op.

## Configuration

All configuration is via environment variables.

### Server

|Variable|Default|Purpose|
|-|-|-|
|`ROUNDTABLE_HTTP_ADDR`|`127.0.0.1:4040`|Listen address.|
|`ROUNDTABLE_HTTP_MCP_PATH`|`/mcp`|MCP endpoint path.|
|`ROUNDTABLE_HTTP_SERVER_NAME`|`roundtable-http-mcp`|Reported MCP server name.|
|`ROUNDTABLE_HTTP_SERVER_VERSION`|from `config.defaultVersion`|Reported MCP server version.|
|`ROUNDTABLE_HTTP_PROBE_TIMEOUT`|`2s`|Duration for the readyz health probe (per backend).|

### Dispatch

|Variable|Default|Purpose|
|-|-|-|
|`ROUNDTABLE_HTTP_ROLES_DIR`|(embedded)|Global roles directory. Overrides embedded defaults.|
|`ROUNDTABLE_HTTP_PROJECT_ROLES_DIR`|(none)|Project-local roles directory, searched before global.|
|`ROUNDTABLE_DEFAULT_AGENTS`|(all three)|JSON-encoded array string of default agents when the tool call omits `agents`.|

### Executable resolution

For each CLI name (`gemini`, `codex`, `claude`), `ResolveExecutable` tries:

1. `ROUNDTABLE_<NAME>_PATH` — explicit override. Must exist on disk.
2. `ROUNDTABLE_EXTRA_PATH` — colon-separated directories, searched before system PATH.
3. `exec.LookPath(name)` — system PATH lookup.

Returns empty string if not found. The backend records `status: "not_found"` in that case.

The child process environment always includes `ROUNDTABLE_ACTIVE=1` (set by `subprocessEnv`) so the inner CLIs can detect they are running under Roundtable.

## Health and observability

| Endpoint | Behavior |
|-|-|
| `GET /` | Plain text banner with server name and MCP path. |
| `GET /healthz` | Always returns `200 ok` — liveness only. |
| `GET /readyz` | Returns `200 ready` if backends are healthy, `503 not ready: ...` otherwise. In native mode, calls `Healthy(ctx)` on each backend with a cached 10s TTL; stale entries trigger a reprobe on the request goroutine. |
| `GET /metricsz` | JSON counter snapshot. |

Metrics counters (atomic, never reset):

|Counter|Incremented when|
|-|-|
|`total_requests`|Any tool call arrives.|
|`dispatch_errors`|Dispatch returned an error or the tool handler panicked.|

## Concurrency model

| Component | Concurrency mechanism |
|-|-|
| HTTP server | `http.Server` with streamable HTTP transport. Each MCP call runs on its own goroutine. |
| Tool handler | Spawns a worker goroutine; main goroutine selects on `done`, `ctx.Done()`, and a 30s ticker for progress notifications. |
| Native `Run()` | Two sequential phases, each using goroutines + a buffered channel for fan-in. Probe phase collects into a fixed-index slice; run phase collects into a counted loop. |
| GeminiBackend / ClaudeBackend / CodexFallbackBackend | `sync.Mutex` protects the cached `execPath`. `Run` is otherwise stateless — each call spawns its own subprocess. |
| CodexBackend | Three mutexes: `mu` (subprocess handles + stdin writes), `pendingMu` (request id -> response channel), `notifMu` (thread id -> notification channel). A single `readLoop` goroutine fans incoming messages out to the right channel. `done` is closed on EOF so callers can unblock. |
| SubprocessRunner | Each process gets its own process group (`Setpgid=true`). `exec.CommandContext` + `cmd.Cancel` issue `kill -SIGKILL -PGID` on context cancel. A deferred `killProcessGroup` after `Wait` reaps orphan grandchildren. `cmd.WaitDelay = 2s` ensures deterministic cleanup. |

## Error handling

- **Panic recovery** — the tool handler and every `Run` goroutine in `run.go` wrap their work in a `recover()` block that emits an error `Result` or increments `dispatch_errors`.
- **Timeouts** — the Go context is the single source of truth. Each backend's `Run` must honor `ctx.Done()`; `SubprocessRunner` converts `ctx.Err() != nil` into `TimedOut=true` and `BuildResult` maps that to `status: "timeout"` with a synthetic response message.
- **Process orphans** — atomic process group kill on Unix (`kill -SIGKILL -PGID`), plus a deferred cleanup after `Wait` to catch grandchildren that escaped the parent PID. On Windows the runner uses `cmd.Process.Kill()`.
- **Output limits** — stdout capped at 1 MB, stderr at 512 KB, probe output at 64 KB. `LimitedWriter` reports full consumption to avoid broken-pipe errors in the child, then sets a `truncated` flag that propagates to the `Result`.
- **Graceful shutdown** — `main.go` installs a `signal.NotifyContext` on SIGINT/SIGTERM. On signal, it calls `server.Shutdown` with a 30s context, and `stopBackends` releases Codex app-server.

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
  go build -o roundtable-http-mcp ./cmd/roundtable-http-mcp
```

### Test

```bash
make test
```

Go test layout:

|File|Coverage|
|-|-|
|`internal/httpmcp/server_test.go`|Dispatch function wiring, readyz with mock probes, panic recovery, metrics, 404 handling.|
|`internal/roundtable/run_test.go`|`Run()` with mock backends, agent resolution, per-agent roles, probe failure.|
|`internal/roundtable/runner_test.go`|Fake CLI scripts, timeout kill, truncation.|
|`internal/roundtable/gemini_test.go`|Arg ordering, JSON + stderr fallback, rate-limit detection.|
|`internal/roundtable/claude_test.go`|Arg ordering, ANSI stripping, `is_error`.|
|`internal/roundtable/codex_rpc_test.go`|Fake app-server pipe, handshake, notification routing, interrupt.|
|`internal/roundtable/codex_fallback_test.go`|JSONL event parsing, resume modes.|
|`internal/roundtable/prompt_test.go`, `roles_test.go`, `output_test.go`, `result_test.go`, `domain_test.go`|Pure-function unit tests.|
|`internal/roundtable/mock_backend_test.go`|Shared `mockBackend` test double used by `run_test.go`.|

### Run locally

```bash
./roundtable-http-mcp
# or with explicit paths
ROUNDTABLE_GEMINI_PATH=/usr/local/bin/gemini \
ROUNDTABLE_CODEX_PATH=/usr/local/bin/codex \
ROUNDTABLE_CLAUDE_PATH=/usr/local/bin/claude \
  ./roundtable-http-mcp
```

Register with Claude Code:

```bash
claude mcp add --transport http roundtable http://127.0.0.1:4040/mcp
```

## History

The project originally shipped as an Elixir/OTP stdio MCP server and was migrated to pure Go in April 2026. See `git log` for the full migration commits if you need the archaeology.
