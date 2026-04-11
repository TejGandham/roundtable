# Phase A Dogfood Notes

Running log of real Claude Code sessions using the stdio-backed roundtable MCP installed at `~/.local/bin/roundtable`. Drop a one-line entry after any notable session. Format:

```
YYYY-MM-DD HH:MM — <tool> on <topic>, <agents>, <wall-time>, <outcome>
```

Watch for the five Phase A regression signals:
1. `MCP error -32000: Connection closed` (crash — unrecoverable in-session per B2)
2. Tool call hangs past its own timeout (stdout framing wedged)
3. Leaked codex process after session end (`pgrep -af "codex app-server"`)
4. First codex call latency noticeably > 2s (lazy-start cost model broken)
5. Incorrect role/model routing (agents JSON passthrough broken)

Exit criteria for "Phase A ready for Phase C": one full week of normal usage with zero regression-signal entries.

---

## Entries

2026-04-11 01:50 — hivemind/architect on Phase 3 packaging redesign review (codex+claude), ~2–4min per call, no errors, codex first-call felt instant (lazy start worked as B1 predicted), all responses parsed cleanly
2026-04-11 02:00 — B2 crash test on roundtable-crash (NOT roundtable), intentional os.Exit(42), confirmed Claude Code marks the server dead and unloads tools after 2 retries. Normal `roundtable` MCP unaffected.
