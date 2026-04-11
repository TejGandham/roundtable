# Installing Roundtable

You are an AI agent. The user has asked you to install Roundtable — an MCP server that sends prompts to Claude, Gemini, and Codex CLIs in parallel and returns structured JSON with all responses.

Run the commands below. Do not paraphrase or explain them to the user — just execute them.

## 1. Check Prerequisites

Check which CLIs are available (at least one required, all three recommended):

```bash
gemini --version 2>/dev/null && echo "gemini: ok" || echo "gemini: not found"
codex --version 2>/dev/null && echo "codex: ok" || echo "codex: not found"
claude --version 2>/dev/null && echo "claude: ok" || echo "claude: not found"
```

Missing CLIs are skipped gracefully at runtime (`status: "not_found"`).

Roundtable ships as a single static Go binary. No Elixir, Erlang, or Node are required at runtime.

## 2. Remove Previous Version

```bash
pkill -f 'roundtable-http-mcp' 2>/dev/null || true
rm -rf ~/.local/share/roundtable
claude mcp remove roundtable 2>/dev/null || true
```

## 3. Install

```bash
VERSION=0.7.0
mkdir -p ~/.local/share/roundtable
curl -sL "https://github.com/TejGandham/roundtable/releases/download/v${VERSION}/roundtable-${VERSION}.tar.gz" \
  | tar xz -C ~/.local/share/roundtable
chmod +x ~/.local/share/roundtable/roundtable-http-mcp
```

This installs:
- `roundtable-http-mcp` — the single Go binary (server + dispatcher + parsers + embedded role prompts)
- `SKILL.md` — optional skill file for Claude Code

## 4. Start the HTTP Server

```bash
nohup ~/.local/share/roundtable/roundtable-http-mcp > /tmp/roundtable.log 2>&1 &
```

Verify it started:

```bash
curl -s http://127.0.0.1:4040/healthz
curl -s http://127.0.0.1:4040/readyz
```

Expected: `ok` and `ready`.

Optional environment variables:

|Env Var|Default|Purpose|
|-|-|-|
|`ROUNDTABLE_HTTP_ADDR`|`127.0.0.1:4040`|Listen address|
|`ROUNDTABLE_HTTP_MCP_PATH`|`/mcp`|MCP endpoint path|
|`ROUNDTABLE_HTTP_ROLES_DIR`|(embedded)|Override directory with custom role prompt files|
|`ROUNDTABLE_HTTP_PROJECT_ROLES_DIR`|(none)|Project-scoped role prompt directory|
|`ROUNDTABLE_DEFAULT_AGENTS`|(all 3)|JSON array of agents to run by default|
|`ROUNDTABLE_GEMINI_PATH`|`$PATH` lookup|Explicit path to the gemini CLI|
|`ROUNDTABLE_CODEX_PATH`|`$PATH` lookup|Explicit path to the codex CLI|
|`ROUNDTABLE_CLAUDE_PATH`|`$PATH` lookup|Explicit path to the claude CLI|
|`ROUNDTABLE_EXTRA_PATH`|(none)|Extra directories to search for CLI binaries|

## 5. Register as MCP Server

```bash
claude mcp add --transport http roundtable http://127.0.0.1:4040/mcp
```

If CLIs are installed in non-standard locations (nvm, Homebrew, Volta), set env vars when starting the server:

```bash
ROUNDTABLE_CLAUDE_PATH=$(which claude) \
ROUNDTABLE_GEMINI_PATH=$(which gemini) \
ROUNDTABLE_CODEX_PATH=$(which codex) \
  nohup ~/.local/share/roundtable/roundtable-http-mcp > /tmp/roundtable.log 2>&1 &
```

Only include env vars for CLIs that exist.

## 6. Install Skill File (Optional)

```bash
mkdir -p ~/.claude/skills/roundtable
cp ~/.local/share/roundtable/SKILL.md ~/.claude/skills/roundtable/
```

## 7. Verify

Tell the user to restart Claude Code, then test with a tool call:

```
Use the roundtable hivemind tool to ask: "What is the best way to handle errors in async Go code?"
```

All five tools should now be available:

|Tool|Use|
|-|-|
|`hivemind`|General multi-model consensus|
|`deepdive`|Deep analysis / extended reasoning|
|`architect`|Implementation planning|
|`challenge`|Devil's advocate / stress-test|
|`xray`|Architecture + code quality review|

## Default Agents (Optional)

Set `ROUNDTABLE_DEFAULT_AGENTS` when starting the server:

```bash
ROUNDTABLE_DEFAULT_AGENTS='[{"cli":"codex"},{"cli":"claude"}]' \
  nohup ~/.local/share/roundtable/roundtable-http-mcp > /tmp/roundtable.log 2>&1 &
```

Per-call `agents` parameter always overrides defaults. See [SKILL.md](SKILL.md) for the full agent schema.

## Monitoring

Check burn-in metrics:

```bash
curl -s http://127.0.0.1:4040/metricsz
```

Returns JSON with `total_requests` and `dispatch_errors` atomic counters.

## Troubleshooting

If `/healthz` doesn't respond:
- Check if the server is running: `pgrep -f roundtable-http-mcp`
- Check logs: `cat /tmp/roundtable.log`

If `/readyz` returns 503:
- Confirm at least one CLI (`gemini`, `codex`, or `claude`) is installed and on PATH
- Check logs for per-backend health messages

If a tool call fails:
- Confirm the CLIs you requested are installed and authenticated
- Check server logs: `cat /tmp/roundtable.log`

## Development (Building from Source)

```bash
mise install
make build
make test
make run
```

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for architecture details.
