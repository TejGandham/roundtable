---
name: roundtable
description: >-
  Multi-model consensus MCP server. Call the roundtable-canvass, roundtable-deliberate,
  roundtable-blueprint, roundtable-critique, or roundtable-crosscheck tools directly — no Bash
  tool needed. Dispatches to Gemini, Codex, and Claude CLIs in parallel by default, and to any
  configured OpenAI-compatible HTTP providers (Kimi, MiniMax, GLM, DeepSeek, etc.) that are
  registered via ROUNDTABLE_PROVIDERS. Returns every response as structured JSON for synthesis.
  Tools: roundtable-canvass (parallel panel query), roundtable-deliberate (structured deliberation
  with alternatives + confidence), roundtable-blueprint (implementation plan: phases, deps, risks,
  milestones), roundtable-critique (adversarial code/design review), roundtable-crosscheck (mixed
  roles across the panel — planner + codereviewer + generalist on one prompt).
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

1. Call the appropriate MCP tool (`roundtable-canvass`, `roundtable-deliberate`, etc.) on the `roundtable` server
2. Parse the JSON response
3. Synthesize all model responses into unified output

**Who's in the panel.** The default panel is the three built-in CLIs (Claude, Gemini, Codex).
Any OpenAI-compatible HTTP provider registered via `ROUNDTABLE_PROVIDERS` (Kimi, MiniMax, GLM,
DeepSeek, and so on) joins the panel too — so expect 3–N responses, not strictly 3. The exact
set is controlled by `ROUNDTABLE_DEFAULT_AGENTS` (panel default) or the per-call `agents`
parameter (override).

## Commands

|Command|MCP Tool|Role Guidance|
|-|-|-|
|**canvass**|`roundtable-canvass`|Ask the question directly. Each panelist answers independently under the default analyst role.|
|**deliberate**|`roundtable-deliberate`|Add: "Provide conclusions, assumptions, alternatives, and confidence level."|
|**blueprint**|`roundtable-blueprint`|Request: phases, dependencies, risks, milestones.|
|**critique**|`roundtable-critique`|Prefix: "Act as critical reviewer. Find flaws, risks, weaknesses."|
|**crosscheck**|`roundtable-crosscheck`|Include `files`. Gemini in planner role, Codex in codereviewer role, Claude as generalist, HTTP providers in default role — one prompt, mixed lenses.|

## MCP Invocation (Primary)

Call MCP tools directly. No Bash tool, no binary path, no shell.

### Tool Parameters

| Parameter | Required | Description |
|-|-|-|
|Parameter|Required|Description|
|-|-|-|
|`prompt`|Yes|The question or task|
|`files`|No|Comma-separated **relative** file paths for context|
|`timeout`|No|Seconds per CLI (default and max: 900). Lower only if the task is quick — the default is the ceiling.|
|`gemini_model`|No|Override Gemini model|
|`codex_model`|No|Override Codex model|
|`claude_model`|No|Override Claude model (e.g., `sonnet`, `opus`)|
|`gemini_resume`|No|Gemini session ID or `latest` to continue a previous conversation|
|`codex_resume`|No|Codex session/thread ID or `last` to continue a previous conversation|
|`claude_resume`|No|Claude session ID to continue a previous conversation|
|`agents`|No|**JSON-encoded string** describing selective dispatch (see below). Pass a string, not an array.|

### Selective Agent Dispatch (`agents` parameter)

The `agents` parameter lets you control exactly which agents run, with what models, and in what roles. When provided, it replaces the default 3-agent dispatch. When omitted, all 3 CLIs run as before.

**Important:** `agents` is passed as a JSON-encoded **string** (not a JSON array object). Serialize your array with `JSON.stringify` or the equivalent, then pass the resulting string value.

Each entry in the encoded array:

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
[{"provider": "gemini"}, {"provider": "codex"}]
```

Run two Codex instances with different models:
```json
[
  {"name": "fast", "provider": "codex", "model": "gpt-5.4"},
  {"name": "deep", "provider": "codex", "model": "gpt-5.3-codex"}
]
```

Mix models and roles for targeted review:
```json
[
  {"name": "arch", "provider": "gemini", "model": "gemini-2.5-pro", "role": "planner"},
  {"name": "review", "provider": "codex", "model": "gpt-5.4", "role": "codereviewer"},
  {"name": "sanity", "provider": "claude", "model": "sonnet", "role": "default"}
]
```

**Notes:**
- Per-agent `model` wins over the per-tool `gemini_model` / `codex_model` / `claude_model` params. If an agent entry omits `model`, the matching per-tool param (if any) is used as a fallback.
- Agent names must be unique; `"meta"` is reserved.
- The tool's default role applies unless overridden per-agent.

### Default Agent Configuration

Set `ROUNDTABLE_DEFAULT_AGENTS` at MCP registration time to configure which agents run by default — so you don't specify them on every call. Uses the same JSON schema as the `agents` parameter above.

**Precedence** (highest to lowest):
1. Per-call `agents` parameter — **always wins**
2. `ROUNDTABLE_DEFAULT_AGENTS` env var — session default
3. Built-in default — all 3 CLIs (gemini, codex, claude)

> **You can always override defaults per-call.** Even if your defaults only include codex and claude, you can pass `agents: [{"provider": "gemini"}]` to get a gemini-only review.

**Examples:**

Only Codex and Claude by default:
```json
[{"provider": "codex"}, {"provider": "claude"}]
```

With model and role:
```json
[
  {"provider": "codex", "model": "o4-mini", "role": "codereviewer"},
  {"provider": "claude", "model": "sonnet"}
]
```

Role-based dispatch:
```json
[
  {"provider": "gemini", "role": "planner"},
  {"provider": "codex", "role": "codereviewer"}
]
```

**Notes:**
- Invalid env var JSON → warning logged, falls back to all 3 CLIs
- The `resume` field is ignored in defaults — session IDs are per-call only
- See [INSTALL.md](INSTALL.md) for registration instructions.

### Per-Project Role Overrides

To use project-scoped role prompts, place them at `<project>/.claude/roundtable/roles/<role>.txt` and start the server with `ROUNDTABLE_HTTP_PROJECT_ROLES_DIR=<path>` set. There is no per-call parameter for this — project roles are resolved at server startup.

The role lookup order is: project dir → global dir (`ROUNDTABLE_HTTP_ROLES_DIR`) → embedded defaults shipped in the binary.

## Output Format

MCP tool calls return JSON with this structure:

```json
{
  "gemini": { "response": "...", "status": "ok|error|timeout|terminated|not_found|probe_failed|rate_limited", "session_id": "...", ... },
  "codex": { "response": "...", "status": "...", "session_id": "...", ... },
  "claude": { "response": "...", "status": "...", "session_id": "...", ... },
  "meta": { "gemini_role": "...", "codex_role": "...", "claude_role": "...", "files_referenced": [...], "total_elapsed_ms": 0 }
}
```

Possible `status` values:
- `ok` — normal response
- `error` — backend returned an error payload or non-zero exit
- `timeout` — backend exceeded the per-CLI deadline
- `terminated` — backend killed by signal
- `not_found` — CLI binary not on PATH
- `probe_failed` — `--version` probe failed
- `rate_limited` — provider rate-limited the request (Gemini detects 429/RESOURCE_EXHAUSTED/quota)

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
Call `roundtable-canvass` with `prompt: "Review the auth architecture"` and `files: "src/auth.ts"`.

**Follow-up call** (MCP):
Call `roundtable-canvass` with `prompt: "What about the token refresh edge case you mentioned?"`, `gemini_resume: "latest"`, `codex_resume: "last"`, and `claude_resume: "<session-id from previous response>"`.

- `gemini_resume: "latest"` resumes Gemini's most recent session
- `codex_resume: "last"` resumes Codex's most recent session
- You can also pass specific session IDs from the previous response's `session_id` fields
- Follow-up prompts still go through role prompt assembly

## Degradation Rules

- If one CLI has `status: "error"`, `"timeout"`, `"terminated"`, or `"probe_failed"`: synthesize with the available responses, note which was unavailable and why.
- If one CLI has `status: "not_found"`: note it's not installed, synthesize with the others.
- If one CLI has `status: "rate_limited"`: tell the user the provider rate-limited the request and suggest retrying or resuming that session.
- If all CLIs fail: report errors, do not attempt synthesis.
- If `parse_error` is set: note the response may be incomplete but still usable.
- Non-zero exit codes are automatically downgraded to `"error"` even if the parser found content.

## Important: Gemini Workspace Constraint

Gemini CLI restricts file access to its current working directory. When using `files` (especially with `roundtable-crosscheck`):

1. Use **relative paths** in `files` (not absolute paths).
2. Gemini inherits its cwd from `roundtable`, which in turn inherits it from the MCP client that fork/exec'd it (Claude Code, etc.). Invoke your MCP client from the project root so Gemini can see the files.

This is a Gemini CLI constraint, not a roundtable issue. Codex and Claude do not have this limitation.

## Prompt Framing

The quality of roundtable output depends on prompt quality. Guidelines:

- **Be specific about what you want evaluated.** "Review this auth flow" is weaker than "Review the token refresh logic in auth.ts — is the race condition between concurrent refresh calls handled correctly?"
- **For roundtable-crosscheck**, list the files and state what you want each model to focus on.
- **For roundtable-critique**, state the proposal clearly before asking for critique — the models need something concrete to push back on.
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
