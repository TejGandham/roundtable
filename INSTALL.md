# Installing Roundtable

Roundtable is an MCP server. Agents call its tools directly over stdio — no Bash tool needed, no output buffer limits.

## Prerequisites

- **Erlang/OTP 28+** on PATH
- **At least one** of the following CLI tools installed and authenticated:
  - **Gemini CLI** (`gemini --version`)
  - **Codex CLI** (`codex --version`)
  - **Claude CLI** (`claude --version`)

All three are recommended for full consensus. Missing CLIs are skipped gracefully (status: `not_found`). The CLI tools must be on `PATH`.

---

## Install

```bash
VERSION=1.2.0
mkdir -p ~/.local/share/roundtable
curl -sL https://github.com/TejGandham/roundtable/releases/download/v${VERSION}/roundtable-mcp-${VERSION}.tar.gz \
  | tar xz -C ~/.local/share/roundtable --strip-components=1
chmod +x ~/.local/share/roundtable/bin/roundtable-mcp
```

Verify the checksum:

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
command = ["~/.local/share/roundtable/bin/roundtable-mcp"]
```

### OpenCode

Add to `~/.config/opencode/config.json`:

```json
{
  "mcp": {
    "roundtable": {
      "command": "~/.local/share/roundtable/bin/roundtable-mcp"
    }
  }
}
```

### Other MCP Clients

```
command: ~/.local/share/roundtable/bin/roundtable-mcp
```

Stdio transport, JSON-RPC, MCP protocol 2025-03-26.

---

## Skill Discovery (Optional)

Copy `SKILL.md` from the release to your agent's skill directory for skill-triggered invocation alongside MCP tool access.

```bash
mkdir -p ~/.claude/skills/roundtable
cp ~/.local/share/roundtable/SKILL.md ~/.claude/skills/roundtable/
```

---

## Per-Project Role Overrides

Projects can customize role prompts:

```
<project>/.claude/roundtable/roles/
├── planner.txt
└── codereviewer.txt
```

Roundtable checks project roles first, falling back to bundled defaults.

---

## Notes

**Agents are both participant and orchestrator**: Claude Code (or any MCP-capable agent) orchestrates roundtable by calling its MCP tools, while also being one of the three participants dispatched by the server. This is not recursive — the server spawns a separate `claude` CLI process for the participant role, independent of the orchestrating agent session.
