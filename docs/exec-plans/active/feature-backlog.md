# Feature Backlog

<!-- KEEL-BOOTSTRAP: not-applicable -->







Smallest independently testable features. Execute top-to-bottom.
Each feature: read spec → write test → write code → verify.

**PRDs:** `docs/exec-plans/prds/<slug>.json` (drafted by `/keel-refine`)
**Principles:** `docs/design-docs/core-beliefs.md`
**Architecture:** `ARCHITECTURE.md`

---

## Bootstrap (orchestrator-direct, no test-writer/implementer pipeline)

- [ ] **F01 Docker dev environment**
  Spec: core-beliefs:Container | Agent: docker-builder
  Test: `docker compose build` succeeds, container has required tools

- [ ] **F02 Project scaffold**
  Spec: [YOUR-SPEC]:technical | Needs: F01 | Agent: scaffolder
  Test: App boots at expected port inside container

- [ ] **F03 Test infrastructure**
  Spec: core-beliefs:Testing | Needs: F02 | Agent: config-writer
  Test: Mock framework configured, test helper compiles

## Foundation (backend pipeline starts here)

<!-- CUSTOMIZE: List your foundation-layer features.
     Each should be a single function/module, independently testable.
     Include spec reference, dependencies, and test criteria. -->

- [ ] **F04 [YOUR FOUNDATION FEATURE]**
  Spec: [spec:section] | Needs: F02, F03
  Test: [specific test criteria]

- [ ] **F01 JSON-Schema-lite subset parser**
  PRD: dispatch-structured-output
  <!-- DRAFTED: 2026-04-27 by backlog-drafter; 0 markers remain -->
  <!-- SOURCE: 5d2e0c8e3f1a4b9c -->

## Service

<!-- CUSTOMIZE: Features that build on foundation — services, processes, coordination -->

- [ ] **F05 [YOUR SERVICE FEATURE]**
  Spec: [spec:section] | Needs: F04
  Test: [specific test criteria]

- [ ] **F02 Schema-to-prompt-suffix builder**
  Needs: F01
  PRD: dispatch-structured-output
  <!-- DRAFTED: 2026-04-27 by backlog-drafter; 0 markers remain -->
  <!-- SOURCE: 5d2e0c8e3f1a4b9c -->

- [ ] **F03 Per-panelist response validator with structured error surfacing**
  Needs: F01, F02
  PRD: dispatch-structured-output
  <!-- DRAFTED: 2026-04-27 by backlog-drafter; 0 markers remain -->
  <!-- SOURCE: 5d2e0c8e3f1a4b9c -->

- [ ] **F04 Wire schema parameter into all five dispatch MCP tools**
  Needs: F01, F02, F03
  PRD: dispatch-structured-output
  <!-- DRAFTED: 2026-04-27 by backlog-drafter; 0 markers remain -->
  <!-- SOURCE: 5d2e0c8e3f1a4b9c -->

## UI

<!-- CUSTOMIZE: UI/frontend features.
     UI entries may carry a Design: field listing committed wireframes,
     comps, or flow diagrams. frontend-designer reads them via Claude
     vision when generating code. Only paths under the repo — no live
     Figma/Miro URLs. -->

- [ ] **F06 [YOUR UI FEATURE]**
  Spec: [spec:section] | Needs: F05
  Design: docs/prds/drafts/[TS]/login-flow.png, docs/design-assets/shared/button-primary.svg
  Test: [specific test criteria]

## Cross-cutting

<!-- CUSTOMIZE: Test fixtures, safety tests, shared infrastructure -->

- [ ] **F07 [YOUR CROSS-CUTTING FEATURE]**
  Spec: core-beliefs:Testing | Needs: F02
  Test: [specific test criteria]
