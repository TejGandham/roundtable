---
name: roundtable
description: >-
  Multi-model consensus MCP server. Call roundtable_hivemind, roundtable_deepdive, roundtable_architect,
  roundtable_challenge, or roundtable_xray directly — no Bash tool needed. Dispatches to Gemini, Codex,
  and Claude in parallel, then synthesizes. Commands: hivemind (consensus), deepdive (extended reasoning),
  architect (implementation plan), challenge (devil's advocate), xray (codebase architecture + code quality).
  Use this skill whenever the user wants a second opinion, consensus, validation, or external
  perspective on ANY technical decision — architecture reviews, design critiques, code quality
  checks, approach comparisons, sanity checks, tradeoff analysis, or stress-testing ideas.
  Triggers on: "roundtable", "second opinion", "what do others think", "consensus", "deep analysis",
  "think through", "explore tradeoffs", "compare approaches", "review my design", "sanity check",
  "validate this", "get feedback", "stress test", "critique", "poke holes", "devil's advocate",
  "review architecture", "analyze codebase", "what's wrong here", "implementation plan", "how to build".
  Also use when the user asks you to run something through multiple models or wants independent
  verification of a technical approach. Do NOT use for simple questions, pure code generation,
  or when user wants only Claude's opinion.
---

# Roundtable - Multi-Model Consensus

Roundtable is an **MCP server**. Call its tools directly — no Bash tool needed.

## Core Rule

1. Call the appropriate MCP tool (`roundtable_hivemind`, `roundtable_deepdive`, etc.)
2. Parse the JSON response
3. Synthesize all model responses into unified output

## Commands

| Command | MCP Tool | Role Guidance |
|-|-|-|
| **hivemind** | `roundtable_hivemind` | Ask the question directly |
| **deepdive** | `roundtable_deepdive` | Add: "Provide conclusions, assumptions, alternatives, and confidence level." |
| **architect** | `roundtable_architect` | Request: phases, dependencies, risks, milestones |
| **challenge** | `roundtable_challenge` | Prefix: "Act as critical reviewer. Find flaws, risks, weaknesses." |
| **xray** | `roundtable_xray` | Include `files`. Gemini analyzes architecture, Codex reviews code quality. |

## MCP Invocation (Primary)

Call MCP tools directly. No Bash tool, no binary path, no shell.

### Tool Parameters

| Parameter | Required | Description |
|-|-|-|
| `prompt` | Yes | The question or task |
| `files` | No | Comma-separated **relative** file paths for context |
| `timeout` | No | Seconds per CLI (default: 900). Don't lower unless the task is quick. |
| `gemini_model` | No | Override Gemini model |
| `codex_model` | No | Override Codex model |
| `claude_model` | No | Override Claude model (e.g., `sonnet`, `opus`) |
| `gemini_resume` | No | Gemini session ID or `latest` to continue a previous conversation |
| `codex_resume` | No | Codex session/thread ID or `last` to continue a previous conversation |
| `claude_resume` | No | Claude session ID to continue a previous conversation |
| `agents` | No | JSON array of agent configs for selective dispatch (see below) |

### Selective Agent Dispatch (`agents` parameter)

The `agents` parameter lets you control exactly which agents run, with what models, and in what roles. When provided, it replaces the default 3-agent dispatch. When omitted, all 3 CLIs run as before.

Each entry in the JSON array:

| Field | Required | Description |
|-|-|-|
| `cli` | Yes | Backend: `"gemini"`, `"codex"`, or `"claude"` |
| `name` | No | Result key in output (defaults to `cli` value; must be unique) |
| `model` | No | Model override for this agent |
| `role` | No | Role override: `"default"`, `"planner"`, `"codereviewer"` |
| `resume` | No | Session ID to continue a previous conversation |

**Examples:**

Skip Claude, only use Gemini and Codex:
```json
[{"cli": "gemini"}, {"cli": "codex"}]
```

Run two Codex instances with different models:
```json
[
  {"name": "fast", "cli": "codex", "model": "gpt-5.4"},
  {"name": "deep", "cli": "codex", "model": "gpt-5.3-codex"}
]
```

Mix models and roles for targeted review:
```json
[
  {"name": "arch", "cli": "gemini", "model": "gemini-2.5-pro", "role": "planner"},
  {"name": "review", "cli": "codex", "model": "gpt-5.4", "role": "codereviewer"},
  {"name": "sanity", "cli": "claude", "model": "sonnet", "role": "default"}
]
```

**Notes:**
- When `agents` is provided, per-tool model params (`gemini_model`, `codex_model`, `claude_model`) are ignored
- Agent names must be unique; `"meta"` is reserved
- The tool's default role applies unless overridden per-agent

### Default Agent Configuration

Set `ROUNDTABLE_DEFAULT_AGENTS` at MCP registration time to configure which agents run by default — so you don't specify them on every call. Uses the same JSON schema as the `agents` parameter above.

**Precedence** (highest to lowest):
1. Per-call `agents` parameter — **always wins**
2. `ROUNDTABLE_DEFAULT_AGENTS` env var — session default
3. Built-in default — all 3 CLIs (gemini, codex, claude)

> **You can always override defaults per-call.** Even if your defaults only include codex and claude, you can pass `agents: [{"cli": "gemini"}]` to get a gemini-only review.

**Examples:**

Only Codex and Claude by default:
```json
[{"cli": "codex"}, {"cli": "claude"}]
```

With model and role:
```json
[
  {"cli": "codex", "model": "o4-mini", "role": "codereviewer"},
  {"cli": "claude", "model": "sonnet"}
]
```

Role-based dispatch:
```json
[
  {"cli": "gemini", "role": "planner"},
  {"cli": "codex", "role": "codereviewer"}
]
```

**Notes:**
- Invalid env var JSON → warning logged, falls back to all 3 CLIs
- The `resume` field is ignored in defaults — session IDs are per-call only
- See [INSTALL.md](INSTALL.md) for registration examples (Claude Code, Codex, OpenCode).

### Per-Project Role Overrides

If a project has `.claude/roundtable/roles/<role>.txt`, pass the directory path via the `project_roles_dir` parameter. This lets projects customize planner/reviewer context for their domain.

## CLI Invocation (Secondary — scripting and standalone use)

For scripting, CI, or use outside an MCP-capable agent, the `roundtable-cli` escript provides the same functionality via flags.

```bash
~/.claude/skills/roundtable/roundtable-cli \
  --prompt "Your question here" \
  --role planner \
  --files src/auth.ts,src/middleware.ts \
  --timeout 300
```

Run from the **project root directory** (Gemini restricts file access to its cwd).

### CLI Parameters

| Flag | Required | Description |
|-|-|-|
| `--prompt` | Yes | The question or task |
| `--role` | No | Role for all CLIs: `default`, `planner`, `codereviewer` (default: `default`) |
| `--gemini-role` | No | Override role for Gemini only (for xray command) |
| `--codex-role` | No | Override role for Codex only (for xray command) |
| `--claude-role` | No | Override role for Claude only |
| `--files` | No | Comma-separated **relative** file paths for context |
| `--gemini-model` | No | Override Gemini model |
| `--codex-model` | No | Override Codex model |
| `--claude-model` | No | Override Claude model |
| `--codex-reasoning` | No | Codex reasoning effort: `xhigh`, `high`, `medium` |
| `--timeout` | No | Seconds per CLI (default: 900). The default is intentionally generous — LLM inference can take minutes, and Gemini retries 429s internally. **Do not set this flag unless you know the task is quick.** |
| `--gemini-resume` | No | Gemini session ID or `latest` to continue a previous conversation |
| `--codex-resume` | No | Codex session/thread ID or `last` to continue a previous conversation |
| `--claude-resume` | No | Claude session ID to continue a previous conversation |
| `--roles-dir` | No | Override global roles directory (default: skill's `roles/` dir) |
| `--project-roles-dir` | No | Project-local roles directory (checked first, falls back to global) |
| `--agents` | No | JSON string of agent configs for selective dispatch (same format as MCP `agents` param) |

## Output Format

Both MCP tools and the CLI return JSON with this structure:

```json
{
  "gemini": { "response": "...", "status": "ok|error|timeout|not_found|probe_failed", "session_id": "...", ... },
  "codex": { "response": "...", "status": "...", "session_id": "...", ... },
  "claude": { "response": "...", "status": "...", "session_id": "...", ... },
  "meta": { "gemini_role": "...", "codex_role": "...", "claude_role": "...", "files_referenced": [...] }
}
```

## Synthesis Template

After calling a roundtable tool, synthesize the results:

```
## [Command Name]

### Gemini
[response summary — key points only, not raw dump]

### Codex
[response summary — key points only, not raw dump]

### Claude
[response summary — key points only, not raw dump]
*(Note: Claude is both the synthesizer and a participant. Treat this as an independent perspective from a separate session.)*

### Synthesis
- **Agreement**: [shared conclusions]
- **Differences**: [divergent views with reasoning]
- **Recommendation**: [unified advice]
```

## Follow-up Conversations

Each response includes `session_id` fields — use these for follow-up rounds.

**First call** (MCP):
Call `roundtable_hivemind` with `prompt: "Review the auth architecture"` and `files: "src/auth.ts"`.

**Follow-up call** (MCP):
Call `roundtable_hivemind` with `prompt: "What about the token refresh edge case you mentioned?"`, `gemini_resume: "latest"`, `codex_resume: "last"`, and `claude_resume: "<session-id from previous response>"`.

- `gemini_resume: "latest"` resumes Gemini's most recent session
- `codex_resume: "last"` resumes Codex's most recent session
- You can also pass specific session IDs from the previous response's `session_id` fields
- Follow-up prompts still go through role prompt assembly

## Degradation Rules

- If one CLI has `status: "error"`, `"timeout"`, or `"probe_failed"`: synthesize with available response, note which was unavailable and why
- If one CLI has `status: "not_found"`: note it's not installed, synthesize with the other
- If both fail: report errors, do not attempt synthesis
- If `parse_error` is set: note the response may be incomplete but still usable
- Non-zero exit codes are automatically downgraded to `"error"` even if the parser found content

## Important: Gemini Workspace Constraint

Gemini CLI restricts file access to its current working directory. When using `files` (especially with `xray`):

1. Use **relative paths** in `files` (not absolute paths)
2. When using the CLI directly, run from the project root

This is a Gemini CLI constraint, not a roundtable issue. Codex does not have this limitation.

## Prompt Framing

The quality of roundtable output depends on prompt quality. Guidelines:

- **Be specific about what you want evaluated.** "Review this auth flow" is weaker than "Review the token refresh logic in auth.ts — is the race condition between concurrent refresh calls handled correctly?"
- **For xray**, list the files and state what you want each model to focus on.
- **For challenge**, state the proposal clearly before asking for critique — the models need something concrete to push back on.
- **Include constraints.** If there are non-negotiable requirements (compliance, latency budgets, existing API contracts), state them so the models don't waste time proposing alternatives that violate them.

## Mistakes to Avoid

| Mistake | Fix |
|-|-|
| Using Bash tool to call roundtable | Call MCP tools directly — no Bash needed |
| Running only one model | ALWAYS use roundtable (dispatches all 3 agents) |
| Dumping raw JSON responses | Summarize key points, find agreement/differences |
| Skipping synthesis | Synthesis IS the value — always include it |
| Using for simple questions | Only use when multi-model perspective adds value |
| Ignoring stderr/status | Check status fields — errors contain useful context |
| Using absolute file paths | Use relative paths from project root — Gemini can't read outside its workspace |
