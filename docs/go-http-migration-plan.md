# Go HTTP Migration Plan

Date: 2026-04-09
Status: Approved direction, phase 1 in progress
Branch: `go-http-phase1`

## Goal

Replace the current long-lived Elixir/Hermes stdio MCP server with a more stable control plane that fails fast, exposes health checks, and can be supervised independently.

The migration explicitly separates two concerns:

1. Transport/control plane stability
2. Core roundtable dispatch logic

The first concern is urgent. The second can be ported after the new boundary is proven in real use.

## Why this migration exists

The current production issue is not primarily prompt assembly, CLI fan-out, or output parsing. It is the lifecycle of the MCP transport process.

Observed failure mode:

- The BEAM process remains alive.
- The MCP server or transport stops making forward progress.
- Claude Code assumes the server is still handling the request.
- The client waits indefinitely until the human kills the task.

The current Elixir implementation already contains defensive work:

- JSON-RPC error responses on transport failure paths
- watchdog liveness checks
- forced BEAM halt when the transport is gone or unresponsive

Those mitigations reduce some failure cases, but they still depend on a long-lived stdio MCP process and client-side detection behaving correctly.

## Decision

Use Go as the new public control plane and HTTP as the primary MCP transport.

Short-term architecture:

`Claude Code -> local HTTP MCP -> Go daemon -> roundtable-cli`

Long-term architecture:

`Claude Code -> local HTTP MCP -> Go daemon -> native Go roundtable core`

## Constraints

The migration must preserve:

- the current Roundtable tool set
- the current JSON result contract returned by the core backend
- the current role system
- selective agent dispatch
- resume/session parameters
- existing CLI-based backend behavior during phase 1

The migration must not:

- change Roundtable into a stateful tmux-first workflow
- keep Elixir and Go as permanent co-equal stacks
- rewrite transport and core in one step

## Lifecycle

### Phase 0: Baseline and branch setup

Objective:

- Freeze the migration plan and create an isolated implementation branch.

Deliverables:

- This plan document
- New branch for migration work

Exit criteria:

- Plan is explicit enough to guide implementation without reopening architecture debates every commit.

### Phase 1: Go HTTP MCP wrapper over existing Elixir CLI backend

Objective:

- Remove the Hermes stdio server from the critical path without rewriting core dispatch logic.

Architecture:

- Go binary starts a local HTTP server bound to `127.0.0.1`.
- Server exposes MCP tools matching the existing Roundtable tool surface.
- Each tool call is translated into a `roundtable-cli` invocation.
- The Go server parses the backend JSON and returns it to the MCP client as tool text content.
- Health endpoints expose process readiness and backend reachability.

Responsibilities in phase 1:

- Go owns:
  - MCP transport
  - process lifecycle
  - health/readiness endpoints
  - request validation at the MCP boundary
  - backend invocation and timeout enforcement
  - structured logging to stderr
- Elixir owns:
  - prompt assembly
  - agent selection and defaults
  - role loading
  - CLI probe/run logic
  - per-CLI parsing
  - output JSON contract

Artifacts:

- `go.mod`
- `cmd/roundtable-http-mcp/main.go`
- internal Go packages for:
  - MCP server/tool registration
  - backend command building
  - HTTP health handlers
  - request/response translation
- docs/install updates for the HTTP server path

Operational behavior:

- If the Go process dies, the HTTP connection fails immediately.
- If the Elixir backend hangs, Go kills the subprocess at the configured deadline and returns an MCP error.
- If the backend exits non-zero with valid JSON error output, Go forwards a meaningful tool error instead of hanging.
- No long-lived backend process exists in phase 1. Each request gets a fresh `roundtable-cli` subprocess.

Phase 1 tradeoff:

- Two runtimes exist temporarily.
- That is acceptable only because the boundary is narrow and one-way: Go calls the Elixir CLI as a backend process.

Exit criteria:

- Claude Code can use the HTTP MCP server locally.
- Tool parity exists for all Roundtable tools.
- A wedged backend subprocess cannot cause indefinite client waits.
- Health checks distinguish startup, ready, and degraded states.
- Real usage runs stably for a defined burn-in period.

### Phase 1.5: Burn-in and hardening

Objective:

- Prove the new transport boundary before porting the core.

Required burn-in window:

- 2 to 4 weeks of real interactive use

Metrics to track:

- total requests
- backend timeout count
- backend non-zero exit count
- backend JSON parse failure count
- MCP request failures by type
- server restarts
- requests abandoned due to transport issues

Non-negotiable success criteria:

- zero indefinite waits
- deterministic failure response on backend timeout
- deterministic failure response on backend crash
- no need for a watchdog equivalent to detect stuck stdio transports

If phase 1.5 fails:

- Fix the Go transport and wrapper before any core porting
- Do not start a native Go core while the wrapper layer is still ambiguous

### Phase 2: Port the core dispatch logic to Go

Objective:

- Remove Elixir from the runtime path once the new boundary is proven.

Port order:

1. Argument and request normalization
2. Role loading and prompt assembly
3. Agent selection and defaulting
4. CLI executable discovery
5. CLI probe logic
6. CLI subprocess execution and cleanup
7. Gemini parser
8. Codex parser
9. Claude parser
10. Output JSON encoding and metadata

Strategy:

- Port one subsystem at a time behind tests.
- Compare native Go output against `roundtable-cli` output for the same fixtures and fake CLIs.
- Keep the Go public API unchanged while swapping internals.

Deliverables:

- Native Go backend package replacing shell-out to `roundtable-cli`
- Compatibility tests that compare Go and Elixir outputs during transition

Exit criteria:

- Native Go backend matches current result contract on representative cases
- Existing fake CLI and E2E style scenarios pass against the Go core
- Elixir backend is no longer required for any supported path

### Phase 3: Remove Elixir MCP/runtime dependencies

Objective:

- Collapse to a single runtime and simplify releases.

Deliverables:

- removal of Hermes MCP dependency
- removal of Elixir release as the primary deployment artifact
- retained or replaced CLI backend only if still useful for scripting compatibility

Decision point:

- Either keep a compatibility `roundtable-cli` wrapper implemented in Go
- Or retire the Elixir CLI entirely and publish only the Go binary/binaries

Preferred end state:

- One primary runtime: Go
- One primary deployment mode: local HTTP MCP
- Optional compatibility CLI implemented in Go

## Why not tmux as the primary replacement

tmux is useful for persistent pair workflows, but it changes the product shape:

- Roundtable today is a request/response tool call.
- A tmux harness is a session orchestration system.
- It introduces persistent state, workspace routing, and UI/session supervision concerns.

That is a valid separate mode, but not the right replacement for the current product contract.

tmux can still inform the design:

- explicit health endpoints
- process reconnect handling
- session state on disk where needed
- separate bridge/control process from worker process

Those patterns are useful. The tmux-centered workflow itself is not the main migration target.

## Why the temporary dual-stack phase is acceptable

It is acceptable because it is intentionally narrow and temporary.

Allowed:

- Go as the only public entrypoint
- Elixir only as an internal backend subprocess
- explicit exit criteria for removing Elixir

Not allowed:

- shipping both Go and Elixir as equal public server implementations
- maintaining two parallel MCP servers indefinitely
- adding new features in both stacks during migration

Governance rule:

- New transport and integration work goes into Go.
- Only bug fixes go into Elixir during phase 1 and 1.5.
- New product behavior should wait for the native Go core unless absolutely required.

## Phase 1 implementation plan

### Scope

Implement a Go HTTP MCP server that:

- registers the same five tools:
  - `hivemind`
  - `deepdive`
  - `architect`
  - `challenge`
  - `xray`
- accepts the same argument surface currently exposed by the MCP tools
- maps those arguments to `roundtable-cli` flags
- executes `roundtable-cli` per request
- returns the backend JSON as tool text content
- exposes `/healthz` and `/readyz`

### Request mapping

Shared tool params to support:

- `prompt`
- `files`
- `timeout`
- `gemini_model`
- `codex_model`
- `claude_model`
- `gemini_resume`
- `codex_resume`
- `claude_resume`
- `agents`

Tool-specific role behavior:

- `hivemind`: `--role default`
- `deepdive`: `--role planner`
- `architect`: `--role planner`
- `challenge`: `--role codereviewer`
- `xray`: `--gemini-role planner --codex-role codereviewer`

`files` behavior:

- keep comma-separated string semantics for parity

`agents` behavior:

- pass the JSON string through untouched to `roundtable-cli --agents`

### Backend invocation

Command strategy:

- Prefer a configured backend path
- Fallback order:
  1. explicit env var path
  2. `./roundtable-cli`
  3. `release/roundtable`
  4. PATH lookup

Backend timeout model:

- Go request timeout must exceed backend tool timeout slightly
- Example:
  - tool timeout: N seconds
  - process deadline: N + 15 seconds

Reason:

- backend needs a small margin to emit JSON and exit cleanly
- Go still remains the final authority and kills the subprocess if needed

### Health model

`/healthz`:

- process is alive
- server loop is responsive

`/readyz`:

- backend binary path resolves
- optional lightweight backend probe succeeds

Readiness should not depend on running a full model dispatch.

Preferred readiness probe:

- execute backend with an intentionally invalid argument and require a fast JSON error response
- or run a lightweight help/usage path if one is available

### Logging

Rules:

- never write logs to HTTP response bodies except structured error messages
- keep request correlation IDs
- log backend launch, exit code, timeout, and parse failures
- default logs to stderr

### Tests for phase 1

Need both unit and end-to-end coverage.

Unit tests:

- tool-to-CLI argument mapping
- backend path resolution
- timeout budget calculation
- response parsing for:
  - valid backend success JSON
  - valid backend error JSON
  - invalid JSON
  - subprocess timeout
  - subprocess launch failure

HTTP/MCP tests:

- initialize succeeds
- tools/list exposes all five tools
- tool call success returns backend JSON text
- backend timeout returns MCP error, not hang
- backend crash returns MCP error, not hang

Integration tests:

- run Go server against fake backend command
- run Go server against real `roundtable-cli` with fake CLIs if toolchain is available

### Release and install changes

Phase 1 release output should include:

- Go HTTP MCP server binary
- existing `roundtable-cli`
- roles directory

Install path should move from:

- stdio MCP registration of `roundtable-mcp`

to:

- start local `roundtable-http-mcp`
- register Claude Code against the local HTTP MCP endpoint

## Current status

Current branch state:

- Branch: `go-http-phase1`
- Public control plane: Go HTTP MCP server
- Backend execution path: existing `roundtable-cli` escript
- Legacy Elixir stdio MCP server: still present in the repository, but no longer the recommended direction for transport work

Current architecture in this branch:

`Claude Code -> local HTTP MCP -> Go daemon -> roundtable-cli -> CLIs`

This is intentionally hybrid:

- Go owns the MCP transport and request lifecycle.
- Elixir still owns prompt assembly, role loading, agent selection, CLI spawning, parsing, and output JSON.

## Work completed

Implemented in phase 1:

- Go toolchain bootstrap in this repo via `mise`
- new Go entrypoint: `cmd/roundtable-http-mcp`
- new internal Go package: `internal/httpmcp`
- official Go SDK based stateless streamable HTTP MCP server
- all five Roundtable tools registered in the Go server:
  - `hivemind`
  - `deepdive`
  - `architect`
  - `challenge`
  - `xray`
- tool-to-CLI mapping that preserves current role behavior and prompt suffixes
- backend path resolution with fallback lookup
- per-request backend subprocess execution through `roundtable-cli`
- timeout enforcement in Go above the backend timeout budget
- subprocess `WaitDelay` for deterministic cleanup of child processes
- `/healthz` and `/readyz`
- `/metricsz` endpoint with JSON burn-in counters:
  - `total_requests`
  - `backend_timeouts`
  - `backend_non_zero_exit`
  - `backend_parse_errors`
- unit coverage for:
  - backend argument mapping
  - backend success/error handling
  - timeout behavior
  - readiness probe behavior
- in-memory MCP tests for:
  - `tools/list`
  - successful tool calls
  - backend failure propagation
- end-to-end HTTP listener tests for:
  - health and readiness endpoints over real HTTP
  - tool list via MCP StreamableHTTP client
  - successful tool call over HTTP
  - backend crash returns MCP error, not hang
  - backend timeout returns MCP error, not hang
  - metrics endpoint reflects request counts
  - unknown paths return 404
- Makefile for building Go + Elixir artifacts
- release packaging updated to ship both `roundtable-http-mcp` and `roundtable-cli`
- INSTALL.md updated with HTTP startup and Claude Code registration instructions
- RELEASING.md updated with Go build steps

## Work pending

Still pending before phase 2:

- complete burn-in period and confirm zero indefinite waits in real usage
- port the Roundtable core from Elixir to Go
- remove the old Elixir MCP release path after the Go-native backend is proven

## Current testing procedure

The current app has two relevant test surfaces:

1. Existing Elixir backend tests
2. New Go HTTP MCP wrapper tests

### Prerequisites

- Erlang/OTP 28
- Elixir 1.19 compatible with OTP 28
- Go 1.26.2

The repo currently uses `mise`:

```bash
mise install
```

For Go commands in this branch, prefer `mise exec`:

```bash
mise exec go@1.26.2 -- go version
```

### Test the existing Elixir backend

```bash
mix test
```

Focused suites:

```bash
mix test test/integration_test.exs
mix test test/roundtable/mcp/server_e2e_test.exs
mix test test/roundtable/mcp/stdio_error_response_test.exs
```

### Test the Go wrapper

```bash
make test-go
```

Or manually with writable Go caches in restricted environments:

```bash
mise exec go@1.26.2 -- env \
  GOTOOLCHAIN=local \
  GOMODCACHE=/tmp/gomodcache \
  GOCACHE=/tmp/gocache \
  go test ./...
```

This validates:

- `internal/httpmcp/backend_test.go` — argument mapping, backend execution, timeout, probe
- `internal/httpmcp/server_test.go` — health endpoints, MCP tool list/call, error propagation
- `internal/httpmcp/e2e_test.go` — real HTTP listener tests for all endpoints and failure modes

### Build and run

```bash
make build
```

Or manually:

```bash
mise exec -- mix escript.build
mise exec go@1.26.2 -- env \
  GOTOOLCHAIN=local \
  GOMODCACHE=/tmp/gomodcache \
  GOCACHE=/tmp/gocache \
  go build -o roundtable-http-mcp ./cmd/roundtable-http-mcp
```

Start the Go HTTP server against the local backend:

```bash
ROUNDTABLE_HTTP_BACKEND_PATH=./roundtable-cli \
./roundtable-http-mcp
```

Optional environment variables:

- `ROUNDTABLE_HTTP_ADDR` default: `127.0.0.1:4040`
- `ROUNDTABLE_HTTP_MCP_PATH` default: `/mcp`
- `ROUNDTABLE_HTTP_PROBE_TIMEOUT` default: `2s`
- `ROUNDTABLE_HTTP_REQUEST_GRACE` default: `15s`
- `ROUNDTABLE_HTTP_ROLES_DIR` optional roles override
- `ROUNDTABLE_HTTP_PROJECT_ROLES_DIR` optional project roles override

### Verify health, readiness, and metrics

With the server running:

```bash
curl -s http://127.0.0.1:4040/healthz
curl -s http://127.0.0.1:4040/readyz
curl -s http://127.0.0.1:4040/metricsz
```

Expected responses:

- `/healthz` -> `ok`
- `/readyz` -> `ready`
- `/metricsz` -> `{"total_requests":0,"backend_timeouts":0,"backend_non_zero_exit":0,"backend_parse_errors":0}`

### Register Claude Code against the HTTP server

```bash
claude mcp add --transport http roundtable http://127.0.0.1:4040/mcp
```

Then verify from Claude Code with `/mcp` or by calling one of the tools.

### Manual functional check

```text
Use roundtable_hivemind to ask: "What is the best way to handle errors in async Elixir code?"
```

Expected behavior in the current branch:

- Claude Code talks to the Go HTTP MCP server
- the Go server shells out to `roundtable-cli`
- `roundtable-cli` dispatches to the installed CLIs
- the JSON payload comes back as MCP tool text content

## Current recommendation

Recommended validation order:

1. `make test` — runs both Elixir and Go test suites
2. `make build` — produces `roundtable-cli` and `roundtable-http-mcp`
3. `ROUNDTABLE_HTTP_BACKEND_PATH=./roundtable-cli ./roundtable-http-mcp`
4. check `/healthz`, `/readyz`, and `/metricsz`
5. `claude mcp add --transport http roundtable http://127.0.0.1:4040/mcp`
6. run a manual tool call from Claude Code

## Risks

### Risk: temporary complexity

Reality:

- two runtimes mean more build steps and more artifacts

Mitigation:

- limit Elixir to backend subprocess usage only
- define sunset criteria now

### Risk: backend CLI startup cost on every request

Reality:

- phase 1 may be slower than a hot in-process runtime

Mitigation:

- stability has priority over startup cost
- evaluate request latency during burn-in
- optimize only after the transport problem is definitively solved

### Risk: HTTP MCP support differences across clients

Mitigation:

- target Claude Code first
- keep a clear local-only bind address
- avoid assuming remote hosting requirements in phase 1

### Risk: backend CLI path ambiguity in releases

Mitigation:

- ship explicit co-located artifacts
- make backend path configurable via env var
- log the resolved backend path at startup

## Non-goals for phase 1

- native Go implementation of prompt assembly
- native Go CLI parsers
- tmux workflow support
- persistent agent sessions
- changing the result contract
- changing the Roundtable tool UX

## Exit decision after phase 1.5

If the HTTP wrapper solves the indefinite-wait problem and burn-in is clean:

- proceed with native Go core port

If the HTTP wrapper still exhibits ambiguous hangs:

- do not port the core yet
- first fix the public transport boundary

## Summary

The migration is intentionally staged.

Phase 1 is not "bring in Go and Elixir forever."
Phase 1 is "use Go to replace the unstable server boundary while preserving the proven backend logic."

The target end state is a single Go stack.
The temporary dual-stack period exists only to reduce migration risk and isolate the actual source of the current instability.
