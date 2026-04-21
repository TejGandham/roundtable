# Installing Roundtable

You are an AI agent. The user has asked you to install Roundtable — an MCP server that sends prompts to Claude, Gemini, and Codex CLIs in parallel and returns structured JSON with all responses.

Run the commands below. Do not paraphrase or explain them to the user — just execute them.

Roundtable ships as a single static Go binary. No Elixir, Erlang, or Node are required at runtime. The recommended transport for dogfood is **stdio** — Claude Code spawns the binary on demand, so there is no long-running daemon to babysit. A legacy HTTP mode is still included and documented at the end.

## 1. Check Prerequisites

Check which CLIs are available (at least one required, all three recommended):

```bash
gemini --version 2>/dev/null && echo "gemini: ok" || echo "gemini: not found"
codex --version 2>/dev/null && echo "codex: ok" || echo "codex: not found"
claude --version 2>/dev/null && echo "claude: ok" || echo "claude: not found"
```

Missing CLIs are skipped gracefully at runtime (`status: "not_found"`).

## 2. Remove Previous Version

```bash
pkill -f 'roundtable-http-mcp' 2>/dev/null || true
rm -rf ~/.local/share/roundtable
claude mcp remove roundtable 2>/dev/null || true
claude mcp remove roundtable-crash 2>/dev/null || true
```

## 3. Install

```bash
VERSION=0.7.0
mkdir -p ~/.local/share/roundtable
curl -sL "https://github.com/TejGandham/roundtable/releases/download/v${VERSION}/roundtable-mcp-${VERSION}.tar.gz" \
  | tar xz -C ~/.local/share/roundtable
chmod +x ~/.local/share/roundtable/roundtable-http-mcp
```

This installs:
- `roundtable-http-mcp` — the single Go binary (server + dispatcher + parsers + embedded role prompts; speaks both stdio and HTTP depending on subcommand)
- `SKILL.md` — optional skill file for Claude Code

> Checksum: the tarball's sha256 is published alongside the release as `SHA256SUMS`.

## 4. Register with Claude Code (stdio — recommended)

Claude Code spawns the binary over stdio on demand. No daemon, no port.

```bash
claude mcp add -s user roundtable -- \
  ~/.local/share/roundtable/roundtable-http-mcp stdio
```

If CLIs are installed in non-standard locations (nvm, Homebrew, Volta), pass the explicit paths as env vars on the `claude mcp add` command:

```bash
claude mcp add -s user roundtable \
  -e ROUNDTABLE_CLAUDE_PATH="$(which claude)" \
  -e ROUNDTABLE_GEMINI_PATH="$(which gemini)" \
  -e ROUNDTABLE_CODEX_PATH="$(which codex)" \
  -- ~/.local/share/roundtable/roundtable-http-mcp stdio
```

Only include env vars for CLIs that exist. Optional environment variables recognised by the binary:

|Env Var|Default|Purpose|
|-|-|-|
|`ROUNDTABLE_HTTP_ROLES_DIR`|(embedded)|Override directory with custom role prompt files|
|`ROUNDTABLE_HTTP_PROJECT_ROLES_DIR`|(none)|Project-scoped role prompt directory|
|`ROUNDTABLE_DEFAULT_AGENTS`|(all 3)|JSON array of agents to run by default|
|`ROUNDTABLE_GEMINI_PATH`|`$PATH` lookup|Explicit path to the gemini CLI|
|`ROUNDTABLE_CODEX_PATH`|`$PATH` lookup|Explicit path to the codex CLI|
|`ROUNDTABLE_CLAUDE_PATH`|`$PATH` lookup|Explicit path to the claude CLI|
|`ROUNDTABLE_EXTRA_PATH`|(none)|Extra directories to search for CLI binaries|

## 5. Install Skill File (Optional)

```bash
mkdir -p ~/.claude/skills/roundtable
cp ~/.local/share/roundtable/SKILL.md ~/.claude/skills/roundtable/
```

## 6. Verify

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

Set `ROUNDTABLE_DEFAULT_AGENTS` at registration time:

```bash
claude mcp add -s user roundtable \
  -e ROUNDTABLE_DEFAULT_AGENTS='[{"cli":"codex"},{"cli":"claude"}]' \
  -- ~/.local/share/roundtable/roundtable-http-mcp stdio
```

Per-call `agents` parameter always overrides defaults. See [SKILL.md](SKILL.md) for the full agent schema.

## Troubleshooting (stdio)

Stdio servers are health-checked by being alive — if Claude Code can spawn the binary and exchange MCP initialize frames, it works.

If a tool call fails:
- Confirm the CLIs you requested are installed and authenticated
- Inspect the binary directly: `~/.local/share/roundtable/roundtable-http-mcp stdio </dev/null` should print MCP startup logs on stderr and exit (stdin closed)
- For richer per-session logs during Phase A dogfood, use the stderr-teeing wrapper at `scripts/roundtable-stdio-wrapper.sh` — it redirects stderr to `~/.local/share/roundtable/logs/stdio-<timestamp>-<pid>.log` while leaving stdin/stdout untouched

## Legacy HTTP Mode (optional)

HTTP mode is still bundled but will be removed in Phase C. Use it only when you need the `/healthz`, `/readyz`, or `/metricsz` endpoints for external monitoring.

### Start the HTTP server

```bash
nohup ~/.local/share/roundtable/roundtable-http-mcp > /tmp/roundtable.log 2>&1 &
curl -s http://127.0.0.1:4040/healthz  # expect: ok
curl -s http://127.0.0.1:4040/readyz   # expect: ready
```

Optional HTTP-only environment variables:

|Env Var|Default|Purpose|
|-|-|-|
|`ROUNDTABLE_HTTP_ADDR`|`127.0.0.1:4040`|Listen address|
|`ROUNDTABLE_HTTP_MCP_PATH`|`/mcp`|MCP endpoint path|

### Register as HTTP MCP server

```bash
claude mcp add --transport http roundtable http://127.0.0.1:4040/mcp
```

### HTTP monitoring

```bash
curl -s http://127.0.0.1:4040/metricsz
```

Returns JSON with `total_requests` and `dispatch_errors` atomic counters.

### HTTP troubleshooting

If `/healthz` doesn't respond:
- Check if the server is running: `pgrep -f roundtable-http-mcp`
- Check logs: `cat /tmp/roundtable.log`

If `/readyz` returns 503:
- Confirm at least one CLI (`gemini`, `codex`, or `claude`) is installed and on PATH
- Check logs for per-backend health messages

## Ollama Cloud provider

Roundtable v0.8+ supports Ollama's cloud-hosted `:cloud` models
(kimi-k2.6, qwen3.5, glm-5.1, minimax-m2.7, gpt-oss, etc.) over HTTPS.
Unlike the subprocess backends (claude/codex/gemini), this one has no
CLI binary — requests go directly to Ollama's REST API.

### Environment

| Variable | Required | Default | Purpose |
|-|-|-|-|
| `OLLAMA_API_KEY` | yes | — | Bearer token from https://ollama.com/settings/keys. If unset, the ollama backend is simply not registered. |
| `OLLAMA_BASE_URL` | no | `https://ollama.com` | Override for self-hosted Ollama or for tests. |
| `OLLAMA_DEFAULT_MODEL` | no | — | Fallback model used when an agent spec doesn't set `model`. Recommended: `kimi-k2.6:cloud` or `gpt-oss:120b-cloud`. |
| `OLLAMA_MAX_CONCURRENT_REQUESTS` | no | `3` | Per-process bulkhead on concurrent `/api/chat` calls. Match your Ollama account tier: **Free=`1`**, **Pro=`3`** (default), **Max=`10`**. Calls above the cap block until a slot frees instead of getting a 429 from Ollama's edge. Read once at startup; restart to change. |
| `OLLAMA_RESPONSE_HEADER_TIMEOUT` | no | `60s` | Max time to wait for Ollama's `/api/chat` to return response headers. With `stream=false` (our default) this is effectively the total-response time. Accepts any `time.Duration` string (`90s`, `2m`, `500ms`). Big-model generation on Pro tier can spike to 60s+ under upstream load; bump this if you hit `status=timeout` frequently. Read once at startup; restart to change. |

### Example: dispatching to one cloud model

```json
{
  "prompt": "Explain context-free grammars with a concrete example.",
  "agents": "[{\"cli\":\"ollama\",\"name\":\"kimi\",\"model\":\"kimi-k2.6:cloud\"}]"
}
```

### Example: `hivemind` with mixed providers

```json
{
  "prompt": "Review this design doc and flag risks.",
  "files": "docs/design.md",
  "agents": "[{\"cli\":\"claude\"},{\"cli\":\"gemini\"},{\"cli\":\"ollama\",\"name\":\"kimi\",\"model\":\"kimi-k2.6:cloud\"},{\"cli\":\"ollama\",\"name\":\"glm\",\"model\":\"glm-5.1:cloud\"}]"
}
```

### Known limitations (Apr 2026)

- **Concurrency cap**: Free tier allows 1 concurrent cloud model call, Pro $20/mo allows 3, Max $100/mo allows 10. Roundtable holds a per-process bulkhead sized by `OLLAMA_MAX_CONCURRENT_REQUESTS` (default 3) so a `hivemind` with more ollama agents than slots queues locally instead of getting silent 429s from Ollama's edge. If multiple `roundtable-http-mcp` processes share an API key, they can still collectively exceed the cap — run a single instance, or set each process's cap to a fraction of the tier total.
- **Output cap**: All `:cloud` models are capped at 16,384 completion tokens. When truncated, `done_reason=length` is surfaced in `metadata`.
- **503 storms**: Ollama Cloud is a preview service; 503s are treated as `rate_limited`. No `Retry-After` is currently published, and Roundtable does not auto-retry (but surfaces `Retry-After` on `metadata.retry_after` when present).
- **US-only inference**: not suitable for EU/GDPR-sensitive deployments.

## Development (Building from Source)

```bash
mise install
make build
make test
make run        # HTTP mode on 127.0.0.1:4040
make run-stdio  # stdio mode on stdin/stdout
```

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for architecture details.
