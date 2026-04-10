# Roundtable Architecture

Multi-model consensus MCP server. Dispatches prompts to Claude, Gemini, and Codex CLIs in parallel, returns structured JSON with all responses and metadata. Supports selective agent dispatch — invoke any subset of CLIs, run the same CLI with different models, and assign per-agent roles.

## Architecture

Go HTTP MCP server as the public control plane. Delegates to an Elixir escript backend (`roundtable-cli`) for prompt assembly, role loading, agent selection, CLI spawning, and output JSON.

```
Claude Code ──HTTP──> roundtable-http-mcp ──subprocess──> roundtable-cli ──parallel──> claude CLI
                                                                                       gemini CLI
                                                                                       codex CLI
```

### Why HTTP over stdio

The previous Elixir/OTP stdio MCP server had a failure mode where the BEAM process stayed alive but the transport stopped making forward progress. Claude Code would wait indefinitely. The Go HTTP server eliminates this:

- If the Go process dies, the HTTP connection fails immediately.
- If the Elixir backend hangs, Go kills the subprocess at the configured deadline and returns an MCP error.
- If the backend exits non-zero with valid JSON, Go forwards a meaningful error instead of hanging.
- No long-lived backend process — each request gets a fresh subprocess.

### Components

| Component | Language | Responsibility |
|-|-|-|
| `roundtable-http-mcp` | Go | MCP transport, HTTP server, health endpoints, metrics, timeout enforcement, process lifecycle |
| `roundtable-cli` | Elixir | Prompt assembly, role loading, agent selection, CLI spawning, output parsing, JSON contract |

### Health and observability

| Endpoint | Purpose |
|-|-|
| `/healthz` | Process alive, server loop responsive |
| `/readyz` | Backend binary resolves, lightweight probe succeeds |
| `/metricsz` | JSON counters: `total_requests`, `backend_timeouts`, `backend_non_zero_exit`, `backend_parse_errors` |

### Backend timeout model

Go request timeout = tool timeout + request grace (default 15s). The backend gets a small margin to emit JSON and exit cleanly. Go remains the final authority and kills the subprocess if needed. Subprocess `WaitDelay` ensures deterministic cleanup of child processes.

### CLI path resolution (Elixir backend)

The Elixir backend resolves CLI paths at runtime:
1. `ROUNDTABLE_<NAME>_PATH` env var (e.g. `ROUNDTABLE_CLAUDE_PATH=/usr/local/bin/claude`)
2. `ROUNDTABLE_EXTRA_PATH` directories (colon-separated, searched before system PATH)
3. System PATH lookup

### Go backend path resolution

The Go server resolves the `roundtable-cli` backend:
1. `ROUNDTABLE_HTTP_BACKEND_PATH` env var
2. `./roundtable-cli` (sibling binary)
3. `release/roundtable` (release directory)
4. PATH lookup for `roundtable-cli` or `roundtable`

### Cross-platform support

| Platform | Shell | Process Cleanup | Orphan Strategy |
|-|-|-|-|
| Linux | `/bin/sh` | `trap 'kill 0' EXIT` + PGID kill | Atomic `kill -KILL -$PGID` |
| macOS | `/bin/sh` | `trap 'kill 0' EXIT` + PGID kill | Atomic `kill -KILL -$PGID` |
| Windows | `cmd.exe` | `taskkill /F /T` | Tree kill via PID |

## MCP Tools

| Tool | Role | Use Case |
|-|-|-|
| `hivemind` | default | General multi-model consensus |
| `deepdive` | planner | Extended reasoning / deep analysis |
| `architect` | planner | Implementation planning |
| `challenge` | codereviewer | Devil's advocate / stress-test |
| `xray` | gemini=planner, codex=codereviewer | Architecture + code quality review |

All tools accept an optional `agents` parameter (JSON array) for selective dispatch — choose which CLIs to invoke, run the same CLI with different models, and assign per-agent roles. When omitted, all 3 CLIs dispatch as default. See [SKILL.md](../SKILL.md) for full parameter docs.

## Configuration

| Env Var | Default | Purpose |
|-|-|-|
| `ROUNDTABLE_HTTP_ADDR` | `127.0.0.1:4040` | Listen address |
| `ROUNDTABLE_HTTP_MCP_PATH` | `/mcp` | MCP endpoint path |
| `ROUNDTABLE_HTTP_BACKEND_PATH` | (auto-resolve) | Backend binary path |
| `ROUNDTABLE_HTTP_PROBE_TIMEOUT` | `2s` | Readiness probe timeout |
| `ROUNDTABLE_HTTP_REQUEST_GRACE` | `15s` | Extra time beyond tool timeout for backend cleanup |
| `ROUNDTABLE_HTTP_ROLES_DIR` | (none) | Override global roles directory |
| `ROUNDTABLE_HTTP_PROJECT_ROLES_DIR` | (none) | Project-local roles directory |
| `ROUNDTABLE_DEFAULT_AGENTS` | (all 3 CLIs) | Default agent dispatch configuration |

## Development

### Prerequisites

- Go 1.26+ (via `mise`)
- Erlang/OTP 28+ and Elixir 1.19+ (for the backend)

### Build

```bash
make build
```

Or manually:

```bash
mise exec go@1.26.2 -- env GOTOOLCHAIN=local GOMODCACHE=/tmp/gomodcache GOCACHE=/tmp/gocache \
  go build -o roundtable-http-mcp ./cmd/roundtable-http-mcp
mise exec -- mix escript.build
```

### Test

```bash
make test
```

Tests cover:
- Unit: argument mapping, backend path resolution, timeout budget, response parsing
- In-memory MCP: tool list, tool calls, backend failure propagation
- End-to-end HTTP: health endpoints, MCP tool calls over real HTTP, timeout and crash error handling, metrics

### Run locally

```bash
ROUNDTABLE_HTTP_BACKEND_PATH=./roundtable-cli ./roundtable-http-mcp
```

Register with Claude Code:

```bash
claude mcp add --transport http roundtable http://127.0.0.1:4040/mcp
```

## Legacy stdio path

The Elixir/OTP stdio MCP server (`roundtable_mcp`) is still present in the repository but is no longer the recommended transport. See the Go HTTP migration plan for context: [docs/go-http-migration-plan.md](go-http-migration-plan.md).

## Design

See [DESIGN.md](../DESIGN.md) for original design document (historical).
