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

### Results — TO FILL IN

```
Date of test:
Claude Code version:

First call (triggers crash):

Second call in same session:

After running `/mcp` reload:

After full Claude Code restart:

Takeaway for INSTALL.md troubleshooting:
```

### Cleanup before Phase C

The plan requires removing `__crash` before the Phase C commit sweep. The temporary code lives in `cmd/roundtable-http-mcp/main.go` with a clear `// PHASE B2 TEMPORARY — remove in Phase C` comment block. Phase C1 (delete httpmcp) is the natural place to do the removal.

```bash
# After Phase B2 results are recorded:
git grep -n "PHASE B2 TEMPORARY" cmd/
# Delete the marked block.
rm scripts/register_crash_mcp.sh
claude mcp remove roundtable-crash
```
