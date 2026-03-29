---
name: roundtable
description: >-
  Multi-model consensus via Gemini and Codex CLIs. Dispatches to both in parallel, then synthesizes.
  Commands: hivemind (consensus), deepdive (extended reasoning), architect (implementation plan),
  challenge (devil's advocate), xray (codebase architecture + code quality).
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

Dispatch to BOTH Gemini AND Codex in parallel via `./roundtable`, then synthesize.

## Core Rule

1. Run `~/.claude/skills/roundtable/roundtable` with appropriate flags
2. Parse the JSON output
3. Synthesize both responses into unified output

Requires Erlang/OTP and Elixir: `brew install elixir`.

## Commands

| Command | Flags | Prompt Guidance |
|-|-|-|
| **hivemind** | `--role default` | Ask the question directly |
| **deepdive** | `--role planner` | Add: "Provide conclusions, assumptions, alternatives, and confidence level." |
| **architect** | `--role planner` | Request: phases, dependencies, risks, milestones |
| **challenge** | `--role codereviewer` | Prefix: "Act as critical reviewer. Find flaws, risks, weaknesses." |
| **xray** | `--gemini-role planner --codex-role codereviewer` | Include `--files`. Gemini analyzes architecture, Codex reviews code quality. |

## Invocation

Run via Bash tool from the **project root directory** (Gemini restricts file access to its cwd):

```bash
~/.claude/skills/roundtable/roundtable \
  --prompt "Your question here" \
  --role planner \
  --files src/auth.ts,src/middleware.ts \
  --timeout 300
```

For spec/design reviews where Codex reads referenced files, use `--timeout 600`.

### Parameters

| Flag | Required | Description |
|-|-|-|
| `--prompt` | Yes | The question or task |
| `--role` | No | Role for both CLIs: `default`, `planner`, `codereviewer` (default: `default`) |
| `--gemini-role` | No | Override role for Gemini only (for xray command) |
| `--codex-role` | No | Override role for Codex only (for xray command) |
| `--files` | No | Comma-separated **relative** file paths for context (CLIs read files themselves) |
| `--gemini-model` | No | Override Gemini model (default: whatever the CLI is configured to use) |
| `--codex-model` | No | Override Codex model (default: whatever the CLI is configured to use) |
| `--codex-reasoning` | No | Codex reasoning effort: `xhigh`, `high`, `medium` (maps to `-c reasoning_effort="..."`) |
| `--timeout` | No | Seconds per CLI (default: 900). Use 300 for code reviews, 600 for spec reviews. Health probe (5s) catches broken CLIs fast. No stall detection — CLIs go silent during inference. Gemini handles 429/529 retries internally. |
| `--gemini-resume` | No | Gemini session ID or `latest` to continue a previous conversation |
| `--codex-resume` | No | Codex session/thread ID or `last` to continue a previous conversation |
| `--roles-dir` | No | Override global roles directory (default: skill's `roles/` dir) |
| `--project-roles-dir` | No | Project-local roles directory (checked first, falls back to global) |

### Per-Project Role Overrides

If a project has `.claude/roundtable/roles/<role>.txt`, pass it via `--project-roles-dir .claude/roundtable/roles`.
This lets projects customize planner/reviewer context for their domain.

## Output Format

The script outputs JSON to stdout with this structure:

```json
{
  "gemini": { "response": "...", "status": "ok|error|timeout|not_found|probe_failed", "session_id": "...", ... },
  "codex": { "response": "...", "status": "ok|error|timeout|not_found|probe_failed", "session_id": "...", ... },
  "meta": { "gemini_role": "...", "codex_role": "...", "files_referenced": [...] }
}
```

## Synthesis Template

After running roundtable, synthesize the results:

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

## Follow-up Conversations

Roundtable supports continuing a previous conversation with both CLIs. Each response includes `session_id` fields — use these for follow-up rounds.

**First call:**
```bash
~/.claude/skills/roundtable/roundtable --prompt "Review the auth architecture" --role planner --files src/auth.ts
```

**Follow-up call** (using session IDs from the first response):
```bash
~/.claude/skills/roundtable/roundtable \
  --prompt "What about the token refresh edge case you mentioned?" \
  --role planner \
  --gemini-resume latest \
  --codex-resume last
```

- `--gemini-resume latest` resumes Gemini's most recent session
- `--codex-resume last` resumes Codex's most recent session
- You can also pass specific session IDs from the previous response's `session_id` fields
- Follow-up prompts still go through role prompt assembly

## Degradation Rules

- If one CLI has `status: "error"`, `"timeout"`, or `"probe_failed"`: synthesize with available response, note which was unavailable and why
- If one CLI has `status: "not_found"`: note it's not installed, synthesize with the other
- If both fail: report errors, do not attempt synthesis
- If `parse_error` is set: note the response may be incomplete but still usable
- Non-zero exit codes are automatically downgraded to `"error"` even if the parser found content

## Important: Gemini Workspace Constraint

Gemini CLI restricts file access to its current working directory. When using `--files` (especially with `xray`), either:

1. Run roundtable **from the project root** so Gemini can access the referenced files
2. Use **relative paths** in `--files` (not absolute paths)

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
| Running only one model | ALWAYS use roundtable (runs both) |
| Dumping raw JSON responses | Summarize key points, find agreement/differences |
| Skipping synthesis | Synthesis IS the value — always include it |
| Using for simple questions | Only use when multi-model perspective adds value |
| Ignoring stderr/status | Check status fields — errors contain useful context |
| Using absolute file paths | Use relative paths from project root — Gemini can't read outside its workspace |
