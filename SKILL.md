---
name: roundtable
description: >-
  Multi-model consensus via Gemini and Codex CLIs. Dispatches to both in parallel, then synthesizes.
  Commands: hivemind (consensus), deepdive (extended reasoning), architect (implementation plan),
  challenge (devil's advocate), xray (codebase architecture + code quality).
  Triggers: "roundtable", "second opinion", "what do others think", "consensus", "deep analysis",
  "think through", "explore tradeoffs", "break down", "implementation plan", "how to build",
  "stress test", "critique", "poke holes", "devil's advocate", "review architecture",
  "analyze codebase", "what's wrong here".
  Do NOT use for simple questions, pure code generation, or when user wants only Claude's opinion.
---

# Roundtable - Multi-Model Consensus

Dispatch to BOTH Gemini AND Codex in parallel via `roundtable.mjs`, then synthesize.

## Core Rule

1. Run `node ~/src/roundtable/roundtable.mjs` with appropriate flags
2. Parse the JSON output
3. Synthesize both responses into unified output

## Commands

| Command | Flags | Prompt Guidance |
|---------|-------|-----------------|
| **hivemind** | `--role default` | Ask the question directly |
| **deepdive** | `--role planner` | Add: "Provide conclusions, assumptions, alternatives, and confidence level." |
| **architect** | `--role planner` | Request: phases, dependencies, risks, milestones |
| **challenge** | `--role codereviewer` | Prefix: "Act as critical reviewer. Find flaws, risks, weaknesses." |
| **xray** | `--gemini-role planner --codex-role codereviewer` | Include `--files`. Gemini analyzes architecture, Codex reviews code quality. |

## Invocation

Run via Bash tool. Always use the full path to roundtable.mjs:

```bash
node ~/src/roundtable/roundtable.mjs \
  --prompt "Your question here" \
  --role planner \
  --files src/auth.ts,src/middleware.ts \
  --timeout 120
```

### Parameters

| Flag | Required | Description |
|------|----------|-------------|
| `--prompt` | Yes | The question or task |
| `--role` | No | Role for both CLIs: `default`, `planner`, `codereviewer` (default: `default`) |
| `--gemini-role` | No | Override role for Gemini only (for xray command) |
| `--codex-role` | No | Override role for Codex only (for xray command) |
| `--files` | No | Comma-separated file paths for context (CLIs read files themselves) |
| `--gemini-model` | No | Override Gemini model (default: whatever the CLI is configured to use) |
| `--codex-model` | No | Override Codex model (default: whatever the CLI is configured to use) |
| `--codex-reasoning` | No | Codex reasoning effort: `xhigh`, `high`, `medium` (maps to `-c reasoning_effort="..."`) |
| `--timeout` | No | Seconds per CLI (default: 900 / 15 min). Health probe (5s) catches broken CLIs fast. No stall detection — CLIs legitimately go silent during model inference. Gemini handles its own 429/529 retries internally. |
| `--gemini-resume` | No | Gemini session ID or `latest` to continue a previous conversation |
| `--codex-resume` | No | Codex session/thread ID or `last` to continue a previous conversation |

### Per-Project Role Overrides

If a project has `.claude/roundtable/roles/<role>.txt`, those override the global defaults.
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
node ~/src/roundtable/roundtable.mjs --prompt "Review the auth architecture" --role planner --files src/auth.ts
```

**Follow-up call** (using session IDs from the first response):
```bash
node ~/src/roundtable/roundtable.mjs \
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

## Mistakes to Avoid

| Mistake | Fix |
|---------|-----|
| Running only one model | ALWAYS use roundtable (runs both) |
| Dumping raw JSON responses | Summarize key points, find agreement/differences |
| Skipping synthesis | Synthesis IS the value — always include it |
| Using for simple questions | Only use when multi-model perspective adds value |
| Ignoring stderr/status | Check status fields — errors contain useful context |
| Using absolute file paths | Use relative paths from project root — Gemini can't read outside its workspace |
