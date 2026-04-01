# Installing Roundtable

Roundtable is an MCP server. Agents call its tools directly over stdio — no Bash tool needed, no output buffer limits.

## Prerequisites

- **Erlang/OTP 27+** (required for both MCP server and CLI)
- **Gemini CLI** installed and authenticated (`gemini --version`)
- **Codex CLI** installed and authenticated (`codex --version`)
- **Claude CLI** installed and authenticated (`claude --version`)

```bash
# macOS
brew install erlang

# Debian/Ubuntu
sudo apt install erlang

# Nix
nix-shell -p erlang
```

---

## Install from Release (Recommended)

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

The release includes the MCP server binary, role prompts, and `SKILL.md`. No Elixir, no `mix`, no source checkout required.

---

## MCP Registration

Register roundtable as an MCP server so your agent can call its tools directly.

### Claude Code

```bash
claude mcp add -s user roundtable -- ~/.local/share/roundtable/bin/roundtable-mcp
```

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

### OpenCode

Add to `~/.config/opencode/config.json` (or workspace `.opencode/config.json`):

```json
{
  "mcp": {
    "roundtable": {
      "command": "~/.local/share/roundtable/bin/roundtable-mcp"
    }
  }
}
```

Restart OpenCode to pick it up.

### Other MCP Clients

Point any MCP-compatible client at the server binary:

```
command: ~/.local/share/roundtable/bin/roundtable-mcp
```

The server communicates over stdio using JSON-RPC (MCP protocol 2025-03-26).

---

## Skill Discovery (Optional)

Agents that support skill files can discover roundtable's documentation automatically. The release tarball includes `SKILL.md`; copy it to your agent's skill directory if you want skill-triggered invocation alongside MCP tool access.

| Agent | Skill directory |
|-|-|
| Claude Code | `~/.claude/skills/roundtable/` |
| Codex | `~/.codex/skills/roundtable/` or `.agents/skills/roundtable/` |
| Gemini CLI | `~/.gemini/skills/roundtable/` or `.agents/skills/roundtable/` |
| OpenCode | `~/.opencode/skills/roundtable.md` (single file, not directory) |

```bash
# Example: Claude Code skill discovery
mkdir -p ~/.claude/skills/roundtable
cp ~/.local/share/roundtable/SKILL.md ~/.claude/skills/roundtable/
```

---

## CLI Installation (Alternative)

The `roundtable-cli` escript provides the same functionality via command-line flags. Use it for scripting, CI, or contexts where MCP registration is not available.

```bash
VERSION=0.2.0
curl -sL https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable/releases/download/v${VERSION}/roundtable-cli \
  -o ~/.local/bin/roundtable-cli
chmod +x ~/.local/bin/roundtable-cli
```

Verify:

```bash
roundtable-cli --prompt "Hello" --timeout 30
```

Expected: JSON with `gemini`, `codex`, and `claude` fields, each with `status: "ok"`.

---

## Install from Source (Development)

For contributing or running the latest unreleased code:

```bash
git clone https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable.git
cd roundtable
mix deps.get
```

**Run the MCP server from source:**

```bash
ROUNDTABLE_MCP=1 mix run --no-halt
```

Then register it with your agent using the full command. For Claude Code:

```bash
claude mcp add -s user roundtable -- bash -c "cd /path/to/roundtable && ROUNDTABLE_MCP=1 mix run --no-halt"
```

**Build a release locally:**

```bash
MIX_ENV=prod mix release roundtable_mcp
```

**Build the CLI escript locally:**

```bash
mix escript.build
# Produces: ./roundtable-cli
```

Requires **Elixir 1.18+** and **Erlang/OTP 27+**.

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

**Gemini as participant and orchestrator**: Gemini is both a participant in roundtable (dispatched by the server) and potentially an orchestrator (activating the skill). When roundtable dispatches to Gemini, it spawns a separate Gemini CLI process — this is expected and not recursive.
