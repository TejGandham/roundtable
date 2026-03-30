# Installing Roundtable

## Prerequisites

- **Erlang/OTP** runtime (the `roundtable` binary is an escript)
- **Gemini CLI** installed and authenticated (`gemini --version`)
- **Codex CLI** installed and authenticated (`codex --version`)

```bash
# macOS
brew install erlang

# Debian/Ubuntu
sudo apt install erlang-base

# Nix
nix-shell -p erlang
```

## What Gets Installed

```
<skill-dir>/roundtable/
├── roundtable            # escript binary
├── SKILL.md              # skill instructions (YAML frontmatter + markdown)
└── roles/
    ├── default.txt       # general analysis prompt
    ├── planner.txt       # architecture/design prompt
    └── codereviewer.txt  # code review prompt
```

## Skill Discovery by Agent

All four major coding agents support skill discovery via `SKILL.md` files, but each has its own directory convention:

| Agent | User-level directory | Workspace-level directory | Format |
|-|-|-|-|
| Claude Code | `~/.claude/skills/<name>/` | `.claude/skills/` | `SKILL.md` in directory |
| Codex | `~/.codex/skills/<name>/` | `.agents/skills/` | `SKILL.md` in directory |
| Gemini CLI | `~/.gemini/skills/<name>/` | `.gemini/skills/` or `.agents/skills/` | `SKILL.md` in directory |
| OpenCode | `~/.opencode/skills/` | — | Single `<name>.md` files |

All agents use progressive disclosure: only the SKILL.md frontmatter (name + description) is loaded initially. Full instructions load when the skill is activated.

`.agents/skills/` is an emerging cross-agent convention supported by Codex and Gemini CLI for workspace-level skills.

## Install from Release

### Claude Code

```bash
mkdir -p ~/.claude/skills/roundtable
curl -sL https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable/releases/download/v1.0.0/roundtable-v1.0.0.tar.gz \
  | tar xz -C ~/.claude/skills/roundtable
chmod +x ~/.claude/skills/roundtable/roundtable
```

Auto-discovered via SKILL.md frontmatter. Restart Claude Code to pick it up.

### Codex

```bash
mkdir -p ~/.codex/skills/roundtable
curl -sL https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable/releases/download/v1.0.0/roundtable-v1.0.0.tar.gz \
  | tar xz -C ~/.codex/skills/roundtable
chmod +x ~/.codex/skills/roundtable/roundtable
```

Auto-discovered via SKILL.md frontmatter. Restart Codex to pick it up.

### Gemini CLI

```bash
mkdir -p ~/.gemini/skills/roundtable
curl -sL https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable/releases/download/v1.0.0/roundtable-v1.0.0.tar.gz \
  | tar xz -C ~/.gemini/skills/roundtable
chmod +x ~/.gemini/skills/roundtable/roundtable
```

Gemini discovers skills from `~/.gemini/skills/` and `~/.agents/skills/` (user-level) or `.gemini/skills/` and `.agents/skills/` (workspace-level). The model activates skills via the `activate_skill` tool, which requires user consent on first use. Restart Gemini CLI to pick it up.

**Note:** Gemini is both a *participant* in roundtable (dispatched by the binary) and potentially an *orchestrator* (activating the skill). When Gemini runs roundtable, the binary spawns a separate Gemini CLI process — this is expected and not recursive.

### OpenCode

OpenCode uses a different skill format (single `.md` files in `~/.opencode/skills/`), not subdirectories with `SKILL.md`. Roundtable's directory-based structure doesn't match OpenCode's native convention.

Two options:

**Option A — Share Claude Code's skill directory** (if both agents are installed):

OpenCode can read from `~/.claude/skills/` if you add an `external_directory` permission in your OpenCode agent config:

```json
{
  "permission": "external_directory",
  "pattern": "/home/<user>/.claude/skills/roundtable/*",
  "action": "allow"
}
```

**Option B — Standalone install with instructions file:**

Install the binary and create an OpenCode-native skill that references it:

```bash
# Install binary
mkdir -p ~/.local/share/roundtable/roles
curl -sL https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable/releases/download/v1.0.0/roundtable-v1.0.0.tar.gz \
  | tar xz -C ~/.local/share/roundtable
chmod +x ~/.local/share/roundtable/roundtable

# Create OpenCode skill pointer
cat > ~/.opencode/skills/roundtable.md << 'EOF'
---
name: roundtable
description: Multi-model consensus via Gemini, Codex, and Claude CLIs. Run when user wants a second opinion, consensus, or validation.
---

Run `~/.local/share/roundtable/roundtable` with `--prompt`, `--role`, `--files`, and `--timeout` flags.
Parse the JSON output and synthesize all model responses.
See ~/.local/share/roundtable/SKILL.md for full documentation.
EOF
```

### Multi-Agent (Shared Install)

If you use multiple agents, install once and symlink to avoid maintaining separate copies:

```bash
# Install to Claude Code (primary)
mkdir -p ~/.claude/skills/roundtable
curl -sL https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable/releases/download/v1.0.0/roundtable-v1.0.0.tar.gz \
  | tar xz -C ~/.claude/skills/roundtable
chmod +x ~/.claude/skills/roundtable/roundtable

# Symlink for other agents
mkdir -p ~/.codex/skills ~/.gemini/skills
ln -s ~/.claude/skills/roundtable ~/.codex/skills/roundtable
ln -s ~/.claude/skills/roundtable ~/.gemini/skills/roundtable
```

### Workspace-Level Install (Per-Project)

For project-scoped installs using the cross-agent `.agents/skills/` convention (supported by Codex and Gemini CLI):

```bash
mkdir -p .agents/skills/roundtable
curl -sL https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable/releases/download/v1.0.0/roundtable-v1.0.0.tar.gz \
  | tar xz -C .agents/skills/roundtable
chmod +x .agents/skills/roundtable/roundtable
```

For Claude Code workspace-level, use `.claude/skills/` instead.

## Install from Source

```bash
git clone https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable.git
cd roundtable

# Requires Elixir + Erlang
# macOS: brew install elixir
# Debian/Ubuntu: sudo apt install elixir

mix deps.get
mix escript.build

# Copy to your agent's skill directory
SKILL_DIR=~/.claude/skills/roundtable  # adjust for your agent
mkdir -p "$SKILL_DIR/roles"
cp roundtable "$SKILL_DIR/"
cp SKILL.md "$SKILL_DIR/"
cp roles/*.txt "$SKILL_DIR/roles/"
```

## Per-Project Role Overrides

Any project can customize role prompts by creating:

```
<project>/.claude/roundtable/roles/
├── planner.txt           # project-specific planner context
└── codereviewer.txt      # project-specific reviewer context
```

The agent passes `--project-roles-dir .claude/roundtable/roles` and roundtable checks project roles first, falling back to the global roles directory.

## Verify Installation

```bash
# Adjust path to match your install location
~/.claude/skills/roundtable/roundtable --prompt "Hello" --timeout 30
```

Expected: JSON output with `gemini` and `codex` fields, both with `status: "ok"`.

If a CLI is missing, you'll see `status: "not_found"` for that model. If auth is expired, you'll see `status: "error"` with details in `stderr`.
