# Installing Roundtable

## Prerequisites

- **Erlang/OTP** runtime (the `roundtable` binary is an escript)
- **Gemini CLI** installed and authenticated (`gemini --version`)
- **Codex CLI** installed and authenticated (`codex --version`)

Install Erlang via your package manager:

```bash
# macOS
brew install erlang

# Debian/Ubuntu
sudo apt install erlang-base
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

## Install from Release

Download the latest release tarball and extract to the skill directory for your agent.

### Claude Code

```bash
mkdir -p ~/.claude/skills/roundtable
curl -sL https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable/releases/download/v1.0.0/roundtable-v1.0.0.tar.gz \
  | tar xz -C ~/.claude/skills/roundtable
chmod +x ~/.claude/skills/roundtable/roundtable
```

Claude Code auto-discovers skills in `~/.claude/skills/` via SKILL.md frontmatter. No config changes needed — restart Claude Code to pick it up.

### OpenCode

OpenCode reads skills from the same `~/.claude/skills/` directory. If you already installed for Claude Code, OpenCode will find it automatically.

If OpenCode is your only agent, use the same path:

```bash
mkdir -p ~/.claude/skills/roundtable
curl -sL https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable/releases/download/v1.0.0/roundtable-v1.0.0.tar.gz \
  | tar xz -C ~/.claude/skills/roundtable
chmod +x ~/.claude/skills/roundtable/roundtable
```

You may need to allow the skill directory in your OpenCode agent permissions:

```json
{
  "permission": "external_directory",
  "pattern": "/home/<user>/.claude/skills/roundtable/*",
  "action": "allow"
}
```

### Codex

Codex discovers skills from `~/.codex/skills/` (or `$CODEX_HOME/skills/`):

```bash
mkdir -p ~/.codex/skills/roundtable
curl -sL https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable/releases/download/v1.0.0/roundtable-v1.0.0.tar.gz \
  | tar xz -C ~/.codex/skills/roundtable
chmod +x ~/.codex/skills/roundtable/roundtable
```

Codex reads the same SKILL.md frontmatter format. Restart Codex to pick up the new skill.

**Note:** The SKILL.md hardcodes `~/.claude/skills/roundtable/roundtable` as the binary path. For Codex, either:
- Symlink: `ln -s ~/.codex/skills/roundtable/roundtable ~/.claude/skills/roundtable/roundtable`
- Or update the path in your copy of SKILL.md to `~/.codex/skills/roundtable/roundtable`

### Gemini CLI

Gemini CLI has its own skill system using the same `SKILL.md` format. It discovers skills from `~/.gemini/skills/` (user-level) or `.gemini/skills/` (workspace-level), with progressive disclosure — only name and description are loaded initially, full instructions load when the model calls `activate_skill`.

```bash
mkdir -p ~/.gemini/skills/roundtable
curl -sL https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable/releases/download/v1.0.0/roundtable-v1.0.0.tar.gz \
  | tar xz -C ~/.gemini/skills/roundtable
chmod +x ~/.gemini/skills/roundtable/roundtable
```

Restart Gemini CLI to pick up the new skill. The model will see roundtable in its available skills and can activate it via the `activate_skill` tool.

**Note:** The SKILL.md hardcodes `~/.claude/skills/roundtable/roundtable` as the binary path. For Gemini, either:
- Symlink: `ln -s ~/.gemini/skills/roundtable/roundtable ~/.claude/skills/roundtable/roundtable`
- Or update the path in your copy of SKILL.md to `~/.gemini/skills/roundtable/roundtable`

**Note:** Gemini is both a *participant* in roundtable (dispatched by the binary) and potentially an *orchestrator* (activating the skill). When Gemini runs roundtable, the binary spawns a separate Gemini CLI process — this is expected and not recursive.

### Multi-Agent (Shared Install)

If you use multiple agents, install once and symlink to avoid maintaining separate copies:

```bash
# Install to Claude Code (primary)
mkdir -p ~/.claude/skills/roundtable
curl -sL https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable/releases/download/v1.0.0/roundtable-v1.0.0.tar.gz \
  | tar xz -C ~/.claude/skills/roundtable
chmod +x ~/.claude/skills/roundtable/roundtable

# Symlink for other agents
ln -s ~/.claude/skills/roundtable ~/.codex/skills/roundtable
ln -s ~/.claude/skills/roundtable ~/.gemini/skills/roundtable
```

This way SKILL.md's hardcoded `~/.claude/skills/roundtable/roundtable` path works for all agents.

## Install from Source

```bash
git clone https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable.git
cd roundtable

# Requires Elixir + Erlang
brew install elixir  # or: sudo apt install elixir

mix deps.get
mix escript.build

# Copy to your agent's skill directory
# Use ~/.claude/skills, ~/.codex/skills, or ~/.gemini/skills
SKILL_DIR=~/.claude/skills/roundtable
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
~/.claude/skills/roundtable/roundtable --prompt "Hello" --timeout 30
```

Expected: JSON output with `gemini` and `codex` fields, both with `status: "ok"`.

If a CLI is missing, you'll see `status: "not_found"` for that model. If auth is expired, you'll see `status: "error"` with details in `stderr`.
