# Installing Roundtable

Roundtable is an MCP server. Agents call its tools directly over stdio — no Bash tool needed, no output buffer limits.

## Prerequisites

- **[mise](https://mise.jdx.dev)** — manages Erlang 28 + Elixir 1.19 automatically
- **Gemini CLI** installed and authenticated (`gemini --version`)
- **Codex CLI** installed and authenticated (`codex --version`)
- **Claude CLI** installed and authenticated (`claude --version`)

The CLI tools must be on `PATH` for the roundtable server to dispatch to them.

---

## Install from Source

```bash
git clone https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable.git
cd roundtable
```

**Install the toolchain:**

```bash
curl https://mise.run | sh
mise install          # reads .mise.toml → installs Erlang 28 + Elixir 1.19
```

**Fetch deps** (automatically patches hermes_mcp for stdio transport fix):

```bash
eval "$(mise activate bash)"   # or add to ~/.bashrc
mix deps.get
```

**Verify:**

```bash
mix test
```

Expected: 167 tests, 0 failures.

---

## MCP Registration

Register roundtable as an MCP server so your agent can call its tools directly. The server spawns `claude`, `codex`, and `gemini` as child processes, so the registration command must ensure these CLIs are on `PATH`.

### Claude Code

```bash
claude mcp add -s user roundtable -- bash -c \
  'export PATH="$HOME/.local/bin:$(dirname $(readlink -f $(which node))):$PATH" && \
   eval "$(mise activate bash)" && \
   cd /path/to/roundtable && ROUNDTABLE_MCP=1 mix run --no-halt'
```

Replace `/path/to/roundtable` with the actual clone path.

Verify:

```bash
claude mcp list | grep roundtable
```

Restart Claude Code. These tools will be available:

| Tool | Purpose |
|-|-|
| `roundtable_hivemind` | Multi-model consensus (general) |
| `roundtable_deepdive` | Extended reasoning / deep analysis |
| `roundtable_architect` | Implementation planning |
| `roundtable_challenge` | Devil's advocate / stress-test ideas |
| `roundtable_xray` | Codebase architecture + code quality |

### Codex

Add to `~/.codex/config.toml`:

```toml
[mcp_servers.roundtable]
command = ["bash", "-c", "export PATH=\"$HOME/.local/bin:$(dirname $(readlink -f $(which node))):$PATH\" && eval \"$(mise activate bash)\" && cd /path/to/roundtable && ROUNDTABLE_MCP=1 mix run --no-halt"]
```

Replace `/path/to/roundtable` with the actual clone path. Restart Codex to pick it up.

### OpenCode

Add to `~/.config/opencode/config.json` (or workspace `.opencode/config.json`):

```json
{
  "mcp": {
    "roundtable": {
      "command": "bash",
      "args": ["-c", "export PATH=\"$HOME/.local/bin:$(dirname $(readlink -f $(which node))):$PATH\" && eval \"$(mise activate bash)\" && cd /path/to/roundtable && ROUNDTABLE_MCP=1 mix run --no-halt"]
    }
  }
}
```

### Other MCP Clients

Launch the server with:

```bash
cd /path/to/roundtable && ROUNDTABLE_MCP=1 mix run --no-halt
```

The server communicates over stdio using JSON-RPC (MCP protocol 2025-03-26). Ensure `claude`, `codex`, and `gemini` are on `PATH` in the server's environment.

---

## Skill Discovery (Optional)

Agents that support skill files can discover roundtable's documentation automatically. Copy `SKILL.md` to your agent's skill directory for skill-triggered invocation alongside MCP tool access.

| Agent | Skill directory |
|-|-|
| Claude Code | `~/.claude/skills/roundtable/` |
| Codex | `~/.codex/skills/roundtable/` or `.agents/skills/roundtable/` |
| Gemini CLI | `~/.gemini/skills/roundtable/` or `.agents/skills/roundtable/` |
| OpenCode | `~/.opencode/skills/roundtable.md` (single file, not directory) |

```bash
# Example: Claude Code skill discovery
mkdir -p ~/.claude/skills/roundtable
cp /path/to/roundtable/SKILL.md ~/.claude/skills/roundtable/
```

---

## CLI Usage (Alternative)

The `roundtable-cli` escript provides the same functionality via command-line flags. Use it for scripting, CI, or contexts where MCP registration is not available.

```bash
mix escript.build
# Produces: ./roundtable-cli
```

```bash
./roundtable-cli --prompt "Hello" --timeout 30
```

Expected: JSON with `gemini`, `codex`, and `claude` fields, each with `status: "ok"`.

Note: the escript requires Erlang on `PATH` and may not inherit the same `PATH` as your shell — the MCP server is the recommended integration path.

---

## Per-Project Role Overrides

Any project can customize role prompts by creating:

```
<project>/.claude/roundtable/roles/
├── planner.txt
└── codereviewer.txt
```

The agent passes `--project-roles-dir .claude/roundtable/roles` and roundtable checks project roles first, falling back to the bundled defaults.

---

## Notes

**Agents are both participant and orchestrator**: Claude Code (or any MCP-capable agent) orchestrates roundtable by calling its MCP tools, while also being one of the three participants dispatched by the server. This is not recursive — the server spawns a separate `claude` CLI process for the participant role, independent of the orchestrating agent session. The same applies to Gemini CLI and Codex when used as orchestrators.
