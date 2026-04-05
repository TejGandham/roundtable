# Installing Roundtable

Roundtable is an MCP server. Agents call its tools directly over stdio.

## Prerequisites

- **Erlang/OTP 28+** on PATH
- **At least one** CLI tool installed and authenticated:
  - `gemini --version`
  - `codex --version`
  - `claude --version`

All three are recommended. Missing CLIs are skipped gracefully (`status: "not_found"`).

---

## Install

Always remove any previous version first:

```bash
pkill -f 'roundtable_mcp' 2>/dev/null || true
rm -rf ~/.local/share/roundtable
claude mcp remove roundtable 2>/dev/null || true
```

Then install:

```bash
VERSION=0.6.0
mkdir -p ~/.local/share/roundtable
curl -sL https://github.com/TejGandham/roundtable/releases/download/v${VERSION}/roundtable-mcp-${VERSION}.tar.gz \
  | tar xz -C ~/.local/share/roundtable --strip-components=1
chmod +x ~/.local/share/roundtable/bin/roundtable-mcp
```

Verify checksum:

```bash
curl -sL https://github.com/TejGandham/roundtable/releases/download/v${VERSION}/SHA256SUMS \
  | grep roundtable-mcp | sha256sum --check
```

---

## Register

### Claude Code

```bash
claude mcp add -s user roundtable -- ~/.local/share/roundtable/bin/roundtable-mcp
```

Restart Claude Code. These tools become available:

| Tool | Purpose |
|-|-|
| `roundtable_hivemind` | Multi-model consensus (general) |
| `roundtable_deepdive` | Extended reasoning / deep analysis |
| `roundtable_architect` | Implementation planning |
| `roundtable_challenge` | Devil's advocate / stress-test ideas |
| `roundtable_xray` | Architecture + code quality review |

All tools accept an `agents` parameter for selective dispatch. See [SKILL.md](SKILL.md) for full parameter docs.

### Other MCP Clients

Stdio transport, JSON-RPC, MCP protocol 2025-03-26. Command:

```
~/.local/share/roundtable/bin/roundtable-mcp
```

---

## CLI Path Configuration

MCP servers inherit a minimal `PATH` that often excludes nvm, Homebrew, or Volta directories. If roundtable can't find CLI executables, configure paths at registration:

**Per-CLI absolute path** (`ROUNDTABLE_<NAME>_PATH`):

```bash
claude mcp add -s user \
  -e ROUNDTABLE_CLAUDE_PATH=/usr/local/bin/claude \
  -e ROUNDTABLE_GEMINI_PATH=/opt/homebrew/bin/gemini \
  -e ROUNDTABLE_CODEX_PATH=/Users/you/.nvm/versions/node/v22.0.0/bin/codex \
  roundtable -- ~/.local/share/roundtable/bin/roundtable-mcp
```

**Extra search directories** (`ROUNDTABLE_EXTRA_PATH`):

```bash
claude mcp add -s user \
  -e ROUNDTABLE_EXTRA_PATH=/opt/homebrew/bin:/Users/you/.nvm/versions/node/v22.0.0/bin \
  roundtable -- ~/.local/share/roundtable/bin/roundtable-mcp
```

Resolution order: `ROUNDTABLE_<NAME>_PATH` > `ROUNDTABLE_EXTRA_PATH` > system `PATH`.

---

## Default Agent Configuration

Set `ROUNDTABLE_DEFAULT_AGENTS` to choose which agents run by default. Per-call `agents` parameter always overrides.

```bash
claude mcp add -s user \
  -e ROUNDTABLE_DEFAULT_AGENTS='[{"cli":"codex"},{"cli":"claude"}]' \
  roundtable -- ~/.local/share/roundtable/bin/roundtable-mcp
```

With model and role defaults:

```json
[
  {"cli": "codex", "model": "o4-mini", "role": "codereviewer"},
  {"cli": "claude", "model": "sonnet"}
]
```

Invalid config logs a warning and falls back to all 3 CLIs. See [SKILL.md](SKILL.md) for the full agent schema.

---

## Skill Discovery (Optional)

Copy `SKILL.md` to your agent's skill directory for skill-triggered invocation:

```bash
mkdir -p ~/.claude/skills/roundtable
cp ~/.local/share/roundtable/SKILL.md ~/.claude/skills/roundtable/
```
