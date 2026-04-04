# Roundtable Architecture

Multi-model consensus MCP server. Dispatches prompts to Claude, Gemini, and Codex CLIs in parallel, returns structured JSON with all responses and metadata. Supports selective agent dispatch — invoke any subset of CLIs, run the same CLI with different models, and assign per-agent roles.

## Architecture

Elixir/OTP release running as an MCP server over stdio. Spawns CLI subprocesses via `Port.open` with platform-specific shell wrappers and process group isolation.

```
Claude Code ──stdio──> Roundtable MCP ──parallel──> claude CLI
                                                   gemini CLI
                                                   codex CLI
```

### Cross-Platform Support

| Platform | Shell | Process Cleanup | Orphan Strategy |
|-|-|-|-|
| Linux | `/bin/sh` | `trap 'kill 0' EXIT` + PGID kill | Atomic `kill -KILL -$PGID` |
| macOS | `/bin/sh` | `trap 'kill 0' EXIT` + PGID kill | Atomic `kill -KILL -$PGID` |
| Windows | `cmd.exe` | `taskkill /F /T` | Tree kill via PID |

### CLI Path Resolution

MCP servers inherit a minimal PATH. Resolution order:
1. `ROUNDTABLE_<NAME>_PATH` env var (e.g. `ROUNDTABLE_CLAUDE_PATH=/usr/local/bin/claude`)
2. `ROUNDTABLE_EXTRA_PATH` directories (colon-separated, searched before system PATH)
3. `System.find_executable/1` (system PATH)

## MCP Tools

| Tool | Role | Use Case |
|-|-|-|
| `hivemind` | default | General multi-model consensus |
| `deepdive` | planner | Extended reasoning / deep analysis |
| `architect` | planner | Implementation planning |
| `challenge` | codereviewer | Devil's advocate / stress-test |
| `xray` | gemini=planner, codex=codereviewer | Architecture + code quality review |

All tools accept an optional `agents` parameter (JSON array) for selective dispatch — choose which CLIs to invoke, run the same CLI with different models, and assign per-agent roles. When omitted, all 3 CLIs dispatch as default. See [SKILL.md](../SKILL.md) for full parameter docs.

## Development

```bash
mix deps.get
mix test
```

### Build Release

```bash
MIX_ENV=prod mix release roundtable_mcp
```

Release output: `_build/prod/rel/roundtable_mcp/`

### Run in Dev

```bash
ROUNDTABLE_MCP=1 mix run --no-halt
```

## Design

See [DESIGN.md](../DESIGN.md) for original design document (historical).
