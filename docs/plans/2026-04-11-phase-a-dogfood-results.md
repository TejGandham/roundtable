# Phase A Dogfood Results

Date: 2026-04-11
Branch: `phase-3a-stdio`
Tester: in-session automated smoke

## Install

```
make build
cp roundtable-http-mcp ~/.local/bin/roundtable
```

## Registration

Key lesson for INSTALL.md: variadic `-e` flags must come **before** `-s user`, otherwise commander.js consumes the server name as another env var value. The working form:

```
claude mcp add \
  -e ROUNDTABLE_GEMINI_PATH=/home/dev/.nvm/versions/node/v24.14.1/bin/gemini \
  -e ROUNDTABLE_CODEX_PATH=/home/dev/.nvm/versions/node/v24.14.1/bin/codex \
  -e ROUNDTABLE_CLAUDE_PATH=/home/dev/.local/bin/claude \
  -s user \
  roundtable \
  -- /home/dev/.local/bin/roundtable stdio
```

The broken form (which Phase 3 install.sh must avoid):

```
# WRONG — parser treats "roundtable" as an env var value
claude mcp add -s user -e A=1 -e B=2 -e C=3 roundtable -- ...
```

`claude mcp list` output after registration:
```
roundtable: /home/dev/.local/bin/roundtable stdio - ✓ Connected
```

## Startup latency

Measured via a Go smoke program that spawns `roundtable stdio` via `mcp.CommandTransport`, calls `initialize` + `ListTools`, and prints wall-clock durations.

5 consecutive runs:

|Run|spawn→initialize (ms)|initialize→tools/list (ms)|total (ms)|
|-|-|-|-|
|1|4|0|5|
|2|4|0|4|
|3|5|0|5|
|4|5|0|6|
|5|5|0|5|

**p50 = 5ms, p99 = 6ms.** Way below the 1500ms threshold documented in the plan's Phase B1 risk gate. This is the no-codex path — initialize + tools/list does not trigger `ensureStarted`, so the app-server never spawns.

## Lazy-start verification

After 5 back-to-back stdio sessions (each does initialize + tools/list, then exits):

```
$ pgrep -f "codex app-server"
(empty — no real matches; the one pgrep returned was self-matching
from its own command line)
```

Zero codex app-server processes spawned for tool-list-only sessions. `ensureStarted` correctly defers the launch until the first `Run()` call.

## Orphan prevention

Covered by `internal/roundtable/codex_rpc_orphan_linux_test.go` added in Task A3. Test passed 5/5 times in the stress run. Manual kill -9 during a live session still requires Claude Code restart to retest end-to-end; deferred to Phase B2.

## Issues found during dogfood

1. **`claude mcp add` flag ordering gotcha** — documented above, must be reflected in `install.sh` generation of the registration line (Task D3) and in `INSTALL.md` (Task E1).
2. None of the more serious issues we'd been worried about (stdout pollution, framing corruption, codex cold-start) surfaced.

## Phase A exit criteria — met

- [x] `roundtable stdio` subcommand exists and serves MCP over stdio
- [x] Full test suite green (`make test` / `go vet` clean)
- [x] stdio smoke test (spawn + initialize + list) < 10ms on this machine
- [x] Lazy codex start confirmed
- [x] Codex orphan test passes 5/5 on Linux
- [x] HTTP path still works for any pre-existing registrations
- [x] Registered with Claude Code via stdio, `claude mcp list` shows Connected

## Ready for Phase B

Phase B is about verifying two assumptions with empirical data:
- **B1**: Codex app-server cold-start p50/p95 (requires a real codex tool call, measurable from inside this session if we issue one before the branch lands)
- **B2**: Claude Code auto-recovery when a stdio MCP crashes mid-session

B1 can be done right after this commit (one real codex call via the registered stdio MCP). B2 needs a deliberate `os.Exit(1)` in a tool handler, which the plan covers with a `__crash` hidden subcommand.
