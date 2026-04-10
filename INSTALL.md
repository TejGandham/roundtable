# Installing Roundtable

You are an AI agent. The user has asked you to install Roundtable — an MCP server that sends prompts to Claude, Gemini, and Codex CLIs in parallel and returns structured JSON with all responses.

Run the commands below. Do not paraphrase or explain them to the user — just execute them.

## 1. Check Prerequisites

```bash
erl -noshell -eval 'io:format("~s~n", [erlang:system_info(otp_release)]), halt().'
```

Must print `28` or higher. If not, install Erlang/OTP 28+ first.

Check which CLIs are available (at least one required, all three recommended):

```bash
gemini --version 2>/dev/null && echo "gemini: ok" || echo "gemini: not found"
codex --version 2>/dev/null && echo "codex: ok" || echo "codex: not found"
claude --version 2>/dev/null && echo "claude: ok" || echo "claude: not found"
```

Missing CLIs are skipped gracefully at runtime (`status: "not_found"`).

## 2. Remove Previous Version

```bash
pkill -f 'roundtable_mcp' 2>/dev/null || true
rm -rf ~/.local/share/roundtable
claude mcp remove roundtable 2>/dev/null || true
```

## 3. Install

```bash
VERSION=0.6.0
mkdir -p ~/.local/share/roundtable
curl -sL "https://github.com/TejGandham/roundtable/releases/download/v${VERSION}/roundtable-mcp-${VERSION}.tar.gz" \
  | tar xz -C ~/.local/share/roundtable --strip-components=1
chmod +x ~/.local/share/roundtable/bin/roundtable-mcp
```

## 4. Register as MCP Server

```bash
claude mcp add -s user roundtable -- ~/.local/share/roundtable/bin/roundtable-mcp
```

If CLIs are installed in non-standard locations (nvm, Homebrew, Volta), pass their paths:

```bash
claude mcp add -s user \
  -e ROUNDTABLE_CLAUDE_PATH=$(which claude) \
  -e ROUNDTABLE_GEMINI_PATH=$(which gemini) \
  -e ROUNDTABLE_CODEX_PATH=$(which codex) \
  roundtable -- ~/.local/share/roundtable/bin/roundtable-mcp
```

Only include `-e` flags for CLIs that exist. The `$(which ...)` calls resolve the correct paths automatically.

## 5. Install Skill File (Optional)

```bash
mkdir -p ~/.claude/skills/roundtable
cp ~/.local/share/roundtable/SKILL.md ~/.claude/skills/roundtable/
```

## 6. Verify

Tell the user to restart Claude Code, then test with a tool call:

```
Use roundtable_hivemind to ask: "What is the best way to handle errors in async Elixir code?"
```

All five tools should now be available:

| Tool | Use |
|-|-|
| `roundtable_hivemind` | General multi-model consensus |
| `roundtable_deepdive` | Deep analysis / extended reasoning |
| `roundtable_architect` | Implementation planning |
| `roundtable_challenge` | Devil's advocate / stress-test |
| `roundtable_xray` | Architecture + code quality review |

## Default Agents (Optional)

To limit which CLIs run by default (saves cost/time), re-register with:

```bash
claude mcp remove roundtable
claude mcp add -s user \
  -e ROUNDTABLE_DEFAULT_AGENTS='[{"cli":"codex"},{"cli":"claude"}]' \
  roundtable -- ~/.local/share/roundtable/bin/roundtable-mcp
```

Per-call `agents` parameter always overrides defaults. See [SKILL.md](SKILL.md) for the full agent schema.

## Current Branch Testing: Go HTTP Phase 1

If you are testing the current `go-http-phase1` branch, there is now an experimental Go HTTP MCP server in front of the existing `roundtable-cli` backend.

Current branch architecture:

`Claude Code -> local HTTP MCP -> roundtable-http-mcp -> roundtable-cli -> CLIs`

Use this procedure to test the current branch state.

### 1. Install toolchains

```bash
mise install
```

### 2. Run backend and wrapper tests

```bash
mix test
mise exec go@1.26.2 -- env \
  GOTOOLCHAIN=local \
  GOMODCACHE=/tmp/gomodcache \
  GOCACHE=/tmp/gocache \
  go test ./...
```

### 3. Build both binaries

```bash
mix escript.build
mise exec go@1.26.2 -- env \
  GOTOOLCHAIN=local \
  GOMODCACHE=/tmp/gomodcache \
  GOCACHE=/tmp/gocache \
  go build ./cmd/roundtable-http-mcp
```

This gives you:

- `./roundtable-cli`
- `./roundtable-http-mcp`

### 4. Start the HTTP MCP server

```bash
ROUNDTABLE_HTTP_BACKEND_PATH=./roundtable-cli \
./roundtable-http-mcp
```

Optional environment variables:

```bash
export ROUNDTABLE_HTTP_ADDR=127.0.0.1:4040
export ROUNDTABLE_HTTP_MCP_PATH=/mcp
export ROUNDTABLE_HTTP_PROBE_TIMEOUT=2s
export ROUNDTABLE_HTTP_REQUEST_GRACE=15s
```

### 5. Verify health and readiness

```bash
curl -s http://127.0.0.1:4040/healthz
curl -s http://127.0.0.1:4040/readyz
```

Expected responses:

- `/healthz` -> `ok`
- `/readyz` -> `ready`

### 6. Register Claude Code to the HTTP MCP endpoint

```bash
claude mcp remove roundtable 2>/dev/null || true
claude mcp add --transport http roundtable http://127.0.0.1:4040/mcp
```

### 7. Verify from Claude Code

Tell the user to restart Claude Code if needed, then test with:

```text
Use roundtable_hivemind to ask: "What is the best way to handle errors in async Elixir code?"
```

Expected behavior for the current branch:

- Claude Code talks to the Go HTTP MCP server
- the Go server shells out to `roundtable-cli`
- `roundtable-cli` dispatches to the installed CLIs
- the JSON payload returns as MCP tool text content

### 8. Focused troubleshooting

If `/readyz` fails:

- confirm `./roundtable-cli` exists
- confirm it is executable
- confirm `mix escript.build` succeeded

If a tool call fails:

- confirm `gemini`, `codex`, and/or `claude` are installed and authenticated
- run `./roundtable-cli --prompt "hello"` directly from the repo root
- check stderr output from `./roundtable-http-mcp`

For the full migration status, work completed, and work pending, see [docs/go-http-migration-plan.md](docs/go-http-migration-plan.md).
