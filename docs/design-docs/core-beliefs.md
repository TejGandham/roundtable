# Core Beliefs



Non-negotiable principles that govern every decision in roundtable.

## Domain Safety

<!-- CUSTOMIZE: What are your domain's non-negotiable safety rules?
     Examples:
     - Git: Never force-pull, never switch branches, --ff-only always.
     - API: Validate at boundaries, auth every endpoint, no raw SQL.
     - Data: Idempotent transforms, schema validation, no silent data loss.
     - Financial: Audit trail on mutations, no float currency, double-entry.
     See examples/domain-invariants/ for detailed templates. -->

- [YOUR SAFETY RULE 1]
- [YOUR SAFETY RULE 2]
- [YOUR SAFETY RULE 3]

## Source of Truth

<!-- What is the authoritative data source? How do you handle state? -->

- [YOUR DATA PRINCIPLE]

## Design Philosophy

<!-- What's the user experience philosophy? -->

- [YOUR DESIGN PRINCIPLE]

## Container / Runtime

<!-- Non-negotiable runtime constraints -->

- Docker for everything. No local runtime dependencies.
- If it doesn't work in Docker, it doesn't work.

## Claude Is The Builder

- Claude is responsible for design, development, testing, documentation, maintenance.
- The human steers. Claude executes.
- Every decision must be traceable to a document in this repo.
- Repository is the system of record. If it's not here, it doesn't exist.

## Testing Strategy: Spec-Driven Testing

Tests enforce spec conformance, not discover design. Every spec assertion has
a corresponding test. When specs change, tests change first.

### Layer 0: Spec Consistency

Docs must not contradict each other. Before writing tests, verify that
structured PRDs (`docs/exec-plans/prds/<slug>.json`), design-docs,
and ARCHITECTURE.md agree.

### Layer 1: Safety Invariants

<!-- CUSTOMIZE: What safety tests MUST use real I/O? Never mock safety. -->

From core-beliefs safety rules. These are the first tests written,
last tests deleted. **Must use real I/O** — mocking safety means testing your mock.

- [YOUR SAFETY TEST 1]
- [YOUR SAFETY TEST 2]

### Layer 2a: Integration (Slow)

<!-- CUSTOMIZE: What integration tests run real external calls? -->

Real external calls against test fixtures/environments.
Tagged as slow so fast loops can skip them.

- [YOUR INTEGRATION TEST CATEGORY]

### Layer 2b: Pure Domain Logic (Fast)

No I/O. Tests for derived fields, pure functions, business rules.

- [YOUR PURE LOGIC TEST CATEGORY]

### Layer 3: Service / Process Behavior

<!-- CUSTOMIZE: What gets mocked at the service layer? -->

Service behavior with mocked external dependencies.
Uses your mock framework for deterministic, fast tests.

- [YOUR SERVICE TEST CATEGORY]

### Layer 4: UI / Component Behavior

<!-- CUSTOMIZE: What UI testing approach? -->

UI behavior with mocked service layer. Fast and deterministic.

- [YOUR UI TEST CATEGORY]

### Layer 5: Acceptance + Container Smoke

Validates the full stack boots correctly inside the container.

- Container builds successfully
- Test suite passes inside container
- App boots and responds to health check
<!-- CUSTOMIZE: Add project-specific acceptance criteria -->

### Testing Infrastructure

<!-- CUSTOMIZE: Define your test infrastructure early.
     - Behaviour/interface for dependency injection
     - Mock framework configuration
     - Fixture/factory helpers
     - Test tags for filtering -->

- **[INTERFACE]**: Define interface early. Real impl for Layers 1-2a. Mocks for Layers 3-4.
- **[FIXTURE HELPER]**: Shared helper to create test scenarios.
- **[TEST TAGS]**: Tags for filtering slow vs fast tests.
