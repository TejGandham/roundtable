---
name: roundtable
description: >-
  Multi-model consensus tools backed by the Roundtable MCP server. Call roundtable-canvass,
  roundtable-deliberate, roundtable-blueprint, roundtable-critique, roundtable-crosscheck, or
  roundtable-converge directly — no Bash tool needed. The Pi package exposes native tools through
  its package-owned stdio bridge. Dispatches to Antigravity, Copilot, Codex, and Claude CLIs in parallel by default,
  and to any configured OpenAI-compatible HTTP providers (Kimi, MiniMax, GLM, DeepSeek, etc.) that are
  registered via ROUNDTABLE_PROVIDERS. Returns every response as structured JSON for synthesis.
  Tools: roundtable-canvass (parallel panel query), roundtable-deliberate (structured deliberation
  with alternatives + confidence), roundtable-blueprint (implementation plan: phases, deps, risks,
  milestones), roundtable-critique (adversarial code/design review), roundtable-crosscheck (mixed
  roles across the panel — planner + codereviewer + generalist on one prompt),
  roundtable-converge (replay a prior dispatch — each panelist sees the other panelists' answers
  under redacted peer-N aliases and produces an (a) hold-or-revise stance, (b) agreement list,
  (c) draft converged recommendation).
  Use this skill whenever the user wants a second opinion, consensus, validation, or external
  perspective on ANY technical decision — architecture reviews, design critiques, code quality
  checks, approach comparisons, sanity checks, tradeoff analysis, or stress-testing ideas.
  Triggers on: "roundtable", "second opinion", "what do others think", "consensus", "deep analysis",
  "think through", "explore tradeoffs", "compare approaches", "review my design", "sanity check",
  "validate this", "get feedback", "stress test", "critique", "poke holes", "devil's advocate",
  "review architecture", "analyze codebase", "what's wrong here", "implementation plan", "how to build",
  "converge", "second round", "synthesize the panel", "after seeing the other answers".
  Also use when the user asks you to run something through multiple models or wants independent
  verification of a technical approach. Do NOT use for simple questions, pure code generation,
  or when user wants only Claude's opinion.
---

# Roundtable - Multi-Model Consensus

Roundtable is backed by an **MCP server**. Call its tools directly — no Bash tool needed. Claude Code uses the server registration from `.mcp.json`; Codex uses its configured MCP server; Pi registers the same six names as native tools and owns the stdio bridge itself.

## Core Rule

1. Call the appropriate direct tool (`roundtable-canvass`, `roundtable-deliberate`, `roundtable-converge`, etc.)
2. Parse the JSON response
3. Synthesize all model responses into unified output

**Who's in the panel.** The default panel is the four built-in CLIs (Antigravity, Copilot, Codex, Claude).
OpenAI-compatible HTTP providers registered via `ROUNDTABLE_PROVIDERS` (Kimi, MiniMax, GLM,
DeepSeek, and so on) can join when selected via `ROUNDTABLE_DEFAULT_AGENTS` (panel default)
or the per-call `agents` parameter (override).

## Commands

|Command|MCP Tool|Role Guidance|
|-|-|-|
|**canvass**|`roundtable-canvass`|Ask the question directly. Each panelist answers independently under the default analyst role.|
|**deliberate**|`roundtable-deliberate`|Add: "Provide conclusions, assumptions, alternatives, and confidence level."|
|**blueprint**|`roundtable-blueprint`|Request: phases, dependencies, risks, milestones.|
|**critique**|`roundtable-critique`|Prefix: "Act as critical reviewer. Find flaws, risks, weaknesses."|
|**crosscheck**|`roundtable-crosscheck`|Include `files`. Antigravity in planner role, Codex in codereviewer role, Claude as generalist, Copilot and HTTP providers in default role — one prompt, mixed lenses.|
|**converge**|`roundtable-converge`|Replay a prior dispatch. Each panelist sees the original prompt and the other panelists' answers (under anonymized `peer-N` labels) and produces (a) revised or held stance, (b) list of peers it agrees with, (c) draft converged recommendation. Different parameter shape — see below.|

## Tool Invocation

Call the Roundtable tools directly. No Bash tool, binary path, shell, or generic MCP proxy call. On Pi, the package-owned bridge gives the server request 120 seconds beyond the provider deadline for startup, response serialization, and cleanup.

### Tool Parameters

| Parameter | Required | Description |
|-|-|-|
|Parameter|Required|Description|
|-|-|-|
|`prompt`|Yes|The question or task|
|`files`|No|Comma-separated **relative** file paths for context|
|`timeout`|No|Seconds per CLI (default and max: 900). Lower only if the task is quick. On Pi the outer MCP request allows this deadline plus 120 seconds of bridge overhead.|
|`codex_model`|No|Override Codex model|
|`claude_model`|No|Override Claude model (e.g., `sonnet`, `opus`)|
|`codex_resume`|No|Codex thread ID (from a prior turn's `session_id`) to continue a previous conversation. The `last` sentinel is rejected on the app-server path — pass an explicit thread ID.|
|`claude_resume`|No|Claude session ID to continue a previous conversation|
|`antigravity_resume`|No|Antigravity conversation ID to continue with `agy --conversation`; best-effort because print-mode output may include prior transcript text|
|`copilot_resume`|No|Copilot session ID to continue a previous conversation|
|`agents`|No|**JSON-encoded string** describing selective dispatch (see below). Pass a string, not an array.|

### Selective Agent Dispatch (`agents` parameter)

The `agents` parameter lets you control exactly which agents run, with what models, and in what roles. When provided, it replaces the default 4-agent dispatch. When omitted, all 4 CLIs run as before.

**Important:** `agents` is passed as a JSON-encoded **string** (not a JSON array object). Serialize your array with `JSON.stringify` or the equivalent, then pass the resulting string value.

Each entry in the encoded array:

| Field | Required | Description |
|-|-|-|
| `provider` | Yes | Backend: `"codex"`, `"claude"`, `"antigravity"`, `"copilot"`, or a provider registered via `ROUNDTABLE_PROVIDERS` |
| `name` | No | Result key in output (defaults to `provider` value; must be unique) |
| `model` | No | Model override for this agent |
| `role` | No | Role override: `"default"`, `"planner"`, `"codereviewer"` |
| `resume` | No | Session ID to continue a previous conversation |

**Examples:**

Skip Claude, only use Antigravity and Codex:
```json
[{"provider": "antigravity"}, {"provider": "codex"}]
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
  {"name": "arch", "provider": "antigravity", "role": "planner"},
  {"name": "review", "provider": "codex", "model": "gpt-5.4", "role": "codereviewer"},
  {"name": "sanity", "provider": "claude", "model": "sonnet", "role": "default"},
  {"name": "copilot-extra", "provider": "copilot", "role": "default"}
]
```

**Notes:**
- Per-agent `model` wins over the per-tool `codex_model` / `claude_model` params. If an agent entry omits `model`, the matching per-tool param (if any) is used as a fallback.
- Antigravity does not have a dedicated model flag; a per-agent `model` is reported in output but is not passed to `agy`.
- Antigravity resume is best-effort: current `agy --conversation --print` output may include prior transcript text before the fresh answer, and Roundtable preserves that text.
- Agent names must be unique; `"meta"` is reserved.
- The tool's default role applies unless overridden per-agent.

### Default Agent Configuration

Set `ROUNDTABLE_DEFAULT_AGENTS` at MCP registration time to configure which agents run by default — so you don't specify them on every call. Uses the same JSON schema as the `agents` parameter above.

**Precedence** (highest to lowest):
1. Per-call `agents` parameter — **always wins**
2. `ROUNDTABLE_DEFAULT_AGENTS` env var — session default
3. Built-in default — all 4 CLIs (antigravity, copilot, codex, claude)

> **You can always override defaults per-call.** Even if your defaults only include codex and claude, you can pass `agents: [{"provider": "antigravity"}]` to get an Antigravity-only review.

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
  {"provider": "antigravity", "role": "planner"},
  {"provider": "codex", "role": "codereviewer"}
]
```

**Notes:**
- Invalid env var JSON → warning logged, falls back to all 4 CLIs
- The `resume` field is ignored in defaults — session IDs are per-call only
- See [INSTALL.md](INSTALL.md) for registration instructions.

### Converge Tool Parameters

`roundtable-converge` has a narrower input — it replays a prior dispatch, so the panel set is derived from the prior. It does NOT accept `prompt`, `files`, `agents`, `*_model`, `*_resume`, or `timeout`.

| Parameter | Required | Description |
|-|-|-|
| `prior_result` | Yes | A `DispatchResult` JSON object — the output of any prior roundtable tool call. Recipient set for convergence is derived from this object's per-panelist keys. Each panelist runs in `default` role; per-CLI model/resume overrides are not threaded in v1. |
| `original_prompt` | Yes | The prompt that produced `prior_result`. Required because `DispatchResult` does not preserve the original prompt. |

Returns the same `DispatchResult` shape as the other tools. Each panelist's `response` is freeform text following the (a)/(b)/(c) structure: (a) revised or held stance, (b) which peers they agree with (by redacted `peer-N` label), (c) draft converged recommendation.

**When to use converge:**
- After a `canvass` / `deliberate` / `crosscheck` call where panelists disagreed, and you want a synthesized round where each panelist sees what others said and revises.
- When you want the panel to converge on a recommendation rather than produce independent answers.

**Edge cases:**
- Empty `prior_result.Results` → `IsError: true`, message `"prior_result has no recipients to converge"`.
- Each panelist sees its peers under redacted `peer-1`, `peer-2`, ... labels — never the peer's real name. The panelist NEVER sees its own prior response in its converge prompt.
- Legacy priors produced before the `provider` field was added fall back to using the recipient name as the backend lookup key — works for default-fanout recipient names (antigravity/copilot/codex/claude) but custom-named priors will produce `NotFoundResult` for unregistered names.
- Backends not registered → that recipient gets a `NotFoundResult` row in the output, dispatch continues for the other recipients.

**Synthesis pattern:** when reporting a converge result, contrast each panelist's prior stance with their revised stance. Highlight (b) agreement clusters — they often hint at where the panel actually converged versus where it just hedged.

### Per-Project Role Overrides

To use project-scoped role prompts, place them at `<project>/.claude/roundtable/roles/<role>.txt` and start the server with `ROUNDTABLE_PROJECT_ROLES_DIR=<path>` set. There is no per-call parameter for this — project roles are resolved at server startup.

The role lookup order is: project dir → global dir (`ROUNDTABLE_ROLES_DIR`) → embedded defaults shipped in the binary.

## Output Format

MCP tool calls return JSON with this structure:

```json
{
  "antigravity": { "response": "...", "status": "ok|error|timeout|terminated|not_found|probe_failed|rate_limited|auth_required", ... },
  "copilot": { "response": "...", "status": "...", "session_id": "...", ... },
  "codex": { "response": "...", "status": "...", "session_id": "...", ... },
  "claude": { "response": "...", "status": "...", "session_id": "...", ... },
  "meta": { "antigravity_role": "...", "copilot_role": "...", "codex_role": "...", "claude_role": "...", "files_referenced": [...], "total_elapsed_ms": 0 }
}
```

Possible `status` values:
- `ok` — normal response
- `error` — backend returned an error payload or non-zero exit
- `timeout` — backend exceeded the per-CLI deadline
- `terminated` — backend killed by signal
- `not_found` — CLI binary not on PATH
- `probe_failed` — `--version` probe failed
- `rate_limited` — provider rate-limited the request (Antigravity detects 429/RESOURCE_EXHAUSTED/quota-style text)
- `auth_required` — the CLI's stored credential has expired, so it needs a fresh sign-in (Codex detects its expired-refresh-token message)

## Synthesis Template

After calling a roundtable tool, synthesize the results:

```
## [Command Name]

### Antigravity
[response summary — key points only, not raw dump]

### Copilot
[response summary — key points only, not raw dump, when present]

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
Call `roundtable-canvass` with `prompt: "What about the token refresh edge case you mentioned?"`, `antigravity_resume: "<conversation-id from previous response>"`, `codex_resume: "<session_id from previous response>"`, and `claude_resume: "<session-id from previous response>"`.

- `antigravity_resume: "<conversation-id>"` resumes an Antigravity conversation best-effort; current `agy --conversation --print` output may include prior transcript text
- `codex_resume: "<thread-id>"` resumes a Codex thread via the app-server's `thread/resume` — pass the explicit thread ID from a prior turn's `session_id`. The `last` sentinel is rejected on the app-server path (it applies only to the exec fallback's `--last` mapping).
- You can also pass specific session IDs from the previous response's `session_id` fields
- Follow-up prompts still go through role prompt assembly

## Degradation Rules

- If one CLI has `status: "error"`, `"timeout"`, `"terminated"`, or `"probe_failed"`: synthesize with the available responses, note which was unavailable and why.
- If one CLI has `status: "not_found"`: note it's not installed, synthesize with the others.
- If one CLI has `status: "rate_limited"`: tell the user the provider rate-limited the request and suggest retrying or resuming that session.
- If one CLI has `status: "auth_required"`: tell the user that CLI's credential expired and they need to sign in to it again — this is not a bug in their code — then synthesize with the others.
- If all CLIs fail: report errors, do not attempt synthesis.
- If `parse_error` is set: note the response may be incomplete but still usable.
- Non-zero exit codes are automatically downgraded to `"error"` even if the parser found content.

## Prompt Framing

The quality of roundtable output depends on prompt quality. Guidelines:

- **Be specific about what you want evaluated.** "Review this auth flow" is weaker than "Review the token refresh logic in auth.ts — is the race condition between concurrent refresh calls handled correctly?"
- **For roundtable-crosscheck**, list the files and state what you want each model to focus on.
- **For roundtable-critique**, state the proposal clearly before asking for critique — the models need something concrete to push back on.
- **Include constraints.** If there are non-negotiable requirements (compliance, latency budgets, existing API contracts), state them so the models don't waste time proposing alternatives that violate them.

### Keep Your Own Opinion Out of the Prompt

You are usually mid-task when you consult the panel, which means you already lean toward an answer. A prompt that carries that lean anchors the panelists, and the "consensus" you get back is your own opinion echoed. Compose every panel prompt neutrally:

- **State the question without revealing your preferred answer.** Never include your working hypothesis or tentative conclusion. Ask "How should the retry logic handle partial failures?" — not "I think exponential backoff is right here — validate."
- **Present competing alternatives symmetrically.** Give each option the same depth of description, neutral ordering and labels ("Option A / Option B", not "the obvious fix" vs. "the workaround"), and no evaluative adjectives that favor one.
- **No leading questions.** "Isn't the cache invalidation the problem?" and "Confirm this design is sound" invite agreement, not judgment. Ask open forms instead: "What are the likely causes?" "Assess this design."
- **Keep constraints, strip opinions.** Genuine requirements (compliance, latency budgets, existing API contracts) belong in the prompt. Your predictions, preferences, and hunches do not.
- **Critiquing your own proposal? Present it as an artifact.** Describe what the proposal does and ask the panel to attack it. No advocacy — leave out why you thought it was the best approach.
- **Relay the user's stated requirements; never inject your own lean.** What the user explicitly asked for is input the panel needs. What you privately concluded along the way is contamination.

## Mistakes to Avoid

| Mistake | Fix |
|-|-|
| Using Bash tool to call roundtable | Call MCP tools directly — no Bash needed |
| Running only one model | ALWAYS use roundtable (dispatches the full default panel unless overridden) |
| Dumping raw JSON responses | Summarize key points, find agreement/differences |
| Skipping synthesis | Synthesis IS the value — always include it |
| Using for simple questions | Only use when multi-model perspective adds value |
| Ignoring stderr/status | Check status fields — errors contain useful context |
| Using absolute file paths | Use relative paths from the project root you launched the MCP client from |
| Leading or opinion-loaded prompts ("I think X — validate") | Frame neutrally: withhold your own lean and let panelists judge on the merits |
