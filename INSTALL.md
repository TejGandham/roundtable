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
```

## 3. Install

Releases ship one tarball per platform (`darwin-arm64`, `linux-amd64`), plus a single `SHA256SUMS` file covering all of them. The snippet below detects your platform, verifies the checksum, extracts to `~/.local/share/roundtable`, and aliases the arch-suffixed binary to the canonical name `roundtable-http-mcp` that the rest of this guide assumes.

```bash
VERSION=0.8.0

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"      # darwin | linux
case "$(uname -m)" in
  arm64|aarch64) ARCH=arm64 ;;
  x86_64|amd64)  ARCH=amd64 ;;
  *) echo "unsupported arch: $(uname -m)"; return 1 2>/dev/null || exit 1 ;;
esac
ASSET="roundtable-${VERSION}-${OS}-${ARCH}.tar.gz"
BASE="https://github.com/TejGandham/roundtable/releases/download/v${VERSION}"

mkdir -p ~/.local/share/roundtable
cd ~/.local/share/roundtable

curl -fsSLO "${BASE}/${ASSET}"
curl -fsSLO "${BASE}/SHA256SUMS"
grep "  ${ASSET}$" SHA256SUMS | shasum -a 256 -c -

tar xzf "${ASSET}"
rm -f "${ASSET}"
ln -sf "roundtable-http-mcp-${OS}-${ARCH}" roundtable-http-mcp
chmod +x "roundtable-http-mcp-${OS}-${ARCH}"

# macOS only: strip the quarantine attribute so Gatekeeper doesn't block the
# unsigned binary on first launch. No-op on Linux.
[ "$OS" = "darwin" ] && xattr -d com.apple.quarantine "roundtable-http-mcp-${OS}-${ARCH}" 2>/dev/null || true
```

This installs:
- `roundtable-http-mcp-${OS}-${ARCH}` — the single Go binary (server + dispatcher + parsers + embedded role prompts; speaks both stdio and HTTP depending on subcommand)
- `roundtable-http-mcp` — symlink to the above so commands in the rest of this guide stay platform-agnostic
- `SKILL.md` — optional skill file for Claude Code

Supported platforms: `darwin-arm64` (Apple Silicon — M1/M2/M3/M4) and `linux-amd64`. Intel Macs and Linux arm64 are not currently released; build from source via `make build`.

> **Note for post-rename releases:** starting with the first release after the `roundtable-http-mcp` → `roundtable` binary rename, tarballs will contain `roundtable-${OS}-${ARCH}` instead of `roundtable-http-mcp-${OS}-${ARCH}`. Update the symlink target and the `chmod`/`xattr` lines accordingly when bumping `VERSION` past that release.

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
  -e ROUNDTABLE_DEFAULT_AGENTS='[{"provider":"codex"},{"provider":"claude"}]' \
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

Returns JSON with scalar counters (`total_requests`, `dispatch_errors`),
per-provider-per-model request counters (`roundtable_provider_requests_total`
keyed as `provider/model/status`), duration histograms
(`roundtable_provider_request_duration_ms_sum`,
`roundtable_provider_request_duration_ms_count` keyed as `provider/model`),
and the registered provider list (`roundtable_providers_registered`).

### HTTP troubleshooting

If `/healthz` doesn't respond:
- Check if the server is running: `pgrep -f roundtable-http-mcp`
- Check logs: `cat /tmp/roundtable.log`

If `/readyz` returns 503:
- Confirm at least one CLI (`gemini`, `codex`, or `claude`) is installed and on PATH
- Check logs for per-backend health messages

## Providers (HTTP)

Roundtable dispatches to OpenAI-compatible HTTP providers declared in the
`ROUNDTABLE_PROVIDERS` environment variable — a JSON array where each
entry names one provider (Fireworks, Moonshot, z.ai, DeepSeek, Groq, etc.).

### Minimal example

Set both the provider-specific secret env vars **and** the
`ROUNDTABLE_PROVIDERS` JSON blob in your MCP server env:

```bash
claude mcp add -s user roundtable \
  -e FIREWORKS_API_KEY="fw_..." \
  -e MOONSHOT_API_KEY="sk-..." \
  -e ZAI_API_KEY="sk-..." \
  -e ROUNDTABLE_PROVIDERS='[{"id":"fireworks-kimi","base_url":"https://api.fireworks.ai/inference/v1","api_key_env":"FIREWORKS_API_KEY","default_model":"accounts/fireworks/models/kimi-k2p6","max_concurrent":5},{"id":"fireworks-minimax","base_url":"https://api.fireworks.ai/inference/v1","api_key_env":"FIREWORKS_API_KEY","default_model":"accounts/fireworks/models/minimax-m2p7","max_concurrent":5},{"id":"moonshot","base_url":"https://api.moonshot.cn/v1","api_key_env":"MOONSHOT_API_KEY","default_model":"kimi-k2-0711-preview","max_concurrent":5},{"id":"zai","base_url":"https://api.z.ai/v1","api_key_env":"ZAI_API_KEY","default_model":"glm-4.6","max_concurrent":3}]' \
  -- ~/.local/share/roundtable/roundtable-http-mcp stdio
```

Two Fireworks entries share a single `FIREWORKS_API_KEY` but are
registered under distinct ids (`fireworks-kimi`, `fireworks-minimax`)
because Fireworks multiplexes many open-weight models under one API —
splitting by provider id gives independent concurrency budgets and
per-model metric labels.

### Fields

| Field | Required | Description |
|-|-|-|
| `id` | yes | Operator-chosen identifier. Must not collide with the built-in subprocess ids `gemini`, `codex`, or `claude`. Also cannot duplicate another `id` within the same array. |
| `base_url` | yes | Root URL; `/chat/completions` is appended at request time. |
| `api_key_env` | yes | Name of the env var holding the secret. The secret itself is **not** in this JSON — this indirection lets you rotate a key by updating the secret env var without re-encoding `ROUNDTABLE_PROVIDERS`, and keeps the blob safe to paste in bug reports. |
| `default_model` | no | Used when `AgentSpec.Model` is empty. |
| `max_concurrent` | no (default `3`) | Per-process concurrency cap (semaphore). Size this to match the provider's tier: Fireworks defaults to generous rate limits, Moonshot varies by account, etc. Check your provider's dashboard. |
| `response_header_timeout` | no (default `"60s"`) | `http.Transport.ResponseHeaderTimeout`. With `stream: false` (always, for now) this effectively caps **total** response time — raise for slow providers running long-context deepdives. Accepts any `time.Duration` string (`90s`, `2m`, `500ms`). |
| `gate_slow_log_threshold` | no (default `"100ms"`) | Wait above which the concurrency-gate `Acquire` emits a debug log. Useful for operators tuning `max_concurrent`. |

### Agent-spec JSON examples

Target one registered provider:

```json
[{"name":"kimi-moonshot","provider":"moonshot","model":"kimi-k2-0711-preview"}]
```

Fan out across multiple providers in one dispatch (e.g., compare kimi vs minimax on Fireworks, plus moonshot):

```json
[
  {"provider":"gemini"},
  {"provider":"codex"},
  {"provider":"claude"},
  {"provider":"fireworks-kimi","name":"kimi"},
  {"provider":"fireworks-minimax","name":"minimax"},
  {"provider":"moonshot","model":"kimi-k2-0711-preview","name":"kimi-moonshot"}
]
```

### Defaults

**HTTP providers are never in the default agent set** (which remains
`gemini + codex + claude`). To use them, either include an explicit
`agents` JSON on the request, or override the defaults at the operator
level via `ROUNDTABLE_DEFAULT_AGENTS`. The invariant is codified as
`TestDefaultAgents_ExcludesAllHTTPProviders` — adding an HTTP provider
to the default set breaks the build.

### Fail-loud parsing

A single missing comma in `ROUNDTABLE_PROVIDERS` disables **every**
HTTP provider for that process. This is deliberate: silent partial
registration (some providers succeed, one is dropped on parse failure)
would be worse because a subsequent dispatch against the missing
provider would return `not_found` with no indication that a config
parse failure caused the absence. The startup logs tell you what
happened:

- `INFO provider registered id=... base_url=... default_model=... max_concurrent=...` — one line per successfully registered provider.
- `WARN provider skipped — credential env var unset id=... api_key_env=...` — credentials missing; FR-3 skip. Callers see `not_found` per-agent; `/readyz` stays green.
- `ERROR ROUNDTABLE_PROVIDERS parse failed; no HTTP providers registered error=...` — JSON-level issue. Only subprocess backends register.

### Secret rotation

Because `api_key_env` names an env var (rather than embedding the
secret in the blob), rotating a key means updating a single env var.
The value is read via `os.Getenv` at request time, so the new key
takes effect immediately without restarting Roundtable.

### Enumerating registered providers

`/metricsz` (HTTP mode only) includes a `roundtable_providers_registered`
field listing each registered provider's `id`, `base_url`, and
`default_model` — a machine-readable enumeration surface for operators
writing dashboards or deploy checks.

Metric keys use `|` as the tuple delimiter:
`roundtable_provider_requests_total` is keyed `provider|model|status`
and `roundtable_provider_request_duration_ms_sum` is keyed
`provider|model`. Slashes are not used because real model ids (e.g.
`accounts/fireworks/models/kimi-k2p6`) contain them. Provider ids
containing `/`, `|`, or whitespace are rejected at load time.

A special model label, **`_other`**, appears when a provider's observed
distinct-model count exceeds an internal cap (32) or when a client
sends a model label longer than 128 characters. This is a
cardinality-DoS guard — see FR-28. Dashboards should treat keys like
`provider|_other|status` as a bucket containing the overflow.

### Known limitations (Apr 2026)

- **Output truncation**: When a response's `finish_reason` is `length`, `output_truncated: true` is set on `metadata` along with the raw `finish_reason`. Callers can check this generically without knowing any provider's conventions.
- **Jurisdictional note**: Fireworks is US-hosted; Moonshot (CN) and z.ai (CN) have their own terms and jurisdictional profiles. Read the provider's terms before sending regulated data.
- **Rate limits surface as `rate_limited`**: 429 and 503 from any provider map to `status: "rate_limited"` with `Retry-After` surfaced on `metadata.retry_after` when the header is present. No auto-retry is performed.

## Development (Building from Source)

```bash
mise install
make build
make test
make run        # HTTP mode on 127.0.0.1:4040
make run-stdio  # stdio mode on stdin/stdout
```

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for architecture details.
