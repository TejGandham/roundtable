# F12 — Bounded-allocation guards in dispatchschema parser and MCP schema-input boundary

<!-- This is a handoff file template. Copy it for each feature:
     docs/exec-plans/active/handoffs/F{id}-{feature-name}.md

     RULES:
     - The YAML frontmatter block below is the machine-readable state.
       The pipeline orchestrator updates it after each agent step.
     - Agent sections below are append-only markdown. Each agent reads
       all upstream sections, then appends its own.
     - Decision-heavy agents (pre-check, designers, arch-advisor) populate
       ### Decisions and ### Constraints for downstream.
     - Implementer populates ### Decisions only (no constraints — its
       downstream agents are its reviewers).
     - Test-writer and researcher populate ### Decisions optionally.
     - Downstream agents READ upstream Decisions and Constraints FIRST.
     - Move to docs/exec-plans/completed/handoffs/ when feature lands. -->

---
status: READY-TO-LAND
pipeline: backend
prd_ref: docs/exec-plans/prds/dispatchschema-bounded-allocation.json#F12

# Pre-check routing (set by pre-check, read by orchestrator)
intent: architecture
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
remote_name: origin                  # Forgejo brahma; github is auto-mirror
roundtable_enabled: true             # CLAUDE.md absent → default true
pr_url:                              # set after gh pr create at Step 9

# Roundtable pre-check review (Step 1.3)
roundtable_precheck_attempt: 2
roundtable_precheck_verdict: CONCERNS
roundtable_precheck_skipped:         # true (with reason) if MCP unavailable

# Roundtable design review (Step 2.5)
roundtable_design_attempt: 2
roundtable_design_verdict: APPROVED
roundtable_skipped:                  # true (with reason) if MCP unavailable

# Roundtable landing review (Step 8.5)
roundtable_landing_attempt: 1
roundtable_landing_verdict: CONCERNS

# Roundtable-triggered gate re-run counters (separate from initial passes)
roundtable_retry_code_review_attempt: 0
roundtable_retry_spec_review_attempt: 0
roundtable_retry_safety_attempt: 0
---

## pre-check

## Execution Brief: Bounded-allocation guards in dispatchschema parser and MCP schema-input boundary

**PRD:** /mnt/agent-storage/vader/src/roundtable/docs/exec-plans/prds/dispatchschema-bounded-allocation.json
**Feature ID:** F12
**Feature index:** 0
**Feature pointer base:** /features/0
**Layer:** foundation
**PRD-level invariants:** none
**Dependencies:** MET — backlog shows `F01 [x]` (parser shipped) and `F04 [x]` (MCP boundary wiring shipped). Intra-PRD `needs[]` is empty; cross-PRD `Needs:` is `F01, F04` per backlog.
**Research needed:** NO — pure stdlib hardening on already-shipped infrastructure. No new dependencies, libraries, or protocols.
**Designer needed:** NO — `layer == "foundation"`. No UI surface; the contract specifies exact constants, exact file sites, and exact error semantics, which is design-equivalent.
**Implementer needed:** YES — production code changes (constants + cap checks + typed error + byte-cap branch in two boundary files).
**Safety auditor needed:** YES — this feature exists *because* the parser/boundary was an unbounded-allocation DoS vector on external schema bytes from MCP. The feature touches the security-relevant boundary that ingests untrusted input from the MCP client. Tech-debt entries `INV-001` and `F04 MINOR DoS` both close on F12. Safety auditor must verify that every cap fires *before* allocation proportional to attacker-controlled N, and that the typed-error boundary cannot leak unbounded memory to a caller that retries.
**Arch-advisor needed:** NO — standard complexity, additive caps + one new typed error. No new patterns, no module boundaries crossed, no API surface change beyond exposing the caps as exported constants and adding a typed error type.

**Intent:** mid-sized
**Complexity:** standard

**What to build:**
Add four exported constants (`MaxProperties=256`, `MaxEnumEntries=256`, `MaxRequiredEntries=256`, `MaxSchemaBytes=65536`) to the `dispatchschema` package. Enforce the three count caps inside `Parse` (in `schema.go`) by returning a typed `*ParseError` whose `Kind` distinguishes bound-violation from malformed-JSON. Enforce `MaxSchemaBytes` at the MCP boundary in **both** `internal/stdiomcp/server.go` and `cmd/roundtable/main.go` *before* calling `dispatchschema.Parse`, wrapping the cap-violation as `invalid schema parameter: schema exceeds maximum size of 65536 bytes` so callers see the existing prefix.

**New files:**
- (none) — F12 is additive to existing files. No new files required.

**Modified files:**
- `internal/roundtable/dispatchschema/schema.go` — add four exported `const`s at top; add `ParseError` struct with `Kind`/`Message` fields and `Error() string` method; add untyped string `Kind*` constants (`KindBoundExceeded`, `KindMalformed` at minimum — see Constraints for the full set); replace existing `fmt.Errorf` returns in `Parse` / `parseProperties` / `parseField` / `parseRequired` with `*ParseError`-returning variants while preserving every existing error message verbatim (regression test demands existing parser-rejection assertions keep passing); insert cap checks at: (a) properties count after `delim == '{'` in `parseProperties` (before the loop allocates fields), (b) enum length after `json.Unmarshal(rawEnum, &values)` in `parseField`, (c) required length after `json.Unmarshal(raw, &names)` in `parseRequired`.
- `internal/stdiomcp/server.go` — after `bytes.TrimSpace(input.Schema)`, before the `dispatchschema.Parse` call (line 113), check `len(trimmedSchema) > dispatchschema.MaxSchemaBytes` and short-circuit with the same `IsError: true` envelope using the prefix `"roundtable dispatch error: invalid schema parameter: schema exceeds maximum size of 65536 bytes"` (mirror the existing path's structure exactly — same logger, same envelope).
- `cmd/roundtable/main.go` — same byte-cap check after `bytes.TrimSpace(input.Schema)`, before `dispatchschema.Parse` (line 204), returning `fmt.Errorf("invalid schema parameter: schema exceeds maximum size of %d bytes", dispatchschema.MaxSchemaBytes)`.
- `internal/roundtable/dispatchschema/schema_test.go` — add table-driven boundary tests for the three count caps (N=256 pass, N=257 fail, typed error returned).
- `internal/stdiomcp/server_schema_test.go` AND `cmd/roundtable/` (a new `main_schema_test.go` if no `_test.go` exists in `cmd/roundtable/`) — add byte-cap boundary tests (65536 pass, 65537 fail, error prefix matches, backend not invoked). Tech-debt item at `tech-debt-tracker.md:55` ("F04 LOW: production `buildStdioDispatch` parse path not directly exercised in CI") is *adjacent*; F12's boundary test in `cmd/roundtable/` partly addresses it but does not close it — keep that entry open.

**Existing patterns to follow:**
- `internal/roundtable/files.go:12` — `const defaultMaxFileBytes = 128 * 1024` style for cap constants. Use the same pattern but **exported** (uppercase first letter) so the boundary code in `stdiomcp` and `cmd/roundtable` can reference them as a single source of truth.
- `internal/roundtable/dispatchschema/validate.go:26-73` — `KindMissingFence` / `KindJSONParse` / `KindSchemaViolation` untyped string constants on `ValidationError`. Use the same idiom for `ParseError.Kind` (untyped string consts; struct field `Kind string`). Per the user note in the prompt, ParseError does **not** yet exist on `Parse` — F01 introduced this idiom only on `ValidationError`. F12 introduces it on `ParseError`.
- `internal/stdiomcp/server.go:111-122` — existing schema fast-fail block (introduced by F04). The byte cap goes immediately *before* this block so it short-circuits without invoking `Parse`.
- `cmd/roundtable/main.go:201-209` — mirror site of the same pattern. Keep them parallel.

**Assertion traceability:**
- `/features/0/oracle/assertions/0` (enum 257 fails / 256 passes) → table-driven test in `schema_test.go`; build a schema with `enum` of length 256 then 257 on a string-typed field; assert success/`*ParseError` with `Kind == KindBoundExceeded`.
- `/features/0/oracle/assertions/1` (required 257 fails / 256 passes) → table-driven test in `schema_test.go`; build matching properties (256 entries to satisfy cross-ref), then a `required` array of 256 / 257 entries; assert success/typed bound error. Note: this implicitly forces `MaxProperties` to also accept 256 — keep the property generator separate so the required-cap test isolates the required cap.
- `/features/0/oracle/assertions/2` (properties 257 fails / 256 passes) → table-driven test in `schema_test.go`; generate properties of size 256 / 257; assert success/typed bound error.
- `/features/0/oracle/assertions/3` (Schema bytes > 65536 fails at boundary) → boundary test in BOTH `internal/stdiomcp/server_schema_test.go` AND a new test in `cmd/roundtable/`. Construct `input.Schema` of length exactly 65536 (passes byte cap, succeeds at Parse — easiest payload: a valid schema padded with whitespace inside the JSON object) and 65537 (fails byte cap with `IsError: true` and the wrapped prefix). Assert backend not invoked on the 65537 case (mirror `TestSchemaParam_MalformedSchema_BackendNotCalled` pattern at `server_schema_test.go:247`).
- `/features/0/oracle/assertions/4` (typed Kind, not string match) → assertion inside the cap tests above: `var pErr *dispatchschema.ParseError; errors.As(err, &pErr); pErr.Kind == dispatchschema.KindBoundExceeded`. Tests must NOT use `strings.Contains(err.Error(), "exceeds")`.
- `/features/0/oracle/assertions/5` (existing tests still pass) → regression: run the full `make test` suite green. The existing `TestParseRejectionModes` / `TestParseEdgeCases` / `TestSchemaParam_MalformedSchema_ErrorMessage` use string-content assertions on parser errors (`server_schema_test.go:269`-style). Implementer MUST preserve every existing parser error message *string* verbatim when wrapping into `*ParseError` so substring-content assertions in those tests keep passing — `ParseError.Error()` returns `"dispatchschema: <existing message>"` exactly as today.

**Edge cases:**
- **Exact boundary** (256 / 65536) must pass; 257 / 65537 must fail. No off-by-one; the cap is inclusive at the maximum, exclusive above.
- **Byte cap measured pre-trim or post-trim?** The MCP boundary already calls `bytes.TrimSpace(input.Schema)` and uses `trimmedSchema` for length checks. F12 should measure the byte cap on `len(trimmedSchema)` (post-trim) so a client cannot inflate the cap with leading/trailing whitespace, but ALSO so the test fixture for the 65536-pass case is constructible with a valid trimmed schema. Document this choice in implementer's `### Decisions`.
- **Empty / null / whitespace input** must continue to be treated as "no schema" exactly as today (the trim-and-compare-to-`null` block at `server.go:111-112` and `main.go:202-203` is unchanged). The byte cap fires only on the path where `Parse` would be called.
- **MaxProperties cap must fire before `MaxRequiredEntries`** when both would fail — but more importantly, `parseRequired`'s declared-field cross-check loop (`schema.go:271-279`) iterates `len(names)` against a map of `len(fields)`. Capping `required` AND `properties` at 256 means worst-case work is 256×O(1) lookups; capping properties first prevents an attacker-controlled `properties` blob from inflating the `declared` map allocation before required is even reached.
- **Order of cap checks inside parseField for enum:** the current code unmarshals into `[]string` (`schema.go:255`). The enum length cap MUST fire after `Unmarshal` (no way to count entries pre-decode without reimplementing the JSON tokenizer), but a 16 KiB outer byte cap means worst-case enum allocation is ~16 KiB worth of strings — acceptable. Document that this is why `MaxSchemaBytes` is the outer guard: it bounds total decode-time allocation regardless of which inner cap is reached first.
- **`Parse` is called twice in the request path** — once eagerly in `registerTool` (`server.go:113`, F04 fast-fail) and once in `buildStdioDispatch` (`main.go:204`). Both must see the cap. The byte cap must be present at BOTH sites; the count caps live inside `Parse` itself so they automatically apply at both.
- **`ParseError` wrapping vs. replacing `fmt.Errorf`:** existing tests assert error message *content* (substring). Implementer MUST NOT change the message strings; only wrap them in the typed struct. Use `errors.As` to recover the typed error in new tests; existing tests stay on substring.

**Risks:**
- **Wire-format / API drift:** introducing `*ParseError` changes the concrete return type of `Parse` from `error` (formerly produced by `fmt.Errorf`) to `*ParseError` (still satisfies `error`). Callers using `errors.Is`/`errors.As` are unaffected; callers using string-equality on `err.Error()` are theoretically affected but in practice both call sites use `%v` formatting (`server.go:118`, `main.go:206`) which routes through `Error()`. As long as `(*ParseError).Error()` returns the existing message bytes verbatim, the wire format is stable.
- **Test-suite regression from message rewording:** any reword of an existing error string will break `TestParseRejectionModes` substring assertions. Mitigation: implementer adds the typed wrapper *without* touching message text; reviewer diff-checks every `Errorf` site for byte-identical message content.
- **Boundary-byte-cap coverage at `cmd/roundtable/main.go`:** `cmd/roundtable/` has no `_test.go` today (verified by `ls`). Adding a focused test there is the right move; it also closes adjacent tech-debt item `tech-debt-tracker.md:55` to a partial extent — note this in implementer Decisions but do not re-scope F12 to fully close that item.
- **Adversarial schema crafted to skirt the count caps via deeply nested objects:** the lite-subset parser already rejects nested objects (`schema.go:223-225` — only `string`/`number`/`boolean` allowed). No new attack surface there.

**Verify command:** `make test`

**Path convention:** Go layout — `cmd/<binary>/main.go` for entry points; `internal/<package>/` for non-exported packages; tests are `_test.go` adjacent to source. Build artifacts go to repo root (`./roundtable`). Module root is `/mnt/agent-storage/vader/src/roundtable`.

**Constraints for downstream:**

### Constraints for downstream

- **MUST** export the four caps as package-level `const`s in `dispatchschema` (`MaxProperties`, `MaxEnumEntries`, `MaxRequiredEntries`, `MaxSchemaBytes`) so both boundary call sites (`stdiomcp/server.go` and `cmd/roundtable/main.go`) reference the *same* symbol. No magic numbers at the boundary sites.
- **MUST** preserve every existing parser error message string verbatim when wrapping in `*ParseError`. Existing tests in `schema_test.go` use substring assertions on error content; reword-on-wrap is a regression. The new typed `Kind` is *additive*: `KindBoundExceeded` for the three new cap-violation paths, `KindMalformed` (or equivalent) for everything else `Parse` already rejects. Define both constants explicitly in F12 so callers can branch on either.
- **MUST** apply the byte cap at BOTH boundary sites (`internal/stdiomcp/server.go` registerTool block AND `cmd/roundtable/main.go` buildStdioDispatch closure). Asymmetric application is the F04 LOW finding (`tech-debt-tracker.md:55`) repeating itself; F12 is the chance to fix it.
- **MUST NOT** introduce a new package, a new file, or a new dependency. F12 is additive to four existing files (plus tests). The `imports_test.go` leaf-package guard in `dispatchschema/` must continue to pass — no `internal/roundtable` import from `dispatchschema`.
- **MUST NOT** add configurability, env-var overrides, or runtime-mutable cap values. The contract specifies fixed integers; making them tunable is gold-plating and expands the attack surface. If a future feature genuinely needs tunable caps, that goes through `/keel-refine` as a new feature.

**Ready:** YES
**Next hop:** test-writer

### Resolved feature (verbatim from keel-feature-resolve.py)

```json
{
  "ok": true,
  "feature_id": "F12",
  "feature_index": 0,
  "feature_pointer_base": "/features/0",
  "prd_path": "/mnt/agent-storage/vader/src/roundtable/docs/exec-plans/prds/dispatchschema-bounded-allocation.json",
  "canonical_prd_path": "/mnt/agent-storage/vader/src/roundtable/docs/exec-plans/prds/dispatchschema-bounded-allocation.json",
  "title": "Bounded-allocation guards in dispatchschema parser and MCP schema-input boundary",
  "layer": "foundation",
  "oracle": {
    "type": "unit",
    "assertions": [
      "Calling `Parse` with a schema whose enum has 257 entries returns the typed bound-exceeded error; 256 entries passes.",
      "Calling `Parse` with a schema whose top-level `required` has 257 entries returns the typed bound-exceeded error; 256 entries passes.",
      "Calling `Parse` with a schema whose top-level `properties` count is 257 returns the typed bound-exceeded error; 256 entries passes.",
      "Calling the MCP boundary with `input.Schema` of 65537 bytes returns the wrapped `invalid schema parameter:` error before Parse runs; 65536 bytes passes.",
      "Bound-violation errors from `Parse` are structurally distinguishable from generic malformed-JSON errors (caller can branch on a typed Kind, not a string match).",
      "Existing parser, prompt-suffix, validator, and five-tool MCP wiring tests continue to pass."
    ],
    "tooling": "Go testing package; table-driven tests for each cap with exact-at-boundary cases (N=256 pass, N=257 fail; bytes=65536 pass, bytes=65537 fail)."
  },
  "contract": {
    "MaxProperties": 256,
    "MaxEnumEntries": 256,
    "MaxRequiredEntries": 256,
    "MaxSchemaBytes": 65536,
    "mcp_boundary_error_prefix": "invalid schema parameter:",
    "parser_sites": [
      "internal/roundtable/dispatchschema/schema.go"
    ],
    "boundary_sites": [
      "internal/stdiomcp/server.go",
      "cmd/roundtable/main.go"
    ]
  },
  "needs": [],
  "prd_invariants_exercised": [],
  "backlog_fields": {
    "prd_slug": "dispatchschema-bounded-allocation",
    "prd_exempt_reason": null,
    "spec_ref": null,
    "design_refs": [],
    "needs_ids": [
      "F01",
      "F04"
    ],
    "human_markers": []
  },
  "classification": "JSON_PRD_PATH"
}
```

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

**Test files:**
- `/mnt/agent-storage/vader/src/roundtable/internal/roundtable/dispatchschema/schema_test.go` (appended)
- `/mnt/agent-storage/vader/src/roundtable/internal/stdiomcp/server_schema_test.go` (appended)
- `/mnt/agent-storage/vader/src/roundtable/cmd/roundtable/main_schema_test.go` (new file)

**Test functions written (F12 additions only):**

`schema_test.go`:
- `TestParse_PropertiesCap_BoundaryAtCap`
- `TestParse_PropertiesCap_OverCapFails`
- `TestParse_EnumCap_BoundaryAtCap`
- `TestParse_EnumCap_OverCapFails`
- `TestParse_RequiredCap_BoundaryAtCap`
- `TestParse_RequiredCap_OverCapFails`
- `TestParse_KindMalformedPreservesCause`
- `TestSafeParse_BytecapAtBoundary`
- `TestSafeParse_BytecapOverCapFails`
- `TestSafeParse_WhitespaceFloodDoS`
- `TestSafeParse_WhitespaceUnderCapReturnsNilNil`
- `TestSafeParse_NullReturnsNilNil`
- `TestSafeParse_EmptyReturnsNilNil`

`server_schema_test.go`:
- `TestSchemaParam_OverByteCap_BackendNotCalled`
- `TestSchemaParam_WhitespaceFloodDoS_BackendNotCalled`
- `TestSchemaParam_ByteCap_AtBoundary`
- `TestSchemaParam_OverByteCap_ErrorChainRecoverable`

`main_schema_test.go`:
- `TestBuildStdioDispatch_OverByteCap_ReturnsTypedParseError`
- `TestBuildStdioDispatch_BoundaryByteCap_AtCap`

**Verification run:**

```
$ go vet ./internal/roundtable/dispatchschema/
vet: ...schema_test.go:388:46: undefined: dispatchschema.MaxProperties

$ go vet ./internal/stdiomcp/
vet: ...server_schema_test.go:534:57: undefined: dispatchschema.MaxSchemaBytes

$ go vet ./cmd/roundtable/
vet: ...main_schema_test.go:91:52: undefined: dispatchschema.MaxSchemaBytes
```

All three packages fail with `undefined: dispatchschema.<symbol>` errors only — no syntax errors in the test code itself. Status: **RED-NEW** (symbols not yet implemented).

### Decisions (optional)

- `callToolRawSchema` helper added to `server_schema_test.go` — the standard `callTool` helper accepts `map[string]any` which is re-encoded by the MCP client, making exact byte-count control impossible. The new helper serializes arguments as a raw JSON string so `ToolInput.Schema` receives exactly the bytes we intend.
- `TestSchemaParam_WhitespaceFloodDoS_BackendNotCalled` uses the same over-cap object fixture as `TestSchemaParam_OverByteCap_BackendNotCalled` at the MCP boundary, because the MCP transport requires valid JSON for the schema field (raw whitespace would be rejected at the transport layer before reaching `SafeParse`). The pre-trim whitespace flood semantics are covered by `TestSafeParse_WhitespaceFloodDoS` at the unit level.
- `TestSchemaParam_OverByteCap_ErrorChainRecoverable` tests the `%w` chain in-process (wrapping SafeParse's output as the server does) rather than through the MCP transport, because the MCP transport serializes errors to strings, erasing the Go error type.
- `TestParse_RequiredCap_OverCapFails` uses 256 properties + 257 required names (one fabricated "overflow" entry) to isolate the required cap; required cap fires before cross-ref validation per arch-advisor/designer constraint.
- All count-cap tests assert `strings.Contains(pErr.Message, "257 (max 256)")` (designer MUST constraint) and discriminate via `errors.As` only (never `strings.Contains` on `err.Error()` for Kind discrimination).

## implementer

## Implementation Report: Bounded-allocation guards in dispatchschema parser and MCP schema-input boundary

**Files created/modified:**
- `/mnt/agent-storage/vader/src/roundtable/internal/roundtable/dispatchschema/schema.go` — added 4 cap consts (`MaxProperties`, `MaxEnumEntries`, `MaxRequiredEntries`, `MaxSchemaBytes`); added `ParseError{Kind, Message, Cause error}` with `Error()` returning `Message` and `Unwrap()` returning `Cause`; added `KindBoundExceeded` / `KindMalformed` consts; added `SafeParse(raw)` measuring `len(raw) > MaxSchemaBytes` BEFORE `bytes.TrimSpace`, returning `(nil, nil)` for empty/null/whitespace-only after trim; replaced every `fmt.Errorf` in Parse / parseProperties / parseField / parseRequired / classifyTopLevelDecodeErr with `&ParseError{KindMalformed, …, Cause: inner}` preserving message strings byte-for-byte; inserted properties cap (fail-fast at N+1 in parseProperties), enum cap (post-Unmarshal in parseField), required cap (post-Unmarshal, BEFORE cross-ref loop in parseRequired). Added godoc on Parse warning that untrusted input must go through SafeParse.
- `/mnt/agent-storage/vader/src/roundtable/internal/stdiomcp/server.go` — collapsed lines 111-122 (trim + null-check + Parse + envelope) to a single `dispatchschema.SafeParse(input.Schema)` call. Used `fmt.Errorf("roundtable dispatch error: invalid schema parameter: %w", err).Error()` to render the wrapped string into the existing `*mcp.TextContent`. Removed now-unused `bytes` import.
- `/mnt/agent-storage/vader/src/roundtable/cmd/roundtable/main.go` — collapsed lines 201-209 (var parsedSchema + trim + null-check + Parse) to `parsedSchema, err := dispatchschema.SafeParse(input.Schema)` plus `fmt.Errorf("invalid schema parameter: %w", err)`. Removed now-unused `bytes` import.

**Change scope:** initial
**Changed paths:**
- /mnt/agent-storage/vader/src/roundtable/internal/roundtable/dispatchschema/schema.go
- /mnt/agent-storage/vader/src/roundtable/internal/stdiomcp/server.go
- /mnt/agent-storage/vader/src/roundtable/cmd/roundtable/main.go

**Test status:** PASS

**Commands run:**

```
$ go vet ./...
(clean)

$ go test ./internal/roundtable/dispatchschema/ -run "TestParse_PropertiesCap_OverCapFails|TestParse_EnumCap_OverCapFails|TestParse_RequiredCap_OverCapFails|TestSafeParse_BytecapOverCapFails|TestSafeParse_BytecapAtBoundary|TestSafeParse_WhitespaceFloodDoS|TestParse_KindMalformedPreservesCause" -count=1 -v
=== RUN   TestParse_PropertiesCap_OverCapFails
--- PASS: TestParse_PropertiesCap_OverCapFails (0.00s)
=== RUN   TestParse_EnumCap_OverCapFails
--- PASS: TestParse_EnumCap_OverCapFails (0.00s)
=== RUN   TestParse_RequiredCap_OverCapFails
--- PASS: TestParse_RequiredCap_OverCapFails (0.00s)
=== RUN   TestParse_KindMalformedPreservesCause
--- PASS: TestParse_KindMalformedPreservesCause (0.00s)
=== RUN   TestSafeParse_BytecapAtBoundary
--- PASS: TestSafeParse_BytecapAtBoundary (0.00s)
=== RUN   TestSafeParse_BytecapOverCapFails
--- PASS: TestSafeParse_BytecapOverCapFails (0.00s)
=== RUN   TestSafeParse_WhitespaceFloodDoS
--- PASS: TestSafeParse_WhitespaceFloodDoS (0.00s)
PASS
ok  	github.com/TejGandham/roundtable/internal/roundtable/dispatchschema	0.007s

$ go test ./... -count=1
ok  	github.com/TejGandham/roundtable/cmd/roundtable	0.012s
ok  	github.com/TejGandham/roundtable/internal/roundtable	3.701s
ok  	github.com/TejGandham/roundtable/internal/roundtable/dispatchschema	0.043s
ok  	github.com/TejGandham/roundtable/internal/stdiomcp	0.400s

$ go test -race ./internal/... ./cmd/... -count=1
ok  	github.com/TejGandham/roundtable/internal/roundtable	4.795s
ok  	github.com/TejGandham/roundtable/internal/roundtable/dispatchschema	1.158s
ok  	github.com/TejGandham/roundtable/internal/stdiomcp	1.665s
ok  	github.com/TejGandham/roundtable/cmd/roundtable	1.029s

$ gofmt -l <changed files>
(clean)
```

**Blockers:** none.

### Decisions

- **Server boundary text rendering uses `fmt.Errorf("...%w", err).Error()`** — handoff specified `%w` (per attempt-2 finding), but the MCP envelope requires a string for `*mcp.TextContent.Text`. Wrapping with `%w` then calling `.Error()` produces the same rendered string as `%v` while keeping the source-level wrap idiom symmetric with main.go's `fmt.Errorf("...%w", err)` return. The chain is not preserved across the MCP transport (test `TestSchemaParam_OverByteCap_ErrorChainRecoverable` reconstructs the wrap in-process for the chain-recoverable assertion, so the contract is honored).
- **`Cause` populated on every `KindMalformed` site that previously used `%w`** — `classifyTopLevelDecodeErr`, `parseProperties` token/decode errors, `parseField` Unmarshal errors, `parseRequired` Unmarshal error. The `Message` uses `%v` of the inner error so the verbatim message text is preserved for substring assertions (`TestParseRejectionModes`, `TestParseEdgeCases`), while `Cause` carries the typed inner for `errors.As` (test `TestParse_KindMalformedPreservesCause` asserts `errors.As(pErr.Cause, &json.SyntaxError{})` succeeds for truncated JSON `{`).
- **Properties cap reports `n` post-append (not pre-append)** — the loop appends then checks `len(fields) > MaxProperties`, so the over-cap error reports `257 (max 256)` per the designer's `"257 (max 256)"` substring assertion.
- **Required cap fires BEFORE cross-ref validation** — placed immediately after `json.Unmarshal(raw, &names)` and before the `declared` map allocation. `TestParse_RequiredCap_OverCapFails` builds 256 properties + 257 required names (one fabricated "overflow") and the cap fires before the cross-ref would have flagged "overflow" as undeclared — confirming the design constraint.
- **Removed `bytes` import from server.go and main.go** — `SafeParse` now owns the trim and null-check, leaving no `bytes.TrimSpace` / `bytes.Equal` at the boundary sites. Goimports / go vet both clean.

**Next hop:** code-reviewer

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

## Landing Report: Bounded-allocation guards in dispatchschema parser and MCP schema-input boundary

**Pipeline:** backend
**Verification:** go build ./... clean; go vet ./... clean; go test ./... -count=1 all 4 packages PASS; go test -race ./internal/... -count=1 all 3 packages PASS (no data races). All 19 new test functions confirmed PASS individually: 13 in dispatchschema/schema_test.go, 4 in stdiomcp/server_schema_test.go, 2 in cmd/roundtable/main_schema_test.go.
**Spec conformance:** CONFIRMED
**Safety audit:** PASS
**Code review:** APPROVED
**Architecture review:** SOUND
**Doc drift:** MINOR — ARCHITECTURE.md line 119 describes the boundary path as calling `dispatchschema.Parse` directly with manual trim/null-check; the implementation now calls `dispatchschema.SafeParse`. The prose is descriptively stale. Not a blocker: the section is summary prose, no load-bearing spec depends on it. Should be updated in a chore sweep.

**Status:** VERIFIED
**Blockers (if any):** none

**Next hop:** orchestrator (runs roundtable review if enabled, then Step 9 post-landing procedure)

## roundtable-landing-review
<!-- Multi-model advisory review of implementation (Step 8.5, if roundtable enabled).
     Orchestrator calls roundtable-crosscheck + roundtable-critique tools. Output appended here. -->

## roundtable-precheck-review

**Attempt 1.** Critique panel: claude-opus-4-7, codex (cli-default), fireworks-deepseek (deepseek-v4-pro), fireworks-kimi (kimi-k2p6), fireworks-minimax (minimax-m2p7), gemini (gemini-3.1-pro-preview). 6/6 responded.

### Consensus findings

**1. `designer_needed: NO` → flip to YES.** All 6 panelists agree. F12 introduces `*ParseError` as a NEW typed error in `dispatchschema` (parser today returns plain `fmt.Errorf`). Kind enum values, error-wrapping (`%w` / `Unwrap()`), `errors.As` discriminability, and relationship to the existing `ValidationError` in `validate.go` are non-trivial design choices that test-writer must encode into oracle assertions. Without designer sign-off, the Kind taxonomy will likely be inconsistent with `ValidationError` conventions, and a future parser error will fragment the error surface.

**2. `complexity: standard` → upgrade to `complex`.** All 6 panelists agree. Cross-file invariant (64KB cap duplicated across `server.go` AND `main.go`), new typed error type with Kind enum, and three-package surface (`dispatchschema`, `stdiomcp`, `cmd/roundtable`). "Standard" undersells the blast radius.

**3. `arch_advisor_needed: NO` → flip to YES.** 5/6 panelists agree. Byte cap is enforced OUTSIDE `Parse` at two call sites, while count caps live INSIDE `Parse`. This split is an architectural invariant that future Parse call sites can violate (any new caller forgetting the byte cap = DoS bypass). Arch should decide: (a) move byte cap INTO Parse, or (b) introduce a sanctioned `SafeParse` wrapper, or (c) explicitly accept and document the duplication with a parity-checking test.

**4. `safety_auditor_needed: YES` — correct, but scope is undefined.** 5/6 panelists agree. Audit checklist must specify: (a) every `dispatchschema.Parse` call site has the byte cap upstream (grep both binaries + tests), (b) byte cap fires BEFORE JSON decode/Parse (cheap-reject ordering, allocation-before-cap is a security property), (c) error messages do not echo schema content (info leak), (d) cap inclusivity consistent with "at-and-just-over" oracle assertions, (e) dual-site parity (server.go and main.go enforce the same prefix verbatim), (f) F04 wiring cannot replace schema bytes after boundary validation.

**5. Contract gaps blocking test-writer.** 4/6 panelists agree. Underspecified items: (a) Kind enum values not enumerated, (b) cap inclusivity ambiguity ("at-and-just-over" — PRD says 256 passes / 257 fails so inclusive — but message text needs the verbatim format), (c) count-cap error message format (PRD only specifies the byte-cap prefix), (d) "structured error distinguishable from malformed JSON" mechanism not named (errors.As? Kind ==? type assertion?), (e) byte measurement: `len([]byte(schema))` after `bytes.TrimSpace`.

### Verdict

**roundtable_precheck_verdict: CONCERNS (attempt 1).** Pre-check brief is materially incomplete on the four routing flags above plus contract ambiguity. Sending back to pre-check for revision.

## pre-check (attempt 2)

Roundtable critique panel raised five findings (6/6 consensus on top three). Concede on findings 1, 2, 4, 5; concede on finding 3 with a specific scoping note. YAML routing flags updated in place: `intent: architecture`, `complexity: complex`, `designer_needed: YES`, `arch_advisor_needed: YES`.

### Response to panel findings

**(1) `designer_needed: NO → YES` — concede.** I underweighted the `*ParseError` taxonomy decision. The package already has a precedent — `ValidationError` in `validate.go` lines 27-86 with three untyped string `Kind` constants (`KindMissingFence`, `KindJSONParse`, `KindSchemaViolation`). Introducing a parallel `ParseError` requires deciding: (a) Kind enum granularity (one `KindBoundExceeded` covering all three caps, vs. per-cap kinds), (b) wrap vs. replace existing `fmt.Errorf` returns, (c) `Unwrap()` chain for `errors.Is/As`, (d) whether `ParseError` and `ValidationError` should share a parent interface, (e) whether the field path grammar in `ValidationError`'s godoc applies to `ParseError`. Test-writer cannot encode `errors.As(err, &pErr); pErr.Kind == KindBoundExceeded` assertions without these answers. Backend-designer.

**(2) `complexity: standard → complex` — concede.** Three-package surface (`dispatchschema`, `stdiomcp`, `cmd/roundtable`), cross-file invariant (byte cap duplicated at two boundary call sites), new typed error type with Kind enum, and regression preservation across all existing parser-rejection tests. The blast radius is bigger than "standard."

**(3) `arch_advisor_needed: NO → YES` — concede with scope.** The split (byte cap outside `Parse` at two sites, count caps inside `Parse`) is a real structural invariant. A future Parse caller (e.g., a config loader, a CLI subcommand, a test helper that calls Parse with externally-supplied bytes) can silently bypass the byte cap. Three architecturally-meaningful options:
  - **(a) Move byte cap into `Parse`.** Simplest. `Parse` becomes the single chokepoint. Cost: changes Parse's contract (now rejects oversize input even from trusted callers).
  - **(b) Introduce `SafeParse(raw)` that applies the byte cap then delegates to `Parse`.** Two-layer API. Cost: requires audit to ensure all *external-input* call sites use SafeParse; adds API surface.
  - **(c) Document and accept the split, add a parity-checking test.** Cheapest now, riskiest later. Requires a grep-based test or a test that enumerates all call sites of `dispatchschema.Parse` and verifies each has the cap upstream.
  
  Arch-advisor decides which. I lean (a) for simplicity but flagging this as the architectural call rather than pre-empting it.

**(4) `safety_auditor_needed: YES, scope undefined` — concede.** Audit checklist now enumerated explicitly in Constraints. Six items per panel; I add a seventh on cap-constant single-source-of-truth.

**(5) Contract gaps blocking test-writer — concede.** I added more specifics to Constraints below: byte measurement = `len(trimmedSchema)` post-`bytes.TrimSpace` (matches existing F04 boundary code); cap inclusivity = N≤cap passes, N>cap fails (256 ✓, 257 ✗; 65536 ✓, 65537 ✗); discrimination mechanism = `errors.As(err, &target)` against `*ParseError` (no substring matching); Kind enum names deferred to backend-designer (the panel's finding 5(d) explicitly says "implementer's choice per PRD design_facts" — the `Kind` literal *names* are designer's call, but the brief now flags that this taxonomy decision exists and downstream must enumerate it before test-writer encodes assertions).

### Revised execution brief

## Execution Brief: Bounded-allocation guards in dispatchschema parser and MCP schema-input boundary

**PRD:** /mnt/agent-storage/vader/src/roundtable/docs/exec-plans/prds/dispatchschema-bounded-allocation.json
**Feature ID:** F12
**Feature index:** 0
**Feature pointer base:** /features/0
**Layer:** foundation
**PRD-level invariants:** none
**Dependencies:** MET — backlog shows `F01 [x]` (parser shipped) and `F04 [x]` (MCP boundary wiring shipped). Intra-PRD `needs[]` is empty; cross-PRD `Needs:` is `F01, F04` per backlog.
**Research needed:** NO — pure stdlib hardening on already-shipped infrastructure. No new dependencies, libraries, or protocols.
**Designer needed:** YES — `*ParseError` introduction parallel to existing `ValidationError` (validate.go:27-86) is a non-trivial taxonomy/wrapping decision; test-writer needs concrete `Kind` constants and `errors.As` discriminability semantics encoded before writing assertions.
**Implementer needed:** YES — production code changes (constants + cap checks + typed error + byte-cap branch in two boundary files).
**Safety auditor needed:** YES — feature exists *because* parser/boundary was an unbounded-allocation DoS vector on external schema bytes from MCP. Audit scope enumerated in Constraints (six items + cap-constant single-source-of-truth).
**Arch-advisor needed:** YES — the split between byte-cap-outside-Parse and count-caps-inside-Parse is a structural invariant. Arch-advisor decides between three options: (a) move byte cap into Parse, (b) introduce `SafeParse` wrapper, (c) document split + add parity-checking test. Decision must precede backend-designer so `*ParseError` shape is consistent with the chosen call-site discipline.

**Intent:** architecture
**Complexity:** complex

**What to build:**
Add four exported caps (`MaxProperties=256`, `MaxEnumEntries=256`, `MaxRequiredEntries=256`, `MaxSchemaBytes=65536`) to the `dispatchschema` package. Enforce the three count caps inside `Parse` (`schema.go`) by returning a typed `*ParseError` whose `Kind` distinguishes bound-violation from malformed-JSON. Enforce `MaxSchemaBytes` at the MCP boundary in **both** `internal/stdiomcp/server.go` and `cmd/roundtable/main.go` *before* calling `dispatchschema.Parse`, wrapping the cap-violation as `invalid schema parameter: schema exceeds maximum size of 65536 bytes` so callers see the existing prefix. (Arch-advisor may relocate the byte cap into Parse itself; if so, the boundary sites short-circuit on a byte-cap kind from `*ParseError` instead of duplicating the check.)

**New files:**
- (none) — F12 is additive to existing files. No new files required. (If arch-advisor selects option (b) `SafeParse`, that lives in `schema.go` alongside `Parse` — still no new file.)

**Modified files:**
- `internal/roundtable/dispatchschema/schema.go` — add four exported `const`s at top; add `ParseError` struct with `Kind`/`Message` fields and `Error() string` method; add untyped string `Kind*` constants matching the `validate.go:27-46` idiom (specific names deferred to backend-designer); replace existing `fmt.Errorf` returns in `Parse` / `parseProperties` / `parseField` / `parseRequired` with `*ParseError`-returning variants while preserving every existing error message verbatim (regression test demands existing parser-rejection assertions keep passing); insert cap checks at: (a) properties count after `delim == '{'` in `parseProperties` (line 167) — count entries as the streaming loop accumulates, fail fast at N+1; (b) enum length after `json.Unmarshal(rawEnum, &values)` in `parseField` (line 256); (c) required length after `json.Unmarshal(raw, &names)` in `parseRequired` (line 269).
- `internal/stdiomcp/server.go` — after `bytes.TrimSpace(input.Schema)` (line 111), before the `dispatchschema.Parse` call (line 113), check `len(trimmedSchema) > dispatchschema.MaxSchemaBytes` and short-circuit with the same `IsError: true` envelope using the prefix `"roundtable dispatch error: invalid schema parameter: schema exceeds maximum size of 65536 bytes"` (mirror the existing path's structure exactly — same logger, same envelope). If arch-advisor option (a) wins, this becomes a `*ParseError`-with-byte-cap-Kind path that surfaces through the existing Parse error block.
- `cmd/roundtable/main.go` — same byte-cap check after `bytes.TrimSpace(input.Schema)` (line 202), before `dispatchschema.Parse` (line 204), returning `fmt.Errorf("invalid schema parameter: schema exceeds maximum size of %d bytes", dispatchschema.MaxSchemaBytes)`.
- `internal/roundtable/dispatchschema/schema_test.go` — add table-driven boundary tests for the three count caps (N=256 pass, N=257 fail, typed error returned via `errors.As`).
- `internal/stdiomcp/server_schema_test.go` AND a new `cmd/roundtable/main_schema_test.go` (no `_test.go` exists in `cmd/roundtable/` today) — add byte-cap boundary tests (65536 pass, 65537 fail, error prefix matches verbatim, backend not invoked). Tech-debt item at `tech-debt-tracker.md:55` is *adjacent*; F12's `cmd/roundtable/` boundary test partly addresses it but does not close it — keep that entry open.

**Existing patterns to follow:**
- `internal/roundtable/files.go:12` — `const defaultMaxFileBytes = 128 * 1024` style for cap constants. Use the same pattern but **exported** (uppercase first letter) so the boundary code in `stdiomcp` and `cmd/roundtable` can reference them as a single source of truth.
- `internal/roundtable/dispatchschema/validate.go:27-46` — `KindMissingFence` / `KindJSONParse` / `KindSchemaViolation` untyped string constants on `ValidationError`. Use the same idiom for `ParseError.Kind` (untyped string consts; struct field `Kind string`). Specific Kind names are backend-designer's call; the *idiom* is fixed.
- `internal/roundtable/dispatchschema/validate.go:69-86` — `ValidationError` struct shape (`Kind`, `Field`, `Message`, `Excerpt` with json tags). Backend-designer decides whether `ParseError` mirrors this shape (with `Excerpt` for the offending substring) or stays minimal (`Kind`/`Message` only). Note: `ValidationError.Excerpt` carries sensitive content under egress policy — `ParseError` may or may not want that surface area.
- `internal/stdiomcp/server.go:111-122` — existing schema fast-fail block (introduced by F04). The byte cap goes immediately *before* this block so it short-circuits without invoking `Parse` (option c) — OR is folded into Parse (option a/b).
- `cmd/roundtable/main.go:201-209` — mirror site of the same pattern. Keep them parallel.
- `internal/stdiomcp/server_schema_test.go:247` — `TestSchemaParam_MalformedSchema_BackendNotCalled` is the pattern for asserting "boundary error AND backend not invoked"; mirror this for byte-cap tests.

**Assertion traceability:**
- `/features/0/oracle/assertions/0` (enum 257 fails / 256 passes) → table-driven test in `schema_test.go`; build a schema with `enum` of length 256 then 257 on a string-typed field; assert success/`*ParseError` discriminated via `errors.As` with `Kind == <bound-exceeded-name-from-designer>`.
- `/features/0/oracle/assertions/1` (required 257 fails / 256 passes) → table-driven test in `schema_test.go`; build matching properties (256 entries to satisfy cross-ref), then a `required` array of 256 / 257 entries; assert success/typed bound error. Note: this implicitly forces `MaxProperties` to also accept 256 — keep the property generator separate so the required-cap test isolates the required cap.
- `/features/0/oracle/assertions/2` (properties 257 fails / 256 passes) → table-driven test in `schema_test.go`; generate properties of size 256 / 257; assert success/typed bound error.
- `/features/0/oracle/assertions/3` (Schema bytes > 65536 fails at boundary) → boundary test in BOTH `internal/stdiomcp/server_schema_test.go` AND a new test in `cmd/roundtable/main_schema_test.go`. Construct `input.Schema` of length exactly 65536 bytes (passes byte cap, succeeds at Parse — easiest payload: a valid schema padded with whitespace inside the JSON object) and 65537 (fails byte cap with `IsError: true` and the wrapped prefix). Assert backend not invoked on the 65537 case (mirror `TestSchemaParam_MalformedSchema_BackendNotCalled` at `server_schema_test.go:247`).
- `/features/0/oracle/assertions/4` (typed Kind, not string match) → assertion inside the cap tests above: `var pErr *dispatchschema.ParseError; if !errors.As(err, &pErr) { t.Fatal(...) }; if pErr.Kind != dispatchschema.<bound-exceeded-name> { t.Fatal(...) }`. Tests MUST NOT use `strings.Contains(err.Error(), "exceeds")` for the discrimination — substring matching defeats the typed-error contract.
- `/features/0/oracle/assertions/5` (existing tests still pass) → regression: `make test` green. Existing `TestParseRejectionModes` / `TestParseEdgeCases` / `TestSchemaParam_MalformedSchema_ErrorMessage` use string-content assertions on parser errors. Implementer MUST preserve every existing parser error message *string* verbatim when wrapping into `*ParseError` so substring-content assertions in those tests keep passing. `(*ParseError).Error()` returns the existing message bytes verbatim (or `"dispatchschema: <existing message>"` exactly as today, depending on backend-designer's decision on whether the wrapper restates the package prefix).

**Edge cases:**
- **Exact boundary** (256 / 65536) MUST pass; 257 / 65537 MUST fail. Cap is inclusive at the maximum (N ≤ cap passes, N > cap fails). No off-by-one.
- **Byte cap measured post-trim.** The MCP boundary already calls `bytes.TrimSpace(input.Schema)` and uses `trimmedSchema` for length checks (`server.go:111`, `main.go:202`). F12 measures the byte cap as `len(trimmedSchema)` (post-trim) so a client cannot inflate the cap with leading/trailing whitespace, and so the test fixture for the 65536-pass case is constructible with a valid trimmed schema. Implementer documents this in `### Decisions`.
- **Empty / null / whitespace input** continues to be treated as "no schema" exactly as today (the trim-and-compare-to-`null` block at `server.go:111-112` and `main.go:202-203` is unchanged). The byte cap fires only on the path where `Parse` would be called.
- **Order of caps inside `parseProperties`.** The streaming decoder loop (`schema.go:170-190`) walks properties one at a time. Implementer can fail-fast at iteration N+1 (cheaper than counting first then checking). Same pattern for `parseRequired`/enum: count *after* unmarshal since neither offers a streaming entry count without re-tokenizing, but the outer `MaxSchemaBytes` cap bounds total decode-time allocation regardless of which inner cap fires first.
- **`Parse` called twice in the request path.** Once eagerly in `registerTool` (`server.go:113`, F04 fast-fail) and once in `buildStdioDispatch` (`main.go:204`). Both must see the byte cap. Count caps live inside `Parse` itself so they automatically apply at both. **This duplication is the structural invariant arch-advisor must rule on.**
- **`ParseError` wrapping vs. replacing `fmt.Errorf`.** Existing tests assert error message *content* (substring). Implementer MUST NOT change message strings; only wrap them in the typed struct. `errors.As` recovers the typed error in new tests; existing tests stay on substring.

**Risks:**
- **Wire-format / API drift:** introducing `*ParseError` changes `Parse`'s concrete return type from `error` (`fmt.Errorf`) to `*ParseError` (still satisfies `error`). Callers using `errors.Is`/`errors.As` are unaffected; callers using string-equality on `err.Error()` are theoretically affected, but in practice both call sites use `%v` formatting (`server.go:118`, `main.go:206`) which routes through `Error()`. As long as `(*ParseError).Error()` returns the existing message bytes verbatim, the wire format is stable.
- **Test-suite regression from message rewording:** any reword of an existing error string breaks `TestParseRejectionModes` substring assertions. Mitigation: implementer adds the typed wrapper *without* touching message text; reviewer diff-checks every `Errorf` site for byte-identical message content.
- **`cmd/roundtable/main.go` boundary-byte-cap coverage:** `cmd/roundtable/` has no `_test.go` today. Adding a focused `main_schema_test.go` is the right move; it also closes adjacent tech-debt item `tech-debt-tracker.md:55` to a partial extent — note this in implementer Decisions but do not re-scope F12 to fully close that item.
- **Adversarial schema crafted to skirt count caps via deeply nested objects:** the lite-subset parser already rejects nested objects (`schema.go:223-225` — only `string`/`number`/`boolean` allowed). No new attack surface.
- **Structural-invariant drift:** if arch-advisor chooses option (c) (document the split), a future contributor adding a third Parse call site MUST add the byte cap upstream. Mitigation is the parity test arch-advisor would mandate.

**Verify command:** `make test`

**Path convention:** Go layout — `cmd/<binary>/main.go` for entry points; `internal/<package>/` for non-exported packages; tests are `_test.go` adjacent to source. Build artifacts go to repo root (`./roundtable`). Module root is `/mnt/agent-storage/vader/src/roundtable`.

### Constraints for downstream

- **MUST** export the four caps as package-level `const`s in `dispatchschema` (`MaxProperties`, `MaxEnumEntries`, `MaxRequiredEntries`, `MaxSchemaBytes`) so both boundary call sites reference the *same* symbol. No magic numbers at the boundary sites.
- **MUST** preserve every existing parser error message string verbatim when wrapping in `*ParseError`. Existing tests in `schema_test.go` use substring assertions on error content; reword-on-wrap is a regression. The new typed `Kind` is *additive*: cap-violation paths get a new Kind value; existing reject paths get an "everything else" Kind. Backend-designer decides whether that is a single `KindBoundExceeded` vs. per-cap kinds, and decides the malformed-bucket name(s).
- **MUST** apply the byte cap at BOTH boundary sites OR move it into `Parse` per arch-advisor decision. Asymmetric application is the failure mode the panel surfaced; either fix it via single-chokepoint or via a parity-checking test.
- **MUST** measure the byte cap as `len(trimmedSchema)` (post-`bytes.TrimSpace`). Pre-trim measurement is rejected because (a) it inflates the effective cap with whitespace and (b) it diverges from the existing F04 boundary code that already operates on `trimmedSchema`.
- **MUST** apply cap inclusivity as N ≤ cap passes, N > cap fails. 256 entries passes, 257 fails. 65536 bytes passes, 65537 fails.
- **MUST** use `errors.As(err, &target)` against `*ParseError` to discriminate bound-exceeded from malformed-JSON in tests (PRD oracle assertion 4). Substring matching on `err.Error()` is forbidden for the discrimination assertion.
- **MUST** safety-audit scope (verbatim, for safety-auditor):
  1. Every `dispatchschema.Parse` call site has the byte cap upstream — grep both binaries + tests.
  2. Byte cap fires BEFORE JSON decode/Parse (cheap-reject ordering — allocation-before-cap is a security property).
  3. Error messages do NOT echo schema content (info leak surface — `ValidationError.Excerpt` carries sensitive bytes by design; `ParseError` should not unless backend-designer makes that an explicit choice).
  4. Cap inclusivity consistent across implementation and tests (256 passes, 257 fails; 65536 passes, 65537 fails — not 256-fails or 65535-fails by mistake).
  5. Dual-site parity for the byte-cap prefix string: `server.go` and `main.go` produce the *same* `invalid schema parameter:` prefix verbatim. (If arch-advisor option (a) wins, this collapses into single-site.)
  6. F04 wiring cannot replace schema bytes between boundary validation and Parse — confirm `trimmedSchema` is the value passed to Parse and not re-derived from `input.Schema` after the cap fires.
  7. Cap constants are referenced by exported symbol from both boundary sites — no magic numbers, no per-site constant drift.
- **MUST NOT** introduce a new package, a new file (other than `cmd/roundtable/main_schema_test.go`), or a new dependency. F12 is additive to existing files. The `imports_test.go` leaf-package guard in `dispatchschema/` must continue to pass — no `internal/roundtable` import from `dispatchschema`.
- **MUST NOT** add configurability, env-var overrides, or runtime-mutable cap values. Contract specifies fixed integers; making them tunable is gold-plating and expands the attack surface. Future-tunable caps go through `/keel-refine` as a separate feature.
- **MUST NOT** (slop watch) add docstrings to existing untouched code, refactor adjacent helpers, or extract a "boundary-cap utility" for two call sites — premature abstraction.

**Ready:** YES — pending arch-advisor structural decision and backend-designer Kind taxonomy.
**Next hop:** arch-advisor (Step 1.7) → backend-designer → test-writer.

### Resolved feature (verbatim from keel-feature-resolve.py)

(Unchanged from attempt 1 — see above.)

### Roundtable attempt 2 — verification critique

Tally: 2 APPROVED (codex, deepseek), 4 CONCERNS (claude, kimi, minimax, gemini). Per skill Step 1.3.7, attempt 2 still divergent → proceed with revised routing as advisory CONCERNS.

**Unresolved concerns (forwarded downstream — not blocking the pipeline but tracked):**

1. **CRITICAL (Gemini) — whitespace pre-trim allocation vector.** Brief specifies `len(trimmedSchema)` post-`bytes.TrimSpace`. An attacker submitting 100 MB of pure whitespace passes the byte cap (post-trim is empty) but already forced the server to allocate and trim. Mitigation: byte-cap MUST measure `len(input.Schema)` BEFORE `bytes.TrimSpace`, so allocation-bound payloads never reach trim. **Forward to arch-advisor and implementer as a correctness constraint.**

2. **HIGH (Claude/Kimi/Minimax) — option-conditional constraints.** Brief's `len(trimmedSchema)` measurement and dual-site parity audit-item presuppose option (c). If arch-advisor picks (a) move-into-Parse, several constraints invert (Parse can't see pre-trim, boundary sites no longer reference the constant). **Forward to arch-advisor: emit option-conditional constraints in the consultation output.**

3. **HIGH (Gemini) — option (c) is fail-open.** Documentation-based parity is enforced by developer discipline, not the type system. A future Parse caller forgetting the byte cap is a silent DoS bypass. Gemini argues for unexported `parse` + exported `SafeParse` (compiler-enforced). **Forward to arch-advisor as a strong nudge toward (a) or (b).**

4. **MEDIUM (Claude) — byte-cap message format drift.** server.go uses literal `"...maximum size of 65536 bytes"`; main.go uses `fmt.Errorf("...%d bytes", MaxSchemaBytes)`. Audit-item 5 (dual-site parity) will flag this. **Forward to backend-designer: pick one form and apply uniformly.**

5. **MEDIUM (Codex) — option (a) trim-semantic ambiguity.** If byte cap moves into Parse, does Parse trim before measuring? Direct callers with whitespace-padded input would be capped on raw bytes while boundary callers cap post-trim. Resolved by concern (1) — measure raw bytes uniformly. **Forward to arch-advisor.**

### Verdict

**roundtable_precheck_verdict: CONCERNS (attempt 2).** Routing accepted as revised; concerns 1, 2, 3 forwarded to arch-advisor; concern 4 to backend-designer; concern 5 follows from concern 1's resolution.

## arch-advisor-consultation

**Bottom line:** Pick **option (b) with a twist** — introduce exported `SafeParse(raw json.RawMessage)` as the sanctioned entry point for any external/untrusted bytes; keep `Parse` exported but document it as "trusted in-memory bytes only." The byte cap MUST measure `len(raw)` BEFORE any trim — Gemini's pre-trim DoS concern is correct and forces this regardless of placement. `SafeParse` does: raw-byte cap → `bytes.TrimSpace` → null/empty short-circuit → `Parse`. Both boundary sites collapse from "trim + null-check + cap + Parse" to a single `SafeParse` call.

**Action plan:**
1. Add `SafeParse(raw json.RawMessage) (*Schema, error)` in `internal/roundtable/dispatchschema/schema.go`. Checks `len(raw) > MaxSchemaBytes` against the *raw* (pre-trim) length, returns `*ParseError{Kind: KindBoundExceeded, Message: "schema exceeds maximum size of 65536 bytes"}` on violation, then trims, applies the existing null/empty short-circuit (returning `(nil, nil)` for "no schema"), and delegates to `Parse`.
2. Keep `Parse` exported but add a godoc note: "Parse expects already-trimmed, non-empty bytes from a trusted source. Untrusted input MUST go through SafeParse."
3. The three count caps (`MaxProperties`, `MaxEnumEntries`, `MaxRequiredEntries`) live inside `Parse` as the brief specifies, fail-fast at N+1.
4. In `internal/stdiomcp/server.go` (lines 111-122), replace the trim + null-check + Parse block with a single `SafeParse(input.Schema)` call; surface the existing wrapped prefix on `KindBoundExceeded`.
5. In `cmd/roundtable/main.go` (lines 201-209), parallel change: `parsedSchema, err := dispatchschema.SafeParse(input.Schema)`.
6. `ParseError{Kind, Message}` — no `Excerpt` field (schema bytes are attacker-controlled; echoing them is info-leak, audit item 3). Define `KindBoundExceeded` and `KindMalformed` (umbrella for everything else).
7. Tests: `schema_test.go` exercises `Parse` directly for count caps. `server_schema_test.go` and the new `cmd/roundtable/main_schema_test.go` exercise the byte cap through `SafeParse` via the boundary, and include a regression test that 100 MB of pure whitespace returns `KindBoundExceeded` *without* any successful Parse — the Gemini pre-trim DoS witness.

**Why this approach:**
- Compiler-enforced front door: future Parse callers reading external bytes have a single obvious sanctioned API (`SafeParse`); reviewers see `dispatchschema.Parse(...)` on untrusted input and immediately flag it.
- Resolves the pre-trim DoS by construction: cap is measured on `len(raw)` at the only place that ever sees raw bytes from the wire.
- Eliminates dual-site duplication of trim + null-check + cap. `server.go` and `main.go` collapse to ~2 lines each, which closes the message-format-drift concern (Claude's MEDIUM #4) trivially.
- Preserves the existing `Parse` contract for the parser package's own tests and any in-package use.

**Watch out for:**
- `SafeParse`'s null/empty short-circuit must return `(nil, nil)` for "no schema" so the boundary's existing `if parsedSchema != nil` semantics survive. Document this explicitly in godoc.
- Keeping `Parse` exported is the pragmatic middle ground — unexporting breaks the parser package's direct count-cap tests.

### Constraints for downstream

- **MUST** add exported `SafeParse(raw json.RawMessage) (*Schema, error)` in `internal/roundtable/dispatchschema/schema.go`. `SafeParse` is the single sanctioned entry for external/untrusted bytes; its godoc must say so verbatim.
- **MUST** measure the byte cap as `len(raw)` BEFORE `bytes.TrimSpace` inside `SafeParse`. Pre-trim measurement is non-negotiable — post-trim is fail-open against whitespace-flood DoS. This supersedes the pre-check brief's "post-trim" guidance.
- **MUST** have `SafeParse` return `(nil, nil)` for empty / `null` / whitespace-only input (after the cap fires on raw length).
- **MUST** keep `Parse` exported with a godoc warning: "expects trimmed, non-empty bytes from a trusted in-process source; untrusted input must go through SafeParse." Count caps (`MaxProperties`, `MaxEnumEntries`, `MaxRequiredEntries`) live inside `Parse` so they apply to both paths.
- **MUST** replace the trim + null-check + Parse blocks at `internal/stdiomcp/server.go:111-122` and `cmd/roundtable/main.go:201-209` with a single `SafeParse(input.Schema)` call. No magic-number byte-cap check at boundary sites — `SafeParse` owns it.
- **MUST** define `*ParseError{Kind, Message}` without an `Excerpt` field.
- **MUST** define `KindBoundExceeded` for all three count caps AND the byte cap (single Kind, not per-cap kinds). The `Message` field carries the specific cap name.
- **MUST NOT** add an unexported `parse` variant or hide the existing `Parse`. Unexporting breaks count-cap tests.
- **MUST NOT** duplicate the byte-cap constant or message string at boundary sites. After this change, `MaxSchemaBytes` is referenced only inside `SafeParse`.
- **MUST NOT** preserve `bytes.TrimSpace` + null-check at boundary sites — leaving it in is the "documentation-based parity" failure mode.

### Decisions

- **Option (b) selected**, in the form: exported `SafeParse` + retained exported `Parse`. Compiler-enforced via the symmetry that all external-input call sites use `SafeParse`; reviewer-enforced for the residual exported `Parse` surface via godoc.
- **Pre-trim byte cap is the binding constraint** (Gemini CRITICAL concern #1). This invalidates the pre-check brief's "post-trim" measurement.
- **`Parse` stays exported** for the package's internal count-cap tests and to keep the API surface honest about what's a parser vs. what's a safety wrapper.
- **Single `KindBoundExceeded`** covers byte cap + three count caps. The `Message` field carries the specific cap name for humans.
- **`ParseError` has no `Excerpt`** — diverges intentionally from `ValidationError`'s shape because schema bytes are attacker-controlled (audit item 3).

## backend-designer

**Module:** `internal/roundtable/dispatchschema`
**Layer:** foundation
**Depends on:** `encoding/json`, `bytes`, `fmt` (stdlib only — preserves `imports_test.go` leaf-package guard)
**Called by:** `internal/stdiomcp/server.go` (registerTool), `cmd/roundtable/main.go` (buildStdioDispatch)

### 1. `ParseError` struct shape

```go
// ParseError describes a *Parse* failure. Distinct from ValidationError —
// ValidationError reports per-panelist response failures; ParseError reports
// schema-document failures. Schema bytes are attacker-controlled at the MCP
// boundary, so ParseError deliberately omits an Excerpt field (info-leak
// audit item; see safety-auditor scope).
type ParseError struct {
    Kind    string `json:"kind"`
    Message string `json:"message"`
}

func (e *ParseError) Error() string { return e.Message }
```

- **No `Unwrap()`.** Callers discriminate via `errors.As(err, &pErr)` then branch on `pErr.Kind`. Inner `fmt.Errorf` `%w`-wrapped json errors are flattened into `Message` via `%v`.
- **No `Field` field.** Field names appear inline in `Message` (preserves verbatim error strings).

### 2. `Kind` constants

```go
const (
    KindBoundExceeded = "bound_exceeded"  // any of the four caps
    KindMalformed     = "malformed"        // umbrella for everything else
)
```

### 3. Error message templates

**KindBoundExceeded:**
- Byte cap: `"schema exceeds maximum size of 65536 bytes"` (literal — not `%d`-formatted)
- Properties: `fmt.Sprintf("dispatchschema: %q has too many entries: %d (max %d)", "properties", n, MaxProperties)`
- Enum: `fmt.Sprintf("dispatchschema: field %q enum has too many entries: %d (max %d)", name, n, MaxEnumEntries)`
- Required: `fmt.Sprintf("dispatchschema: %q has too many entries: %d (max %d)", "required", n, MaxRequiredEntries)`

**KindMalformed messages — preserved VERBATIM from existing `fmt.Errorf` calls** in Parse/parseProperties/parseField/parseRequired/classifyTopLevelDecodeErr. `%w`-wrapped inner errors collapse to `%v` inside `Message`.

### 4. Boundary error wrapping

**`internal/stdiomcp/server.go`** lines 111-122 → single `SafeParse(input.Schema)` call; on err, surface existing wrapped prefix `"roundtable dispatch error: invalid schema parameter: %v"`.

**`cmd/roundtable/main.go`** lines 201-209 → single `SafeParse(input.Schema)` call; `fmt.Errorf("invalid schema parameter: %w", err)`. `%w` preserves `errors.As` chain.

### 5. `SafeParse` signature + flow

```go
// SafeParse is the sanctioned entry point for schemas sourced from
// untrusted bytes (MCP input, network, file, anything not a literal in
// our own source). It enforces MaxSchemaBytes on the RAW (pre-trim)
// length to bound allocation against whitespace-flood DoS, then trims
// surrounding whitespace and short-circuits absent/null/empty input
// to (nil, nil) — the canonical "no schema" sentinel.
//
// The byte cap is measured BEFORE bytes.TrimSpace.
//
// Return semantics:
//   - (nil, nil)                       — no schema (empty / "null" / whitespace-only after trim)
//   - (nil, *ParseError{KindBoundExceeded, ...})
//   - (nil, *ParseError{KindMalformed, ...})  — any Parse rejection
//   - (*Schema, nil)                   — success
func SafeParse(raw json.RawMessage) (*Schema, error)
```

Flow:
1. `if len(raw) > MaxSchemaBytes { return nil, &ParseError{KindBoundExceeded, "schema exceeds maximum size of 65536 bytes"} }` — pre-trim
2. `trimmed := bytes.TrimSpace(raw)`
3. `if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) { return nil, nil }`
4. `return Parse(trimmed)`

### 6. Cap constants (top of `schema.go`)

```go
const (
    MaxProperties      = 256
    MaxEnumEntries     = 256
    MaxRequiredEntries = 256
    MaxSchemaBytes     = 65536
)
```

### 7. Cap insertion sites inside `Parse`

- **Properties:** in `parseProperties` loop (~line 170-190); track `len(fields)` after each append; fail-fast at N+1.
- **Enum:** in `parseField` after `json.Unmarshal(rawEnum, &values)` (~line 255).
- **Required:** in `parseRequired` after `json.Unmarshal(raw, &names)` (~line 268), BEFORE cross-ref loop.

### Decisions

- **Single `KindBoundExceeded` for all four caps** — arch-advisor mandate; `Message` carries cap identity.
- **No `Unwrap()` on `ParseError`** — `Kind`-based discrimination is the contract.
- **No `Excerpt` field** — info-leak audit item 3.
- **Byte-cap message is literal** `"schema exceeds maximum size of 65536 bytes"` — eliminates dual-site message-format-drift by construction.
- **`KindMalformed` umbrella** — every existing rejection message names the offending construct; per-rejection Kinds would be cosmetic.

### Constraints for downstream

- **MUST** assert `ParseError` discrimination via `errors.As(err, &pErr); pErr.Kind == dispatchschema.KindBoundExceeded`. NEVER `strings.Contains(err.Error(), "exceeds")` for the discrimination assertion.
- **MUST** preserve every existing `fmt.Errorf` message string byte-for-byte when wrapping into `ParseError{KindMalformed}`.
- **MUST** use exact byte-cap message `"schema exceeds maximum size of 65536 bytes"` (literal) — not `fmt.Sprintf`.
- **MUST** include a test that 100 MB+ of pure whitespace returns `KindBoundExceeded` from `SafeParse` (Gemini pre-trim DoS witness).
- **MUST NOT** preserve `bytes.TrimSpace` + null-check at boundary sites after the change — `SafeParse` owns them.

## roundtable-design-review

**Attempt 1.** Critique panel: 5 CONCERNS, 1 REJECT (kimi). 5 actionable findings, 4-5/6 panel agreement each:

1. **Add `Unwrap()` to `ParseError`** — preserve `errors.As(err, &json.SyntaxError{})` through the typed wrapper.
2. **server.go boundary `%v` erases Kind** — switch to `%w` for symmetry with main.go.
3. **Tests must assert exact `n=257` count** — blocks drift to lazy-bulk-check post-loop.
4. **Whitespace fixture should be 65537 bytes, not 100 MB** — same coverage, no CI memory pressure.
5. **Byte-cap message should use `fmt.Sprintf` for SSOT** — literal will drift if cap changes.

Advisory items NOT incorporated:
- Kimi's "post-Unmarshal enum cap is DoS" — bounded by byte cap (worst-case 65536 bytes → ~21k strings → ~336KB). Defer streaming decoder to future feature.
- Kimi's "split into KindSizeExceeded / KindCountExceeded" — over-engineering; arch-advisor mandated single Kind.

**Verdict (attempt 1): CONCERNS** — sent back to designer.

**Attempt 2** (designer revision below as `## backend-designer (attempt 2)`): all 5 findings accepted 1:1; no pushback. Per skill Step 2.5.7, re-running critique on a mechanical 1:1 application of attempt-1 findings would be theatrical. Marking attempt 2 verdict directly.

**roundtable_design_verdict: APPROVED (attempt 2)** — designer's revised brief addresses every critique finding with concrete code-level changes (`Cause` field + `Unwrap()`, `%w` everywhere, `fmt.Sprintf` for byte-cap, MUST assertion on `"257 (max 256)"` substring, MUST whitespace fixture = MaxSchemaBytes+1).

## backend-designer (attempt 2)

Five attempt-1 findings accepted 1:1. Advisory items deferred per arch-advisor's single-Kind ruling and byte-cap bounding.

### 1. `ParseError` struct — UPDATED with `Cause` + `Unwrap()`

```go
type ParseError struct {
    Kind    string `json:"kind"`
    Message string `json:"message"`
    Cause   error  `json:"-"` // optional inner error (e.g. *json.SyntaxError); not serialized
}

func (e *ParseError) Error() string { return e.Message }
func (e *ParseError) Unwrap() error { return e.Cause }
```

Every `KindMalformed` site that today does `fmt.Errorf("...: %w", inner)` MUST populate `Cause: inner`. `KindBoundExceeded` sites set `Cause: nil`.

### 2. Boundary error wrapping — `%w` everywhere

Both sites use `fmt.Errorf` with `%w` for symmetry and to preserve the `errors.As` chain across the envelope.

- `internal/stdiomcp/server.go`: `fmt.Errorf("roundtable dispatch error: invalid schema parameter: %w", err)`
- `cmd/roundtable/main.go`: `fmt.Errorf("invalid schema parameter: %w", err)` (unchanged)

### 3. Error message templates — fmt.Sprintf for SSOT

**KindBoundExceeded:**
- Byte: `fmt.Sprintf("schema exceeds maximum size of %d bytes", MaxSchemaBytes)`
- Properties: `fmt.Sprintf("dispatchschema: %q has too many entries: %d (max %d)", "properties", n, MaxProperties)`
- Enum: `fmt.Sprintf("dispatchschema: field %q enum has too many entries: %d (max %d)", name, n, MaxEnumEntries)`
- Required: `fmt.Sprintf("dispatchschema: %q has too many entries: %d (max %d)", "required", n, MaxRequiredEntries)`

### Decisions (revised)

- `Cause error` + `Unwrap()` added — preserves `errors.As(err, &json.SyntaxError{})`.
- Boundary sites use `%w` uniformly.
- All four `KindBoundExceeded` messages use `fmt.Sprintf` against the cap constant.
- Single `KindBoundExceeded` retained (arch-advisor mandate).
- No `Excerpt` field retained (info-leak audit item 3).

### Constraints for downstream (revised)

- **MUST** discriminate via `errors.As(err, &pErr); pErr.Kind == dispatchschema.KindBoundExceeded`. Boundary tests MUST recover `*ParseError` THROUGH the `%w` envelope.
- **MUST** populate `ParseError.Cause` with the inner `%w`-wrapped error at every `KindMalformed` site that wraps json/decoder errors. Tests MUST assert `errors.As(pErr.Cause, &jsonErr)` succeeds for at least one malformed-JSON test case.
- **MUST** assert error message contains the literal `"257 (max 256)"` substring for the properties/enum/required count caps (proves fail-fast at N+1, not lazy bulk-check).
- **MUST** use the `MaxSchemaBytes+1` (65537) whitespace fixture in `server_schema_test.go` and `cmd/roundtable/main_schema_test.go`. NOT 100 MB.
- **MUST** preserve every existing parser error message string byte-for-byte when wrapping into `ParseError{KindMalformed, Cause: inner}`.

### Audit Report (attempt 1)

**Verdict:** PASS

**PRD:** prds/active/F12-dispatchschema-bounded-allocation.json
**Feature ID:** F12

**Files scanned:**
- internal/roundtable/dispatchschema/schema.go
- internal/stdiomcp/server.go
- internal/stdiomcp/types.go
- cmd/roundtable/main.go
- internal/roundtable/dispatchschema/schema_test.go
- internal/stdiomcp/server_schema_test.go
- cmd/roundtable/main_schema_test.go

**Audit results (7 items):**

1. **Every `dispatchschema.Parse` callsite has a byte cap upstream — PASS.** Grep across `internal/`, `cmd/`, and tests: only two production callers exist (server.go:108, main.go:200), both call `SafeParse`. The legacy `Parse` is invoked from `SafeParse` itself (post-cap, post-trim, line 157) and from in-package tests where bytes are trusted Go-source literals. No external-input path bypasses `SafeParse`.

2. **Byte cap fires BEFORE JSON decode AND BEFORE `bytes.TrimSpace` — PASS.** schema.go:147 is the literal first statement of `SafeParse`. `bytes.TrimSpace` is line 153 (after cap), `Parse` invocation is line 157 (after cap). Gemini's pre-trim DoS witness (65537 whitespace bytes) is exercised by `TestSafeParse_WhitespaceFloodDoS` (schema_test.go:652) and verified to never reach trim or Parse. The order is correct.

3. **Error messages don't echo schema content — PASS (with one LOW note).** Walked all 26 `fmt.Sprintf` callsites in schema.go. None embed raw schema bytes. Cap-violation messages name the cap (`MaxSchemaBytes`), not the input. JSON-decode errors use `%v` on the inner error and `Cause` chaining; `*json.SyntaxError` and `*json.UnmarshalTypeError` carry byte offsets / type-tags, not raw payload content — verified by reading encoding/json's struct definitions. `ParseError` struct deliberately omits an `Excerpt` field (line 39 comment confirms intent). LOW: property-name `name` is included in field-level error messages (e.g., schema.go:365 `"field %q has unsupported type %q"`); these are JSON object keys, already bounded in count by `MaxProperties=256` and in aggregate by `MaxSchemaBytes=65536`. Not a F12-scope finding — F12 threat model is unbounded allocation, not info-leak via bounded property names.

4. **Cap inclusivity consistent — PASS.** Implementation: `SafeParse` schema.go:147 uses `len(raw) > MaxSchemaBytes` (strict-greater → at-cap passes). Count caps use the same form (parseProperties:309, parseField:421, parseRequired:446). Tests: `TestSafeParse_BytecapAtBoundary` asserts MaxSchemaBytes (65536) succeeds; `TestSafeParse_BytecapOverCapFails` asserts MaxSchemaBytes+1 (65537) fails with `KindBoundExceeded`. Implementation and tests agree. Property-cap N=256/257 boundary covered by parseProperties tests (verified via the F12 oracle).

5. **Dual-site parity — PASS.** server.go:113 wraps with `fmt.Errorf("roundtable dispatch error: invalid schema parameter: %w", err)`; main.go:202 wraps with `fmt.Errorf("invalid schema parameter: %w", err)`. Both share the substring `"invalid schema parameter:"` verbatim; the server.go layer prepends the MCP IsError envelope `"roundtable dispatch error: "` because the stdio handler expects that prefix on tool errors. Both use `%w` so `errors.As(err, &*ParseError)` succeeds — verified by `TestBuildStdioDispatch_OverByteCap_ReturnsTypedParseError` (main) and `TestSchemaParam_OverByteCap_ErrorChainRecoverable` (server). No drift on the typed-error chain.

6. **F04 wiring cannot replace bytes after boundary validation — PASS.** Flow: client → MCP transport → `ToolInput.Schema json.RawMessage` (types.go:27) → server.go:108 `SafeParse(input.Schema)` validates → `dispatch(ctx, spec, input)` at server.go:130 passes the SAME `input` value → main.go:200 `SafeParse(input.Schema)` re-validates the same `json.RawMessage` slice. `ToolInput` is passed by value but `json.RawMessage` is a slice header — both `SafeParse` calls observe the same underlying bytes. There is no codepath between the two calls that re-reads schema from a different source or mutates `input.Schema`. The double `SafeParse` is intentional, idempotent, and the second call cannot fail differently than the first (deterministic on identical bytes).

7. **Cap constants single-source-of-truth — PASS.** All four caps declared exactly once in schema.go:25-32 (`MaxProperties`, `MaxEnumEntries`, `MaxRequiredEntries`, `MaxSchemaBytes`). All callsites in tests reference `dispatchschema.MaxSchemaBytes` (verified across main_schema_test.go, server_schema_test.go, schema_test.go). No duplicated literals (`65536`, `256`) at boundary sites — boundary callers don't reference the constants at all; they delegate to `SafeParse`, which is the proper encapsulation.

**Violations:** None.

**Next hop:** landing-verifier

## arch-advisor-verification

**Verdict:** SOUND

**Bottom line:** Implementation honors every CONSULT mandate. The `Cause`/`Unwrap()` addition from roundtable critique attempt 1 is a strict superset of my original `{Kind, Message}` shape — additive, zero-valued on `KindBoundExceeded`, and strengthens `errors.As` composability without compromising the discrimination contract. The `SafeParse` chokepoint pattern is clean enough that a future F13-style untrusted-bytes entry would extend it naturally.

**Findings:**

1. **CONSULT mandate compliance — full.** SafeParse exported (schema.go:146); pre-trim byte cap on `len(raw)` before bytes.TrimSpace (line 147 vs 153); `(nil, nil)` short-circuit (line 154); Parse retained with godoc warning (lines 164-166); both boundary sites collapsed to one-line SafeParse calls; bytes import removed from both. Single `KindBoundExceeded` covering all four caps. No `Excerpt` field. No magic numbers at boundary sites.

2. **`Cause`/`Unwrap()` deviation is structurally sound.** The addition is additive: `KindBoundExceeded` paths set `Cause: nil`; only `KindMalformed` sites that previously did `%w`-wrap populate it. Preserves `errors.As(err, &jsonErr)` through the typed wrapper — the future-trap roundtable attempt 1 flagged. **Do not revert.**

3. **F13 extensibility.** A future untrusted-bytes parser entry would call `dispatchschema.SafeParse` by reflex; the godoc on Parse makes the policy explicit; reviewers see `dispatchschema.Parse(externalBytes)` and immediately flag it. Pattern scales.

4. **Boundary `%w` symmetry.** main.go:202 returns `fmt.Errorf("invalid schema parameter: %w", err)` directly; server.go:113 wraps with `%w` then calls `.Error()` because `*mcp.TextContent.Text` requires a string. Source-level idioms aligned; rendering divergence forced by MCP envelope, not drift.

5. **Audit cross-check.** Safety-auditor's 7-item PASS already verified the structural surface. No gaps.

**Optional future consideration:** `errAs` helper at schema.go:492-498 is a hand-rolled type assertion that avoids importing `errors`. With `Unwrap()` now in the package surface, adding `errors` import and replacing `errAs` with `errors.As` would be more idiomatic. Out of F12 scope; quick follow-up if anyone touches this file.

## landing-verifier

**Pipeline:** backend
**Verification:** `go build ./...` clean; `go vet ./...` clean; `go test ./... -count=1` — all 4 packages PASS; `go test -race ./internal/... -count=1` — all 3 internal packages PASS, no data races. All 19 new test functions confirmed individually PASS. No dangling TODO/FIXME in F12 production code.
**Spec conformance:** CONFIRMED  
**Safety audit:** PASS  
**Code review:** APPROVED  
**Architecture review:** SOUND  
**Doc drift:** MINOR — ARCHITECTURE.md line 119 describes the boundary as calling `dispatchschema.Parse` directly with manual trim/null-check; the implementation now delegates to `dispatchschema.SafeParse`. Will be swept by doc-gardener in Step 9.

**Verdict:** VERIFIED

## roundtable-landing-review

**Attempt 1.** Crosscheck panel: claude, codex, deepseek, kimi, minimax, gemini. 3 APPROVED, 3 CONCERNS.

**APPROVED panelists:** claude (DoS closed; bypass paths none in tree; no Cause-chain leak; minor errAs nit non-blocking), deepseek (gap closed, no bypass, no leak, nothing blocks), kimi (DoS closed, audit Cause for raw-input leak as advisory).

**CONCERNS / unresolved findings (logged as advisory per Step 8.5.8 — not blocking):**

1. **HIGH (codex): MCP SDK applies argument validation BEFORE handler runs.** `mcp.AddTool`-registered tools have the SDK unmarshal `req.Params.Arguments` into `map[string]any` and validate/remarshal before `ToolInput.Schema` reaches `SafeParse`. A large `schema` field could force allocation/CPU pre-F12-cap. **Outside F12 scope** — addressing this means switching to low-level MCP handlers for all schema-bearing tools, which is a separate feature. The MCP transport layer typically has its own request body cap; F12's 64 KB schema-field cap is still meaningful within that envelope. Logged as tech-debt.

2. **MEDIUM (gemini/kimi/minimax): `Parse` remains exported as a footgun.** Future callers could bypass `SafeParse`. Mitigated by godoc warning per arch-advisor + designer; unexporting (rename to `parse`) would break the parser package's own count-cap tests. Pattern is consistent with arch-advisor's CONSULT mandate. Logged as tech-debt for opportunistic tightening.

3. **LOW (minimax/kimi): Verify `Parse` error messages don't leak sensitive context post-rewrite.** Implementation preserves existing message strings byte-for-byte (no new info-leak surface). Cause-chain carries `*json.SyntaxError`/`*json.UnmarshalTypeError` (offsets, types) — never raw schema content. Already covered by safety-auditor's audit item 3 PASS.

4. **LOW (claude/multiple): `errAs` helper at schema.go:492 should be replaced with `errors.As`.** Minor idiom cleanup — already noted by arch-advisor as opportunistic follow-up.

**roundtable_landing_verdict: CONCERNS** — All concerns logged as tech-debt (Post-MVP entries to be added in Step 9 sub-step 2). None block landing per Step 8.5.8.
