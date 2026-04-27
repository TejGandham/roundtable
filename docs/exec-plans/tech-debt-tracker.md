# Tech Debt Tracker

Known shortcuts, deferred improvements, and open questions.

<!-- Items get added as features land. Mark resolved items with [x].
     Review this file during garbage collection sweeps. -->

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

## Post-MVP

<!-- Improvements to make after core features land -->
