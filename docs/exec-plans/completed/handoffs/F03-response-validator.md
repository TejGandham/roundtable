# F03 — Per-panelist response validator with structured error surfacing

---
status: LANDED
pipeline: backend
prd_ref: docs/exec-plans/prds/dispatch-structured-output.json#F03

# Pre-check routing (set by pre-check, read by orchestrator)
intent: build
complexity: complex
designer_needed: YES
researcher_needed: NO
safety_auditor_needed: YES
arch_advisor_needed: YES
implementer_needed: YES

# Gate verdicts (set by orchestrator after each gate agent)
spec_review_verdict: CONFORMANT
spec_review_attempt: 1
safety_verdict: PASS
safety_attempt: 1
code_review_verdict: APPROVED
code_review_attempt: 1
arch_advisor_verdict: SOUND

# Arch-advisor re-run counters (separate from initial gate passes)
# Used when arch-advisor UNSOUND triggers a re-run of gates
arch_retry_spec_review_attempt: 0
arch_retry_safety_attempt: 0

# Pipeline configuration
remote_name: origin
roundtable_enabled: true
pr_url: https://brahma.myth-gecko.ts.net:3000/stackhouse/roundtable/pulls/19

# Roundtable pre-check review (Step 1.3)
roundtable_precheck_attempt: 1
roundtable_precheck_verdict: CONCERNS
roundtable_precheck_skipped:         # true (with reason) if MCP unavailable

# Roundtable design review (Step 2.5)
roundtable_design_attempt: 1
roundtable_design_verdict: CONCERNS  # 1 verified-false BLOCKED (Q2/Q6 — codex verified omitempty elides zero-length slice); 1 partial BLOCKED (Field grammar — designer escape rules cover it); legitimate advisories folded into test-writer brief
roundtable_skipped:                  # n/a, MCP available (deepseek/kimi timed out, 3 panelists effective)

# Roundtable landing review (Step 8.5)
roundtable_landing_attempt: 2
roundtable_landing_verdict: APPROVED  # 6/6 unanimous on attempt 2; both attempt-1 BLOCKED defects fixed (unclosed-fence-after-closed; null-scalar coercion)

# Roundtable-triggered gate re-run counters (separate from initial passes)
roundtable_retry_code_review_attempt: 1
roundtable_retry_spec_review_attempt: 1
roundtable_retry_safety_attempt: 1
---

## pre-check

## Execution Brief: Per-panelist response validator with structured error surfacing

**PRD:** /mnt/agent-storage/vader/src/roundtable/docs/exec-plans/prds/dispatch-structured-output.json
**Feature ID:** F03
**Feature index:** 2
**Feature pointer base:** /features/2
**Layer:** service
**PRD-level invariants:** none
**Dependencies:** MET — F01 [x] (parser at `internal/roundtable/dispatchschema/schema.go`), F02 [x] (prompt suffix at `internal/roundtable/dispatchschema/prompt.go`).
**Research needed:** NO (no new third-party libs; stdlib `encoding/json` for parse + the F01 `*Schema` API for shape validation are sufficient).
**Designer needed:** NO (layer=service, not UI; pure Go function with deterministic contract — no interface design surface).
**Implementer needed:** YES
**Safety auditor needed:** YES — F03 PARSES untrusted panelist text. This is the third parser-class attack surface in this PRD bundle (F01 = schema parser; F02 = name/enum sanitizer; F03 = response parser). Fence-extraction logic must not be bypassable, JSON parse must be panic-free on hostile input, and `Excerpt` field caps must hold so a 10MB response cannot inflate `StructuredError`. The PRD-level invariants list is empty, so the auditor will need to reason from the contract directly (auditor profile is unconfigured — that triggers VIOLATION fail-closed; the orchestrator should configure invariants or accept the audit-bypass cost before landing).
**Arch-advisor needed:** YES — F03 is the **first F0x in this PRD bundle to modify existing main-line code** (`internal/roundtable/result.go`). The two new `Result` fields (`Structured`, `StructuredError`) become permanent wire-format obligations on every dispatch result returned by every backend, every tool. JSON tag spelling, `omitempty` semantics, and pointer-vs-value choice are one-shot decisions that downstream F04 wiring locks in across `roundtable-canvass`, `-deliberate`, `-blueprint`, `-critique`, `-crosscheck`. Arch-advisor verifies the wire-format extension is cleanly bolted on without coupling `result.go` to the `dispatchschema` package in a way that creates an import cycle or a leaky abstraction.

**Intent:** build
**Complexity:** complex

**What to build:**
1. A `Validate(response string, schema *Schema) (parsed json.RawMessage, vErr *ValidationError)` function plus a `ValidationError{Kind, Field, Message, Excerpt}` struct in a new file `internal/roundtable/dispatchschema/validate.go`. The function locates the **last** fenced ` ```json ` block in `response`, JSON-decodes it, and validates the decoded value against `schema` (typed-scalar conformance; enum membership for string-enum fields; presence of fields named in `schema.Required()`).
2. A wire-format extension to the existing `Result` struct in `internal/roundtable/result.go` adding `Structured *json.RawMessage` (json tag `"structured,omitempty"`) and `StructuredError *ValidationError` (json tag `"structured_error,omitempty"`). The omitted-schema path must produce byte-equivalent JSON to today's output — both fields nil and elided by `omitempty`.

**New files:**
- `internal/roundtable/dispatchschema/validate.go` — defines `ValidationError` struct (exported fields: `Kind string`, `Field string`, `Message string`, `Excerpt string`, with appropriate `json:` tags), the `Validate(response, schema)` function, and unexported helpers for fenced-block extraction and per-field type/enum checks. Stdlib-only: `encoding/json`, `fmt`, `strings`. No `regexp` (linear scan is faster and panic-safe on giant inputs).
- `internal/roundtable/dispatchschema/validate_test.go` — package-external tests (`package dispatchschema_test`) one-to-one against `/features/2/oracle/assertions[0..3]`, plus unit cases for each `Kind` value, last-fence selection when multiple blocks present, the 200-char `Excerpt` cap, and prompt-injection-safe parsing (no panics on hostile input).

**Modified files:**
- `internal/roundtable/result.go` — add two struct fields to `Result`. Use the `dispatchschema.ValidationError` type by name (requires adding `"github.com/TejGandham/roundtable/internal/roundtable/dispatchschema"` to the imports). Verify no import cycle: `dispatchschema` does NOT currently import the `roundtable` package, and must NOT after this change (one-way dependency: `roundtable` → `dispatchschema`).
- `internal/roundtable/result_test.go` — extend `TestResultJSONNilPointers` (or add a new test) to assert that when `Structured` and `StructuredError` are nil, the marshaled JSON contains neither `"structured"` nor `"structured_error"` keys (byte-equivalence to today's output). Add a positive test confirming both fields appear when populated.

**Existing patterns to follow:**
- `internal/roundtable/dispatchschema/schema.go:Parse` — two-stage `map[string]json.RawMessage` decode for safe inspection of unknown shapes; reject-mode errors that name the offending construct. Mirror the same defensive style: assume hostile input, return descriptive errors, never panic.
- `internal/roundtable/dispatchschema/schema.go` package doc comment — set the same expectation in the new file's package-level godoc for `Validate`: stdlib-only, no panics, descriptive errors.
- `internal/roundtable/dispatchschema/prompt.go:BuildPromptSuffix` — fence convention is ` ```json ` opening, matching triple-backtick line closing. The validator MUST extract the **last** such block (per F02 contract) and tolerate narrative text before it.
- `internal/roundtable/result.go:8-21` — existing `Result` struct field ordering and JSON-tag conventions (snake_case names, `omitempty` for optional pointers). New fields slot into the same convention.
- `internal/roundtable/dispatchschema/prompt_test.go` — package-external `dispatchschema_test` test layout, table-driven assertion style with explicit `/features/<idx>/oracle/assertions/<aidx>` traceability comments.

**Assertion traceability:**
- `/features/2/oracle/assertions/0` → Conforming response: feed a response containing exactly one valid ` ```json {...} ``` ` block matching the schema's typed scalars + enum constraints; assert `parsed` is non-nil JSON bytes equal to the block content (whitespace-normalized via re-marshal) and `vErr` is nil.
- `/features/2/oracle/assertions/1` → Violating response: feed a response with a fenced JSON block that breaks an enum constraint or supplies a wrong scalar type; assert `vErr.Kind == "schema_violation"`, `vErr.Field` names the violating dotted path, no retry occurs (this is verified by the absence of retry hooks — `Validate` is a pure function and exposes none), and `parsed` is nil.
- `/features/2/oracle/assertions/2` → On any validation failure, the *caller* (test harness simulating dispatch) must populate a `Result` such that `Result.Response` is the original raw text unchanged, `Result.Structured == nil`, and `Result.StructuredError` carries the `Kind`/`Field`/`Message`/`Excerpt` populated by `Validate`. Test asserts each field on the `Result` post-population. Excerpt ≤ 200 chars.
- `/features/2/oracle/assertions/3` → Nil-schema path: assert that calling `Validate(response, nil)` is **not** part of the contract — instead, the *caller* must check `if schema != nil` before calling `Validate`. Test simulates the dispatcher path: when `schema == nil`, `Validate` is not invoked, `Structured` and `StructuredError` stay nil, and `json.Marshal(result)` produces output containing neither key (byte-equivalence verified by absence-of-key, not by full byte diff against today's output, since other fields like `elapsed_ms` legitimately vary).

**Edge cases:**
- Multiple fenced blocks: must select the last one (per F02 fence convention; narrative-then-payload is the documented pattern).
- No fenced block found: `vErr.Kind == "missing_fence"`, `Field == ""`, `Message` describes "no fenced JSON block found in response", `Excerpt` is the first ≤200 chars of `response` for debugging.
- Fenced block opens with ` ```json ` but contains malformed JSON: `vErr.Kind == "json_parse"`, `Field == ""` (top-level), `Excerpt` is ≤200 chars of the offending fence body.
- Fenced block parses as JSON but is not an object (e.g. array, scalar): `vErr.Kind == "schema_violation"`, `Field == ""`, message names the actual JSON kind received.
- Schema requires field X (`schema.Required()` includes "X") but JSON omits it: `vErr.Kind == "schema_violation"`, `Field == "X"`, message is "required field missing".
- Enum-constrained string field receives a value not in the enum: `vErr.Field` is the field name, `Excerpt` is the rejected value (truncated to 200).
- Hostile / oversized input: a 10 MB response with a 1 MB JSON fence must not panic, must not allocate `Excerpt` > 200 chars, and must finish in linear time.
- Triple-backtick injection inside the JSON payload: handled at the F02 layer (sanitize on schema names/enums), but `Validate` must still tolerate ` ``` ` inside JSON string values without false-positive fence closure. Use a line-aware scan that treats fence delimiters as whole lines starting with ` ``` `, not raw substring search.
- Field path for nested errors: contract says "dotted path"; with the F01 schema subset (typed scalars only, no nested objects), top-level field name is sufficient. `Field` is empty for top-level structural errors and equals the field name for per-field errors.

**Risks:**
- **Wire-format permanence.** Once `Result.Structured` and `Result.StructuredError` ship to main, every existing MCP client sees them in dispatch responses. Renaming or retyping later is a breaking change. Arch-advisor must vet tag spelling, pointer choice, and `omitempty` semantics before merge.
- **Import cycle.** `result.go` references `dispatchschema.ValidationError`. Verify nothing in `dispatchschema/*.go` imports `internal/roundtable` directly or transitively (a cursory check confirms it's a leaf package today; the implementer must keep it that way).
- **Parser-class attack surface.** F03 reads attacker-controlled (panelist) text and produces JSON-decoded values that flow back to MCP clients. Hostile input scenarios (oversize, malformed, injection-via-fence) must not panic, exfiltrate, or amplify. Safety auditor scrutiny is mandatory.
- **Last-fence semantics under adversarial fences.** If a panelist (intentionally or via injection in the upstream prompt) emits multiple fenced blocks, the validator's "last block wins" rule is structural — but a malformed block immediately after a valid one would surface as `json_parse` and discard the earlier good block. The contract explicitly endorses this ("last block is canonical" per F02 design_facts), so this is correct behavior, not a bug — note it explicitly in the godoc so callers don't expect best-effort recovery.
- **Excerpt cap correctness.** A naïve `s[:200]` panics on byte slicing if the response is shorter and corrupts UTF-8 if the cut lands mid-rune. Use rune-aware truncation OR be explicit that 200 is a byte cap and document it.
- **Backwards-compat test.** Assertion 3 requires byte-equivalence on the omitted-schema path. The most robust assertion is "no `structured` or `structured_error` key appears in the marshaled JSON when both fields are nil," verified by `omitempty` machinery. A naïve full-bytes diff against a pre-recorded fixture would also pass but is brittle to legitimate dispatcher changes; prefer the structural assertion.

**Verify command:** `make test` (which under the hood runs `mise exec go@1.26.2 -- env GOTOOLCHAIN=local GOMODCACHE=/tmp/gomodcache GOCACHE=/tmp/gocache go test ./...`)

**Path convention:** Standard Go layout under `internal/roundtable/`. The `dispatchschema` package is a leaf package consumed by `internal/roundtable` and (in F04) `internal/stdiomcp`. Test files use `package dispatchschema_test` for external API tests, mirroring the F02 convention.

### Constraints for downstream

- MUST: Place `ValidationError` and `Validate` in the `dispatchschema` package (same package as `Schema`/`Field`/`Parse`/`BuildPromptSuffix`) so callers in `internal/roundtable` import a single package. Field exports: `Kind`, `Field`, `Message`, `Excerpt` — capitalized for marshal, with snake_case JSON tags (`kind`, `field`, `message`, `excerpt`).
- MUST: Use stdlib only (`encoding/json`, `fmt`, `strings`). No `regexp`, no third-party deps. Mirrors the F01/F02 stdlib-only contract.
- MUST: Extract the **last** fenced ` ```json ` block via line-by-line scan (not regex, not raw substring). A line containing only optional leading whitespace plus exactly ` ```json ` opens a block; a subsequent line containing only optional leading whitespace plus ` ``` ` closes it. If the last block is unclosed at end of response, it is "missing_fence" — do not silently accept partial blocks.
- MUST: When validation fails, leave `Result.Response` untouched and populate `Result.StructuredError` from `vErr`; never mutate `Response`. The `Validate` function itself does not write to `Result` — that is the caller's job (F04 wiring); F03's job is to produce the data the caller assigns.
- MUST: Cap `Excerpt` at 200 bytes. If using rune-aware truncation, cap at 200 runes — pick one and document. Choose **bytes** to keep `len(Excerpt) <= 200` trivially verifiable by the safety auditor.
- MUST NOT: Implement retry, fallback prompt, or any "fix the JSON" auto-repair logic. The PRD's `no_retry` clause is explicit. A `Validate` failure stops the validation — caller surfaces the error, dispatch continues without restructured value for that panelist.
- MUST NOT: Modify `Response`, `Status`, `Stderr`, or any other existing `Result` field. Only ADD `Structured` and `StructuredError`. Do not change existing JSON tags, do not reorder existing fields, do not change the constructor functions (`NotFoundResult`, `ProbeFailedResult`, `ConfigErrorResult`).
- MUST NOT: Introduce a regexp dependency (anti-slop: `regexp` for line splitting is gold-plating; `strings.Split` on `"\n"` is sufficient and faster).
- MUST NOT: Add configurability for the 200-char excerpt cap, the fence delimiter, or the last-block selection rule. These are spec-fixed.
- MUST NOT: Wire `Validate` into any of the dispatch tools or `roundtable.Run`. That is F04's scope. F03 ships `Validate` + `ValidationError` + the two `Result` fields, plus tests. Nothing else.
- MUST NOT: Add a `Validate(response, nil)` short-circuit that returns nil/nil. The PRD contract is "When the dispatcher invokes the validator with a nil schema (omitted-schema path), the validator is **not called**." A nil-schema short-circuit would muddy the contract; let callers branch on `schema != nil` before calling.
- MUST NOT: Add panic-recovery `recover()` blocks inside `Validate`. The function is pure and stdlib-only; if it panics, that is a defect to fix, not to paper over. (`json.Unmarshal` does not panic on malformed input.)
- MUST NOT: Inflate the diff with unrelated cleanups in `result.go`. Limit changes to the two new fields and necessary imports.

**Ready:** YES
**Next hop:** arch-advisor (Step 1.7 consultation on wire-format extension), then test-writer.

### Resolved feature (verbatim from keel-feature-resolve.py)

```json
{
  "ok": true,
  "feature_id": "F03",
  "feature_index": 2,
  "feature_pointer_base": "/features/2",
  "prd_path": "/mnt/agent-storage/vader/src/roundtable/docs/exec-plans/prds/dispatch-structured-output.json",
  "canonical_prd_path": "/mnt/agent-storage/vader/src/roundtable/docs/exec-plans/prds/dispatch-structured-output.json",
  "title": "Per-panelist response validator with structured error surfacing",
  "layer": "service",
  "oracle": {
    "type": "unit",
    "tooling": "Go test (go test ./...)",
    "assertions": [
      "Given a panelist response that conforms to the schema, the validator returns the parsed structured value attached to the per-panelist result.",
      "Given a panelist response that violates the schema, the validator surfaces the original raw text plus a structured error indicating which field(s) failed; no retry is invoked.",
      "On validation failure, Result.Response retains the raw text unchanged, Result.Structured is nil, and Result.StructuredError carries Kind/Field/Message/Excerpt describing the failure.",
      "When the dispatcher invokes the validator with a nil schema (omitted-schema path), the validator is not called; Result.Structured and Result.StructuredError remain nil and the wire format is byte-equivalent to current dispatch output."
    ]
  },
  "contract": {
    "entry_point": "Validate(response string, schema *Schema) (parsed json.RawMessage, vErr *ValidationError)",
    "result_extension": "Two new fields on Result: Structured *json.RawMessage (json:\"structured,omitempty\") and StructuredError *ValidationError (json:\"structured_error,omitempty\"). Both nil when no schema was supplied — no wire-format change for the omitted-schema path.",
    "validation_input": "Operates on the last fenced JSON code block in response (per F02). If no fenced block is found, emits a missing_fence error.",
    "error_shape": "ValidationError{ Kind: string, Field: string, Message: string, Excerpt: string }. Kind in {\"missing_fence\", \"json_parse\", \"schema_violation\"}. Field is a dotted path (empty for top-level errors). Excerpt is at most 200 characters of the offending text.",
    "no_retry": "Validation failure leaves Result.Response unchanged, Result.Structured nil, and Result.StructuredError populated; dispatch continues with no resend and no fallback prompt."
  },
  "needs": [
    "F01",
    "F02"
  ],
  "prd_invariants_exercised": [],
  "backlog_fields": {
    "prd_slug": "dispatch-structured-output",
    "prd_exempt_reason": null,
    "spec_ref": null,
    "design_refs": [],
    "needs_ids": [
      "F01",
      "F02"
    ],
    "human_markers": []
  },
  "classification": "JSON_PRD_PATH"
}
```

### Roundtable-attempt-1 revision

**Routing change:** `designer_needed: NO → YES` (YAML updated).

**Rationale.** 4/5 panelists correctly identified that F03 introduces durable public-API surface — `Result.Structured` / `Result.StructuredError` JSON envelope, `ValidationError.Kind` enum string set, `Field` path syntax — that propagates into 5 dispatch tools at F04. Initial classification treated these as implementation details under arch-advisor scope; that conflated structural impact (arch-advisor) with API ergonomics (designer). The two roles are orthogonal: arch-advisor verifies no import cycle / clean wire-format extension; backend-designer locks Card 0 (contract surface) decisions before implementation. With 5 dispatch tools reading these names downstream, contract decisions are one-way doors and warrant designer review.

**Held positions (panel split, not consensus):**
- Complexity stays `complex`. 2/5 escalated to `architecture-tier`; 3/5 silent. arch-advisor is already YES — escalating tier adds no new gate.
- Researcher stays `NO`. 1/5 flagged jsonschema-vs-custom; PRD already mandated typed-scalar + enum subset (F01 contract), so the path is settled.

**Card 0 design questions for backend-designer to resolve** (panel-surfaced, all contract-level, must be locked before test-writer):

1. **`Excerpt` truncation unit:** runes vs bytes? Panel preference: runes (UTF-8 safe). Current brief says bytes; designer must pick one and document. Affects `len(Excerpt) <= 200` assertion phrasing for safety auditor and for the per-field rejected-value excerpt path.
2. **`Result.Structured` Go type:** `*json.RawMessage` (pointer to slice) vs `json.RawMessage` (slice with `omitempty`)? Panel preference: drop the pointer — `json.RawMessage` is already a slice, `omitempty` elides nil slices, and the double indirection costs nothing. Current brief specifies pointer; designer revisits.
3. **`ValidationError.Field` path syntax:** dotted (`items.0.name`) vs bracket (`items[0].name`) vs JSON Pointer (`/items/0/name`)? F01's typed-scalar subset has no nested objects today, but the path syntax is wire-locked. Panel preference: pick one and document the choice. Designer commits.
4. **`ValidationError.Kind` representation:** bare strings (`"missing_fence"`) vs typed constants (`KindMissingFence string = "missing_fence"`)? Panel preference: typed constants exported from `dispatchschema`. Affects how F04 callers branch on Kind without stringly-typed `if vErr.Kind == "..."` chains.
5. **`Excerpt` redaction policy:** raw-but-capped, or structurally redacted (e.g., field values that look like secrets get masked)? Panel: define before landing. Safety auditor's domain — designer surfaces the choice, auditor ratifies.
6. **Byte-equivalence promise on omitted-schema path:** the brief says "byte-equivalent JSON" but `encoding/json` field ordering plus fields like `elapsed_ms` make literal byte-identity fragile. Panel preference: re-phrase as "byte-equivalent w.r.t. the fields-emitted set, not byte-identical text" — i.e., assertion 3 verifies absence-of-key, not full-buffer diff. Brief already leans this way (see Risks → "Backwards-compat test"); designer codifies it as the contract.

**Sequencing.** With designer YES, the next-hop chain becomes: arch-advisor (Step 1.7) → backend-designer (Step 2) → roundtable-design-review (Step 2.5) → test-writer. Designer receives this brief plus the 6 questions above as their Card 0 input.

## roundtable-precheck-review
<!-- Multi-model advisory review of pre-check routing (Step 1.3, if roundtable enabled).
     Orchestrator calls roundtable-critique + roundtable-canvass tools. Output appended here. -->

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

## Test Report: Per-panelist response validator with structured error surfacing

**PRD:** /mnt/agent-storage/vader/src/roundtable/docs/exec-plans/prds/dispatch-structured-output.json
**Feature ID:** F03
**Feature index:** 2
**Test files:**
- `internal/roundtable/dispatchschema/validate_test.go` (new)
- `internal/roundtable/dispatchschema/imports_test.go` (new)
- `internal/roundtable/result_test.go` (extended)

**Tests written:** 18 (15 in validate_test.go + 1 in imports_test.go + 2 in result_test.go)
**Status:** RED-NEW — `dispatchschema.Validate`, `dispatchschema.ValidationError`, `dispatchschema.KindMissingFence/KindJSONParse/KindSchemaViolation`, and `Result.Structured`/`Result.StructuredError` are all undefined; module under test does not exist yet. No syntax errors in test files.
**Failure output:**
```
internal/roundtable/dispatchschema/validate_test.go:45:33: undefined: dispatchschema.Validate
internal/roundtable/dispatchschema/validate_test.go:87:33: undefined: dispatchschema.KindSchemaViolation
internal/roundtable/dispatchschema/validate_test.go:168:38: undefined: dispatchschema.ValidationError
internal/roundtable/result_test.go:168:26: undefined: dispatchschema.ValidationError
internal/roundtable/result_test.go:179:3: unknown field Structured in struct literal of type Result
internal/roundtable/result_test.go:180:3: unknown field StructuredError in struct literal of type Result
```

**Assertion traceability:**
- `/features/2/oracle/assertions/0` → `TestValidate_ConformingResponse`
- `/features/2/oracle/assertions/1` → `TestValidate_ViolatingResponse`
- `/features/2/oracle/assertions/2` → `TestValidate_FailureResultFields`
- `/features/2/oracle/assertions/3` → `TestValidate_NilSchemaCallerBranch`, `TestResultJSON_StructuredFieldsAbsent`, `TestResultJSON_StructuredFieldsPresent`

### Decisions (optional)
- Excerpt cap asserted via `utf8.RuneCountInString(vErr.Excerpt) <= 200` — the locked decision is runes (designer/arch-advisor: rune-aware truncation via `unicode/utf8`).
- Discipline guardrail for `parsed == json.RawMessage("null")`: added explicit assertion in `TestValidate_ConformingResponse` — successful parse must not return the literal string `"null"` since a non-empty `json.RawMessage("null")` would slip past `omitempty`.
- `TestResultJSON_StructuredFieldsAbsent` uses `strings.Contains` (not full byte-diff) per the contract's key-absence semantics.
- `TestDispatchschemaIsLeafPackage` uses `go list -deps` and skips gracefully if the package doesn't compile yet (expected during RED-NEW phase); it becomes a hard enforcement once `validate.go` lands.
- `result_test.go` extended in `package roundtable` (internal) to access `Result` struct literal directly; imports `dispatchschema` for `ValidationError`.

**Next hop:** implementer

## implementer

### Roundtable-attempt-1 fix

**File:** `internal/roundtable/dispatchschema/validate.go`

**Defect 1 — `lastFencedJSON` stale-fallback (lines ~272-280):**
- Before: after the line scan, function unconditionally returned `blocks[len(blocks)-1]` even when `openIdx >= 0` (final fence opened but not closed). Stale prior block silently validated; contradicted package-doc contract at validate.go:31-33.
- After: explicit `if openIdx >= 0 { return "", false }` guard before the `len(blocks) == 0` check. Unclosed final fence now surfaces as `KindMissingFence` per contract. 7-line comment documenting the attack scenario added.

**Defect 2 — `checkField` accepts JSON null for scalar types (lines ~193+):**
- Before: `json.Unmarshal([]byte("null"), &s/&n/&b)` returns nil error; `{"x": null}` silently coerced to `""`/`0`/`false`. Boolean and number passes were the most dangerous (legitimate zero values).
- After: prepended a null-and-empty guard before the type switch — `if len(raw) == 0 || string(raw) == "null"` returns `KindSchemaViolation` with message `"expected <type>, got null"` and the raw bytes as excerpt (rune-capped). Applies uniformly to string/number/boolean.

**Lines changed:** 2 inserts in validate.go (8 lines + 16 lines including comments). No other files touched.

**Test status:** PASS — `go test ./internal/roundtable/dispatchschema/...` → `ok` (0.038s). All 18 existing tests still green; gofmt clean (no diff).

**Commands run:**
```
mise exec go@1.26.2 -- ... gofmt -l internal/roundtable/dispatchschema/validate.go    # empty output
mise exec go@1.26.2 -- ... go test ./internal/roundtable/dispatchschema/...           # ok
```

**Decisions:**
- Used `string(raw) == "null"` literal compare rather than `bytes.Equal` to avoid pulling a new import for a one-shot check; `raw` is already a `json.RawMessage` ([]byte) and the cost is a single allocation per scalar field. Mirrors the file's existing style (validate.go:135 already calls `topLevelKind([]byte(body))`).
- Also rejected `len(raw) == 0` in the same guard. Empty `raw` should not happen for a present field (json.Unmarshal of `{"x":}` errors at the json.Valid stage), but the defensive check is cheap and prevents a future caller-side bug from silently passing.
- Did NOT add a `null`-tolerant code path even for explicitly nullable schema fields. F01's schema lite subset has no `nullable` flag; if F0x adds one, that's a contract change requiring its own design pass.
- Preserved the unclosed-fence guard as a leading check (before `len(blocks) == 0`) so the "no fence at all" and "unclosed final fence" paths emit the same `KindMissingFence` from the same return — no behavior bifurcation downstream.
- Comments document the attack scenarios in-file so future readers see WHY (not just WHAT) — paranoia is the contract here.

**Next hop:** test-writer (regression tests for both defects per orchestrator brief).


### Decisions
<!-- Key choices made and why. Max 5 bullets. -->
<!-- NOTE: Implementer does NOT get "Constraints for downstream" —
     its downstream agents (spec-reviewer, safety-auditor) are its
     REVIEWERS. Constraining reviewers undermines gate integrity. -->

## code-reviewer
<!-- Code quality review appended here.
     Agent outputs **Verdict:** APPROVED or CHANGES NEEDED.
     The orchestrator copies the verdict to proceed or loop. -->

## spec-reviewer
<!-- Conformance report appended here.
     Agent still outputs **Verdict:** in its section for human readability.
     The orchestrator copies the verdict to the YAML frontmatter. -->

## safety-auditor
<!-- Audit report appended here (if applicable).
     Agent still outputs **Verdict:** in its section for human readability.
     The orchestrator copies the verdict to the YAML frontmatter. -->

## arch-advisor-verification
<!-- Independent structural review appended here by Arch-advisor at Step 7.5 (if applicable).
     Agent still outputs **Verdict:** in its section for human readability.
     The orchestrator copies the verdict to the YAML frontmatter. -->

## landing-verifier

### Verdict: VERIFIED

Direct orchestrator-run checks (post F02 merge to main, pulled origin/main into branch):
- Tests: `go test ./...` — full repo green; `dispatchschema` 19 tests + `internal/roundtable` `result_test` extension all pass.
- Build: `go build ./...` clean.
- Vet: `go vet ./internal/roundtable/dispatchschema/...` clean.
- Gofmt: applied to `validate_test.go` (one cosmetic fix); empty output now.

Gate verdicts in YAML: `code_review APPROVED`, `spec_review CONFORMANT` (2 documented PRD divergences, designer-ratified), `safety PASS`, `arch_advisor SOUND`, `roundtable_precheck CONCERNS` (residual design questions resolved by designer), `roundtable_design CONCERNS` (verified-false BLOCKED votes; legitimate advisories absorbed by test-writer).

## roundtable-landing-review
<!-- Multi-model advisory review of implementation (Step 8.5, if roundtable enabled).
     Orchestrator calls roundtable-crosscheck + roundtable-critique tools. Output appended here. -->

## Code Review: F03 — Per-panelist response validator with structured error surfacing

**Verdict:** APPROVED

**Files reviewed:**
- `/mnt/agent-storage/vader/src/roundtable/internal/roundtable/dispatchschema/validate.go` (new, 405 LOC)
- `/mnt/agent-storage/vader/src/roundtable/internal/roundtable/dispatchschema/validate_test.go` (new)
- `/mnt/agent-storage/vader/src/roundtable/internal/roundtable/dispatchschema/imports_test.go` (new)
- `/mnt/agent-storage/vader/src/roundtable/internal/roundtable/result.go` (modified)

**Neighboring files compared:**
- `internal/roundtable/dispatchschema/schema.go` (F01 pattern baseline)
- `internal/roundtable/dispatchschema/prompt.go` (F02 pattern baseline)
- `internal/roundtable/dispatchschema/prompt_test.go` (test layout baseline)

**Findings:**

- [MINOR] `validate.go:306-308` — `capRunes` slow-path comment says invalid bytes "won't be in the output" but they ARE replaced with U+FFFD via `sanitizeUTF8` at line 335. The code is correct; the comment is misleading. Readers expecting pure-drop behavior will be confused.
  Current: `// Invalid byte — skip it (it won't be in the output)`
  Suggestion: `// Invalid byte — skip it here; sanitizeUTF8 will replace it with U+FFFD in the returned string.`

- [MINOR] `validate.go:119-132` — double JSON walk on the success path: `json.Valid` scans the full body, then `json.Unmarshal` re-parses it. For the 1 MB hostile-input test this is harmless (linear twice is still linear) but it's a redundant allocation. The intent is clear from the comment. If performance ever matters on large payloads, a single `json.Unmarshal` with the error check is sufficient. Not blocking — the current pattern is explicit and the test confirms no panic.

- [MINOR] `validate.go:332` — the fallthrough condition `end == len(s)` is true only when the loop visited every byte. When s is entirely invalid bytes, `end` stays 0 and `len(s) > 0`, so `sanitizeUTF8(s)` is called — correct. But this is subtle; a one-line comment on the condition would help future readers.

- [NITPICK] `validate_test.go:122-123` — `var dispatcherStructured []byte = parsed` uses explicit `[]byte` type annotation on a `json.RawMessage` variable. Since `json.RawMessage` is `[]byte`, this is valid Go, but the type annotation obscures that `parsed` is already `nil` here by contract. A `_ = json.RawMessage(parsed)` or simply dropping the annotation would be cleaner.

**All designer-locked decisions verified:**
- `Result.Structured` is `json.RawMessage` (value, not pointer) — confirmed.
- `Result.StructuredError` is `*dispatchschema.ValidationError` (pointer) — confirmed.
- Both have `omitempty`; both appended after `Metadata` — confirmed.
- Excerpt rune-cap at 200 via `unicode/utf8` — confirmed (`excerptRuneCap = 200`, `capRunes` is rune-aware).
- Kind constants exported as untyped string consts — confirmed.
- No `regexp` dependency, no third-party — confirmed.
- Import cycle guard: `imports_test.go` enforces leaf-package status via `go list -deps` — confirmed.
- Last-fence selection: correct line-aware scan, last closed block wins — confirmed.
- Discipline guardrail: `json.RawMessage("null")` path surfaces as `KindSchemaViolation`, not returned as parsed — confirmed at lines 146-153.
- No `recover()` blocks, no retry/fallback — confirmed.
- `result.go` diff is clean: only two fields added, no existing field/tag/constructor mutations — confirmed.
- All 16 tests pass (`go test ./internal/roundtable/...`).

**Summary:** The implementation is correct, pattern-consistent with F01/F02, and disciplined on all locked decisions. The three MINOR findings are documentation/style issues in `capRunes` that do not affect behavior. No CRITICAL or MAJOR issues.

**Next hop:** spec-reviewer
