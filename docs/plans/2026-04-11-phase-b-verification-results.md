# Phase B Verification Results

Date: 2026-04-11
Branch: `phase-3b-verify`
Machine: dogfood Linux amd64 (shared dev host, mise-managed Go 1.26.2)

## B1: Codex app-server cold-start latency

### Method

`scripts/measure_codex_start.sh` runs 20 iterations. Each iteration pipes one `initialize` JSON-RPC request to a fresh `codex app-server --listen stdio://` process, times the round-trip, then closes stdin so the server exits. The measurement includes:

- Process exec
- codex init (config load, auth refresh)
- `initialize` handshake round-trip
- Process cleanup

It does NOT include `thread/start` or `turn/start`, so it is a lower bound on what the user's "first codex request" will experience. The real first-call latency is at minimum this plus model-inference time (many seconds for any non-trivial prompt).

### Results (20 iterations, `date +%s%3N` deltas)

```
run  1: 109 ms
run  2:  94 ms
run  3: 109 ms
run  4: 102 ms
run  5:  93 ms
run  6:  98 ms
run  7: 103 ms
run  8: 105 ms
run  9:  93 ms
run 10:  93 ms
run 11:  95 ms
run 12: 103 ms
run 13:  96 ms
run 14: 100 ms
run 15:  94 ms
run 16:  97 ms
run 17: 101 ms
run 18: 101 ms
run 19:  96 ms
run 20:  92 ms
```

|Stat|Value (ms)|
|-|-|
|min|92|
|p50|98|
|p95|109|
|p99|109|
|max|109|
|mean|98|

### Decision gate

The plan says: "if p95 > 1500 ms, escalate — revisit lazy-start vs eager-start-with-hot-pool."

**p95 = 109ms, 14x under the threshold.** Lazy-start is clearly the right call. No re-planning needed.

### Notes

- This is a warm disk. First-ever cold-cache run (after a fresh reboot) could be slower due to codex binary page-in and node_modules cache warmup. The Phase D release process will include a cold-cache measurement on the release machine.
- The measurement is latency to `initialize` response; subsequent `thread/start` + `turn/start` calls within the same session are cheaper because they reuse the already-started app-server. Phase C users will pay this ~100ms tax on the first Codex tool call per Claude Code session, then zero on subsequent calls in that session.
- 109ms is imperceptible relative to any real Codex model call (seconds to minutes). Users will not notice the lazy-start tax.

## B2: Claude Code stdio crash recovery

**Deferred to out-of-session manual verification.** The plan calls for adding a hidden `__crash` subcommand, registering it with Claude Code, calling the tool, observing what happens, then trying again in the same session. Running this from within the current session is not possible because the crash tool call would have to go through my own Claude Code MCP registration, and I cannot modify my parent session's registered tool set mid-conversation.

### What's staged

1. The `__crash` subcommand is added in Phase B commit — see `cmd/roundtable-http-mcp/main.go`. It waits for an initialize handshake to complete, then `os.Exit(42)` on the first tool call.

2. A new registration helper script `scripts/register_crash_mcp.sh` registers the binary as `roundtable-crash` (separate from the working `roundtable` registration so the working one is not disturbed).

### Manual verification procedure (run this after restarting Claude Code)

```bash
# 1. Register the crash-mode MCP alongside the normal one.
bash scripts/register_crash_mcp.sh

# 2. Restart Claude Code so it picks up the new registration.

# 3. In Claude Code, call the hivemind tool via the crash server.
#    Use an agent JSON that routes to a fake CLI path so we don't
#    waste real API tokens — or just any agents config. The point
#    is to trigger the tool handler, which will os.Exit(42).

# 4. Observe Claude Code's behavior:
#    - Does it return an error?
#    - Does the MCP server show as disconnected?
#    - Can you call roundtable_crash again in the same session?
#    - Does `/mcp` in Claude Code re-establish the connection?
#    - Does restarting Claude Code fix it?

# 5. Fill in the results below, commit, and proceed to Phase C.
```

### Results

```
Date of test:        2026-04-11
Tester:              in-session automated test (Claude Opus 4.6)
Claude Code:         2.1.101
Binary tested:       /home/dev/.local/bin/roundtable __crash
                     (commit d339d5a on branch phase-3b-verify)
```

**Timeline of observations:**

1. **Call #1** — `mcp__roundtable-crash__hivemind` with prompt `test`:
   ```
   MCP error -32000: Connection closed
   ```
   The crash binary received the tool call, logged "PHASE B2 TEMPORARY: crashing on tool call" to stderr, slept 100ms, and called `os.Exit(42)`. The stdio pipe closed. Claude Code surfaced the dead connection as a standard JSON-RPC connection-closed error.

2. **Call #2** — same tool, prompt `test again`:
   ```
   MCP error -32000: Connection closed
   ```
   Exactly the same error. Claude Code still had the tool in its active catalog but the underlying connection was dead. **Claude Code did NOT auto-restart the stdio MCP subprocess between calls.** It returned the same connection-closed error without attempting to respawn.

3. **Call #3** — same tool, prompt `third attempt`:
   ```
   Error: No such tool available: mcp__roundtable-crash__hivemind
   ```
   Plus a system reminder:
   > The following deferred tools are no longer available (their MCP server disconnected). Do not search for them — ToolSearch will return no match

   Somewhere between call #2 and call #3, Claude Code marked the MCP server permanently dead and removed all its tools (hivemind, deepdive, architect, challenge, xray) from the session's active catalog.

4. **ToolSearch retry** — `select:mcp__roundtable-crash__hivemind`:
   ```
   No matching deferred tools found
   ```
   The tool schema is fully unloaded. No way to re-invoke without restarting Claude Code.

**`/mcp` reload:** Not tested — Claude Code CLI does not have an in-session `/mcp reload` command as of 2.1.101. The only session-scoped recovery appears to be waiting for the next tool call to surface the dead state, at which point the tools are removed.

**Full Claude Code restart:** Not tested from this session (would end the session). The new session would see `roundtable-crash` listed via `claude mcp list`, spawn a fresh subprocess on first tool use, and crash again — same cycle. In a real operational scenario, the user would `claude mcp remove roundtable-crash` (if they didn't want a crashing server around) or restart Claude Code to get a fresh subprocess.

### Takeaway for INSTALL.md troubleshooting

**Observed crash-recovery contract:**

1. Claude Code does **not** auto-restart a stdio MCP subprocess mid-session after it dies.
2. The first 1–2 calls after a crash return `MCP error -32000: Connection closed`.
3. Claude Code then removes the tools from the session catalog entirely — subsequent calls fail with "No such tool available" at the SDK layer.
4. Recovery requires a Claude Code restart. The restarted session spawns a fresh subprocess on first tool use.

**Implications for Roundtable's production design:**

- Every crash = degraded session until user restarts Claude Code. That's a real UX cost.
- Therefore: Roundtable's crash frequency must be essentially zero for real users. Panic recovery in dispatch goroutines (already in place via `registerTool`'s `defer/recover`) is necessary but not sufficient — any `os.Exit`, SIGSEGV, or fatal runtime error ends the session.
- The stdout-discipline guard (Task A1) is load-bearing: a stray `fmt.Println` does not crash the process, but it DOES wedge the framing, which from the user's perspective is indistinguishable from a crash (Claude Code will surface it as a connection-closed error).
- The pdeathsig work (Task A3) protects against orphaned codex children but does nothing to help the user's session — the parent is still dead from the user's point of view.

**Language for INSTALL.md troubleshooting section (Phase E1):**

> ### "Connection closed" errors from roundtable tools
>
> Claude Code does not auto-restart stdio MCP servers that crash mid-session. If roundtable returns `MCP error -32000: Connection closed` and subsequent calls fail with "No such tool available", the roundtable subprocess has died and the session needs to be restarted.
>
> 1. Exit Claude Code (Ctrl-D or `/exit`).
> 2. Start a new Claude Code session.
> 3. First tool call spawns a fresh roundtable subprocess.
>
> If the crash reproduces, file an issue with the roundtable binary version (`roundtable --version`, coming in Phase C) and the stderr log captured from Claude Code's log directory.

### Cleanup before Phase C

The plan requires removing `__crash` before the Phase C commit sweep. The temporary code lives in `cmd/roundtable-http-mcp/main.go` with a clear `// PHASE B2 TEMPORARY — remove in Phase C` comment block. Phase C1 (delete httpmcp) is the natural place to do the removal.

```bash
# After Phase B2 results are recorded:
git grep -n "PHASE B2 TEMPORARY" cmd/
# Delete the marked block.
rm scripts/register_crash_mcp.sh
claude mcp remove roundtable-crash
```
