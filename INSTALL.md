# Installing Roundtable

Roundtable is an MCP server. Agents call its tools directly over stdio — no Bash tool needed, no output buffer limits.

## Prerequisites

- **Elixir + Erlang/OTP** (to run the MCP server or build the CLI)
- **Gemini CLI** installed and authenticated (`gemini --version`)
- **Codex CLI** installed and authenticated (`codex --version`)
- **Claude CLI** installed and authenticated (`claude --version`)

```bash
# macOS
brew install elixir

# Debian/Ubuntu
sudo apt install elixir

# Nix
nix-shell -p elixir
```

## MCP Registration (Recommended)

Register roundtable as an MCP server so your agent can call its tools directly.

### Install the release

Download the latest release and extract it to a stable path:

```bash
VERSION=0.2.0
mkdir -p ~/.local/share/roundtable
curl -sL https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable/releases/download/v${VERSION}/roundtable-mcp-${VERSION}.tar.gz \
  | tar xz -C ~/.local/share/roundtable --strip-components=1
chmod +x ~/.local/share/roundtable/bin/roundtable-mcp
```

Verify the checksum:

```bash
curl -sL https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable/releases/download/v${VERSION}/SHA256SUMS \
  | grep roundtable-mcp | sha256sum --check
```

### Claude Code

```bash
claude mcp add -s user roundtable -- ~/.local/share/roundtable/bin/roundtable-mcp
```

Verify it's registered:

```bash
claude mcp list | grep roundtable
```

Restart Claude Code. The following tools will be available:

| Tool | Purpose |
|-|-|
| `roundtable_hivemind` | Multi-model consensus (general) |
| `roundtable_deepdive` | Extended reasoning / deep analysis |
| `roundtable_architect` | Implementation planning |
| `roundtable_challenge` | Devil's advocate / stress-test ideas |
| `roundtable_xray` | Codebase architecture + code quality |

### OpenCode

Add to your OpenCode config (`~/.config/opencode/config.json` or workspace `.opencode/config.json`):

```json
{
  "mcp": {
    "roundtable": {
      "command": "/home/user/.local/share/roundtable/bin/roundtable-mcp"
    }
  }
}
```

Replace `/home/user` with your actual home directory. Restart OpenCode to pick it up.

---

## Skill Discovery

All four major coding agents support skill discovery via `SKILL.md` files. The roundtable skill file documents the MCP tools and how to use them.

| Agent | User-level directory | Workspace-level directory | Format |
|-|-|-|-|
| Claude Code | `~/.claude/skills/<name>/` | `.claude/skills/` | `SKILL.md` in directory |
| Codex | `~/.codex/skills/<name>/` | `.agents/skills/` | `SKILL.md` in directory |
| Gemini CLI | `~/.gemini/skills/<name>/` | `.gemini/skills/` or `.agents/skills/` | `SKILL.md` in directory |
| OpenCode | `~/.opencode/skills/` | — | Single `<name>.md` files |

Copy `SKILL.md` to your agent's skill directory so it knows when and how to invoke roundtable tools.

---

## CLI Installation (Alternative)

The `roundtable-cli` escript provides the same functionality as the MCP tools via command-line flags. Use it for scripting, CI pipelines, or any context where MCP registration is not available.

**Requires Erlang/OTP 25+** on the target machine.

```bash
VERSION=0.2.0
curl -sL https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable/releases/download/v${VERSION}/roundtable-cli \
  -o ~/.local/bin/roundtable-cli
chmod +x ~/.local/bin/roundtable-cli
```

### Verify CLI Installation

```bash
roundtable-cli --prompt "Hello" --timeout 30
```

Expected: JSON output with `gemini`, `codex`, and `claude` fields, each with `status: "ok"`.

---

## Per-Project Role Overrides

Any project can customize role prompts by creating:

```
<project>/.claude/roundtable/roles/
├── planner.txt           # project-specific planner context
└── codereviewer.txt      # project-specific reviewer context
```

The agent passes `--project-roles-dir .claude/roundtable/roles` and roundtable checks project roles first, falling back to the global roles directory.

---

## Notes

**Gemini as participant and orchestrator**: Gemini is both a *participant* in roundtable (dispatched by the server) and potentially an *orchestrator* (activating the skill). When roundtable dispatches to Gemini, it spawns a separate Gemini CLI process — this is expected and not recursive.

**OpenCode skill format**: OpenCode uses single `.md` files in `~/.opencode/skills/`, not subdirectories. If you want OpenCode to discover roundtable via skill files rather than MCP, create `~/.opencode/skills/roundtable.md` pointing to the installed binary or MCP server.
