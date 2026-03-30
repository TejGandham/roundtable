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

### Claude Code

From the roundtable project root:

```bash
claude mcp add -s user roundtable -- mix run --no-halt
```

This registers the server at user scope. Verify it's registered:

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
      "command": "mix",
      "args": ["run", "--no-halt"],
      "cwd": "/path/to/roundtable"
    }
  }
}
```

Replace `/path/to/roundtable` with the absolute path to your cloned roundtable repo. Restart OpenCode to pick it up.

### Install from Source (required for MCP)

```bash
git clone https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable.git
cd roundtable
mix deps.get
```

The MCP server starts with `mix run --no-halt` from the project root. No build step needed.

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

The `roundtable-cli` escript is available for standalone use, scripting, and CI pipelines where MCP registration isn't practical.

### Build from Source

```bash
git clone https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable.git
cd roundtable
mix deps.get
mix escript.build
# Produces: ./roundtable-cli
```

### Install from Release

```bash
# Claude Code
mkdir -p ~/.claude/skills/roundtable
curl -sL https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable/releases/download/v1.0.0/roundtable-v1.0.0.tar.gz \
  | tar xz -C ~/.claude/skills/roundtable
chmod +x ~/.claude/skills/roundtable/roundtable-cli

# Codex
mkdir -p ~/.codex/skills/roundtable
curl -sL https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable/releases/download/v1.0.0/roundtable-v1.0.0.tar.gz \
  | tar xz -C ~/.codex/skills/roundtable
chmod +x ~/.codex/skills/roundtable/roundtable-cli

# Gemini CLI
mkdir -p ~/.gemini/skills/roundtable
curl -sL https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable/releases/download/v1.0.0/roundtable-v1.0.0.tar.gz \
  | tar xz -C ~/.gemini/skills/roundtable
chmod +x ~/.gemini/skills/roundtable/roundtable-cli
```

### Multi-Agent (Shared Install)

```bash
# Install to Claude Code (primary)
mkdir -p ~/.claude/skills/roundtable
curl -sL https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable/releases/download/v1.0.0/roundtable-v1.0.0.tar.gz \
  | tar xz -C ~/.claude/skills/roundtable
chmod +x ~/.claude/skills/roundtable/roundtable-cli

# Symlink for other agents
mkdir -p ~/.codex/skills ~/.gemini/skills
ln -s ~/.claude/skills/roundtable ~/.codex/skills/roundtable
ln -s ~/.claude/skills/roundtable ~/.gemini/skills/roundtable
```

### Workspace-Level Install

For project-scoped installs using the cross-agent `.agents/skills/` convention (supported by Codex and Gemini CLI):

```bash
mkdir -p .agents/skills/roundtable
curl -sL https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable/releases/download/v1.0.0/roundtable-v1.0.0.tar.gz \
  | tar xz -C .agents/skills/roundtable
chmod +x .agents/skills/roundtable/roundtable-cli
```

For Claude Code workspace-level, use `.claude/skills/` instead.

### Verify CLI Installation

```bash
~/.claude/skills/roundtable/roundtable-cli --prompt "Hello" --timeout 30
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
