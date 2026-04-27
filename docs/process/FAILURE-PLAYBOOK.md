# Pipeline Failure Playbook

What to do when the pipeline doesn't produce a clean landing. This is a
decision tree, not a troubleshooting guide — follow the first matching case.

---

## Decision Tree

```
Pipeline stalls or produces bad output
  │
  ├─ spec-reviewer finds CRITICAL deviation?
  │    ├─ Spec is correct, code is wrong
  │    │    → Send back to implementer with reviewer's findings
  │    │    → Max 2 attempts. If still failing → Escalate (see below)
  │    │
  │    └─ Spec is wrong or incomplete
  │         → STOP pipeline. Fix the spec first.
  │         → Restart from pre-check after spec update.
  │
  ├─ Implementer can't make tests pass?
  │    ├─ Tests are correct (match spec)
  │    │    → Loop to implementer once with specific guidance
  │    │    → If still failing → the feature is too large. Decompose it.
  │    │
  │    └─ Tests are wrong (don't match spec, impossible assertions, bad mocks)
  │         → Send back to test-writer with implementer's findings
  │         → Test-writer rewrites, implementer retries
  │
  ├─ safety-auditor finds violations?
  │    → NEVER skip. Fix the violation.
  │    → Send back to implementer with auditor's findings
  │    → Re-run safety-auditor after fix. No shortcuts.
  │    → If 3+ loops: escalate to human. The invariant may need review.
  │
  ├─ Arch-advisor verification returns UNSOUND?
  │    → Send architecture findings to implementer
  │    → Implementer fixes, re-run spec-reviewer + safety-auditor + Arch-advisor
  │    → Max 1 Arch-advisor retry. If still UNSOUND → Escalate to human.
  │    → This is an architecture-level problem, not a code-level one.
  │
  ├─ Roundtable pre-check review raises concerns? (Step 1.3)
  │    → Send consensus findings back to pre-check for revision
  │    → Max 2 roundtable pre-check attempts
  │    → If still CONCERNS after 2 attempts: proceed with pre-check's
  │      latest classification (advisory, not blocking)
  │    → Roundtable is advisory — pre-check remains the authoritative router
  │
  ├─ Roundtable design review raises concerns? (Step 2.5)
  │    → Send findings back to designer for revision
  │    → Max 2 roundtable design attempts
  │    → If still CONCERNS after 2 attempts: proceed anyway (advisory)
  │    → Roundtable is advisory — it does not block the pipeline
  │
  ├─ Roundtable landing review raises concerns? (Step 8.5)
  │    → Send findings back to implementer
  │    → Implementer fixes, re-run full gate chain:
  │      code-reviewer → spec-reviewer → safety-auditor? → arch-advisor? → landing-verifier
  │    → Max 1 roundtable-triggered gate re-run per gate
  │    → Re-run roundtable landing review (attempt 2)
  │    → If still CONCERNS after 2 attempts: proceed anyway (advisory)
  │    → If a roundtable-triggered gate re-run itself fails: escalate to human
  │
  ├─ Push rejected at Step 9?
  │    → STOP, print the raw git error
  │    → Human resolves (e.g., auth, branch protection) and reruns push
  │    → Commit is already local — no work lost
  │
  ├─ gh pr create fails at Step 9?
  │    → Print manual PR instructions — branch is pushed
  │    → Human opens the PR on the forge UI
  │    → Do not fail the pipeline
  │
  ├─ pre-check routed wrong? (skipped designer when one was needed)
  │    → Insert the missing stage now
  │    → Designer reads the handoff, produces design brief
  │    → Resume pipeline from test-writer
  │
  ├─ landing-verifier reports BLOCKED?
  │    → Read the BLOCKED reason — it tells you which upstream stage failed
  │    → Fix that stage, re-run landing-verifier
  │
  ├─ /keel-refine preflight fails (bootstrap gate not satisfied)?
  │    → Skill prints an A/B/C remediation message; follow it.
  │    → [A] Greenfield: tick F01–F03 as [x] in
  │        docs/exec-plans/active/feature-backlog.md, then re-run.
  │    → [B] Brownfield (primary path for an already-adopted repo):
  │        paste `<!-- KEEL-BOOTSTRAP: not-applicable -->` on its own line
  │        between the **Architecture:** preamble and the first `---`
  │        divider in feature-backlog.md. Re-run /keel-refine.
  │    → [C] Brownfield, first-time adoption: run /keel-adopt.
  │        Phase 5d stamps the marker automatically.
  │    → WARNING: if /keel-adopt has already run once, do NOT re-run it
  │      (it will overwrite CLAUDE.md and ARCHITECTURE.md). Use [B].
  │    → Full context: docs/process/BROWNFIELD.md §6.
  │
  └─ Agent produces garbled or off-topic output?
       → Re-run the same agent (model hiccup, not a process failure)
       → If still garbled → the handoff context may be too large
       → Summarize the handoff, keeping only the current agent's inputs
```

## PRD-scope halts

### pre-check blocks on missing PRD link

**Symptom:** `/keel-pipeline F##` halts with *"F## references PRD 'X' but `docs/exec-plans/prds/X.json` does not exist."*

**Cause:** Typo in `PRD:` field, or PRD file was renamed/deleted.

**Fix:**
- If the PRD should exist: create the file at the referenced path (narrative only, no feature list).
- If the slug was a typo: correct the F## entry's `PRD:` field in the backlog.
- If the F## is legacy work: change `PRD: <slug>` to `PRD-exempt: legacy`.

### pre-check blocks on invalid PRD-exempt reason

**Symptom:** `/keel-pipeline F##` halts with *"F## declares PRD-exempt with reason '<x>'; must be one of legacy/bootstrap/infra/trivial."*

**Cause:** Free-form reason used instead of one of the four allowed values.

**Fix:** Edit the F## entry to use one of the four allowed reasons. If none fit, the feature likely should have a PRD — author one.

### validate-prds.py reports orphaned PRD file

**Symptom:** CI validator reports *"PRD file `docs/exec-plans/prds/<slug>.json` is not referenced by any F##."*

**Cause:** A PRD was drafted but all its F## were dropped or never added.

**Fix:** Either delete the orphaned PRD file (git log preserves history) or add F## entries that reference it.

### validate-prds.py reports F## ID mentioned in PRD prose

**Symptom:** Validator reports *"PRD prose `<slug>.md` contains F## reference — narrative must use theme-level language, not IDs."*

**Cause:** Someone pasted an F## list into the PRD narrative (common drift toward Jira-docification).

**Fix:** Rewrite the prose to describe themes/scope, not IDs. The feature list lives on `docs/exec-plans/active/feature-backlog.md` (F## entries tagged `PRD: <slug>`) — don't cache it in the PRD file. For a JSON PRD, `uv run scripts/keel-prd-view.py docs/exec-plans/prds/<slug>.json` renders the canonical view.

## Rules

1. **Never "try harder."** If the implementer fails twice on the same tests,
   the problem is upstream — wrong spec, wrong tests, or feature too large.

2. **Max 2 implementation loops.** Implementer gets the initial attempt plus
   one retry with specific guidance. After that, decompose the feature or
   fix upstream.

3. **Spec changes restart the pipeline.** If you modify the spec mid-pipeline,
   go back to pre-check. Don't patch downstream — the whole chain depends on
   spec accuracy.

4. **Safety violations are never negotiable.** The safety-auditor is a hard
   gate. You fix the code, not the rule. If the rule itself is wrong, that's
   a core-beliefs discussion — update the invariant deliberately, not as a
   pipeline workaround.

5. **Test-writer can be sent backwards.** If the implementer identifies that
   tests are wrong (impossible contract, incorrect mock setup), the handoff
   file gets a `BLOCKED: test-issue` entry and routes back to test-writer.
   This is the one sanctioned backward path in the pipeline.

6. **Decompose before you thrash.** If a feature touches 3+ layers and
   the implementer can't satisfy tests after 2 attempts, the feature may
   be too large. Split it into smaller independently testable units.

## Escalation

When to involve the human orchestrator directly:

- Spec is ambiguous and two valid interpretations exist
- Feature requires a design decision not in any existing doc
- Agent consistently produces off-topic output (model capability gap)
- Domain invariant needs updating (core-beliefs change)
- Feature is blocked by external dependency (API not ready, library missing)

The orchestrator's job at escalation is to **make a decision and encode it
in the repo** — update a spec, add a design doc, modify core-beliefs — then
restart the pipeline from the appropriate stage.
