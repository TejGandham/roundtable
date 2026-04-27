# F01 — JSON-Schema-lite subset parser

---
status: LANDED
pipeline: backend
prd_ref: docs/exec-plans/prds/dispatch-structured-output.json#F01

# Pre-check routing (set by pre-check, read by orchestrator)
intent: build
complexity: standard
designer_needed: NO
researcher_needed: NO
safety_auditor_needed: YES
arch_advisor_needed: NO
implementer_needed: YES

# Gate verdicts (set by orchestrator after each gate agent)
spec_review_verdict: CONFORMANT
spec_review_attempt: 1
safety_verdict: PASS
safety_attempt: 1
code_review_verdict: APPROVED
code_review_attempt: 1
arch_advisor_verdict:        # SOUND | UNSOUND (verify mode only)

# Arch-advisor re-run counters (separate from initial gate passes)
# Used when arch-advisor UNSOUND triggers a re-run of gates
arch_retry_spec_review_attempt: 0
arch_retry_safety_attempt: 0

# Pipeline configuration
remote_name: origin
roundtable_enabled: true
pr_url: https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable/pulls/17

# Roundtable pre-check review (Step 1.3)
roundtable_precheck_attempt: 1
roundtable_precheck_verdict: APPROVED  # pre-check accepted consensus on attempt 1; attempt 2 unnecessary
roundtable_precheck_skipped:         # n/a, MCP available

# Roundtable design review (Step 2.5)
roundtable_design_attempt: 0
roundtable_design_verdict:           # APPROVED | CONCERNS
roundtable_skipped:                  # true (with reason) if MCP unavailable

# Roundtable landing review (Step 8.5)
roundtable_landing_attempt: 2
roundtable_landing_verdict: APPROVED  # 5/6 APPROVED, 1 advisory CONCERNS (stale errAs doc-comment, non-blocking)

# Roundtable-triggered gate re-run counters (separate from initial passes)
roundtable_retry_code_review_attempt: 1
roundtable_retry_spec_review_attempt: 1
roundtable_retry_safety_attempt: 1
---

## pre-check

```yaml
intent: build
complexity: standard
designer_needed: NO
researcher_needed: NO
safety_auditor_needed: YES
arch_advisor_needed: NO
implementer_needed: YES
```

## Execution Brief: JSON-Schema-lite subset parser

**PRD:** docs/exec-plans/prds/dispatch-structured-output.json
**Feature ID:** F01
**Feature index:** 0
**Feature pointer base:** /features/0
**Layer:** foundation
**PRD-level invariants:** none
**Dependencies:** MET — F01 has no needs[] (foundation feature, first in PRD)
**Research needed:** NO (uses stdlib `encoding/json` only — already idiomatic in `internal/roundtable/`)
**Designer needed:** NO (layer is `foundation`, not `ui`; pure parsing function with bounded surface)
**Implementer needed:** YES
**Safety auditor needed:** NO (pure parser; no auth, credentials, network, or filesystem touch; PRD invariants_exercised is empty)
**Arch-advisor needed:** NO (single new package, ~1-2 files, no cross-module structural change)

**Intent:** build
**Complexity:** standard

**What to build:**
A new Go package `internal/roundtable/dispatchschema` exposing `Parse(raw json.RawMessage) (*Schema, error)`. Parse accepts a JSON-Schema-lite subset (top-level object with typed scalar fields — string/number/boolean — optional `enum` on string fields, optional `required: [field, ...]`) and returns a parsed `*Schema` value. Rejects nested objects, arrays, `$ref`, `anyOf`/`oneOf`/`allOf`, `format`, `additionalProperties: true`, and any keyword outside the supported subset, with an error that names the offending construct.

**New files:**
- `internal/roundtable/dispatchschema/schema.go` — `package dispatchschema`; `Schema` struct, `Field` struct, `Parse(raw json.RawMessage) (*Schema, error)`.
- `internal/roundtable/dispatchschema/schema_test.go` — unit tests covering the four oracle assertions.

**Modified files:**
- (none — F01 is a new isolated package; downstream features F02/F03/F04 will import it)

**Existing patterns to follow:**
- `internal/roundtable/result.go:69-101` (`Meta.MarshalJSON` / `UnmarshalJSON`) — idiomatic stdlib `encoding/json` two-stage decode (`map[string]json.RawMessage` then per-key `json.Unmarshal`). Use the same pattern in `Parse` to identify unknown / disallowed keywords without committing to a single struct shape up front.
- `internal/roundtable/result.go:23-44` (`NotFoundResult`, `ProbeFailedResult`) — descriptive error message style: name the offending construct in the error string (e.g., `fmt.Errorf("dispatchschema: unsupported keyword %q at field %q", kw, field)`).
- `internal/roundtable/codex_fallback.go:1-9` — package-header / import-block convention.

**Assertion traceability:**
- `/features/0/oracle/assertions/0` → Build a `json.RawMessage` containing `{"type":"object","properties":{"deliverable":{"type":"string"},"score":{"type":"number"}}}`; assert `Parse` returns `*Schema, nil` and the parsed schema exposes both fields with their declared scalar types.
- `/features/0/oracle/assertions/1` → Parse the feedback example shape `{"type":"object","properties":{"placement":{"type":"string","enum":["a","b","c","d"]},"confidence":{"type":"string","enum":["low","med","high"]}}}`; assert the returned `*Schema` exposes `placement.Enum == ["a","b","c","d"]` and `confidence.Enum == ["low","med","high"]` so F02/F03 can read allowed values.
- `/features/0/oracle/assertions/2` → Parse a schema containing a top-level keyword outside the subset (e.g., `format`, `additionalProperties: true`); assert error is non-nil and the message identifies the offending construct by name.
- `/features/0/oracle/assertions/3` → Drive each rejection mode separately — nested `properties` containing an object-typed property, `type: array`, `$ref`, `anyOf`, `oneOf`, `allOf`, `format`, `additionalProperties: true`. Assert each yields a non-nil error whose message names the offending construct (table-driven test).

**Edge cases:**
- Empty `properties: {}` — should the parser accept (zero-field schema) or reject? Contract is silent; default to accept (empty is well-formed; F02 will produce a trivial suffix).
- Missing top-level `type` key — assertion 2 implies "outside subset" rejection covers this; treat absent `type` as malformed and return descriptive error.
- `required: ["fieldNotInProperties"]` — contract permits `required` array but doesn't say whether unknown field names are validated. Safe default: reject with descriptive error (cheap consistency check, prevents silent F02/F03 mismatches).
- `enum` on non-string fields (e.g., `enum` on a number field) — contract restricts enum to string fields; reject with descriptive error.
- Top-level non-object JSON (`null`, array, scalar) — reject with descriptive error.

**Risks:**
- Over-engineering: this is a foundation parser used only by F02/F03/F04. Resist building extension hooks, `$ref` resolvers, or registry plumbing not in the contract.
- Importing a third-party JSON Schema library (e.g., `github.com/google/jsonschema-go` already an indirect dep) — DO NOT. The whole point of the "lite" subset is rejecting full-schema constructs; using a full library inverts the contract.
- `Schema` struct shape leaks into F02/F03/F04 design. Keep public surface minimal — exported `Schema`, `Field` (with `Type`, `Enum`, `Required` accessible), nothing else.

**Verify command:** `mise exec go@1.26.2 -- env GOTOOLCHAIN=local GOMODCACHE=/tmp/gomodcache GOCACHE=/tmp/gocache go test ./...`

**Path convention:** Go modules under `internal/<package>/`. New foundation package goes at `internal/roundtable/dispatchschema/` (sibling to `internal/roundtable/` per the contract's `package_path`).

### Constraints for downstream

- MUST: Place code at `internal/roundtable/dispatchschema/schema.go` with `package dispatchschema`; entry point signature exactly `Parse(raw json.RawMessage) (*Schema, error)`.
- MUST: Use only stdlib `encoding/json`; follow the two-stage decode pattern in `internal/roundtable/result.go:80-101`. Error messages MUST name the offending construct (keyword, field, type) so F02/F03 callers can route on it.
- MUST NOT: Import any third-party JSON Schema library (incl. `github.com/google/jsonschema-go`). The "lite" subset contract is defined by what we reject, not what we delegate.
- MUST NOT: Add scope beyond the F01 contract — no prompt-suffix building (F02), no response validation (F03), no MCP wiring (F04). Do not export helpers downstream features haven't asked for.
- MUST NOT: Modify any file outside `internal/roundtable/dispatchschema/`. F01 is an additive new package.

**Ready:** YES
**Next hop:** test-writer

### Resolved feature (verbatim from keel-feature-resolve.py)

```json
{
  "ok": true,
  "feature_id": "F01",
  "feature_index": 0,
  "feature_pointer_base": "/features/0",
  "prd_path": "/mnt/agent-storage/vader/src/roundtable/docs/exec-plans/prds/dispatch-structured-output.json",
  "canonical_prd_path": "/mnt/agent-storage/vader/src/roundtable/docs/exec-plans/prds/dispatch-structured-output.json",
  "title": "JSON-Schema-lite subset parser",
  "layer": "foundation",
  "oracle": {
    "type": "unit",
    "tooling": "Go test (go test ./...)",
    "assertions": [
      "Parser accepts a schema object with typed scalar fields and returns a parsed schema value without error.",
      "Parser accepts enum constraints on string fields per the feedback example shape (placement: a|b|c|d, confidence: low|med|high) and exposes the allowed values for downstream validation.",
      "Parser rejects schema constructs outside the supported subset with a descriptive error.",
      "Parser rejects nested objects, arrays, $ref, anyOf/oneOf/allOf, format, and additionalProperties:true with errors that name the offending construct."
    ]
  },
  "contract": {
    "package_path": "internal/roundtable/dispatchschema",
    "entry_point": "Parse(raw json.RawMessage) (*Schema, error)",
    "supported_constructs": "Top-level object with typed scalar fields (string, number, boolean); optional enum constraint on string fields; optional required: [field, ...] array.",
    "rejection_modes": "Returns a descriptive error for nested objects, arrays, anyOf/oneOf/allOf, $ref, format, additionalProperties: true, or any keyword outside the supported subset."
  },
  "needs": [],
  "prd_invariants_exercised": [],
  "backlog_fields": {
    "prd_slug": "dispatch-structured-output",
    "prd_exempt_reason": null,
    "spec_ref": null,
    "design_refs": [],
    "needs_ids": [],
    "human_markers": []
  },
  "classification": "JSON_PRD_PATH"
}
```

## roundtable-precheck-review

### Attempt 1 — critique (6-panelist canvass)

Panel: claude-opus-4-7, codex (cli-default), gemini-3.1-pro-preview, fireworks-deepseek-v4-pro, fireworks-kimi-k2p6, fireworks-minimax-m2p7.

Verdicts: 5 CONCERNS, 1 BLOCKED (gemini). No APPROVED.

**Strong consensus (6/6): flip `safety_auditor_needed` NO → YES.**
Reason: parser is an attack surface for malformed input — DoS via large enum arrays / deeply nested input, panic on malformed bytes, type-confusion (number where string-enum expected), allocation amplification. Absence of registered INV-### is not a reason to skip safety review.

**Marginal signal (3/6): flip `arch_advisor_needed` NO → YES.**
Reason: new sibling package `internal/roundtable/dispatchschema` is a structural decision that downstream F02/F03/F04 will couple to.

**Marginal signal (2/6): flip `researcher_needed` NO → YES.**
Reason: even a "lite" subset borrows JSON Schema semantics; `enum`, `required`, `additionalProperties` interactions are spec-adjacent.

**Single-panelist signal (1/6): flip `designer_needed` NO → YES (backend-designer).**
Claude flagged that internal `Schema`/`Field` shape, error type (sentinel vs structured), and field-order preservation are pinned only by the contract and ripple into all consumers.

**Counter-signals on complexity (cancel):** kimi → trivial; minimax → architecture-tier. Hold at standard.

### Synthesis

The unanimous flag is `safety_auditor_needed`. The pre-check rationale was "no INV-### registered → nothing to audit" — but the panel argues the auditor's job is precisely to surface input-hardening risks the parser introduces, regardless of registered invariants. Pre-check should re-evaluate this flag and decide whether (a) flip to YES, (b) hold and add an explicit "no auditable invariants in repo" rationale, or (c) treat parser hardening as an implementer + code-reviewer responsibility (test for malformed inputs, fuzz-test, bounded-allocation contract).

### Attempt 1 — pre-check revision (consensus sent back)

**Decision: FLIP `safety_auditor_needed: NO → YES`.**

The panel is right and the original rationale was wrong. `Parse(raw json.RawMessage)` is an external-input trust boundary: F04 wires this parser to MCP-supplied schema bytes, which means malformed/adversarial input reaches it via the dispatch path. The risks the panel enumerated — DoS via large enum arrays or deeply nested input, panic on malformed bytes, type-confusion (number where string-enum expected), allocation amplification on `map[string]json.RawMessage` decode of attacker-shaped input — are precisely the class safety-auditor exists to surface. "No INV-### registered" is not an exemption; per safety-auditor.md the auditor's role includes flagging input-hardening gaps and proposing invariant registrations where they're missing. Treating this as implementer + code-reviewer territory only (option c) inverts the gate's purpose: those agents enforce contracts, the auditor surfaces the contracts that are missing. Marginal flags (arch-advisor, researcher) held — sub-majority and the contract surface is small enough that designer-less direct-to-test-writer remains correct. Updated YAML in frontmatter (line 14) and `## pre-check` block (line 64); both now read `safety_auditor_needed: YES`.

## researcher
<!-- Research brief appended here (if applicable) -->

### Decisions (optional)
<!-- Key choices made and why. Max 5 bullets. -->

## arch-advisor-consultation
<!-- Architecture guidance appended here by Arch-advisor at Step 1.7 (if applicable) -->

### Constraints for downstream
<!-- Arch-advisor's MUST/MUST NOT directives for designers/implementers. -->

## backend-designer / frontend-designer
<!-- Design brief appended here (if applicable) -->

### Decisions
<!-- Key choices made and why. Max 5 bullets. -->
### Constraints for downstream
<!-- MUST/MUST NOT directives for downstream agents. Max 5 bullets. -->

## roundtable-design-review
<!-- Multi-model advisory review of designer output (Step 2.5, if roundtable enabled).
     Orchestrator calls roundtable-blueprint + roundtable-critique tools. Output appended here. -->

## test-writer

**File:** `internal/roundtable/dispatchschema/schema_test.go`
**Package:** `dispatchschema_test` (black-box)

**Test functions and assertion traceability:**

| Test function | Assertion pointer |
|-|-|
| `TestParseScalarFields` | `/features/0/oracle/assertions/0` |
| `TestParseEnumConstraints` | `/features/0/oracle/assertions/1` |
| `TestParseRequiredField` | supplementary (required accessor) |
| `TestParseEmptyProperties` | edge case: empty properties accepted |
| `TestParseRejectsUnsupportedSubset` | `/features/0/oracle/assertions/2` |
| `TestParseRejectionModes` (table, 8 cases) | `/features/0/oracle/assertions/3` |
| `TestParseEdgeCases` (6 sub-tests) | edge cases from execution brief |
| `TestParseRobustness` (5 sub-tests + 1 explicit error check) | safety-auditor robustness concern |

**Rejection mode table cases (assertion 3):** `nested-properties-object`, `type-array`, `$ref`, `anyOf`, `oneOf`, `allOf`, `format`, `additionalProperties-true`.

**Expected initial failure mode:** `no non-test Go files in .../dispatchschema` — package under test does not exist yet. This is correct TDD ordering (RED-NEW).

**Status:** RED-NEW

### Decisions (optional)
- Used `dispatchschema_test` (black-box) package — public surface only as constrained.
- Accessor names assumed: `schema.Fields() []Field`, `field.Name() string`, `field.Type() string`, `field.Enum() []string`, `schema.Required() []string`. Implementer must match these names.
- Robustness sub-tests use a `recover()` deferred guard so a panic is reported as a test failure, not a process crash.
- `TestParseRejectionModes` is table-driven and names each of the 8 rejection modes separately per the brief's explicit instruction.

## implementer

**File:** `internal/roundtable/dispatchschema/schema.go`
**LOC:** 306 (incl. package doc + comments)
**Change scope:** initial
**Imports:** `encoding/json`, `fmt`, `strings` (stdlib only — no third-party JSON Schema lib)

**Exported surface:**
- `func Parse(raw json.RawMessage) (*Schema, error)`
- `type Schema struct{...}` (unexported fields)
- `func (s *Schema) Fields() []Field`
- `func (s *Schema) Required() []string`
- `type Field struct{...}` (unexported fields)
- `func (f Field) Name() string`
- `func (f Field) Type() string`
- `func (f Field) Enum() []string`

**Test status:** PASS — 8 test functions, 27 sub-tests, all green.
**Verify command:** `mise exec go@1.26.2 -- env GOTOOLCHAIN=local GOMODCACHE=/tmp/gomodcache GOCACHE=/tmp/gocache go test ./internal/roundtable/dispatchschema/...`

**Final test output (last ~10 lines):**
```
--- PASS: TestParseRobustness (0.00s)
    --- PASS: TestParseRobustness/truncated-json (0.00s)
    --- PASS: TestParseRobustness/empty-input (0.00s)
    --- PASS: TestParseRobustness/deeply-nested-array-in-enum (0.00s)
    --- PASS: TestParseRobustness/null-property-value (0.00s)
    --- PASS: TestParseRobustness/number-as-property-value (0.00s)
    --- PASS: TestParseRobustness/truncated-must-error (0.00s)
PASS
ok  	github.com/TejGandham/roundtable/internal/roundtable/dispatchschema	0.003s
```

`gofmt -l` clean; `go vet` clean.

### Decisions
- **Two-stage decode pattern from `internal/roundtable/result.go:80-101`**: top-level `map[string]json.RawMessage` then per-key targeted `json.Unmarshal`. Keeps the parser permissive at stage 1 so unknown keywords are visible by name, then strict at stage 2 with descriptive errors that name the offending construct.
- **Order of checks inside `parseField` matters**: validate `type` first when present (so `type:"object"` / `type:"array"` errors name the type), then scan for unknown keywords (`$ref`, `format`), then require `type`. Initial implementation reversed this and the `nested-properties-object` test failed because `unsupported keyword "properties"` fired before the `type:"object"` check — fixed by reordering.
- **Property order preserved via `json.Decoder` token stream** on the `properties` raw bytes; `map[string]json.RawMessage` would discard order. Tests don't assert order on `Fields()` directly but `Enum()` order is asserted, and downstream F02 (prompt-suffix builder) will likely care about field order.
- **`required` array cross-checked against declared properties** (edge case from execution brief). Reject with the offending name in the error so F03 callers can route on it.
- **No `recover()` needed**: `encoding/json` returns errors, doesn't panic, on the malformed inputs the test exercises (truncated, empty, deeply nested, null property, number-as-property). Robustness tests pass without a panic guard. Empty input is handled explicitly with a `len(raw) == 0` short-circuit before unmarshal.

**Next hop:** code-reviewer
<!-- NOTE: Implementer does NOT get "Constraints for downstream" —
     its downstream agents (spec-reviewer, safety-auditor) are its
     REVIEWERS. Constraining reviewers undermines gate integrity. -->

### Roundtable-attempt-1 fix

**Concern:** Step 8.5 roundtable landing review (1 BLOCKED + 2 advisory) flagged
unnecessary byte→string copy in `parseProperties`.

**Change scope:** rework (from roundtable-landing-review findings)
**File:** `internal/roundtable/dispatchschema/schema.go`

**Lines changed:**
- Imports (lines 15-19): swapped `"strings"` → `"bytes"` (strings was only used at the call site below).
- Line 159 (`parseProperties`):
  - Before: `dec := json.NewDecoder(strings.NewReader(string(raw)))`
  - After:  `dec := json.NewDecoder(bytes.NewReader(raw))`

**Behavior:** unchanged — `bytes.NewReader` and `strings.NewReader(string(raw))`
present the same `io.Reader` byte stream to `json.NewDecoder`. The fix removes
one full-payload allocation (the `string(raw)` copy).

**Test result:** PASS
`mise exec go@1.26.2 -- env GOTOOLCHAIN=local GOMODCACHE=/tmp/gomodcache GOCACHE=/tmp/gocache go test ./internal/roundtable/dispatchschema/...`
→ `ok  github.com/TejGandham/roundtable/internal/roundtable/dispatchschema  0.003s`

## code-reviewer
<!-- Code quality review appended here.
     Agent outputs **Verdict:** APPROVED or CHANGES NEEDED.
     The orchestrator copies the verdict to proceed or loop. -->

## spec-reviewer

### Spec Conformance: JSON-Schema-lite subset parser

**Verdict:** CONFORMANT
**Attempt:** 1
**PRD:** docs/exec-plans/prds/dispatch-structured-output.json#F01
**Code:** internal/roundtable/dispatchschema/schema.go
**Tests:** internal/roundtable/dispatchschema/schema_test.go

**Deviations:** None.

**Contract conformance:**

| Key | Declared | Observed |
|-|-|-|
| `package_path` | `internal/roundtable/dispatchschema` | File at that path; `package dispatchschema` declared |
| `entry_point` | `Parse(raw json.RawMessage) (*Schema, error)` | Exact signature match at schema.go:89 |
| `supported_constructs` | string/number/boolean scalars; optional enum on string; optional required array | `allowedScalarTypes` map + enum guard at line 251 + required parsing at line 143 |
| `rejection_modes` | nested objects, arrays, anyOf/oneOf/allOf, $ref, format, additionalProperties:true, any keyword outside subset | Closed-set `allowedTopLevel` + `allowedFieldKeys` maps; errors name the construct verbatim |

**Oracle assertion coverage:**

| Pointer | Assertion | Test | Verdict |
|-|-|-|-|
| `/features/0/oracle/assertions/0` | Parser accepts typed scalar fields, returns schema without error | `TestParseScalarFields` | COVERED |
| `/features/0/oracle/assertions/1` | Parser accepts enum on string fields, exposes allowed values | `TestParseEnumConstraints` | COVERED |
| `/features/0/oracle/assertions/2` | Parser rejects unsupported constructs with descriptive error | `TestParseRejectsUnsupportedSubset` | COVERED |
| `/features/0/oracle/assertions/3` | Rejects 8 specific constructs, error names the construct | `TestParseRejectionModes` (8 table cases) | COVERED |

**Constraint conformance (from pre-check):**
- MUST stdlib only / two-stage decode pattern → PASS (`encoding/json`, `fmt`, `strings` only).
- MUST NOT third-party JSON Schema lib → PASS.
- MUST NOT scope beyond F01 contract → PASS.
- MUST NOT files outside new package → PASS.

**Notes (review-debt, not deviations):**
- [MINOR] `errAs` hand-rolls a type assertion instead of `errors.As`. Flagged by code-reviewer.
- [MINOR] No upper bound on enum/required array length. Safety auditor to formalize.

**Coverage gaps:** None.

**Next hop:** safety-auditor

## safety-auditor
<!-- Audit report appended here (if applicable).
     Agent still outputs **Verdict:** in its section for human readability.
     The orchestrator copies the verdict to the YAML frontmatter. -->

## arch-advisor-verification
<!-- Independent structural review appended here by Arch-advisor at Step 7.5 (if applicable).
     Agent still outputs **Verdict:** in its section for human readability.
     The orchestrator copies the verdict to the YAML frontmatter. -->

## landing-verifier
## Landing Report: JSON-Schema-lite subset parser

**Pipeline:** backend
**Verification:** tests PASS (8 functions, 27 sub-tests, cached from implementer run — source unchanged); full repo build clean; go vet clean; gofmt clean
**Spec conformance:** CONFIRMED
**Safety audit:** PASS
**Code review:** APPROVED
**Architecture review:** N/A (no arch-advisor dispatched)
**Doc drift:** NONE

**Gate verdicts in YAML frontmatter:**
- code_review_verdict: APPROVED
- spec_review_verdict: CONFORMANT
- safety_verdict: PASS
- roundtable_precheck_verdict: APPROVED

**Untracked files:** handoff, new package dir, scripts/__pycache__ — all expected; no unintended mutations.

**Deferred (not blocking):** Safety auditor recommends registering INV-001 (bounded-allocation contract for external-input parsers) in a separate /keel-refine cycle before F04 lands.

**Status:** VERIFIED

**Next hop:** orchestrator (runs roundtable review if enabled, then Step 9 post-landing procedure)


## roundtable-landing-review

### Attempt 1 — crosscheck (6 panelists)

| Panelist | Verdict | Note |
|-|-|-|
| Claude (analyst) | APPROVED | All 4 pre-check concerns closed by implementation |
| Codex (codereviewer) | APPROVED (overall, with 2 sub-CONCERNS) | Allocation, accessor mutation |
| Gemini (planner) | **BLOCKED** | `string(raw)` byte→string copy must fix |
| Fireworks-deepseek | APPROVED | All concerns resolved |
| Fireworks-kimi | APPROVED (overall, with 1 codereviewer-line CONCERNS) | Allocation copy; double type unmarshal; swallowed decoder error; `enum:null` handling |
| Fireworks-minimax | APPROVED | All 4 concerns resolved by implementation |

**Consensus action:** apply `bytes.NewReader(raw)` fix to address `string(raw)` allocation amplification.

### Attempt 1 — implementer fix

Single-line change at `internal/roundtable/dispatchschema/schema.go:159`:
- `strings.NewReader(string(raw))` → `bytes.NewReader(raw)`
- Import block: `"strings"` removed, `"bytes"` added

Tests: PASS (cached). Build: clean. Vet: clean. Gofmt: clean.

### Attempt 1 — gate re-run chain

| Gate | Counter | Verdict |
|-|-|-|
| code-reviewer | `roundtable_retry_code_review_attempt: 1` | APPROVED |
| spec-reviewer | `roundtable_retry_spec_review_attempt: 1` | CONFORMANT |
| safety-auditor | `roundtable_retry_safety_attempt: 1` | PASS (fix REDUCED prior RISK-class allocation finding) |
| landing-verifier | (orchestrator-direct) | tests + build + vet + gofmt all clean |

### Attempt 2 — crosscheck (6 panelists)

| Panelist | Verdict | Note |
|-|-|-|
| Claude (analyst) | CONCERNS (advisory) | Stale `errAs` doc-comment references `strings` after the import was removed — non-load-bearing nit |
| Codex (codereviewer) | APPROVED | Fix correct, no new issues |
| Gemini (planner) | APPROVED | Original BLOCKED concern fully resolved |
| Fireworks-deepseek | APPROVED | Zero-copy pass-through confirmed |
| Fireworks-kimi | APPROVED | Equivalent at the `json.NewDecoder` boundary |
| Fireworks-minimax | APPROVED | No new surface area |

### Synthesis

5/6 APPROVED. 1 advisory CONCERNS on a non-load-bearing doc comment (Claude). The original BLOCKED concern (gemini) is fully resolved. Setting `roundtable_landing_verdict: APPROVED`. Status → READY-TO-LAND.

The doc-comment nit is recorded as follow-up tech-debt; not worth a third gate cycle for one stale word.

## Code Review: JSON-Schema-lite subset parser (F01)

**Verdict:** APPROVED

**Files reviewed:**
- `/mnt/agent-storage/vader/src/roundtable/internal/roundtable/dispatchschema/schema.go` (306 LOC)
- `/mnt/agent-storage/vader/src/roundtable/internal/roundtable/dispatchschema/schema_test.go`

**Neighboring files compared:**
- `internal/roundtable/result.go:69-101` (two-stage decode pattern reference)
- `internal/roundtable/codex_fallback.go` (package/import convention)
- `internal/roundtable/openai_http.go` (errors.As usage in codebase)

---

### Findings

- [MINOR] `schema.go:300-306` — `errAs` hand-rolls a type assertion instead of using `errors.As`. The comment justifies skipping the `errors` import, but `errors.As` correctly unwraps error chains while the direct cast (`err.(*json.UnmarshalTypeError)`) silently fails if encoding/json ever wraps the error. The `strings` import is already present for lower-value work; the cost of adding `errors` is one import line. The codebase uses `errors.As/Is` in `openai_http.go` — this is inconsistent with the established pattern.
  Current: direct type assertion with no chain traversal
  Suggestion: import `errors`, replace `errAs` body with `errors.As(err, target)`; drop the helper entirely and call inline

- [MINOR] `schema.go:254-257` — No upper bound on enum values. An attacker-supplied schema with `"enum": [10,000 strings]` allocates without limit. The pre-check roundtable named allocation amplification on large enums as the primary DoS vector. The safety auditor will surface an invariant registration request here, but code-reviewer notes it as a missing hardening gap: a simple cap (e.g., 256 or 1024 values) would make the bound explicit and testable before the safety gate.
  Current: `json.Unmarshal(rawEnum, &values)` with no size check
  Suggestion: after unmarshal, `if len(values) > maxEnumValues { return error }` where `maxEnumValues` is a package-level const

- [MINOR] `schema.go:266-280` — `parseRequired` has the same unbounded allocation issue for large `required` arrays. Same cap pattern applies. Low severity in isolation (required arrays are typically tiny) but consistent with the enum concern.

- [NITPICK] `schema.go:159` — `strings.NewReader(string(raw))` allocates a string copy of the raw bytes. `bytes.NewReader(raw)` avoids the copy. `strings` import is still needed elsewhere (none — actually the `strings` package import at line 18 is only used here). Switching to `bytes.NewReader` would drop the `strings` import.
  Current: `strings.NewReader(string(raw))`
  Suggestion: `bytes.NewReader(raw)` and swap `"strings"` for `"bytes"` in the import block

- [NITPICK] `schema.go:193-195` — The closing `}` token is silently discarded (`_, _ = dec.Token()`). The comment acknowledges this is fine because the decoder surfaces errors earlier. This is correct, but the comment is subtly wrong: a maliciously crafted trailing garbage after the closing brace (e.g., `{...}JUNK`) would silently pass because the decoder stops at `}`. In practice, the outer `json.Unmarshal` of `raw` already validated the full document at the top-level stage, so trailing junk in `rawProps` is impossible — but the comment should say that, not imply it's a streaming-decoder guarantee.

---

### Summary

Implementation is clean, well-commented, follows the two-stage decode pattern from `result.go`, and correctly orders type-check before keyword-scan inside `parseField` to produce accurate error messages. All oracle assertions are covered. The only meaningful finding is the `errAs` helper deviating from the established `errors.As` pattern (inconsistency risk on future encoding/json wrapping), and missing allocation caps on enum/required arrays (safety auditor will formalize these as invariants). No CRITICAL or MAJOR findings.

**Verdict:** APPROVED

**Next hop:** spec-reviewer

## Safety Audit: JSON-Schema-lite subset parser

**Verdict:** PASS

**PRD:** docs/exec-plans/prds/dispatch-structured-output.json#F01
**Feature ID:** F01
**Files scanned:**
- `internal/roundtable/dispatchschema/schema.go` (306 LOC)
- `internal/roundtable/dispatchschema/schema_test.go` (316 LOC)

### Repo state

No INV-### registered in the repo (CLAUDE.md `## Safety Rules` section is absent; PRD `prd_invariants_exercised: []`). This audit was dispatched on the unanimous (6/6) pre-check roundtable flag that the parser is an external-input attack surface deserving review regardless of registered invariants. Findings are evaluated against the 5 attack vectors the panel named.

### Attack-vector evaluation

| # | Vector | Code site | Evaluation | Class |
|-|-|-|-|-|
| 1 | DoS via large `enum` array | `schema.go:254-258` | `json.Unmarshal(rawEnum, &values)` has no upper bound. Adversarial `enum: [10000+ strings]` allocates without limit. Bounded only by upstream MCP transport message size. | RISK |
| 1b | DoS via large `required` array | `schema.go:266-280` | Same unbounded `json.Unmarshal` pattern. Same shape of risk; lower in practice (required arrays typically tiny). | RISK |
| 2 | Type-confusion (number where string-enum expected) | `schema.go:250-258` | `if typ != "string" { reject }` runs before `json.Unmarshal(rawEnum, &[]string{})` — the latter additionally fails on non-string values via `*json.UnmarshalTypeError`. Two-layer defense. `TestParseEdgeCases/enum-on-non-string-field` covers the symmetric case. | DEFENDED |
| 3 | Panic surfaces | `schema.go` (entire) | All token type assertions (`tok.(json.Delim)`, `tok.(string)`) are `ok`-guarded. `errAs` uses guarded assertion. Pointer derefs nil-checked (`Fields()`, `Required()`). `encoding/json` returns errors, not panics, on malformed bytes. `TestParseRobustness` exercises 5 malformed inputs under `recover()` deferred guard — all pass. | DEFENDED |
| 4 | Allocation amplification (deep-nested input) | `schema.go:97`, `schema.go:204` | Top-level decode is `map[string]json.RawMessage` — values stay as raw byte slices, no recursive allocation. `parseField` body decode is the same shape. Nested objects in property descriptors are rejected at field-level (`type:object` errors) before any sub-parse runs. Stack depth bounded by Go stdlib's internal recursion limit. | DEFENDED |
| 5 | Token-stream pitfalls (`json.Decoder` in `parseProperties`) | `schema.go:159-195` | First-token `{`-check guarded (line 164-167). `dec.More()` loop properly handles EOF. Name-token string-check guarded (line 175-178). `dec.Decode(&body)` into `json.RawMessage` cannot decode-fail except on malformed JSON, which the outer `json.Unmarshal(raw, &top)` at line 98 has already rejected. The discarded closing-`}` token (line 194) is safe for that reason — code-reviewer's NITPICK on the comment wording is accurate but not a vulnerability. | DEFENDED |

### Secondary findings

- `schema.go:159` — `strings.NewReader(string(raw))` copies the raw bytes, doubling peak memory by 1x. Bounded by input size, not amplifying. NITPICK only (also flagged by code-reviewer).
- No upper bound on number of properties in a schema (separate from `enum`/`required`). Same shape of unbounded-allocation risk on `top["properties"]` if attacker supplies `properties: {<10000 entries>}`. Same mitigation as #1.
- No upper bound on input size itself. The trust boundary is the caller of `Parse(raw json.RawMessage)`; whether to bound `len(raw)` belongs to the caller's contract (F04 MCP wiring), not this parser.

### Classification

No VIOLATION-class findings. The 5 enumerated attack vectors are either fully defended (vectors 2, 3, 4, 5) or are RISK-class without a registered contract to violate (vector 1, 1b — unbounded allocation on `enum`/`required`).

The unbounded-allocation issue is real but not a violation because:
1. No INV-### in the repo defines a bounded-allocation contract.
2. The threat model is MCP-supplied schemas via F04 — bounded by the MCP transport's own message size limit upstream of this parser.
3. The code-reviewer already flagged the same gap as MINOR, and `spec-reviewer` noted "Safety auditor to formalize" — both reviewers correctly identified this as the auditor's call on whether to escalate.

### Invariant registration recommendation

**REGISTER INV-001 NOW.** This feature is the right trigger because:

1. F01 introduces the first parser of external/untrusted bytes in the codebase. The decision will recur for every future parser (response validators, prompt parsers, tool-call decoders).
2. F04 will wire this parser to MCP-supplied bytes — that's a concrete external trust boundary landing in the same release train.
3. The bound is a simple numeric contract (e.g., `maxEnumValues = 256`, `maxRequiredFields = 256`, `maxProperties = 256`) — cheap to specify, cheap to test, cheap to enforce.
4. Retrofitting an invariant later means revisiting every parser already merged. Establishing the precedent on F01 prevents that drift.

**Proposed INV-001 text:**
> Any parser exposed to external/untrusted input MUST cap the size of decoded collections (arrays, maps, property lists) with named package-level constants and reject inputs exceeding the cap with a descriptive error that names the offending construct and the bound.

Registering this invariant requires a separate `/keel-refine` cycle to add `## Safety Rules` to CLAUDE.md and is out of scope for F01's pipeline. **F01 does NOT block on invariant registration** — under the current "no INV-### registered" repo state, the parser is materially safe within the documented threat model.

### Next hop

`landing-verifier`


## code-reviewer

### Roundtable-attempt-1 re-review

**Verdict:** APPROVED

Import block: `"strings"` removed, `"bytes"` added. Line 159: `bytes.NewReader(raw)` confirmed. Tests: PASS (cached).
