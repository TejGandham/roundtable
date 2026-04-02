# Installing Roundtable

Roundtable is an MCP server. Agents call its tools directly over stdio — no Bash tool needed, no output buffer limits.

## Prerequisites

- **Erlang/OTP 28+** (release install) or **[mise](https://mise.jdx.dev)** (source install — manages Erlang + Elixir automatically)
- **At least one** of the following CLI tools installed and authenticated:
  - **Gemini CLI** (`gemini --version`)
  - **Codex CLI** (`codex --version`)
  - **Claude CLI** (`claude --version`)

All three are recommended for full consensus. Missing CLIs are skipped gracefully (status: `not_found`). The CLI tools must be on `PATH` for the roundtable server to dispatch to them.

---

## Install from Release (Recommended)

Download the latest release and extract it:

```bash
VERSION=1.2.0
HOST=https://brahma.myth-gecko.ts.net:3000
mkdir -p ~/.local/share/roundtable
curl -sL -H "Authorization: token YOUR_TOKEN" \
  $HOST/stackhouse/roundtable/releases/download/v${VERSION}/roundtable-mcp-${VERSION}.tar.gz \
  | tar xz -C ~/.local/share/roundtable --strip-components=1
chmod +x ~/.local/share/roundtable/bin/roundtable-mcp
```

> **Note**: The repository is private. Replace `YOUR_TOKEN` with a Forgejo API token, or use `tea` CLI credentials.

Verify the checksum:

```bash
curl -sL -H "Authorization: token YOUR_TOKEN" \
  $HOST/stackhouse/roundtable/releases/download/v${VERSION}/SHA256SUMS \
  | grep roundtable-mcp | sha256sum --check
```

The release contains:

```
roundtable_mcp/
├── bin/
│   ├── roundtable-mcp      ← register this as the MCP command
│   └── roundtable_mcp      ← release runner (called by roundtable-mcp)
├── lib/                     ← compiled BEAM bytecode
├── releases/                ← release metadata
├── SKILL.md                 ← skill file for agent discovery
└── INSTALL.md
```

Requires **Erlang/OTP 28+** on PATH. No Elixir or mix needed.

---

## MCP Registration

Register roundtable as an MCP server so your agent can call its tools directly.

### Claude Code (release install)

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

### Codex (release install)

Add to `~/.codex/config.toml`:

```toml
[mcp_servers.roundtable]
command = ["~/.local/share/roundtable/bin/roundtable-mcp"]
```

Restart Codex to pick it up.

### OpenCode (release install)

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

### Other MCP Clients

Point any MCP-compatible client at the server binary:

```
command: ~/.local/share/roundtable/bin/roundtable-mcp
```

The server communicates over stdio using JSON-RPC (MCP protocol 2025-03-26).

---

## Skill Discovery (Optional)

Agents that support skill files can discover roundtable's documentation automatically. The release includes `SKILL.md`; copy it to your agent's skill directory for skill-triggered invocation alongside MCP tool access.

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

## Install from Source (Development)

For contributing or running the latest unreleased code:

```bash
git clone https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable.git
cd roundtable
```

**Install the toolchain with [mise](https://mise.jdx.dev):**

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

**Run the MCP server from source:**

```bash
ROUNDTABLE_MCP=1 mix run --no-halt
```

### Registering a source MCP server

The server spawns `claude`, `codex`, and `gemini` as child processes. The registration command must ensure these CLIs are on `PATH`.

**Claude Code:**

```bash
claude mcp add -s user roundtable -- bash -c \
  'export PATH="$HOME/.local/bin:$(dirname $(readlink -f $(which node))):$PATH" && \
   eval "$(mise activate bash)" && \
   cd /path/to/roundtable && ROUNDTABLE_MCP=1 mix run --no-halt'
```

**Codex** (add to `~/.codex/config.toml`):

```toml
[mcp_servers.roundtable]
command = ["bash", "-c", "export PATH=\"$HOME/.local/bin:$(dirname $(readlink -f $(which node))):$PATH\" && eval \"$(mise activate bash)\" && cd /path/to/roundtable && ROUNDTABLE_MCP=1 mix run --no-halt"]
```

Replace `/path/to/roundtable` with the actual clone path.

**Build a release locally:**

```bash
MIX_ENV=prod mix release roundtable_mcp
```

---

## Cutting a Release

Push a version tag to trigger the CI release workflow:

```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

The Forgejo Actions workflow (`.forgejo/workflows/release.yml`) will:
1. Run the full test suite
2. Build the MCP release tarball (`roundtable-mcp-VERSION.tar.gz`)
3. Generate `SHA256SUMS`
4. Publish to the Forgejo release page

**Manual release** (if CI is unavailable):

```bash
eval "$(mise activate bash)"
MIX_ENV=prod mix deps.get --only prod
MIX_ENV=prod mix release roundtable_mcp
cp SKILL.md INSTALL.md _build/prod/rel/roundtable_mcp/
chmod +x _build/prod/rel/roundtable_mcp/bin/roundtable-mcp
tar czf roundtable-mcp-X.Y.Z.tar.gz -C _build/prod/rel roundtable_mcp
sha256sum roundtable-mcp-X.Y.Z.tar.gz > SHA256SUMS
```

Then upload via Forgejo UI or API.

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
