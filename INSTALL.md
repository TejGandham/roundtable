# Installing Roundtable

Roundtable is an MCP server. Agents call its tools directly over stdio — no Bash tool needed, no output buffer limits.

## Prerequisites

- **Erlang/OTP 28+** on PATH
- **At least one** of the following CLI tools installed and authenticated:
  - **Gemini CLI** (`gemini --version`)
  - **Codex CLI** (`codex --version`)
  - **Claude CLI** (`claude --version`)

All three are recommended for full consensus. Missing CLIs are skipped gracefully (status: `not_found`). See **CLI Path Configuration** below if CLIs aren't on the MCP server's PATH.

---

## Uninstall

Remove an existing installation before upgrading or if no longer needed:

```bash
# Remove MCP registration (Claude Code)
claude mcp remove roundtable

# Remove installed files
rm -rf ~/.local/share/roundtable

# Remove skill file (if installed)
rm -rf ~/.claude/skills/roundtable
```

For other clients, remove the `roundtable` entry from the relevant config file (`~/.codex/config.toml`, `~/.config/opencode/config.json`, etc.).

---

## Install

```bash
VERSION=0.5.1
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

All tools support an `agents` parameter for selective dispatch — choose which CLIs to invoke, run the same CLI with different models, and assign per-agent roles. See [SKILL.md](SKILL.md) for full usage docs.

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

## CLI Path Configuration

MCP servers inherit a minimal `PATH` from the host agent, which often excludes directories managed by nvm, Homebrew, Volta, or pyenv. If roundtable can't find CLI executables, configure their paths via environment variables.

### Per-CLI absolute path

Set `ROUNDTABLE_<NAME>_PATH` to the full path of each CLI binary:

```bash
# Claude Code registration with env vars
claude mcp add -s user -e ROUNDTABLE_CLAUDE_PATH=/usr/local/bin/claude \
  -e ROUNDTABLE_GEMINI_PATH=/opt/homebrew/bin/gemini \
  -e ROUNDTABLE_CODEX_PATH=/Users/you/.nvm/versions/node/v22.0.0/bin/codex \
  roundtable -- ~/.local/share/roundtable/bin/roundtable-mcp
```

### Extra search PATH

Set `ROUNDTABLE_EXTRA_PATH` with colon-separated directories to search before the system PATH:

```bash
claude mcp add -s user \
  -e ROUNDTABLE_EXTRA_PATH=/opt/homebrew/bin:/Users/you/.nvm/versions/node/v22.0.0/bin \
  roundtable -- ~/.local/share/roundtable/bin/roundtable-mcp
```

Resolution order: `ROUNDTABLE_<NAME>_PATH` > `ROUNDTABLE_EXTRA_PATH` > system `PATH`.

---

## Default Agent Configuration

Set `ROUNDTABLE_DEFAULT_AGENTS` to configure which agents run by default — without specifying them on every tool call. Uses the same JSON schema as the `agents` tool parameter (see [SKILL.md](SKILL.md) for schema details).

### Precedence

| Priority | Source | Description |
|-|-|-|
| 1 (highest) | Per-call `agents` parameter | Overrides everything — always wins |
| 2 | `ROUNDTABLE_DEFAULT_AGENTS` env var | Session default set at registration |
| 3 (fallback) | Built-in default | All 3 CLIs: gemini, codex, claude |

> **The per-call `agents` parameter always overrides your defaults.** You can request a gemini-only review even if gemini isn't in your default configuration — just pass `agents` in the tool call.

### Examples

Run only Codex and Claude by default:
```json
[{"cli": "codex"}, {"cli": "claude"}]
```

With model and role defaults:
```json
[
  {"cli": "codex", "model": "o4-mini", "role": "codereviewer"},
  {"cli": "claude", "model": "sonnet"}
]
```

### Register with Default Agents

**Claude Code**:
```bash
claude mcp add -s user \
  -e ROUNDTABLE_DEFAULT_AGENTS='[{"cli":"codex"},{"cli":"claude"}]' \
  roundtable -- ~/.local/share/roundtable/bin/roundtable-mcp
```

**Codex** — add to `~/.codex/config.toml`:
```toml
[mcp_servers.roundtable]
command = ["~/.local/share/roundtable/bin/roundtable-mcp"]
env = { ROUNDTABLE_DEFAULT_AGENTS = '[{"cli":"codex"},{"cli":"claude"}]' }
```

**OpenCode** — add to `~/.config/opencode/config.json`:
```json
{
  "mcp": {
    "roundtable": {
      "command": "~/.local/share/roundtable/bin/roundtable-mcp",
      "env": {
        "ROUNDTABLE_DEFAULT_AGENTS": "[{\"cli\":\"codex\"},{\"cli\":\"claude\"}]"
      }
    }
  }
}
```

### Notes

- **Invalid config**: If the env var contains invalid JSON or an unrecognized schema, roundtable logs a warning and falls back to all 3 CLIs — it never crashes on bad config.
- **`resume` field**: The `resume` field in default agent configs is ignored. Session IDs are per-call and must be passed explicitly via `codex_resume`, `claude_resume`, etc.
- **CLI escript**: The env var also applies to `roundtable-cli` invocations (both share the same dispatch logic).

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
