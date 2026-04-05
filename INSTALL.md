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
