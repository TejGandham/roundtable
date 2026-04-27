# Tech Debt Tracker

Known shortcuts, deferred improvements, and open questions.

<!-- Items get added as features land. Resolved items are deleted —
     git log records the landing. Review this file during garbage
     collection sweeps. -->

## Pre-Implementation

<!-- Spec drift, open questions discovered before coding starts -->

### Open Questions

- [ ] [YOUR OPEN QUESTION]

## During Implementation

<!-- Shortcuts taken, unexpected issues discovered during feature work -->

- [ ] **`openai_http.go` references a transitional spec.** `internal/roundtable/openai_http.go:397` and `internal/roundtable/openai_http_test.go:731` cite `docs/superpowers/specs/2026-04-22-openai-http-tool-calls-diagnostic-design.md` as the design rationale for the `tool_calls` finish-reason diagnostic. Per AGENTS.md "Doc conventions," `docs/superpowers/` is transitional — load-bearing code should not depend on it. Migration: either inline the load-bearing rationale into the code comment block, or move the spec to a permanent home (e.g., `docs/design-docs/openai-http-tool-calls-diagnostic.md`). Resolve before next major refactor of `openai_http.go`.

- [ ] **Register INV-001: parser bounded-allocation contract.** Surfaced by F01 (`internal/roundtable/dispatchschema/`). The parser accepts external bytes (via the future `--schema` MCP parameter wired in F04) but has no upper bounds on `enum: []` length, `required: []` length, or property count. Safety-auditor flagged 2 RISK-class findings; pre-check roundtable raised it as the panel's unanimous concern. Resolve before F04 lands by registering an INV-### in CLAUDE.md and adding bounded-decode guards to `Parse`.

- [ ] **`dispatchschema.errAs` should use `errors.As`.** `internal/roundtable/dispatchschema/schema.go` hand-rolls a type assertion in the `errAs` helper to avoid importing `errors`. Inconsistent with `internal/roundtable/openai_http.go` (which uses `errors.As`/`errors.Is`). Silently fails if `encoding/json` ever wraps the error chain. Drop the helper, import `errors`, call `errors.As` inline. Code-reviewer flagged as MINOR; not blocking for F01.

- [ ] **Stale `errAs` doc comment.** `internal/roundtable/dispatchschema/schema.go` `errAs` helper's doc comment still references `strings` import that was removed in the F01 roundtable-attempt-1 fix. Roundtable landing review attempt 2 flagged as advisory (Claude). Cleanup is a one-line edit.

- [ ] **`prompt.go` redundant rune lower-bound check.** `internal/roundtable/dispatchschema/prompt.go:93` uses `r >= 0x00 && r <= 0x1F` — the lower bound is tautological for Go `rune` (always ≥ 0). Drop to `r <= 0x1F`. Code-reviewer flagged as NITPICK during F02; non-blocking.

- [ ] **U+2028 / U+2029 line separators in prompt sanitize.** `internal/roundtable/dispatchschema/prompt.go` sanitize pass covers ASCII control chars 0x00–0x1F but not Unicode line separators U+2028 / U+2029. Markdown fences only close on LF (per CommonMark), so this isn't a current vulnerability — but if downstream consumers ever interpret these as line breaks, the sanitize layer should cover them. Defense-in-depth, deferred from F02 safety audit.

- [ ] **Safety-auditor agent template has placeholder invariants.** `.claude/agents/safety-auditor.md` references `[YOUR INVARIANT RULE 1-3]` placeholders that were never replaced. Causes the agent to fail-closed on tight-prompt re-reviews even when the underlying code is fine (observed during F02 roundtable retry — agent passed thoroughly on attempt 1 but fail-closed on the mechanical-fix retry). Should be replaced with this project's actual invariant rules once INV-001 (parser bounded-allocation contract) is registered.

- [ ] **PRD `dispatch-structured-output.json` F03 `result_extension` lists pointer type.** `docs/exec-plans/prds/dispatch-structured-output.json` says `Structured *json.RawMessage`. Backend-designer + arch-advisor ratified value-type `json.RawMessage` (with `omitempty`) during F03 design — the pointer-to-slice is a Go anti-pattern with no functional difference under `omitempty`. F03 implementation uses the value type. PRD is now stale on this key. Resolve by re-running `/keel-refine docs/exec-plans/prds/dispatch-structured-output.json` (re-run mode walks Card 0 + cards) and updating the F03 contract's `result_extension` text to drop the asterisk.

- [ ] **PRD `dispatch-structured-output.json` F04 `backwards_compat_bar` says "byte-equivalent".** Should clarify to "key-set equivalent" — F03 design ratified the reframe (provable via `omitempty` key-absence; literal byte-identity is brittle to stdlib field-ordering or whitespace changes). Resolve via the same `/keel-refine` re-run that fixes the F03 entry above.

## Post-MVP

<!-- Improvements to make after core features land -->
