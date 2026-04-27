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

## Post-MVP

<!-- Improvements to make after core features land -->
