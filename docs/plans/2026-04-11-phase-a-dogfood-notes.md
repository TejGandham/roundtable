# Phase A Dogfood Notes

Running log of real Claude Code sessions using the stdio-backed roundtable MCP installed at `~/.local/bin/roundtable`. Drop a one-line entry after any notable session. Format:

```
YYYY-MM-DD HH:MM — <tool> on <topic>, <agents>, <wall-time>, <outcome>
```

## Observability

Per-session stderr is captured at `~/.local/share/roundtable/logs/stdio-<timestamp>-<pid>.log` via `scripts/roundtable-stdio-wrapper.sh` (installed as `~/.local/bin/roundtable`, real binary at `~/.local/bin/roundtable-bin`). Each Claude Code session that spawns roundtable creates one file; concurrent sessions do not collide.

**Live tail the most recent session:**
```
tail -f ~/.local/share/roundtable/logs/$(ls -t ~/.local/share/roundtable/logs/ | head -1)
```

**Find errors across all sessions in the last hour:**
```
find ~/.local/share/roundtable/logs -mmin -60 -name '*.log' -exec grep -l ERROR {} \;
```

**Correlate a running stdio child with its log file:**
```
RT_PID=$(pgrep -f 'roundtable stdio' | head -1)
ls -t ~/.local/share/roundtable/logs/*-$RT_PID.log
```

Wrapper rationale: Option A (shell wrapper) with `"$@"` forwarding, chosen over Option B (in-process log-file flag) because slog-only tee would not capture panic stacks, Go runtime fatals, direct `fmt.Fprintf(os.Stderr, ...)`, or stderr from spawned Gemini/Claude/Codex subprocesses. The wrapper uses `exec` so there is no persistent shell in the process tree. Removed in Phase D when install.sh ships the single binary directly.

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
