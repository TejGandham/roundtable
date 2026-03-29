# Elixir Port — Design Notes

**Date:** 2026-03-28
**Status:** Complete — implemented as Elixir escript alongside Node.js version

## Why Elixir

Node.js roundtable required 4 self-review cycles to stabilize process management. Each cycle found new issues (orphans, zombie probes, signal propagation, SIGKILL escalation, process group kills). These are all solved at the runtime level by Erlang/OTP's BEAM VM.

### What Elixir gives us for free

| Concern | Node.js (manual) | Elixir/OTP (built-in) |
|-|-|-|
| Process tree kill | `detached: true` + `process.kill(-pid)` + SIGKILL fallback | Supervisor kills entire subtree automatically |
| Orphan prevention | `activeChildren` Set + SIGINT handler | Supervisor owns children, cleans up on shutdown |
| Timeout with cleanup | `setTimeout` + kill + SIGKILL escalation | `Task.async` with `:timeout` — automatic cleanup |
| Health probe | Spawn `--version`, parse exit code, SIGKILL fallback | GenServer health check, supervisor restart strategy |
| Parallel execution | `Promise.allSettled` | `Task.async_stream` |
| Signal handling | `process.on('SIGINT')` | OTP application shutdown |
| Recursion guard | `ROUNDTABLE_ACTIVE` env var | Process registry prevents duplicate names |
| Error isolation | One uncaught error kills everything | Process crash doesn't affect siblings |
| Crash recovery | None — manual restart | Supervisor restart with configurable strategy |

### Findings from 5 self-review cycles (Node.js)

Issues that would not exist in Elixir:

1. **v1:** Codex recursion — spawned roundtable inside itself, hit stdin closure. Elixir: process registry prevents duplicate spawns.
2. **v2:** Stall detection false-killing legitimate work. Elixir: Task supervision with clean timeout, no manual stall heuristics needed.
3. **v3:** `proc.kill()` only kills top PID, not child tools. Elixir: supervisor kills entire process tree.
4. **v4:** Probe zombie processes (SIGTERM without SIGKILL fallback). Elixir: probe is a supervised Task — auto-cleaned.
5. **v5:** SIGKILL fallback timer not cancelled after graceful shutdown — recycled PID risk. Elixir: no manual timers for cleanup.
6. **v5:** Signal termination masking (`exit_code === null`). Elixir: process exit signals are typed (:normal, :killed, {:exit, reason}).

### Issues that persist regardless of runtime

1. Gemini JSON preamble fragility — CLI output format is external
2. Codex JSONL event structure — external contract
3. Arg parsing design — CLI interface, not runtime
4. Output contract consistency — design decision
5. SKILL.md accuracy — documentation

## Architecture for Elixir Port

### Escript (recommended)

Compile to a single executable binary. Invoke as `./roundtable --prompt "..."` — same interface as Node.js version.

```
mix escript.build → ./roundtable
```

Requires Erlang/OTP on the machine (`brew install elixir`).

### Module structure

```
lib/
├── roundtable.ex              # CLI entry point (escript main)
├── roundtable/
│   ├── application.ex         # OTP Application + Supervisor
│   ├── dispatcher.ex          # Parallel CLI dispatch (Task.async_stream)
│   ├── cli/
│   │   ├── gemini.ex          # Gemini spawn + JSON parser
│   │   └── codex.ex           # Codex spawn + JSONL parser
│   ├── prompt/
│   │   ├── assembler.ex       # Role prompt + request + file refs
│   │   └── roles.ex           # Role file resolution (project → global)
│   └── output.ex              # Result builder + JSON output
```

### Supervision tree

```
Roundtable.Supervisor
├── Task.Supervisor (for CLI workers)
│   ├── Gemini worker (Task)
│   └── Codex worker (Task)
└── Roundtable.Dispatcher (GenServer — orchestrates dispatch + collects results)
```

Strategy: `:one_for_one` — if Gemini crashes, Codex keeps running.

### What carries over unchanged

- SKILL.md (Claude Code skill file)
- Role prompt files (`roles/*.txt`)
- Prompt assembly format (`=== REQUEST ===`, `=== FILES ===`)
- Output JSON contract (same structure)
- CLI flags (`--prompt`, `--role`, `--files`, `--timeout`, etc.)
- Gemini/Codex parser logic (ported from JS to Elixir)

### What changes

- `roundtable.mjs` → `./roundtable` (escript binary)
- All manual process management code → OTP supervision
- `activeChildren` Set → Supervisor child tracking
- `cleanupAndExit` → OTP application shutdown
- `ROUNDTABLE_ACTIVE` env var → Process.registered?() check
- `detached: true` + `process.kill(-pid)` → Supervisor.stop()

### Dependencies

- Erlang/OTP (runtime)
- Elixir (build-time, for escript compilation)
- `jason` (JSON parsing — Elixir standard)
- No other deps

### Migration path

1. Build Elixir version alongside Node.js in anvil monorepo
2. Verify output contract parity (same JSON for same inputs)
3. Update SKILL.md to point to `./roundtable` instead of `node roundtable.mjs`
4. Retire `roundtable.mjs`

## Timing

Port happens after:
1. Node.js v6 is stable and reviewed ✅
2. Roundtable moves to anvil monorepo
3. Elixir toolchain is available on target machines
