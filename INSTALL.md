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

## Install from Source

```bash
git clone https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable.git
cd roundtable

# Requires Elixir + Erlang
brew install elixir  # or: sudo apt install elixir

mix deps.get
mix escript.build

# Copy to your agent's skill directory
SKILL_DIR=~/.claude/skills/roundtable  # or ~/.codex/skills/roundtable
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
