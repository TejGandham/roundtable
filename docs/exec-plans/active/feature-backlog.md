# Feature Backlog

<!-- KEEL-BOOTSTRAP: not-applicable -->







Smallest independently testable features. Execute top-to-bottom.
Each feature: read spec → write test → write code → verify.

**PRDs:** `docs/exec-plans/prds/<slug>.json` (drafted by `/keel-refine`)
**Principles:** `docs/design-docs/core-beliefs.md`
**Architecture:** `ARCHITECTURE.md`

---

## Foundation (backend pipeline starts here)

- [x] **F01 JSON-Schema-lite subset parser**
  PRD: dispatch-structured-output
  <!-- SOURCE: 5d2e0c8e3f1a4b9c -->

- [ ] **F05 prior_result input parameter schema accepting DispatchResult JSON shape**
  PRD: roundtable-converge-tool
  <!-- DRAFTED: 2026-04-27 by backlog-drafter; 0 markers remain -->
  <!-- SOURCE: 21dcf517a6d20641 -->

- [ ] **F09 request_extras parser field on providerJSON with reserved-key rejection**
  PRD: provider-request-extras
  <!-- SOURCE: orphan-prd-reconciliation -->

- [ ] **F12 Bounded-allocation guards in dispatchschema parser and MCP schema-input boundary**
  Needs: F01, F04
  PRD: dispatchschema-bounded-allocation
  <!-- DRAFTED: 2026-05-03 by backlog-drafter; 0 markers remain -->
  <!-- SOURCE: 9c4e8a1f7b2d5e63 -->

## Service

- [x] **F02 Schema-to-prompt-suffix builder**
  Needs: F01
  PRD: dispatch-structured-output
  <!-- SOURCE: 5d2e0c8e3f1a4b9c -->

- [x] **F03 Per-panelist response validator with structured error surfacing**
  Needs: F01, F02
  PRD: dispatch-structured-output
  <!-- SOURCE: 5d2e0c8e3f1a4b9c -->

- [ ] **F04 Wire schema parameter into all five dispatch MCP tools**
  Needs: F01, F02, F03
  PRD: dispatch-structured-output
  <!-- DRAFTED: 2026-04-27 by backlog-drafter; 0 markers remain -->
  <!-- SOURCE: 5d2e0c8e3f1a4b9c -->

- [ ] **F06 Per-panelist peer-redaction transform over a prior dispatch result**
  Needs: F05
  PRD: roundtable-converge-tool
  <!-- DRAFTED: 2026-04-27 by backlog-drafter; 0 markers remain -->
  <!-- SOURCE: 21dcf517a6d20641 -->

- [ ] **F07 Convergence-prompt assembly wrapping original prompt, redacted peer view, and (a)/(b)/(c) instructions**
  Needs: F06
  PRD: roundtable-converge-tool
  <!-- DRAFTED: 2026-04-27 by backlog-drafter; 0 markers remain -->
  <!-- SOURCE: 21dcf517a6d20641 -->

- [ ] **F08 Register roundtable-converge MCP tool and wire convergence-prompt assembly through dispatch**
  Needs: F07
  PRD: roundtable-converge-tool
  <!-- DRAFTED: 2026-04-27 by backlog-drafter; 0 markers remain -->
  <!-- SOURCE: 21dcf517a6d20641 -->

- [ ] **F10 request_extras body merge in OpenAIHTTPBackend.Run with reserved-key precedence**
  Needs: F09
  PRD: provider-request-extras
  <!-- SOURCE: orphan-prd-reconciliation -->

## UI

(none yet)

## Cross-cutting

- [ ] **F11 INSTALL.md ROUNDTABLE_PROVIDERS reference and Fireworks DeepSeek V4 max-thinking example**
  Needs: F09, F10
  PRD: provider-request-extras
  <!-- SOURCE: orphan-prd-reconciliation -->

