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

- [ ] **F04 MINOR DRY: hardcoded dispatch-error prefix in `server.go`.** `internal/stdiomcp/server.go:118` hardcodes `"roundtable dispatch error: invalid schema parameter:"` — assembled by two separate `fmt.Errorf` wrappers. If either changes, the prefix strings diverge. Fix: extract a package-level const in `server.go`, OR consolidate via the existing dispatch-error envelope so the inner wrapper supplies only the leaf message. Code-reviewer MINOR, non-blocking.

- [ ] **F04 MINOR DoS: no byte cap on `input.Schema`, no enum-length cap.** `internal/stdiomcp/server.go` accepts unbounded `json.RawMessage` for `input.Schema`; `internal/roundtable/dispatchschema/schema.go` `Parse` has no byte or enum-entry length cap. Pre-existing parity with the uncapped `prompt` field (same trust boundary). Fix: add 64 KB soft cap on `input.Schema` before `Parse`, and a max-enum-entries cap (e.g., 256) inside `Parse`. Safety-auditor MINOR finding 6, non-blocking.

- [ ] **F04 MINOR doc: `dispatchschema` accessor doc comments missing immutability note.** `internal/roundtable/dispatchschema/schema.go` `Fields()`, `Required()`, and `Field.Enum()` return backing slices without a `// returns backing slice, do not mutate` comment. C2 immutability is enforced by convention, not type system. Fix: add one-line doc comments to each accessor. Code-reviewer C2, non-blocking.

- [ ] **F04 LOW: brittle byte-equivalence regression test.** `internal/roundtable/run_schema_test.go` and `internal/stdiomcp/server_schema_test.go` use `bytes.Contains(out, []byte("structured"))==false` as the regression guard for assertion 3. False-positives if a future field name contains the substring. Fix: tighten to JSON-key form `"structured":` (with the colon) OR unmarshal-and-assert key absence. Roundtable landing review attempt 1 finding (multi-panel agreement).

- [ ] **F04 LOW: distinguish `KindEmptyResponse` from `KindMissingFence`.** Today, a panelist returning `Status=ok` with empty `Response` causes `Validate` to emit `KindMissingFence` — semantically correct (no fence emitted) but conflates "backend returned empty body" with "model ignored fence instructions." Fix: add `KindEmptyResponse` to `dispatchschema.ValidationError.Kind` and gate it on `len(strings.TrimSpace(response))==0` before the missing-fence path. Roundtable landing review attempt 1 finding (claude, deepseek, minimax).

- [ ] **F04 LOW: Validate latency in synchronous critical path.** `Validate` runs synchronously in the post-`runCh`-drain loop in `internal/roundtable/run.go:417-429`. For panelists returning large responses (1MB+) with many fields (100+), validation could add tens of ms to dispatch latency. Not user-visible vs. backend latency today. Fix (optional): parallelize the post-drain Validate loop OR add a deadline. Defer until regex-pattern support is added. Roundtable landing review attempt 1 finding (claude, codex, deepseek, kimi).

- [ ] **F04 LOW: production `buildStdioDispatch` parse path not directly exercised in CI.** Tests inject `fakeDispatch` via `connectServer`, bypassing `buildStdioDispatch`. The production parse safety net at `cmd/roundtable/main.go:201` is logically idempotent with the `registerTool` parse but is not directly covered. Fix: add a unit test in `cmd/roundtable/` that calls `buildStdioDispatch` with malformed schema and asserts the wrapped-error structure. Roundtable landing review attempt 1 finding (codex, deepseek, kimi).

- [ ] **keel-refine framework gap: `next_free_id` ignores PRDs that have features but no backlog entries.** The drafter's Phase 2 `next_free_id` resolver scans only `feature-backlog.md` and the target PRD's `features[]`. It does NOT scan the `features[]` of every other PRD under `docs/exec-plans/prds/*.json`, so an orphan PRD (features defined on disk, no backlog rows) is invisible to ID allocation — the drafter will silently re-allocate one of the orphan's IDs for an unrelated feature. Durable fix: extend Phase 2 to enumerate all `docs/exec-plans/prds/*.json` files and union their `features[].id` into the reserved set, OR add `validate-prds.py` orphan-PRD detection at the start of every `/keel-refine` Phase 0 (preflight halt with CTA when an orphan exists). The latter is simpler and surfaces the gap to the human before drafting. Related: `.claude/agents/backlog-drafter.md` source-tag spec enumerates only drafter-emitted formats (PRD path, `sha256[:16]` for prose, `sha256[:16]` for interview); manual-reconciliation entries (`<!-- SOURCE: orphan-prd-reconciliation -->`) match none of them. Either widen the spec to cover non-drafter origins, or add a note that the spec is drafter-scoped and reconciliation entries may carry arbitrary labels.
