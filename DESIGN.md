# Roundtable Skill Design

**Date:** 2026-03-28
**Status:** Historical — implementation diverged from this design (Node.js -> Elixir/OTP MCP server)
**Replaces:** zen skill (PAL/clink MCP dependency)
**Baseline:** PAL clink (full source analysis of TejGandham/pal-mcp-server)

> **Note:** This document captures the original design intent. The actual implementation uses
> Elixir/OTP with Hermes MCP (not Node.js), ships as a BEAM release (not an escript),
> and includes a Platform module for cross-platform process management. See README.md
> for current architecture.

## Problem

The zen skill depends on PAL MCP server (`mcp__pal__clink`) which isn't always available. The underlying CLIs (`gemini`, `codex`) are installed and work reliably when invoked directly. We need a self-contained Claude Code skill that provides the same multi-model consensus workflow without MCP infrastructure.

## Clink Baseline → Roundtable Delta

Roundtable is clink's architecture reimplemented as a Claude Code skill in Node.js. This section maps every clink subsystem to its roundtable equivalent, with explicit rationale for every change.

### What stays the same (proven, keep)

| Clink subsystem | Roundtable equivalent | Why keep |
|-|-|-|
| Prompt assembly: `[system_prompt] + [guidance] + [=== USER REQUEST ===] + [=== FILE REFERENCES ===]` | Same section-delimited plain text format | Clean, debuggable, both CLIs handle it well |
| File references (path + size), not file contents — CLIs read files themselves | Same: pass paths, let CLIs use their own tools | Avoids token waste, avoids ARG_MAX concerns, CLIs do it better |
| Role system with `.txt` prompt files per role (default, planner, codereviewer) | Same: text files, per-role, resolved from disk | Simple, editable, no compilation step |
| Internal args hardcoded (`-o json`, `exec --json`) — guarantees parseable output | Same: output format flags are not configurable | Prevents users from breaking the parser contract |
| Config args additive (layer on top of internal args, never replace) | Same: `--yolo`, `--dangerously-bypass-approvals-and-sandbox` are additive flags | Clink's 3-layer merge is sound for this |
| `<SUMMARY>` tag protocol in system prompts | Keep in role prompts, but don't enforce truncation at roundtable level | The protocol is useful for Claude's synthesis. Roundtable doesn't have MCP's 20k char limit. |
| Error recovery per CLI: Gemini parses JSON error blocks from stderr; Codex tries JSONL parse even on non-zero exit | Same recovery strategy in Node.js parsers | Both CLIs produce useful output on failure — don't discard it |

### What changes (with rationale)

| Clink decision | Problem | Roundtable change | Rationale |
|-|-|-|-|
| Stdin prompt delivery (`process.communicate`) | Codex `exec` does NOT read stdin (open issue openai/codex#1123). PAL's stdin works despite this, not because of it. | **Positional args** for both CLIs | Only delivery method Codex supports. Gemini supports both; positional is primary documented path. ARG_MAX (~1MB macOS) is well above typical roundtable prompts (10-50KB). |
| MCP server + Pydantic models + asyncio | Requires running PAL server, Python runtime, pip packages | **Single Node.js script** using only built-ins (`child_process`, `readline`, `fs`) | Claude Code already has Node.js. Zero additional dependencies. |
| Registry singleton with 3-tier config search (built-in → env → `~/.pal/cli_clients/`) | Over-engineered for 2 CLIs with known, stable configs | **Convention-based**: skill directory + project override directory | Roundtable serves one consumer (Claude Code) with two CLIs. No need for a dynamic registry. |
| 1800s default timeout | 30 minutes is too long for a consensus query | **120s default** (configurable via `--timeout`) | Roundtable is interactive — if a CLI takes >2 min, something is wrong |
| Claude CLI as a participant (`runner: "claude"`, `--append-system-prompt`) | Claude is the orchestrator in roundtable, not a participant | **Added as third agent. Claude Code is both orchestrator and a participant in a separate session.** | Fundamental architectural difference — Claude participates via a separate `claude` CLI invocation, keeping its perspective independent from the orchestrating session |
| Images parameter | Accepted but silently ignored in clink | **Don't include what you won't use** | Honest API surface |
| Output file capture (`flag_template`, tempfile dance) | Complex mechanism for CLIs that write JSON to files instead of stdout | **Remove** — both Gemini and Codex write JSON to stdout | Neither target CLI needs this |
| 20k char output limit + `<SUMMARY>` extraction + metadata pruning | MCP response size constraint | **No hard limit** — pass full output to Claude for synthesis | Roundtable returns to Claude Code directly, no MCP envelope constraint |
| `_agent_capabilities_guidance()` boilerplate | Hardcoded Gemini-specific text about "operating through Gemini CLI agent" | **Per-CLI capabilities text** embedded in role prompts, not hardcoded | Different CLIs have different capabilities. Put it in the prompt file where it's visible and editable. |

## Architecture

### 1. Node.js CLI dispatcher

**Why Node.js:**
- Proper JSONL line-by-line parsing via `readline` (Codex emits JSONL, not single JSON)
- `child_process.spawn` with separated stdout/stderr — no corruption from interleaving
- `Promise.allSettled` for parallel execution with independent failure handling
- Process group cleanup on SIGINT/SIGTERM — no orphaned CLI processes
- Portable across macOS/Linux without BSD vs GNU flag differences
- Zero npm packages — Node built-ins only

**Invocation:**

```
./roundtable \
  --prompt "Review this auth flow" \
  --role planner \
  --files src/auth.ts,src/middleware.ts \
  --gemini-model gemini-2.5-pro \
  --codex-model gpt-5.4 \
  --timeout 120
```

**CLI execution (internal args are hardcoded, config args are additive):**

| CLI | Internal args (hardcoded) | Config args (additive) | Prompt delivery |
|-|-|-|-|
| Gemini | `-o json` | `--yolo -m <model>` | `-p "<assembled prompt>"` |
| Codex | `exec --json` | `--dangerously-bypass-approvals-and-sandbox` | `"<assembled prompt>"` (positional) |
| Claude | `-p --output-format json --dangerously-skip-permissions` | (additive) `--model <model>` | positional arg |

**Prompt assembly (matches clink's `_prepare_prompt_for_role`):**

```
<contents of roles/<role>.txt>

=== REQUEST ===
<user's question>

=== FILES ===
- src/auth.ts (4523 bytes)
- src/middleware.ts (2187 bytes)

Review the files listed above using your own tools to read their contents.
```

Files are **referenced, not embedded** — matching clink's `_format_file_references()`. Each entry shows path and size. If `stat()` fails: `- src/missing.ts (unavailable)`. The target CLI reads files using its own filesystem tools.

**Output contract:**

```json
{
  "gemini": {
    "response": "Model's response text",
    "model": "gemini-2.5-pro",
    "status": "ok",
    "exit_code": 0,
    "stderr": "",
    "elapsed_ms": 8432,
    "parse_error": null,
    "truncated": false
  },
  "codex": {
    "response": "Model's response text",
    "model": "gpt-5.4",
    "status": "ok",
    "exit_code": 0,
    "stderr": "",
    "elapsed_ms": 12105,
    "parse_error": null,
    "truncated": false
  },
  "claude": {
    "response": "Model's response text",
    "model": "claude-sonnet-4-5",
    "status": "ok",
    "exit_code": 0,
    "stderr": "",
    "elapsed_ms": 9871,
    "parse_error": null,
    "truncated": false
  },
  "meta": {
    "total_elapsed_ms": 12105,
    "role": "planner",
    "claude_role": "planner",
    "files_referenced": ["src/auth.ts", "src/middleware.ts"]
  }
}
```

Status values: `ok`, `error`, `timeout`, `not_found` (CLI missing)

**Output parsing (ported from clink's parsers):**

| Parser | Source | Logic |
|-|-|-|
| Gemini | clink `gemini_json` parser | `JSON.parse(stdout)` → extract `response` field. Capture `stats.models.<name>.tokens` for metadata. Detect 429 rate limits from `error` field. |
| Codex | clink `codex_jsonl` parser | Line-by-line JSONL via `readline`. Collect `item.completed` events where `item.type === "agent_message"` → extract `item.text`. Collect `turn.completed` for usage stats. Collect `error` events. Join all agent messages with `\n\n`. |
| Claude | JSON parser | `JSON.decode(stdout)` → extract `result` field. Check `is_error` bool. Extract `modelUsage` keys for model name. |

**Error handling (matches clink's error paths):**

| Error | Clink approach | Roundtable approach |
|-|-|-|
| CLI not found | `shutil.which()` → CLIAgentError | `which` check before spawn → `status: "not_found"` |
| Timeout | `asyncio.wait_for` → kill → CLIAgentError | `setTimeout` → kill process group → `status: "timeout"` |
| Non-zero exit | `_recover_from_error()` hook tries parsing anyway | Same: attempt parse on non-zero exit. If parse succeeds, use it (Codex strategy). If Gemini, check stderr for JSON error blocks. |
| Parse failure | ParserError → CLIAgentError | Capture raw stdout in `response`, set `parse_error` with details |
| Gemini 429 | Detected in parser, synthetic response | Detected in parsed `error` field, `status: "error"`, message preserved |

**Process management:**

- Both CLIs spawned simultaneously via `child_process.spawn` with `{ stdio: ['pipe', 'pipe', 'pipe'] }`
- Each gets independent stdout/stderr buffers
- `Promise.allSettled` waits for both — one failure doesn't abort the other
- SIGINT/SIGTERM handler sends SIGTERM to both process groups
- Output size cap: 1MB per CLI stdout (configurable), sets `truncated: true` if exceeded

### 2. `SKILL.md` — Claude Code skill instructions

**Commands:**

| Command | Role | Prompt guidance |
|-|-|-|
| **hivemind** | default | Ask the question directly |
| **deepdive** | planner | Request: conclusions, assumptions, alternatives, confidence level |
| **architect** | planner | Request: phases, dependencies, risks, milestones |
| **challenge** | codereviewer | Find flaws, risks, weaknesses. Rate by severity. |
| **xray** | gemini=planner, codex=codereviewer | Include file paths. Architecture + code quality. |

**Synthesis template:**

```
## [Command Name]

### Gemini
[response summary — key points only, not raw dump]

### Codex
[response summary — key points only, not raw dump]

### Synthesis
- **Agreement**: [shared conclusions]
- **Differences**: [divergent views with reasoning]
- **Recommendation**: [unified advice]
```

**Degradation rules:**
- If one CLI fails: synthesize with available response, note which model was unavailable and why (from `status` + `stderr`)
- If both fail: report errors, do not attempt synthesis
- If `parse_error` is set: note that the response may be incomplete

### 3. Role System Prompts

**Resolution order (first match wins) — simplified from clink's 3-tier:**
1. Project-local: `<project>/.claude/roundtable/roles/<role>.txt`
2. Global: `~/.claude/skills/roundtable/roles/<role>.txt`

**Built-in roles (adapted from clink's `systemprompts/clink/`):**

**`default.txt`:**
```
You are a senior software engineer providing technical analysis.
Be concise. Lead with your conclusion, then supporting evidence.
If your response would exceed 500 words, emit a <SUMMARY>...</SUMMARY> block
at the end with the key points in under 200 words.
```

**`planner.txt`:**
```
You are a senior software architect designing systems.
Consider edge cases and tradeoffs.
Structure your response as: conclusion, assumptions, alternatives, confidence level.
If your response would exceed 500 words, emit a <SUMMARY>...</SUMMARY> block
at the end with the key points in under 200 words.
```

**`codereviewer.txt`:**
```
You are a senior code reviewer focused on correctness and maintainability.
Find flaws, risks, and weaknesses. Rate findings by severity (critical, high, medium, low).
If your response would exceed 500 words, emit a <SUMMARY>...</SUMMARY> block
at the end with the key points in under 200 words.
```

The `<SUMMARY>` protocol is retained from clink's system prompts. Roundtable doesn't enforce truncation, but Claude can use the summary block for efficient synthesis when responses are long.

### 4. File Context

**Matching clink's `_format_file_references()`:**

Files are passed as references with metadata. The CLIs read file contents using their own tools (Gemini has file reading, Codex has `cat`/read access).

```
=== FILES ===
- src/auth.ts (4523 bytes)
- src/middleware.ts (2187 bytes)
- src/missing.ts (unavailable)

Review the files listed above using your own tools to read their contents.
```

This avoids embedding file contents in the prompt, which would:
- Waste tokens (CLIs would re-tokenize content they could read natively)
- Hit ARG_MAX limits with large files
- Bypass CLI-native context optimizations (caching, etc.)

## File Layout

```
~/.claude/skills/roundtable/
├── SKILL.md              # Claude Code skill instructions
├── roundtable            # CLI dispatcher (escript binary)
├── lib/roundtable/cli/
│   ├── gemini.ex         # Gemini CLI runner
│   ├── codex.ex          # Codex CLI runner
│   └── claude.ex         # Claude CLI runner
└── roles/
    ├── default.txt       # Default role prompt
    ├── planner.txt       # Planner/architect role prompt
    └── codereviewer.txt  # Code reviewer role prompt
```

Per-project overrides (optional):
```
<project>/.claude/roundtable/roles/
├── planner.txt           # Project-specific planner context
└── codereviewer.txt      # Project-specific reviewer context
```

## Robustness Contract

**Roundtable depends on these CLI contracts (same as clink):**
1. Gemini: `-p` (prompt flag), `-o json` (output format) → `{ response }` JSON shape
2. Codex: `exec` subcommand, `--json` flag → JSONL with `item.completed` / `agent_message` events
3. Claude: `-p` (print/non-interactive flag), `--output-format json` → `{ "result", "is_error", "session_id" }` JSON shape

**Roundtable does NOT break if:**
- Models change behavior or quality
- New CLI flags are added (additive changes)
- Auth methods change (handled by CLI, not roundtable)
- New event types appear in JSONL streams (ignored by parser)
- CLI versions update (as long as contracts above hold)

## Migration

1. Install roundtable skill to `~/.claude/skills/roundtable/`
2. Zen skill remains functional (PAL dependency, different trigger)
3. Roundtable triggers on: "roundtable", "second opinion", "consensus", "what do others think"
4. Once validated, zen can be retired and PAL/clink dependency dropped

## Dependencies

- **Runtime:** Node.js (already installed for Claude Code)
- **CLIs:** `gemini` (Homebrew), `codex` (npm global)
- **No npm packages** — uses only Node built-ins (`child_process`, `readline`, `fs`, `path`, `os`)
