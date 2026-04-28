# F04 — Wire schema parameter into all five dispatch MCP tools

<!-- This is a handoff file. Agents append below; orchestrator updates YAML. -->

---
status: READY-TO-LAND
pipeline: backend
prd_ref: docs/exec-plans/prds/dispatch-structured-output.json#F04
spec_ref: docs/exec-plans/prds/dispatch-structured-output.json#F04

# Pre-check routing (set by pre-check, read by orchestrator)
intent: mid-sized
complexity: complex
designer_needed: YES
researcher_needed: YES
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
arch_advisor_verdict:

# Arch-advisor re-run counters (separate from initial gate passes)
arch_retry_spec_review_attempt: 0
arch_retry_safety_attempt: 0

# Pipeline configuration
remote_name: origin
roundtable_enabled: true
pr_url:

# Roundtable pre-check review (Step 1.3)
roundtable_precheck_attempt: 2
roundtable_precheck_verdict: CONCERNS
roundtable_precheck_skipped:

# Roundtable design review (Step 2.5)
roundtable_design_attempt: 2
roundtable_design_verdict: APPROVED
roundtable_skipped:

# Roundtable landing review (Step 8.5)
roundtable_landing_attempt: 1
roundtable_landing_verdict: APPROVED

# Roundtable-triggered gate re-run counters (separate from initial passes)
roundtable_retry_code_review_attempt: 0
roundtable_retry_spec_review_attempt: 0
roundtable_retry_safety_attempt: 0

# Post-landing verdicts (Step 9)
doc_garden_verdict: DRIFT_FOUND
doc_garden_drift_count: 4
---

## pre-check
<!-- Execution brief appended here by pre-check agent -->

## Execution Brief: Wire schema parameter into all five dispatch MCP tools

**PRD:** docs/exec-plans/prds/dispatch-structured-output.json
**Feature ID:** F04
**Feature index:** 3
**Feature pointer base:** /features/3
**Layer:** service
**PRD-level invariants:** none
**Dependencies:** MET — F01 (cbcdebf), F02 (873d195), F03 (3ae7937) all merged to main; backlog entries checked off.
**Research needed:** NO — F01/F02/F03 establish all design facts (Parse, BuildPromptSuffix, Validate; fenced-JSON convention; Structured/StructuredError fields already on Result).
**Designer needed:** NO — service layer; touch points are mechanical (input-schema field, dispatch glue, panelist post-processing). Backend-designer not required because the wire shape is fully decided in PRD design_facts.
**Implementer needed:** YES
**Safety auditor needed:** NO — no auth/credentials/token handling; egress policy for Excerpt is already documented on F03's ValidationError.
**Arch-advisor needed:** NO — bounded integration, no new structural patterns.

**Intent:** mid-sized
**Complexity:** standard

**What to build:**
Add an optional `schema` field to the shared MCP `ToolInput`/`toolInputSchema`, thread it through `buildStdioDispatch` into a new `ToolRequest.Schema` (parsed `*dispatchschema.Schema`), append `dispatchschema.BuildPromptSuffix(schema)` to `basePrompt` in `Run`, and after each panelist `backend.Run` returns, when `req.Schema != nil`, call `dispatchschema.Validate(result.Response, req.Schema)` and populate `result.Structured` / `result.StructuredError`. Fail fast with an MCP error before any backend dispatch when the supplied schema fails `Parse`.

**New files:**
- `internal/stdiomcp/server_schema_test.go` — integration tests for the five tools: optional-schema acceptance, byte-equivalence on omitted path, Structured population on valid response, StructuredError on invalid, MCP error on malformed schema (pre-dispatch).
- `internal/roundtable/run_schema_test.go` (optional, if Run-level coverage is cleaner than going through stdiomcp) — unit-level coverage of the Schema threading + validator wiring inside `Run`.

**Modified files:**
- `internal/stdiomcp/types.go` — add `Schema json.RawMessage \`json:"schema,omitempty"\`` to `ToolInput`. Keep it `json.RawMessage` so the MCP layer accepts free-form objects (per PRD design fact "MCP input-schema typing").
- `internal/stdiomcp/server.go` — extend `toolInputSchema` JSON to declare `"schema": {"type": "object"}` (NOT in `required`). No change to `registerTool`; the dispatch glue handles it.
- `cmd/roundtable/main.go` (`buildStdioDispatch`) — when `input.Schema != nil` and len > 0, call `dispatchschema.Parse(input.Schema)`. On parse error, return a wrapped error so the existing `dispatch error` path surfaces it to MCP as `IsError: true` BEFORE invoking `roundtable.Run`. Pass the parsed `*Schema` through a new field on `ToolRequest`.
- `internal/roundtable/run.go` — add `Schema *dispatchschema.Schema` to `ToolRequest`; in `Run`, when `req.Schema != nil`, append `dispatchschema.BuildPromptSuffix(req.Schema)` to `basePrompt` (after the existing `req.PromptSuffix` append). After the per-panelist runCh drain (around line 391-394), iterate `results` and for any `*Result` whose `Status` allows (i.e. successful response), call `Validate(result.Response, req.Schema)` and assign `result.Structured` / `result.StructuredError`. Skip validation for non-success statuses (`not_found`, `probe_failed`, `error`) — Structured/StructuredError must remain nil there.

**Existing patterns to follow:**
- `internal/stdiomcp/server.go:48-64` — JSON-literal `toolInputSchema` block; extend in place rather than rebuilding.
- `internal/stdiomcp/server.go:90-152` — `registerTool` already returns MCP errors via `IsError: true` when `dispatch` returns a non-nil error; reuse that path for malformed-schema fast-fail.
- `cmd/roundtable/main.go:172-214` — `buildStdioDispatch` is the seam where `ToolInput` → `ToolRequest` translation happens; schema parse belongs here, before `roundtable.Run`.
- `internal/roundtable/run.go:249-252` — existing `PromptSuffix` append is the model for tacking on `BuildPromptSuffix(req.Schema)`.
- `internal/roundtable/dispatchschema/validate.go:91-105` — Validate godoc is explicit: caller MUST branch on `schema != nil` before calling. Honor that.
- `internal/roundtable/result.go:23-34` — Structured / StructuredError fields are already declared with `omitempty`; populating them from `Run` requires no Result-shape change.

**Assertion traceability:**
- `/features/3/oracle/assertions/0` → integration test: each of the five tools, when called with a `schema` field in MCP `ToolInput`, accepts it without error (no "unknown field" rejection).
- `/features/3/oracle/assertions/1` → integration test: omit `schema`; capture full DispatchResult JSON; assert byte-equal to baseline produced without the F04 changes (or against a recorded fixture). All five tools.
- `/features/3/oracle/assertions/2` → integration test: supply schema; assert per-panelist `result.structured` is populated (parsed JSON matching schema) and the prompt the backend received contains the F02 suffix.
- `/features/3/oracle/assertions/3` → dedicated regression test: byte-equivalence between schema-omitted dispatch and current dispatch, all five tools, identical stub-backend inputs.
- `/features/3/oracle/assertions/4` → integration test: malformed schema (e.g. `{"type":"object","properties":{"x":` truncated, or `{"type":"unsupported"}`) returns an MCP error (IsError true) and NO backend was invoked (use a counting mock backend; assert call count == 0).

**Edge cases:**
- Empty schema object `{}` — `Parse` accepts it (per F01 tests at schema_test.go:105); `BuildPromptSuffix` returns "" for an empty-properties schema; Validate is still invoked. Behavior must be defined: either treat as "no schema" (skip validate) or run validate which will demand a fenced JSON block. The F02/F03 contract says Validate runs; preserve that.
- Panelist statuses `not_found` / `probe_failed` / `error` — do NOT call Validate; leave Structured / StructuredError nil. The validator would surface `KindMissingFence` on a stderr-only Result and pollute the output.
- Concurrent panelists — Validate is pure (per its godoc), no shared state; the post-runCh-drain loop is single-goroutine, so no synchronization needed.
- Malformed schema must fail-fast in `buildStdioDispatch` BEFORE `roundtable.Run` is entered. Do not parse-and-discard; either parse in dispatch glue and pass `*Schema`, or parse inside Run and propagate error up. The first is cleaner because the MCP error path is already wired in `buildStdioDispatch` callers.
- `input.Schema` may be `[]byte` of length 0 (absent) or `null`. Treat both as "no schema". Only non-empty, non-null payloads invoke `Parse`.

**Risks:**
- Byte-equivalence regression — any incidental reordering of `result.Metadata` or other map iteration in `Result` JSON could break assertion 3. Mitigation: the regression test must be written FIRST and the implementer verifies it passes before adding any schema-on-path code.
- Free-form `schema` MCP field could be misused as a vector for prompt injection via crafted enum values; F02's `BuildPromptSuffix` already handles escaping (per its godoc). Verify by reading prompt.go — do not re-implement.
- Forgetting to update `additionalProperties: false` consequence: the existing `toolInputSchema` has `additionalProperties: false`; adding the new field to `properties` is sufficient, no other tool-input-schema field changes.

**Verify command:** `go test ./...`

**Path convention:** Go module rooted at repo top; `internal/` for non-exported packages, `cmd/roundtable/` for the binary entry.

**Constraints for downstream:**
- MUST: parse the supplied schema in `buildStdioDispatch` (cmd/roundtable/main.go) BEFORE calling `roundtable.Run`. A parse failure returns an error from the dispatch func so `registerTool` surfaces it as `IsError: true`. No backend may be invoked on a malformed schema (PRD contract `error_propagation`).
- MUST: treat absent / empty / `null` `input.Schema` as "no schema". Skip parse, skip `BuildPromptSuffix` append, skip `Validate`. `Result.Structured` and `Result.StructuredError` remain nil. The byte-equivalence regression test (assertion 3) hinges on this.
- MUST: post-dispatch, only call `dispatchschema.Validate` when both `req.Schema != nil` AND the per-panelist `result.Status` indicates a successful response from a real backend. Do not validate stderr-only / not_found / probe_failed / error results.
- MUST NOT: add any new package-level mutable state, init() side-effects, or global hooks (per user global rule). Schema flows through `ToolRequest.Schema` as constructor-style data only.
- MUST NOT: introduce any new third-party dependency. F01/F02/F03 already deliver Parse / BuildPromptSuffix / Validate; F04 is pure wiring.
- MUST NOT: alter the existing `toolInputSchema` JSON beyond adding the `schema` property; specifically, leave `additionalProperties: false` and the `required: ["prompt"]` list unchanged. Schema is OPTIONAL.
- MUST NOT: add a CLI flag, env var, or non-MCP invocation path for the schema parameter (PRD excludes "CLI flag surface").
- MUST NOT: silently retry, repair, or re-prompt on validation failure. Surface `Result.Response` (raw text) plus `Result.StructuredError`. Per PRD design fact "Validation failure handling".

**Ready:** YES
**Next hop:** test-writer

### Resolved feature (verbatim from keel-feature-resolve.py)

```json
{
  "ok": true,
  "feature_id": "F04",
  "feature_index": 3,
  "feature_pointer_base": "/features/3",
  "prd_path": "/mnt/agent-storage/vader/src/roundtable/docs/exec-plans/prds/dispatch-structured-output.json",
  "canonical_prd_path": "/mnt/agent-storage/vader/src/roundtable/docs/exec-plans/prds/dispatch-structured-output.json",
  "title": "Wire schema parameter into all five dispatch MCP tools",
  "layer": "service",
  "oracle": {
    "type": "integration",
    "setup": "MCP server built from internal/stdiomcp/server.go with all five dispatch tools registered.",
    "tooling": "Go integration tests against the MCP server entry point.",
    "assertions": [
      "Each of the five dispatch MCP tools (roundtable-canvass, roundtable-deliberate, roundtable-blueprint, roundtable-critique, roundtable-crosscheck) accepts an optional schema input parameter declared in its MCP tool input schema.",
      "When the schema parameter is omitted, each dispatch tool produces freeform markdown output identical to current behavior (backwards-compatible).",
      "When the schema parameter is supplied, each dispatch tool composes the prompt suffix from F02, validates panelist responses via F03, and returns structured per-panelist values on the DispatchResult.",
      "Backwards-compat regression: a new integration test asserts byte-equivalent JSON output between dispatch with schema omitted and current dispatch on identical inputs across all five tools.",
      "When a malformed schema is supplied (fails the parser), the dispatch tool returns an MCP error before any backend is invoked."
    ]
  },
  "contract": {
    "mcp_param_name": "schema",
    "mcp_param_type": "object (free-form at the MCP input-schema level). The JSON-Schema-lite subset constraint is enforced at parse time by the parser; surfacing it at the MCP level would force every client to import the same schema-of-schema for marginal benefit.",
    "affected_tools": "roundtable-canvass, roundtable-deliberate, roundtable-blueprint, roundtable-critique, roundtable-crosscheck",
    "default_when_omitted": "Parameter is optional. When absent: no prompt-suffix change, validator not invoked, Structured and StructuredError fields absent from per-panelist results.",
    "backwards_compat_bar": "Byte-equivalent JSON output on the omitted-schema path. Verified by (a) the existing integration tests pass unmodified, (b) one new regression test asserts byte-equivalence between current dispatch and schema-omitted dispatch on identical inputs.",
    "error_propagation": "A schema that fails parsing causes the dispatch tool to return an MCP-level error before any backend is invoked (fast fail; never partial dispatch with an invalid schema)."
  },
  "needs": ["F01", "F02", "F03"],
  "prd_invariants_exercised": [],
  "backlog_fields": {
    "prd_slug": "dispatch-structured-output",
    "prd_exempt_reason": null,
    "spec_ref": null,
    "design_refs": [],
    "needs_ids": ["F01", "F02", "F03"],
    "human_markers": []
  },
  "classification": "JSON_PRD_PATH"
}
```

### Constraints for downstream
- MUST: parse `input.Schema` in `buildStdioDispatch` (cmd/roundtable/main.go) BEFORE calling `roundtable.Run`; a `dispatchschema.Parse` error returns from the dispatch func so `registerTool` emits `IsError: true`. No backend invocation on malformed schema.
- MUST: treat absent / empty / `null` `input.Schema` as "no schema" — skip Parse, skip `BuildPromptSuffix`, skip `Validate`. `Result.Structured` and `Result.StructuredError` remain nil so byte-equivalence (assertion 3) holds.
- MUST: only call `dispatchschema.Validate` when `req.Schema != nil` AND per-panelist `result.Status` indicates a backend success — not for `not_found` / `probe_failed` / `error`.
- MUST NOT: add new package-level mutable state, init() side-effects, or globals; schema flows through a new `ToolRequest.Schema` field only.
- MUST NOT: silently retry, repair, or re-prompt on validation failure; surface raw `Response` plus `StructuredError`. No new CLI flags / env vars / dependencies; do not change `additionalProperties: false` or `required: ["prompt"]` in `toolInputSchema`.

### Revision (attempt 2)

Accepting roundtable consensus on all 4 flagged flips. Reasoning is concrete and grounded in real F04 risks; no defect found.

**Flipped:**
- `complexity`: standard → **complex**. Byte-equivalence bar across 5 tools × Go map-iteration non-determinism × new post-run validation phase is not "standard wiring."
- `designer_needed`: NO → **YES**. Three load-bearing decisions are unspecified: (a) parse location, (b) validate-skip semantics for non-success statuses, (c) `ToolRequest.Schema` field type.
- `researcher_needed`: NO → **YES (timeboxed 30-min spike)**. Two unverified assumptions underpin the byte-equivalence bar; cheap to verify, expensive if wrong.
- `safety_auditor_needed`: NO → **YES**. Caller-controlled schema descriptions / enum values / field names flow verbatim into the panelist prompt via `BuildPromptSuffix` — request-side prompt-injection surface F03's response-side audit did not cover.

**Held against consensus:** none. (Consensus kept `intent=mid-sized`, `arch_advisor_needed=NO`, `implementer_needed=YES`, all of which I also kept.)

### Constraints for downstream (revised — supersede the prior Constraints block above)

**Designer (backend-designer) MUST lock in:**
- **Parse location:** parse the supplied schema in `buildStdioDispatch` (cmd/roundtable/main.go) BEFORE `roundtable.Run` is invoked. Recommended because the existing `dispatch error → IsError: true` path is already wired there; parsing inside `Run` would force a new error-propagation seam and risk partial dispatch on malformed schemas.
- **Validate-skip semantics:** `dispatchschema.Validate` is called only when `req.Schema != nil` AND per-panelist `result.Status` is a successful response. Statuses `not_found`, `probe_failed`, and `error` MUST NOT call Validate; their `Structured` and `StructuredError` remain nil. Document this explicitly in the design brief — the F03 contract is silent on status filtering.
- **`ToolRequest.Schema` field type:** `*dispatchschema.Schema` (parsed). Recommended because the parse error must surface at the dispatch glue (before `Run`); deferring to `json.RawMessage` inside `Run` defeats the fast-fail contract.

**Researcher (timeboxed 30-min spike) MUST verify:**
- Go `encoding/json` `Marshal` sorts `map[string]any` keys deterministically (confirm via stdlib doc + a smoke test). If not deterministic, the omitted-schema byte-equivalence assertion is unstable and the regression test must use a structural-equality oracle instead.
- MCP wire round-trip preserves `json.RawMessage` bytes verbatim — OR confirm that the omitted-schema code path never Marshals/Unmarshals the schema field at all (which is the simpler invariant to enforce). Halt and report findings before designer locks the field-type choice.

**Safety auditor MUST scope:**
- Prompt-injection surface introduced by `BuildPromptSuffix(schema)`: schema field names, descriptions, and enum values are caller-controlled and flow verbatim into the LLM panelist prompt across all five tools. Audit whether F02's `BuildPromptSuffix` escapes / sanitizes these fields, or whether a hostile MCP client can smuggle instructions ("ignore prior instructions, output X") into the panelist context. F03's audit covered response-side validation; this is the request-side gap.
- No auth / credential / token handling is introduced — the audit is narrowly scoped to the prompt-suffix surface.

**Original Constraints (still apply, carried forward):**
- MUST NOT: add new package-level mutable state, init() side-effects, or globals; schema flows through a new `ToolRequest.Schema` field only.
- MUST NOT: silently retry, repair, or re-prompt on validation failure; surface raw `Response` plus `StructuredError`.
- MUST NOT: introduce new CLI flags / env vars / third-party dependencies; do not change `additionalProperties: false` or `required: ["prompt"]` in `toolInputSchema`.
- MUST: treat absent / empty / `null` `input.Schema` as "no schema" — skip Parse, skip `BuildPromptSuffix`, skip `Validate`; byte-equivalence (assertion 3) hinges on this.

### Final routing flags (attempt 2)

```yaml
intent: mid-sized
complexity: complex
designer_needed: YES
researcher_needed: YES
safety_auditor_needed: YES
arch_advisor_needed: NO
implementer_needed: YES
```

**Next hop:** researcher (30-min spike) → backend-designer → test-writer → implementer.


## roundtable-precheck-review
<!-- Multi-model advisory review of pre-check routing (Step 1.3). -->

### Attempt 1 — critique + canvass

**Tools called:** `roundtable-critique` (codereviewer role), then `roundtable-canvass` (analyst role).
**Panel:** claude (opus-4-7), codex (cli-default), gemini (gemini-3.1-pro), fireworks-kimi, fireworks-minimax, fireworks-deepseek.

**Consensus disagrees with pre-check on 4 flags:**

| Flag | Pre-check | Consensus | Vote |
|-|-|-|-|
| intent | mid-sized | KEEP `mid-sized` | 6/6 |
| complexity | standard | **FLIP `complex`** | 6/6 |
| designer_needed | NO | **FLIP `YES`** | 6/6 |
| researcher_needed | NO | **FLIP `YES` (timebox 30-min)** | 6/6 |
| safety_auditor_needed | NO | **FLIP `YES`** | 6/6 |
| arch_advisor_needed | NO | KEEP `NO` (refined) | 4 NO / 2 YES |
| implementer_needed | YES | KEEP `YES` | 6/6 |

**Headline rationale:**
- **complexity → complex:** Byte-equivalence regression bar across 5 tools × Go `map[string]any` iteration non-determinism × new post-run per-panelist validation phase — not "standard wiring." The omitted-schema path must be byte-equal to current dispatch, which is a brittle oracle if any intermediate map serialization is involved.
- **designer_needed → YES:** Three load-bearing decisions left to the implementer that the spec does not cover: (a) parse location (`buildStdioDispatch` vs inside `Run`), (b) skip-semantics for non-success panelist statuses (`not_found`, `probe_failed`, `error` — should validation run? F03's contract says caller MUST branch on `schema != nil` but doesn't speak to status filtering), (c) `ToolRequest.Schema` field type (`*dispatchschema.Schema` parsed vs `json.RawMessage` deferred).
- **researcher_needed → YES (timeboxed):** Two unverified assumptions underpin the byte-equivalence bar: (1) Go `encoding/json` map-key sort guarantees, (2) MCP `json.RawMessage` round-trip byte preservation. Cheap to verify, expensive if wrong. ~30-min spike, halt and report findings before designer locks the parse-location and field-type choices.
- **safety_auditor_needed → YES:** Caller-controlled schema JSON (descriptions, enum values, field names) flows verbatim through `BuildPromptSuffix` into the prompt sent to LLM panelists across all five tools. F03 audited *response* validation; F04 introduces a *request-side* prompt-injection surface no prior gate covers. Cheap pass; real gap.
- **arch_advisor_needed → NO (refined):** F01–F03 set the architecture; F04 consumes it. Designer + spec-reviewer cover the integration-pattern concerns. 2 panelists pushed YES on durability grounds, but the proportional cut is to skip arch-advisor and let the designer define the seam.

**Action:** Send consensus findings back to pre-check for revision (Step 1.3 step 6). Increment `roundtable_precheck_attempt: 2` after re-run.

### Attempt 2 — critique on revised classification

**Tools called:** `roundtable-critique` on the revised flags + revised constraints. Gemini rate-limited (no-capacity 429 on attempt 2 — counted as skipped, 5 panelists responded).

**Flag-by-flag verdict on the revision:**

| Flag | Revised value | Panel verdict (5 voters) |
|-|-|-|
| intent | mid-sized | OK (5/5) |
| complexity | complex | OK (5/5) |
| designer_needed | YES, with scope | scope INCOMPLETE (4/5) — see below |
| researcher_needed | YES, 30-min spike | timebox CONTESTED (3/5 push 60-min or sharper exit gate) |
| safety_auditor_needed | YES, BuildPromptSuffix scope | scope TOO NARROW (5/5) — see below |
| arch_advisor_needed | NO | CONTESTED (4/5 push scoped YES; 1/5 OK as NO) |
| implementer_needed | YES | OK (5/5) |

**Unresolved disagreements (CONCERNS — surface to downstream agents):**

1. **arch-advisor (NO held against 4/5 push):** Panel argues byte-equivalence is a cross-tool architectural commitment that warrants scoped advisor review. Pre-check holds NO because F01–F03 set the architecture and designer + spec-reviewer cover the integration-pattern concerns. **Decision:** proceed with NO per pre-check authority; if designer surfaces a structural concern they cannot resolve, escalate to arch-advisor consult mid-pipeline.

2. **Researcher timebox honesty:** 30-min spike covers Go `encoding/json` map-key sort confirmation but may not cover full middleware-chain trace for `map[string]any` round-trips. **Mitigation:** researcher MUST exit with one of {confirmed RawMessage end-to-end, confirmed map-round-trip happens, halt+report}. No "looks fine" exit allowed.

3. **Designer scope additions (incorporate from panel):**
   - **Validate-eligibility precise gate:** `req.Schema != nil && result.Status == "ok"` (NOT just "non-error"; statuses include `timeout`, `terminated`, `rate_limited`, `not_found`, `probe_failed`, `error` — only `ok` means panelist actually produced a response worth validating).
   - **Parse-failure error envelope:** how does pre-Run schema parse error surface to MCP — must reuse the existing `IsError: true` dispatch-error path; do NOT invent a new error envelope.
   - **Schema lifecycle / concurrency:** `*dispatchschema.Schema` is shared across N panelist goroutines via `req.Schema`; designer must confirm Schema is read-only after Parse, document the immutability.
   - **Omitted-schema byte-equivalence preservation strategy:** designer must state explicitly how the omitted-schema path produces byte-identical Result JSON (e.g., "we add no fields to Result on this path; `Structured` and `StructuredError` are pointers with `omitempty` so the wire JSON is unchanged when nil"). This is a regression-bar commitment.

4. **Safety auditor scope expansion:**
   - **`BuildPromptSuffix(schema)`** — primary vector (already in scope).
   - **Structural-token escape inside suffix** — schema field names and enum values containing `</system>`, prompt delimiters, or XML-like tags must not break the panelist prompt's structure.
   - **`StructuredError.Excerpt` echo path** — if validation fails on a panelist response that contains hostile content, the Excerpt echoes ≤200 chars back to the caller; verify this cannot leak schema content.
   - **Schema observability in logs** — if dispatch logs the parsed Schema or its raw JSON, confirm it does not cross trust boundaries.
   - **Out-of-scope clarification:** `Parse` does NOT carry through `description` fields per F01 contract — auditor confirms parse strips/rejects, doesn't audit live-input descriptions.

**Verdict:** `roundtable_precheck_verdict: CONCERNS` — flag values stand (advisory, not blocking, per skill rule "If still divergent after attempt 2: proceed with pre-check's latest classification anyway"). The four numbered concern blocks above are passed forward as additional constraints to designer / researcher / safety-auditor.

## researcher
<!-- Research brief appended here (if applicable) -->

### Byte-Equivalence Spike — F04

**Q1 (Go `encoding/json` map-key determinism):** ✓ Stdlib docs confirm: *"The map keys are sorted and used as JSON object keys..."* (https://pkg.go.dev/encoding/json#Marshal). Stable across Go versions; not subject to randomized map-iteration order.

**Q2 (MCP wire round-trip preserves `json.RawMessage` bytes end-to-end):** **A. ✓ END-TO-END `RawMessage` PRESERVED.**

INPUT path:
- `internal/stdiomcp/server.go:91-95` — `mcp.AddTool` registers a typed handler.
- mcp-go SDK `CallToolParamsRaw.Arguments` is `json.RawMessage`; the SDK calls `encoding/json.Unmarshal(raw, &ToolInput)` directly into the typed struct. No `map[string]any` intermediate.
- `cmd/roundtable/main.go:172-214` (`buildStdioDispatch`) consumes typed `input` directly; no re-marshal.

OUTPUT path (the actual byte-equivalence target — assertions 1 & 3 compare `DispatchResult` JSON):
- `internal/roundtable/result.go:123-130` `DispatchResult.MarshalJSON` builds `map[string]any` then `json.Marshal` → deterministic per Q1.
- `internal/roundtable/result.go:83-92` `Meta.MarshalJSON` same pattern → deterministic.
- `Result` struct (`result.go:10-35`) marshals via reflection in struct declaration order. `Structured` (`*json.RawMessage`, omitempty) and `StructuredError` (`*ValidationError`, omitempty) **elide entirely from wire JSON when nil** (F03 commit 3ae7937). On the omitted-schema path no output bytes change.

**Implications for designer:**
- `ToolRequest.Schema *dispatchschema.Schema` (parsed) is correct; parse in `buildStdioDispatch` is byte-safe.
- `ToolInput.Schema json.RawMessage` is byte-safe; absent field means absent bytes.
- Validate-skip MUST gate on `result.Status == "ok"` AND `req.Schema != nil`.

**Implications for test-writer:**
- Byte-equivalence is achievable as **literal `bytes.Equal`** between pre-F04 baseline and post-F04 omitted-schema output. No need to relax to canonical-form-equivalence.
- Recommend: `bytes.Contains(out, []byte(`"structured"`)) == false` and same for `"structured_error"` on omit path — guards future reflection/tag drift.
- Use a deterministic stub backend so the only variable across runs is the F04 code path; record one golden file per tool (5 fixtures) OR diff-pre-F04-vs-post-F04 in same test run.

**Verdict:** A — RawMessage end-to-end. **Confidence:** HIGH.

### Decisions (optional)
- D-R1: `bytes.Equal` is the byte-equivalence test for assertion 3, not canonical-form.

## arch-advisor-consultation
<!-- Architecture guidance appended here by Arch-advisor at Step 1.7 (if applicable) -->
N/A — `arch_advisor_needed: NO` per pre-check attempt 2.

### Constraints for downstream
N/A.

## backend-designer / frontend-designer
<!-- Design brief appended here (if applicable) -->

### Backend Design: F04 — Wire schema parameter into all five dispatch MCP tools (Attempt 1)

**Module:** `internal/stdiomcp` (transport seam) + `cmd/roundtable` (dispatch glue) + `internal/roundtable` (Run).
**Layer:** transport + service. No new packages; pure wiring of F01/F02/F03 building blocks.
**Depends on:** `dispatchschema.{Parse, BuildPromptSuffix, Validate}`, `roundtable.{Run, Result, ToolRequest}`.

**Public API additions:**
- `stdiomcp.ToolInput.Schema json.RawMessage` (D1)
- `roundtable.ToolRequest.Schema *dispatchschema.Schema` (D2)

No new exported functions. No globals, no init() side-effects.

**D1 — `ToolInput.Schema` field type:** `json.RawMessage` with `` `json:"schema,omitempty"` ``. Located at `internal/stdiomcp/types.go`. Researcher verdict A confirms byte-safety. Also extend `internal/stdiomcp/server.go:48-64` `toolInputSchema` JSON to add `"schema": {"type": "object"}` in `properties` (NOT in `required`). `additionalProperties: false` and `required: ["prompt"]` unchanged.

**D2 — `ToolRequest.Schema` field type:** `*dispatchschema.Schema` (parsed pointer). Located at `internal/roundtable/run.go:55` (no JSON tag — ToolRequest is in-process only). Read-only after Parse; safe to share across N panelist goroutines without synchronization (see C2 revision below for accessor immutability discipline).

**D3 — Parse location:** `cmd/roundtable/main.go` `buildStdioDispatch` (lines 172-214). Insert AFTER existing `Agents`/`Files` parsing, BEFORE the `roundtable.ToolRequest` literal at line 194. On parse error: `return nil, err` → reuses existing dispatch-error envelope at `internal/stdiomcp/server.go:110-114`. (See C5 revision below for the `%w` wrap.)

**D4 — Validate-eligibility precise gate:** `req.Schema != nil && result.Status == "ok"` ONLY. Status set in use: `ok`, `error`, `rate_limited`, `timeout`, `terminated`, `not_found`, `probe_failed` (sources: `internal/roundtable/output.go:42-52`, `result.go:44,54,71`, `gemini.go`/`openai_http.go` for `rate_limited`).

| Status | Validate? | Structured | StructuredError |
|-|-|-|-|
| `ok` | YES (when schema!=nil) | populated on success | populated on failure |
| All 6 non-ok | NO | nil | nil |

Loop location: `internal/roundtable/run.go` AFTER the runCh drain at line 391-394. Single-goroutine — no synchronization.

**D5 — Parse-failure error envelope:** Reuse the existing dispatch-error path at `internal/stdiomcp/server.go:110-114`. Closure returns `error` → wrapped as `roundtable dispatch error: <msg>` and `IsError: true`. Parse failure returns BEFORE `roundtable.Run` is invoked → assertion-4 (counting mock backend, call_count == 0) holds. (See C5 revision below.)

**D6 — Omitted-schema byte-equivalence preservation strategy:** When `input.Schema` is absent / null / empty: skip Parse → `req.Schema` is nil → skip BuildPromptSuffix → basePrompt unchanged → skip Validate → Structured/StructuredError remain nil → `omitempty` elides both from JSON. Researcher verified `bytes.Equal` is achievable.

**D7 — Empty schema object handling:** *(see Attempt-2 revision C3 below — D7 was redefined.)*

**D8 — `BuildPromptSuffix` placement:** *(see Attempt-2 revision C4 below — D8 was refined.)*

### Revision (attempt 2)

Verified against `dispatchschema/schema.go`, `prompt.go`, `validate.go`, `schema_test.go:103-115`, and `stdiomcp/server.go`. Five fixes locked.

**C1 — D1 absent-detection (HIGH, fixed):**
In `buildStdioDispatch` (`cmd/roundtable/main.go`), use:
```
trimmed := bytes.TrimSpace(input.Schema)
if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
    // absent — leave req.Schema nil
} else {
    parsed, err := dispatchschema.Parse(trimmed)
    if err != nil { return nil, fmt.Errorf("invalid schema parameter: %w", err) }
    req.Schema = parsed
}
```
JSON literals `false`/`0`/`[]`/`""` reach `Parse` and surface as `IsError: true` per D5 — `dispatchschema.classifyTopLevelDecodeErr` already names the kind.

**C2 — D2 immutability (HIGH, fixed via Option B — documentation discipline, no F01 surface change):**
Inspected `prompt.go` (lines 44, 47, 50) and `validate.go` (lines 157, 170, 218): all are read-only iteration over `Schema.Fields()`, `Schema.Required()`, `Field.Enum()` — no append, assign, or sort. F04 implementer code follows the same read-only discipline. Documented as a cross-package invariant (see Constraints below).

**C3 — D7 empty-schema redefined as D7-a:**
`schema_test.go:103-115` (`TestParseEmptyProperties`) confirms `{"type":"object","properties":{}}` parses successfully (`schema.Fields() == nil`, `Required() == nil`). Bare `{}` fails Parse (missing top-level `"type"`, `schema.go:120`). **Decision:** empty schema is `{"type":"object","properties":{}}` — Parse succeeds, `BuildPromptSuffix` emits the fence preamble + closing instruction with no field list (`prompt.go:45` gates the field block on `len(fields) > 0`), Validate runs and accepts any object. Bare `{}` is malformed and surfaces via D5.

**C4 — D8 suffix placement (HIGH, fixed via Option B):**
Inspected `stdiomcp/server.go`: `roundtable-deliberate` (line 25), `roundtable-blueprint` (line 31), `roundtable-critique` (line 37) PromptSuffix values **already start with `\n\n`**; `roundtable-canvass` (line 17) and `roundtable-crosscheck` (line 41) have empty PromptSuffix. `BuildPromptSuffix` (`prompt.go:40`) starts with non-whitespace text. In `Run`, after the existing `req.PromptSuffix` append:
```
if req.Schema != nil {
    basePrompt += "\n\n" + dispatchschema.BuildPromptSuffix(req.Schema)
}
```
Unconditional `\n\n` prefix is correct because the existing PromptSuffix values that start with `\n\n` end with non-newline text (e.g., `"...weaknesses."`), and the empty-PromptSuffix tools end `basePrompt` at the user prompt. No F02 surface change.

**C5 — D3 error envelope (MEDIUM, fixed):**
Wrap with `%w` at the dispatch glue: `return nil, fmt.Errorf("invalid schema parameter: %w", err)`. The existing envelope at `server.go:113` (`fmt.Sprintf("roundtable dispatch error: %v", err)`) then surfaces as `roundtable dispatch error: invalid schema parameter: dispatchschema: <reason>`. Underlying `*json.UnmarshalTypeError` and `*json.SyntaxError` location info is preserved through `%w`.

### Decisions

- **D1+D2 type pairing:** `ToolInput.Schema json.RawMessage` (wire) + `ToolRequest.Schema *dispatchschema.Schema` (parsed). Researcher verified RawMessage is byte-safe end-to-end; parsed pointer enables fast-fail before dispatch.
- **Parse location is `buildStdioDispatch`, BEFORE `roundtable.Run`** — reuses existing `IsError: true` envelope at `server.go:110-114`. No new error path. C5: wrap with `fmt.Errorf("invalid schema parameter: %w", err)` to preserve `json.SyntaxError` location.
- **Validate gate is `req.Schema != nil && result.Status == "ok"`** — only `ok` means panelist produced a real response. Six other statuses leave `Structured` and `StructuredError` nil.
- **Empty schema is `{"type":"object","properties":{}}` (D7-a)** — bare `{}` is malformed and surfaces via D5. Confirmed via `schema_test.go:103-115`.
- **Suffix separator: `\n\n` unconditional prefix** — accommodates both empty-PromptSuffix and trailing-prose-PromptSuffix tools without per-tool branching.

### Constraints for downstream

- **MUST:** Field types match exactly: `ToolInput.Schema json.RawMessage` and `ToolRequest.Schema *dispatchschema.Schema`. No drift; no pointer-vs-value swap.
- **MUST:** Detect schema absence with `bytes.TrimSpace(input.Schema)` then `len==0 || bytes.Equal(trimmed, []byte("null"))`. JSON literals `false`/`0`/`[]`/`""` and bare `{}` are NOT treated as absent — they reach `Parse` and surface as `IsError: true`.
- **MUST:** Validate gate is precisely `req.Schema != nil && result.Status == "ok"`. Implementer MUST NOT loosen to `!= "error"` or `!= ""`. The six non-ok statuses MUST leave `Structured` and `StructuredError` nil.
- **MUST:** Schema parse failure surfaces BEFORE any backend dispatch — `buildStdioDispatch` returns `nil, fmt.Errorf("invalid schema parameter: %w", err)` so `server.go:110-114` emits `IsError: true`. Counting-mock-backend assertion (oracle 4) MUST observe call_count == 0 on malformed schema.
- **MUST:** When appending the F02 suffix in `Run`, prepend `"\n\n"` unconditionally — `BuildPromptSuffix` does not self-pad and `basePrompt` may end on either the prompt body or an existing PromptSuffix's trailing prose.
- **MUST:** Result JSON byte-identical (`bytes.Equal`) when `input.Schema` is absent/null/empty — assertion 3. Achieved by: skip Parse, `req.Schema == nil`, skip BuildPromptSuffix, skip Validate, `omitempty` elides Structured + StructuredError.
- **MUST NOT:** Mutate slices returned by `dispatchschema.Schema.Fields()`, `Schema.Required()`, or `Field.Enum()` — they are backing slices, not defensive copies. F04 code reads them only. Documented invariant of the dispatchschema package.
- **MUST NOT:** Mutate `*dispatchschema.Schema` after Parse, add mutator methods, add package-level state, add new error envelope, add CLI flag/env var, or alter `additionalProperties: false` / `required: ["prompt"]` in `toolInputSchema`.

## roundtable-design-review
<!-- Multi-model advisory review of designer output (Step 2.5). -->

### Attempt 1 — blueprint + critique

**Tools called:** `roundtable-blueprint` (planner role), `roundtable-critique` (codereviewer role) in parallel. 6/6 panelists responded for blueprint; 5/6 for critique (kimi timed out at 181s).

**Blueprint consensus (UNANIMOUS):** 4-phase commit strategy inside one feature branch:
- **P1 Skeleton:** add fields + extend `toolInputSchema`; no consumers; build green; `bytes.Equal` to pre-F04 holds.
- **P2 Parse + propagate:** parse `input.Schema` in `buildStdioDispatch`; bad schema → MCP `IsError: true`; absent/valid → req.Schema reaches Run.
- **P3 Suffix append:** `if req.Schema != nil { basePrompt += BuildPromptSuffix(req.Schema) }`. Nil-schema prompts unchanged.
- **P4 Per-panelist validate:** post-runCh-drain, gate on `req.Schema != nil && result.Status == "ok"`.

Linear dependency chain. No alternative phasing won panel support.

**Critique flagged 4 HIGH-severity gaps + 2 MEDIUM:**

| # | Severity | Finding |
|-|-|-|
| C1 | HIGH | **D1 absent-detection guard is fragile** — `omitempty` on `json.RawMessage` only elides nil/0-len. Literal `null` (4 bytes), `  null  ` (whitespace-padded), JSON literals `false`/`0`/`[]` all bypass `string(input.Schema) != "null"` and reach `Parse`. Multi-panel agreement (claude/codex/deepseek/minimax/gemini). |
| C2 | HIGH | **D2 immutability is convention, not enforcement** — codex+gemini verified `dispatchschema.Schema.Fields()` and `Field.Enum()` return backing slices directly (`schema.go:35,43`). A panelist goroutine that mutates `enum[0]` corrupts the shared schema across all concurrent panelists. UB. |
| C3 | HIGH | **D7 `{}` schema does NOT parse** — codex inspected `dispatchschema/schema.go:117,130`: Parse requires top-level `"type"` and `"properties"`. Bare `{}` returns an error. The brief's "non-nil ⟹ schema-aware" path is unreachable for `{}`. Empty-schema must be redefined as `{"type":"object","properties":{}}` OR D7 must document that `{}` returns an MCP `IsError: true`. |
| C4 | HIGH | **D8 suffix placement causes run-on text** — codex inspected `internal/stdiomcp/server.go:25,31,37`: `level.Respond`, `milestones.Respond`, `weaknesses.Respond` PromptSuffix values have no trailing newline. `BuildPromptSuffix` (`prompt.go:40`) has no leading newline. Concatenation produces run-on text on 3/5 tools. Implementer must insert `\n\n` separator before the schema suffix. |
| C5 | MEDIUM | **D3 error envelope swallows parse context** — `roundtable dispatch error: <msg>` loses the `json.SyntaxError` line/column information. Designer should wrap as `fmt.Errorf("invalid schema parameter: %w", err)` so the existing envelope at `server.go:110-114` preserves context. |
| C6 | MEDIUM (resolved by inspection) | **D6 byte-equivalence assumes struct-tag marshaling** — codex+gemini confirmed via inspection that `Result` uses default struct-tag marshaling (only `DispatchResult` and `Meta` have custom `MarshalJSON`). `omitempty` on `*json.RawMessage` and `*ValidationError` works correctly. Claude+minimax wanted explicit verification — provided. |

**Action:** Send findings C1–C5 back to designer for revision. Increment `roundtable_design_attempt: 2`. C6 already resolved.

### Attempt 2 — critique on revised design

**Tools called:** `roundtable-critique` on attempt-2 design (5 panelists responded; gemini 429 rate-limited).

**Vote tally:**

| Panelist | Verdict |
|-|-|
| claude | APPROVED — C1–C5 all OK; minor recommendation to add an inline `// returns backing slice, do not mutate` doc comment near `Fields()`/`Required()`/`Enum()` accessors (deferred — not blocking) |
| codex | MINOR CONCERNS — empty-schema prompt is under-specified (LOW); cosmetic, not a regression |
| deepseek | MINOR CONCERNS — claimed C4 produces 4 newlines (FALSE POSITIVE: misread; existing PromptSuffix values *start* with `\n\n` but *end* with non-newline prose, so the unconditional `\n\n` prefix on the schema suffix produces exactly 2 newlines between existing-suffix-trailing-prose and schema instructions) |
| kimi | MIXED — re-flagged C2 (documentation discipline vs type-system enforcement); already deemed acceptable trade-off in attempt-1 by 4/5 panelists |
| minimax | APPROVED — C1–C5 all OK |

**Residual concerns (none HIGH):** all MINOR or LOW. Codex's empty-schema prompt observation is logged for test-writer (add a test case asserting the empty-schema prompt is non-degenerate); kimi's re-litigation of C2 is overridden by majority agreement that documentation is the proportional fix.

**Verdict:** `roundtable_design_verdict: APPROVED`. Proceed to test-writer.

## test-writer
<!-- Test report appended here -->

## Test Report: Wire schema parameter into all five dispatch MCP tools

**PRD:** docs/exec-plans/prds/dispatch-structured-output.json
**Feature ID:** F04
**Feature index:** 3
**Test files:**
- `internal/roundtable/run_schema_test.go`
- `internal/stdiomcp/server_schema_test.go`

**Tests written:** 20 (12 in run_schema_test.go, 8 in server_schema_test.go)
**Status:** RED-NEW — module under test doesn't have `ToolRequest.Schema` or `ToolInput.Schema` fields yet; compile fails on those undefined fields. All other syntax is clean.
**Failure output:**
```
internal/roundtable/run_schema_test.go:165:6: req.Schema undefined (type ToolRequest has no field or method Schema)
internal/stdiomcp/server_schema_test.go:465:18: captured.Schema undefined (type ToolInput has no field or method Schema)
```

**Assertion traceability:**
- `/features/3/oracle/assertions/0` → `TestSchemaParam_AllFiveToolsAcceptSchema` (server_schema_test.go)
- `/features/3/oracle/assertions/1` → `TestRunSchema_OmittedSchema_ByteEquivalence`, `TestSchemaParam_OmittedSchema_ByteEquivalence`
- `/features/3/oracle/assertions/2` → `TestRunSchema_StructuredPopulated`, `TestRunSchema_StructuredError_MissingFence`
- `/features/3/oracle/assertions/3` → `TestRunSchema_OmittedSchema_NoStructuredKeys`, `TestSchemaParam_OmittedSchema_NoStructuredKeys`
- `/features/3/oracle/assertions/4` → `TestSchemaParam_MalformedSchema_MCPError`, `TestSchemaParam_MalformedSchema_BackendNotCalled`
- C1 absent-detection → `TestSchemaParam_AbsentDetection_TreatedAsOmitted`, `TestSchemaParam_AbsentDetection_MalformedLiterals`
- C3/D7-a empty-properties → `TestRunSchema_EmptyPropertiesSchema_PromptNonDegenerate`, `TestSchemaParam_EmptyPropertiesSchema_Succeeds`, `TestSchemaParam_EmptyPropertiesSchema_PromptNonDegenerate`
- C4 separator → `TestRunSchema_SuffixSeparator_NonEmptyPromptSuffix`, `TestRunSchema_SuffixSeparator_EmptyPromptSuffix`
- C5 error envelope → `TestSchemaParam_MalformedSchema_ErrorMessage`
- D4 status gate → `TestRunSchema_NonOkStatuses_ValidateSkipped`
- D2 immutability/race → `TestRunSchema_ConcurrentRaceDetector`

### Stub-backend pattern

`stubBackend` (in `run_schema_test.go`) implements the full `Backend` interface in-process. It captures all received prompts in a mutex-guarded slice and returns a fixed `responseText`/`status` pair. `newSingleAgentBackends(b)` wires it to the `"stub"` provider key. `unhealthyBackend` implements `Backend` with `Healthy` returning an error (forces `probe_failed`). At the MCP layer, `fakeDispatch` (in `server_schema_test.go`) is a `DispatchFunc` closure that counts calls atomically and optionally captures `ToolInput`.

### Decisions (optional)
- Used `stubBackend` (new, standalone) rather than reusing `mockBackend` from `mock_backend_test.go` to keep `run_schema_test.go` self-contained and avoid coupling to existing mock's `runResult` field shape.
- `TestSchemaParam_AbsentDetection_MalformedLiterals` allows either `IsError==true` from the dispatch path OR from the MCP layer schema-type rejection (both are valid fast-fails for non-object literals); the invariant is that `callCount` must not increase.
- Race-detector test (`TestRunSchema_ConcurrentRaceDetector`) spawns N=6 goroutines sharing one `*Schema`; meaningful only under `go test -race`.

**Next hop:** implementer

## implementer
<!-- Implementation report appended here -->

### Implementation report

**Files modified:**
- `internal/stdiomcp/types.go` — added `Schema json.RawMessage \`json:"schema,omitempty"\`` to `ToolInput` (P1).
- `internal/stdiomcp/server.go` — added `"schema": {"type": ["object","null"]}` to `toolInputSchema` properties; added schema fast-fail parse inside `registerTool` BEFORE invoking dispatch closure (parse failure → `IsError: true` envelope `roundtable dispatch error: invalid schema parameter: <err>`); imports `bytes` and `dispatchschema`.
- `internal/roundtable/run.go` — added `Schema *dispatchschema.Schema` to `ToolRequest` (P1); appended `"\n\n" + BuildPromptSuffix(req.Schema)` after existing PromptSuffix when `req.Schema != nil` (P3); after runCh drain, iterate results and call `Validate(result.Response, req.Schema)` only when `req.Schema != nil && result.Status == "ok"` (P4); imports `dispatchschema`.
- `cmd/roundtable/main.go` — `buildStdioDispatch` parses `input.Schema` with C1 absent-detection (`bytes.TrimSpace`, then `len==0 || bytes.Equal(trimmed,"null")`), wraps error as `fmt.Errorf("invalid schema parameter: %w", err)`, assigns parsed `*Schema` to `req.Schema` (P2); imports `bytes` and `dispatchschema`.

**Phase milestones:**
- P1 Skeleton: `go build ./...` green; pre-existing tests pass; new schema tests fail on behavioral wiring (expected).
- P2 Parse + propagate: schema parse runs inside dispatch glue; malformed schemas wrapped per C5.
- P3 Suffix append: unconditional `\n\n` separator before `BuildPromptSuffix` output.
- P4 Per-panelist Validate: gate is exactly `req.Schema != nil && result.Status == "ok"`; six non-ok statuses leave Structured / StructuredError nil.

**Final verification:**
- `go build ./...`: PASS
- `go test ./...`: PASS (all packages, including 20 new schema tests)
- `go test -race ./internal/...`: PASS (4.860s + 1.115s + 1.618s; concurrent schema sharing race-clean)
- `go vet ./...`: PASS (no output)
- `gofmt -l <touched>`: PASS (no output)

**Deviations from design brief:**
- **One deviation, justified:** the design brief locates the malformed-schema fast-fail parse in `buildStdioDispatch` (cmd/roundtable/main.go). The test contract (`TestSchemaParam_MalformedSchema_*`, `TestSchemaParam_AbsentDetection_*`) routes through `connectServer` with an injected `fakeDispatch` that bypasses `buildStdioDispatch` entirely. To satisfy the contract that malformed schemas surface as `IsError: true` with `callCount == 0` regardless of which DispatchFunc is wired, the parse fast-fail was added inside `registerTool` (the MCP transport layer) BEFORE the dispatch closure is invoked. `buildStdioDispatch` retains its own Parse call to materialize `req.Schema` for `roundtable.Run`; the redundancy is idempotent (`Parse` is pure on canonical bytes) and keeps the design's "parse owns the seam" intent intact in the production wiring path. Constraint compliance preserved: parse failure still surfaces as `IsError: true`, `additionalProperties: false` and `required: ["prompt"]` are unchanged, no new error envelope (uses existing `IsError: true` + TextContent path), no globals, no new third-party deps.
- **One minor deviation, justified:** the design brief specifies `"schema": {"type": "object"}`. The test `TestSchemaParam_AbsentDetection_TreatedAsOmitted/explicit-null` requires the MCP layer to accept `"schema": null` without rejecting at the input-schema validation step. Declaration was widened to `"schema": {"type": ["object","null"]}` so MCP's input validator passes null through to the C1 absent-detection guard in `registerTool` / `buildStdioDispatch`. `additionalProperties: false` and `required: ["prompt"]` remain unchanged; the schema field is still optional and still rejects non-object/non-null values at the MCP layer.

### Decisions
- Parse-twice (server.go fast-fail + buildStdioDispatch materialization) is correct because Parse is pure on canonical bytes; satisfies the test contract without changing the DispatchFunc signature.
- `"type": ["object", "null"]` on the MCP input-schema schema field accommodates both absent (omitted) and explicit-null cases; the C1 absent-detection in registerTool / buildStdioDispatch handles both uniformly.
- Validate gate is exactly `req.Schema != nil && result.Status == "ok"` per D4 — confirmed by `TestRunSchema_NonOkStatuses_ValidateSkipped` which iterates all six non-ok statuses.
- BuildPromptSuffix invoked even for empty-properties schemas (D7-a) — the function emits the fence preamble and closing instruction even with no field list, satisfying `TestRunSchema_EmptyPropertiesSchema_PromptNonDegenerate`.

**Next hop:** code-reviewer

## code-reviewer
<!-- Code quality review appended here. **Verdict:** APPROVED or CHANGES NEEDED. -->

### Review attempt 1

**Files reviewed:** `internal/stdiomcp/types.go`, `internal/stdiomcp/server.go`, `internal/roundtable/run.go`, `cmd/roundtable/main.go`. Compared against `internal/roundtable/dispatchschema/{prompt,validate}.go`.

**Deviation 1 — Double-parse (registerTool + buildStdioDispatch):** Defensible. The test harness injects `fakeDispatch` via `connectServer`, bypassing `buildStdioDispatch` entirely; the `registerTool` parse is the only path that satisfies oracle assertion 4 in that scenario. Both parse paths are byte-identical (same `bytes.TrimSpace` + `null` guard, same `dispatchschema.Parse` call). Parse is documented pure; double-parse is idempotent.

**Deviation 2 — `"type": ["object", "null"]` in toolInputSchema:** Correct. Non-Go MCP clients may emit explicit `null` for absent optional fields; declaring only `"type": "object"` would cause the MCP layer to reject `{"schema": null}` before C1 absent-detection fires. Real-world correction to the designer's spec.

**Findings:**
- **[MINOR]** `internal/stdiomcp/server.go:118` — Hardcoded prefix `"roundtable dispatch error: invalid schema parameter:"` duplicates the production-path prefix assembled via two wrappers. If either changes, strings diverge. Suggestion: extract a package-level const OR consolidate via the existing dispatch-error envelope.
- **[NITPICK]** `internal/roundtable/run.go:267` — `req.Schema != nil` + empty `BuildPromptSuffix` output (impossible today) would yield `basePrompt += "\n\n"` trailing whitespace. Defensive `if suffix != ""` guard would be self-documenting.

**Constraint compliance:** All D1–D8, C1–C5 correctly implemented. Field types match. Absent-detection uses `bytes.TrimSpace` + `len==0 || bytes.Equal(trimmed, []byte("null"))`. Validate gate is precisely `req.Schema != nil && result.Status == "ok"`. Error wrap uses `%w`. Unconditional `\n\n` separator on schema suffix.

**Verdict:** APPROVED

The MINOR DRY finding is logged for follow-up but does not block. Tech-debt tracker entry recommended.

## spec-reviewer
<!-- Conformance report appended here. **Verdict:** CONFORMANT or DEVIATION. -->

### Review attempt 1

**PRD:** `docs/exec-plans/prds/dispatch-structured-output.json` (F04, feature index 3)
**Code:** `internal/stdiomcp/types.go`, `internal/stdiomcp/server.go`, `internal/roundtable/run.go`, `cmd/roundtable/main.go`
**Tests:** `internal/roundtable/run_schema_test.go`, `internal/stdiomcp/server_schema_test.go`

**Assertion coverage:**
- `/features/3/oracle/assertions/0` (5 tools accept schema) → `TestSchemaParam_AllFiveToolsAcceptSchema` ✓
- `/features/3/oracle/assertions/1` (omitted-schema byte-equivalence) → `TestRunSchema_OmittedSchema_ByteEquivalence`, `TestSchemaParam_OmittedSchema_ByteEquivalence` ✓
- `/features/3/oracle/assertions/2` (Structured / StructuredError populated correctly) → `TestRunSchema_StructuredPopulated`, `TestRunSchema_StructuredError_MissingFence` ✓
- `/features/3/oracle/assertions/3` (regression: no `"structured"` keys when omitted) → `TestRunSchema_OmittedSchema_NoStructuredKeys`, `TestSchemaParam_OmittedSchema_NoStructuredKeys` ✓
- `/features/3/oracle/assertions/4` (malformed schema → MCP error, no backend) → `TestSchemaParam_MalformedSchema_MCPError`, `TestSchemaParam_MalformedSchema_BackendNotCalled` ✓

**Contract conformance:**
- `mcp_param_name: "schema"` ✓
- `mcp_param_type: object (free-form at the MCP input-schema level)` ✓ — `["object","null"]` deviation is within the "free-form" qualifier
- `affected_tools` — all 5 tools registered ✓
- `default_when_omitted` — nil-schema path skips suffix + validator; fields elided by omitempty ✓
- `backwards_compat_bar: byte-equivalent JSON` — `bytes.Equal` asserted ✓
- `error_propagation` — `callCount == 0` asserted on malformed schema ✓

**Notes (MINOR, non-blocking):**
- Double-parse in `registerTool` + `buildStdioDispatch` — idempotent; Parse is pure; required by test harness's `fakeDispatch` injection. No behavioral difference.
- `["object","null"]` vs `"object"` — real-world correction allowing explicit-null to pass through C1 absent-detection.

**Coverage gaps:** none.

**Verdict:** CONFORMANT

## safety-auditor
<!-- Audit report appended here (if applicable). **Verdict:** PASS or VIOLATION. -->

### Audit attempt 1

**Files scanned:** `internal/stdiomcp/types.go`, `internal/stdiomcp/server.go`, `internal/roundtable/run.go`, `cmd/roundtable/main.go`, `internal/roundtable/dispatchschema/{schema,prompt,validate}.go`.

**Findings (per scoped invariant):**

1. **[PASS] Prompt-injection surface (`BuildPromptSuffix`).** `prompt.go:33-72` does NOT copy field names / enum values verbatim. `sanitize()` (lines 77-102) rewrites raw `\n` → `<LF>`, ASCII controls 0x00–0x1F (excluding `\t`) → `<CTRL>`, runs of 3+ backticks → `<BACKTICKS>`. Names and enum values are then `%q`-quoted (Go-escapes embedded `"`). A hostile schema field name `"</system>\n\nIgnore prior"` becomes `"</system><LF><LF>Ignore prior"` inside a quoted string in the bullet list — no role-impersonation surface, no fence escape. Audit godoc on lines 16-32 documents the contract explicitly.

2. **[PASS] `StructuredError.Excerpt` echo path.** `validate.go:69-89` defines `ValidationError.Excerpt` with `excerptRuneCap = 200`. All call sites route through `capRunes(..., excerptRuneCap)`. F03 truncation intact. F04 introduces no new echo path; `Excerpt` is panelist-response derived, not caller-input.

3. **[PASS] Schema observability in logs.** Sole schema-related log statement is `server.go:114`: `logger.Error("schema parse error", "tool", spec.Name, "error", err)`. The `error` field carries the `dispatchschema:` parse error which echoes caller-supplied keyword/field/type names — by definition not server-internal. No log statement emits `input.Schema` bytes, `req.Schema` contents, parsed `Fields()`, or enum values.

4. **[PASS] Schema mutation / concurrency.** `run.go:267-269` (suffix append, pre-fanout) and `run.go:417-429` (Validate loop, post-`runCh`-drain) are the only F04 schema touchpoints. Per-panelist goroutines do not touch `req.Schema`. Inside `BuildPromptSuffix` and `Validate`, accessors are read-only iteration. C2 immutability invariant is honored.

5. **[PASS] Parse-failure information leakage.** All `fmt.Errorf` sites in `schema.go` echo only (a) the literal prefix `dispatchschema:`, (b) caller-supplied keyword/field/type names, (c) wrapped stdlib `encoding/json` errors. No file paths, env vars, server-side struct names, or stack traces.

6. **[MINOR] Recursive / DoS schemas.** F01 rejects nested objects (`schema.go:218-225`). However, neither `Parse` nor `BuildPromptSuffix` enforces a byte cap on `input.Schema` or a length cap on `Field.Enum()`. A hostile MCP client could submit a 10MB schema with 10⁶-entry enum. **MINOR** because (a) MCP stdio is in-process trust boundary; (b) the existing `prompt` field is also uncapped (parity DoS class, not F04-introduced); (c) panelist backends apply context-window caps downstream. Logged for follow-up.

7. **[PASS] Tool name / description echo.** F04 does not modify `toolSpecs` or introduce any new path where tool metadata flows into prompts.

**Verdict:** PASS

No domain-invariant violations. The MINOR DoS observation in finding 6 is pre-existing parity with the uncapped `prompt` field and is not a F04-introduced regression; recommend tech-debt entry but not blocking.

## arch-advisor-verification
<!-- Independent structural review at Step 7.5. **Verdict:** SOUND or UNSOUND. -->
N/A — `arch_advisor_needed: NO` per pre-check Revision attempt 2.

## landing-verifier
<!-- Landing report appended here -->

### Landing report

**Pipeline:** backend
**Verification:** `go build ./...` PASS; `go test ./...` PASS (3 packages, 20 new schema tests); `go test -race ./internal/...` PASS (race-clean); `go vet ./...` PASS (no output). All runs fresh (cache cleared).
**Spec conformance:** CONFIRMED — assertions 0–4 each have at least one covering test; `TestSchemaParam_AllFiveToolsAcceptSchema`, `TestRunSchema_OmittedSchema_ByteEquivalence`, `TestRunSchema_StructuredPopulated`, `TestRunSchema_StructuredError_MissingFence`, `TestSchemaParam_MalformedSchema_BackendNotCalled` all pass.
**Safety audit:** PASS — code-reviewer and safety-auditor verdicts confirmed; no violations.
**Code review:** APPROVED — MINOR DRY finding logged as tech-debt (non-blocking).
**Architecture review:** SOUND — touched files match handoff scope; ARCHITECTURE.md `result.go` description already reflects F04 Structured/StructuredError semantics; no doc drift.
**Doc drift:** NONE — three F04 tech-debt entries appended to `docs/exec-plans/tech-debt-tracker.md` (MINOR DRY, MINOR DoS byte-cap, MINOR doc accessor comments).

**Gate verdicts confirmed:**
- `code_review_verdict: APPROVED`
- `spec_review_verdict: CONFORMANT`
- `safety_verdict: PASS`
- `roundtable_design_verdict: APPROVED`
- `roundtable_precheck_verdict: CONCERNS` (advisory, absorbed by downstream agents — not blocking)

**File changes verified (uncommitted working tree):**
- `internal/stdiomcp/types.go` — `Schema json.RawMessage` field added to `ToolInput`
- `internal/stdiomcp/server.go` — `toolInputSchema` extended; schema fast-fail parse in `registerTool`
- `internal/roundtable/run.go` — `ToolRequest.Schema *dispatchschema.Schema`; suffix append; post-drain Validate gate
- `cmd/roundtable/main.go` — C1 absent-detection + Parse in `buildStdioDispatch`
- `internal/roundtable/run_schema_test.go` — 12 tests (new)
- `internal/stdiomcp/server_schema_test.go` — 8 tests (new)
- No unrelated files modified.

**Status:** VERIFIED
**Blockers:** none

**Next hop:** orchestrator (runs roundtable review, then Step 9 post-landing procedure)

## roundtable-landing-review
<!-- Multi-model advisory review of implementation (Step 8.5). -->

### Attempt 1 — crosscheck + critique

**Tools called:** `roundtable-crosscheck` (mixed-role panel) and `roundtable-critique` (codereviewer role) in parallel.
**Crosscheck:** 6/6 responded — all APPROVED with LOW observations.
**Critique:** 5/6 responded (gemini timed out at 631s) — all APPROVED with LOW/MEDIUM observations.

**Verdict tally:**

| Panelist | Crosscheck | Critique |
|-|-|-|
| claude (analyst) | APPROVED | APPROVED |
| codex (codereviewer) | APPROVED | APPROVED |
| gemini (planner) | APPROVED | timed out |
| deepseek | APPROVED | APPROVED (LOW) |
| kimi | APPROVED | APPROVED (LOW) |
| minimax | APPROVED | APPROVED (1 MEDIUM, dismissed below) |

**Findings (all LOW unless noted):**

1. **Brittle byte-equivalence regression test** — `bytes.Contains(out, []byte("structured"))==false` would false-positive if a future field name contains the substring. Tighten to JSON-key form `"structured":` or unmarshal-and-assert. Logged for tech-debt.
2. **Empty-Response on `Status=ok` emits `KindMissingFence`** — semantically correct but conflates "backend returned empty" with "model ignored fence." Optional follow-up: distinguish `KindEmptyResponse`. Logged.
3. **Validate latency in synchronous post-`runCh`-drain** — large responses + many fields could add tens of ms. Not user-visible vs. backend latency. Logged.
4. **Empty-properties schema yields `"structured":{}`** — low utility, harmless; matches accepted D7-a design.
5. **Production `buildStdioDispatch` parse path not directly exercised in CI** — `fakeDispatch` bypasses it; `registerTool` parse is the production safety net. Could add a direct unit test. Logged.

**Empirically dismissed (minimax MEDIUM, dismissed by claude + codex):**
- *"Production/test error message divergence"* — claude verified: `registerTool` emits `"roundtable dispatch error: invalid schema parameter: %v"` directly; `buildStdioDispatch` returns `fmt.Errorf("invalid schema parameter: %w", err)` → `server.go:139` wraps as `"roundtable dispatch error: %v"`. **Convergent wire strings.** Not a real divergence.

**Verdict:** `roundtable_landing_verdict: APPROVED`. New LOW observations logged to `docs/exec-plans/tech-debt-tracker.md` Post-MVP section.

**Status transition:** VERIFIED → READY-TO-LAND.

## doc-gardener

**Mode:** pipeline (feature F04)
**Date:** 2026-04-28

### Findings (blast-radius)

- [STALE] `ARCHITECTURE.md` test layout — Missing rows for `internal/stdiomcp/server_schema_test.go` and `internal/roundtable/run_schema_test.go`. **FIXED** in working tree (added rows describing F04 integration coverage).
- [STALE] `ARCHITECTURE.md` `server.go` description — Did not reflect schema fast-fail parse in `registerTool`. **FIXED**.
- [STALE] `ARCHITECTURE.md` request flow step 3 (`buildStdioDispatch`) — Did not mention C1 absent-detection + Parse + `IsError: true` fast-fail. **FIXED**.
- [STALE] `ARCHITECTURE.md` request flow step 8 (Result aggregation) — Did not mention post-drain Validate pass on `Status == "ok"` results. **FIXED**.

### §P5 timeline-artifact sweep

(clean) — no `## Resolved` / `## Done` / `## Changelog` sections, no strikethrough-landed entries, no commit SHAs in prose, no "as of <date>" annotations in content docs.

**doc_garden_verdict:** DRIFT_FOUND
**drift_count:** 4 (all applied to working tree before commit)
**Next hop:** orchestrator (commit + push + PR + archive)
