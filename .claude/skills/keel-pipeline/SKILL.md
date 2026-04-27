---
name: keel-pipeline
description: "Orchestrate the KEEL pipeline for a feature. Dispatches agents in sequence: pre-check → test-writer → implementer → code-reviewer → spec-reviewer → safety-auditor? → landing-verifier."
---

# KEEL Pipeline

KEEL — Knowledge-Encoded Engineering Lifecycle.

Orchestrate the full agent pipeline for a feature. You are the **orchestrator** — you dispatch agents, thread handoff files, and enforce the pipeline order from CLAUDE.md.

## Framework principles

Every halt in this pipeline uses P7 (call-to-action) wording. See
[`docs/process/KEEL-PRINCIPLES.md`](../../../docs/process/KEEL-PRINCIPLES.md).
Conflict resolution follows P6 (authority hierarchy): code > spec > backlog > PRD.

## Arguments

The user provides a feature ID and PRD path:
```
/keel-pipeline F04 docs/exec-plans/prds/my-feature.json
```

The PRD path MUST be a structured JSON PRD. If the path ends in `.md`
or any non-JSON extension, HALT with:
> *"PRD path must be a structured JSON file at `docs/exec-plans/prds/<slug>.json`. If you have a markdown spec or prose input, run `/keel-refine` first — it converts non-JSON inputs into structured JSON PRDs. See `NORTH-STAR.md` §'Feature input canon — single path, JSON PRDs only'."*

If no PRD exists yet, tell the user to run `/keel-refine` first — it
is the conversion hub that produces the structured JSON PRD this
pipeline expects.

## Before Starting

1. Read `CLAUDE.md` to determine the correct pipeline variant
2. Read the feature's section of the structured JSON PRD via `jq` (see `.claude/agents/pre-check.md` for the read conventions; the pipeline does not re-implement them)
3. Create the handoff file at `docs/exec-plans/active/handoffs/F{id}-{feature-name}.md`
   by copying from `_TEMPLATE.md`. Then immediately seed the YAML frontmatter:
   - `status: IN-PROGRESS`
   - `pipeline:` — set to `bootstrap`, `backend`, `frontend`, or `cross-cutting`
   - `prd_ref:` — set to the structured JSON PRD path and F## (e.g., `docs/exec-plans/prds/my-feature.json#F04`)
   
   These fields MUST be set before dispatching any agent. Downstream agents
   read them for context and routing.

4. **Clean-tree check.**
   Run `git status --porcelain`. If the output is non-empty after excluding
   the handoff file just created in step 3, STOP. Print:

     Pipeline requires a clean working tree. Commit, stash, or drop the
     following uncommitted changes before re-running:
     <paste the porcelain output>

   Do not proceed. Rationale: Step 9 uses `git add -A` to stage the feature,
   so any unrelated changes in the tree at pipeline start would be silently
   swept into the feature's commit. Phase 1 refuses that ambiguity.

5. **Branch safety check.**
   Run `git rev-parse --abbrev-ref HEAD`. If HEAD is `main` or `master`,
   auto-create the feature branch BEFORE any agent runs:

     git checkout -b keel/F{id}-{slug}

   where `{slug}` is derived from the handoff filename
   (`docs/exec-plans/active/handoffs/F{id}-{slug}.md` → `keel/F{id}-{slug}`).
   The pipeline never commits to main/master, and branches BEFORE writing
   code so that a mid-pipeline halt leaves the feature branch — not main —
   in the partial state.

6. **Remote resolution.**
   Resolve which remote Step 9 will push to. Run `git remote` to list
   configured remotes, then pick one:

   - **0 remotes** → STOP: "Pipeline lands by opening a PR on a forge,
     but this repo has no remotes configured. Add one with
     `git remote add <name> <url>` and re-run, or edit Step 9 of
     `.claude/skills/keel-pipeline/SKILL.md` if you want different
     landing behavior."
   - **1 remote** → use it.
   - **2+ remotes** → check the current branch's upstream
     (`git rev-parse --abbrev-ref @{upstream}` — if the branch doesn't
     yet have an upstream this returns an error, that's fine). If an
     upstream is set, use its remote. Otherwise use `origin` if present.
     Otherwise STOP and list the available remotes so the human picks.

   Store the resolved name in the handoff YAML as `remote_name`. Step 9
   reads this field and never hardcodes a remote.

### Step 0.5: Roundtable availability

After the clean-tree/branch/remote checks, before any agent dispatch:

Check roundtable availability: read `Roundtable review` from CLAUDE.md.
If `true` (or absent — default is true), probe roundtable MCP server.
Store `roundtable_enabled: true|false` in handoff YAML.

## Pipeline Variants

Determine the variant based on what the feature touches:

**Bootstrap** — Docker, scaffolding, config (typically F01-F03):
```
docker-builder → landing-verifier          (F01: container)
scaffolder → landing-verifier              (F02: app skeleton)
config-writer → landing-verifier           (F03: test infra)
```
Bootstrap features are orchestrator-direct: dispatch the specific bootstrap agent, then landing-verifier. No pre-check, no test-writer, no implementer. The bootstrap agent's report serves as the handoff context.

**Backend** — changes to core business logic, services, data layer:
```
pre-check → roundtable-precheck? → researcher? → backend-designer? → roundtable-review? → test-writer → implementer → code-reviewer → spec-reviewer → safety-auditor? → landing-verifier → roundtable-review? → post-landing
```

**Frontend** — changes to UI components, templates, styles, client-side logic:
```
pre-check → roundtable-precheck? → researcher? → frontend-designer → roundtable-review? → test-writer → implementer → code-reviewer → spec-reviewer → landing-verifier → roundtable-review? → post-landing
```

**Cross-cutting** — test infrastructure, config, Docker, docs:
```
pre-check → roundtable-precheck? → test-writer → implementer → code-reviewer → landing-verifier → roundtable-review? → post-landing
```

**Full-stack** — touches both backend and frontend: run backend pipeline, then frontend pipeline, sharing the same handoff file.

## Execution Steps

### Step 0: Bootstrap (F01-F03 only)
If the feature is a bootstrap feature, dispatch the specific bootstrap agent (docker-builder, scaffolder, or config-writer). It produces its report in the handoff file. Skip directly to Step 8 (landing-verifier). Bootstrap features do not use pre-check, designers, test-writer, or implementer.

### Step 1: Pre-check (standard pipeline only)
Dispatch the `pre-check` agent with the feature spec path. It produces an
execution brief in the handoff file. After pre-check completes, update the
handoff YAML frontmatter with routing fields from the brief:
- `intent`, `complexity` — determines which optional agents run
- `designer_needed` — YES/NO (trivial complexity → always NO)
- `researcher_needed` — YES/NO (research intent → always YES)
- `safety_auditor_needed` — YES/NO
- `arch_advisor_needed` — YES if complexity is architecture-tier

Read routing decisions from the YAML frontmatter for all subsequent steps.

### Step 1.3: Roundtable pre-check review (if enabled)

Runs when `roundtable_enabled: true`. Stress-tests pre-check's routing
classification BEFORE downstream agents run — because the routing flags
(`designer_needed`, `researcher_needed`, `safety_auditor_needed`,
`arch_advisor_needed`, `complexity`) cascade through the whole pipeline.
A misclassification either wastes 5+ agent cycles or under-scrutinizes
safety-critical changes. One-way door.

1. Re-check roundtable MCP availability (120s timeout per tool call).
   If unavailable: set `roundtable_precheck_skipped: true` in handoff YAML
   with a one-line reason, print a visible warning to stderr:

     !! Roundtable MCP unavailable ({reason}) — skipping pre-check review.
     !! Configured roundtable_enabled=true in CLAUDE.md; proceeding without it.

   Continue to Step 1.5. Skip is surfaced in the commit verdict block
   (Step 9 sub-step 3).
2. Call `mcp__roundtable__roundtable-critique` with pre-check's execution brief
   + routing flags + spec excerpt. Ask it to attack the classification:
   wrong intent, wrong complexity tier, missing research signal,
   missing safety-auditor flag, missing arch-advisor flag, designer
   needed but flagged NO (or vice versa).
3. Call `mcp__roundtable__roundtable-canvass` with the critique output + original
   pre-check brief to synthesize a consensus routing. Pass both so the
   consensus can choose to keep, flip, or refine individual flags.
4. Append combined output to `## roundtable-precheck-review` in handoff.
5. Set `roundtable_precheck_attempt: 1` in YAML.
6. If the consensus disagrees with pre-check's flags: send the consensus
   findings back to `pre-check`. Pre-check revises the brief, updates the
   YAML routing fields. Increment `roundtable_precheck_attempt` to 2,
   re-run critique + canvass.
7. If still divergent after attempt 2: proceed with pre-check's latest
   classification anyway (advisory, not blocking). Set
   `roundtable_precheck_verdict: CONCERNS` and log the unresolved
   disagreement in the handoff.
8. If consensus agrees: set `roundtable_precheck_verdict: APPROVED`.

Roundtable is advisory. Pre-check remains the authoritative router — this
step only flags blind spots for pre-check to reconsider.

### Step 1.5: Researcher (if needed)
If pre-check set `Research needed: YES`, dispatch `researcher` with the specific questions from the execution brief. Append research brief to handoff file.

### Step 1.7: Arch-advisor consultation (if architecture-tier)
If pre-check set `Arch-advisor needed: YES` or `Complexity: architecture-tier`,
dispatch `arch-advisor` agent in CONSULT mode with the execution brief, spec,
and any research brief. Arch-advisor provides architecture-level guidance
before design/implementation.
Append output to `## arch-advisor-consultation` in the handoff file.

### Step 2: Designer (if needed)
Dispatch `backend-designer` or `frontend-designer` based on pipeline variant. Append output to handoff file.

### Step 2.5: Roundtable design review (if enabled)

Runs only when `designer_needed: YES` AND `roundtable_enabled: true`.

1. Re-check roundtable MCP availability (120s timeout per tool call).
   If unavailable: set `roundtable_skipped: true` in handoff YAML with a
   one-line reason, print a visible warning to stderr:

     !! Roundtable MCP unavailable ({reason}) — skipping design review.
     !! Configured roundtable_enabled=true in CLAUDE.md; proceeding without it.

   Continue to test-writer. Roundtable is advisory, so a flap doesn't halt
   the pipeline — but the skip is surfaced in the commit verdict block
   (Step 9 sub-step 3) so it's visible after the fact.
2. Call `mcp__roundtable__roundtable-blueprint` with designer output from handoff.
3. Call `mcp__roundtable__roundtable-critique` with designer output from handoff.
4. Append combined output to `## roundtable-design-review` in handoff.
5. Set `roundtable_design_attempt: 1` in YAML.
6. If critical concerns raised: send findings back to designer, designer
   revises, increment `roundtable_design_attempt` to 2, re-run blueprint +
   critique.
7. If still concerns after attempt 2: proceed anyway (advisory, not blocking).
   Set `roundtable_design_verdict: CONCERNS`. Log unresolved items in handoff.
8. If no concerns: set `roundtable_design_verdict: APPROVED`.

Roundtable is advisory. It never directly blocks the pipeline — its findings
feed back through the designer for revision, not through authoritative gates.

### Step 3: Test-writer
Dispatch `test-writer` with the handoff file. It writes tests, never implementation. Append output to handoff file.

### Step 4: Implementer (if needed)
If pre-check set `Implementer needed: NO`, skip to Step 6 (spec-reviewer) or Step 8 (landing-verifier).
Otherwise, dispatch `implementer` with the handoff file. It writes code to pass the tests. Never modifies tests. Append output to handoff file.

### Step 5: Code review
Dispatch `code-reviewer` with the handoff file. It reviews code quality —
DRY, patterns, edge cases, architecture fit. Its output includes
`**Verdict:** APPROVED` or `**Verdict:** CHANGES NEEDED`.

After code-reviewer completes, increment `code_review_attempt` and copy
the verdict to `code_review_verdict` in the YAML frontmatter.

If CHANGES NEEDED with CRITICAL or MAJOR findings: send findings
back to `implementer`. Implementer fixes. Re-run code-reviewer.
Max 1 code review loop — if still CHANGES NEEDED, proceed to
spec-reviewer anyway (spec conformance is the harder gate).

### Step 6: Spec-reviewer (max 2 loops)
Dispatch `spec-reviewer` with the handoff file. It verifies code conforms
to specs. Its output includes `**Verdict:** CONFORMANT` or
`**Verdict:** DEVIATION`.

Before dispatching, increment `spec_review_attempt` in the YAML frontmatter
(starting at 1). After spec-reviewer completes, copy the verdict to
`spec_review_verdict` in the YAML frontmatter.

If DEVIATION:
- **Attempt 1:** Send specific deviation findings back to implementer.
  Implementer fixes. Re-run spec-reviewer (set attempt to 2).
- **Attempt 2:** If still DEVIATION, STOP. Do not loop again.
  Escalate to human: either decompose the feature or fix the spec.
  See docs/process/FAILURE-PLAYBOOK.md.

### Step 7: Safety-auditor (if feature touches domain-critical modules)
Dispatch `safety-auditor` with the handoff file. Its output includes
`**Verdict:** PASS` or `**Verdict:** VIOLATION`.

After safety-auditor completes, increment `safety_attempt` and copy the
verdict to `safety_verdict` in the YAML frontmatter.

If VIOLATION: send findings to implementer. Fix. Re-run safety-auditor.
Safety violations are never negotiable — max 3 attempts.
If still VIOLATION after 3 attempts, STOP. Escalate to human — the
invariant rule itself may need review, or the spec and invariant are
genuinely incompatible.

### Step 7.5: Arch-advisor verification (if pre-check classified architecture-tier)
If pre-check set `Arch-advisor needed: YES`, dispatch `arch-advisor` in VERIFY mode
for independent structural review before landing-verifier. Arch-advisor evaluates
whether the implementation is architecturally sound — not just spec-conformant.

If Arch-advisor's verdict is UNSOUND:
- Send findings to implementer with specific architecture issues
- Implementer fixes. Then re-run the full gate sequence:
  spec-reviewer → safety-auditor (if required) → Arch-advisor verification
- Arch-advisor-triggered gate passes use a SEPARATE counter from the
  initial spec-review attempts (those counters do not interact)
- Max 1 Arch-advisor verification retry. If still UNSOUND, escalate to human.

Append output to `## arch-advisor-verification` in the handoff file.

### Step 8: Landing-verifier
Dispatch `landing-verifier` with the handoff file. It runs tests and verifies everything is complete. Its output is `VERIFIED` (all gates passed, tests pass) or `BLOCKED`. If BLOCKED, fix blockers and re-run.

### Step 8.5: Roundtable landing review (if enabled)

Runs for ALL pipeline variants when `roundtable_enabled: true`.

1. Re-check roundtable MCP availability (120s timeout per tool call).
   If unavailable: set `roundtable_skipped: true` in handoff YAML with a
   one-line reason, print a visible warning to stderr:

     !! Roundtable MCP unavailable ({reason}) — skipping landing review.
     !! Configured roundtable_enabled=true in CLAUDE.md; proceeding without it.

   Proceed to Step 9. The skip is surfaced in the commit verdict block
   (Step 9 sub-step 3) so it's visible after the fact.
2. Call `mcp__roundtable__roundtable-crosscheck` with implementation summary from handoff.
3. Call `mcp__roundtable__roundtable-critique` with implementation summary from handoff.
4. Append combined output to `## roundtable-landing-review` in handoff.
5. Set `roundtable_landing_attempt: 1` in YAML.
6. If critical concerns raised: send findings back to implementer, implementer
   fixes, then re-run the full gate chain with separate counters:
   `code-reviewer` (roundtable_retry_code_review_attempt) →
   `spec-reviewer` (roundtable_retry_spec_review_attempt) →
   `safety-auditor?` (roundtable_retry_safety_attempt) →
   `arch-advisor?` → `landing-verifier`.
   Each roundtable-triggered gate re-run gets max 1 attempt. If a re-run gate
   itself fails, escalate to human — do not loop further.
7. After gate chain passes, re-run roundtable landing review (attempt 2).
8. If still concerns after attempt 2: proceed anyway (advisory, not blocking).
   Set `roundtable_landing_verdict: CONCERNS`. Log unresolved concerns.
9. If no concerns: set `roundtable_landing_verdict: APPROVED`.
10. Set handoff status to `READY-TO-LAND`.

When roundtable is disabled (roundtable_enabled: false), skip this step
entirely. The `VERIFIED` status from Step 8 triggers Step 9 directly.

Roundtable is advisory, not authoritative. Its findings feed back through
the existing authoritative gates on re-run. Roundtable never directly
blocks landing; it triggers re-evaluation by the authoritative gates.

### Step 9: Post-landing procedure (doc GC → commit → push → PR → archive)

Triggers on `READY-TO-LAND` (after roundtable review) or `VERIFIED` (when
roundtable is disabled). Sub-steps run in order. **Archive is the last
step** — it only happens when commit, push, and PR creation all succeed.
If any earlier sub-step fails, STOP and print the error. The handoff
stays in `active/`, the orchestrator halts, and the human resolves the
failure before any further action.

1. **Doc garbage collection.**
   Dispatch `doc-gardener` agent unconditionally. NORTH-STAR §Stage 4
   lists automatic GC as a core requirement. Always run; let the agent
   decide whether a sweep finds drift. `doc-gardener` is read-only — it
   returns a findings report.

   **Mode selection by pipeline variant:**
   - **Bootstrap variant** (F01–F03, which skip pre-check + implementer):
     dispatch in ad-hoc mode. The handoff lacks the execution brief and
     implementer report that pipeline mode requires.
   - **Standard variants** (backend / frontend / cross-cutting): dispatch
     in pipeline mode, which scopes findings to the blast radius plus
     the repo-wide §P5 timeline-artifact sweep.

   Prompt shape (pipeline mode — standard variants):
   ```
   **Mode:** pipeline
   **Handoff:** docs/exec-plans/active/handoffs/F##-<slug>.md

   Run per your §Operating modes. Scope: blast-radius (implementer's
   `**Changed paths:**`), feature-ID coverage, contract-surface
   coverage, plus the mandatory repo-wide §P5 sweep. Emit the Doc
   Garden Report with stable subsection headers and a
   `doc_garden_verdict` line.
   ```

   Prompt shape (ad-hoc mode — bootstrap variant):
   ```
   **Mode:** ad-hoc

   Run the full baseline sweep (CLAUDE.md / ARCHITECTURE.md / backlog
   / tech debt / design specs) plus the mandatory repo-wide §P5 sweep.
   Emit the Doc Garden Report with stable subsection headers and a
   `doc_garden_verdict` line.
   ```

   **Verdict capture.** Parse `doc_garden_verdict:` from the report
   (expect `CLEAN` or `DRIFT_FOUND`) and `drift_count:` (integer) and
   record both in the handoff YAML frontmatter as `doc_garden_verdict`
   and `doc_garden_drift_count`. The commit-message verdict block in
   sub-step 3 emits `doc-garden: CLEAN` or `doc-garden: DRIFT_FOUND (N
   fixes applied)` so the outcome survives in git history.

   If the report lists STALE or MISSING items, the orchestrator applies
   the fixes to the working tree NOW (before commit, so they land in
   the same commit — no amend, no post-push mutation, stable PR diff
   from open).

   **Halt handling.** If the agent halts with a pipeline-mode precondition
   failure (missing handoff, missing execution brief, missing implementer
   report), re-dispatch the agent in ad-hoc mode (second prompt shape above)
   — the full sweep produces a superset of what pipeline mode would have
   found. Only STOP Step 9 entirely if the ad-hoc re-dispatch also halts.

2. **Tech-debt log.**
   If `docs/exec-plans/tech-debt-tracker.md` exists, append any new
   shortcuts discovered during the run and check off any resolved items.

3. **Stage and commit.**
   Because "Before Starting" enforced a clean tree, every modified or
   new file in the working tree now is this feature's work. Stage
   everything:

     git add -A

   Compose the commit subject from the PRD:
   - The orchestrator was invoked with a full PRD path (e.g.,
     `docs/exec-plans/prds/my-feature.json`) — that's in conversation
     context from Step 1. Read the target feature's `title` via
     `jq '.features[] | select(.id=="F##") | .title' <prd-path>`.
   - If the PRD or target feature is missing, fall back to the handoff
     slug with hyphens replaced by spaces (e.g., `F42-oauth-pkce-flow`
     → `oauth pkce flow`). The fallback is lossy but deterministic.

   Message format (HEREDOC):

     feat(F{id}): {feature title from PRD .features[].title}

     PRD: {prd_ref from handoff YAML frontmatter}
     Pipeline: {pipeline variant: bootstrap|backend|frontend|cross-cutting}
     Verdicts:
     {verdict_lines}

     🤖 Generated with KEEL pipeline

   Where `{verdict_lines}` is built by iterating over the handoff YAML
   frontmatter and emitting one line per verdict field that is set to a
   non-empty value. Skip any verdict whose field is unset (agent did not
   run in this pipeline variant). Format per line:

     spec-review: CONFORMANT (attempt 1)
     safety:      PASS (attempt 1)
     arch-advisor: SOUND
     code-review: APPROVED (attempt 1)
     doc-garden:  CLEAN | DRIFT_FOUND (N fixes applied)
     roundtable-precheck: APPROVED (attempt 1)
     roundtable-design: APPROVED (attempt 1)
     roundtable-landing: APPROVED (attempt 1)

   If `roundtable_skipped: true` is set, emit a SKIPPED line in place of
   the roundtable verdicts so the skip survives in git history:

     roundtable: SKIPPED ({reason from handoff})

   If all verdict fields are unset (bootstrap variant), emit the single
   line: `Verdicts: n/a (bootstrap variant)`.

   Commit with the constructed message.

4. **Push the branch.**
   Read `remote_name` from the handoff YAML (set at Step 0 item 6) and
   run `git push -u <remote_name> HEAD`. On failure, STOP and print the
   raw git error. The commit is still local; the handoff stays in
   `active/`. The human resolves the error and re-runs — Step 9 picks
   up from sub-step 4 because the handoff is still active.

5. **Open a PR.**
   Probe `gh`: `command -v gh` and `gh auth status`.
   - If both succeed: `gh pr create --fill` (ready-for-review). On
     success, record the returned URL in the handoff YAML as `pr_url`.
   - If `gh pr create` errors (auth expired, rate limit, branch policy,
     network): STOP and print:

         PR creation failed: <raw error>
         Branch keel/F{id}-{slug} is pushed to <remote_name>.
         Open a PR manually, or resolve the error and re-run the
         pipeline to retry PR creation.

     Handoff stays in `active/`; no archive, no status change.

   - If `gh` is missing or unauthenticated: print:

         No forge CLI available — branch keel/F{id}-{slug} is pushed
         to <remote_name>. Open a PR manually on your forge.

     This is not an error — it's an environment gap. Continue to
     sub-step 6. The human opens the PR by hand; `pr_url` stays unset.

   KEEL only ships a PR-based landing flow. If a project needs direct
   merge-to-base or a different forge integration, edit this skill file
   in the installed `.claude/skills/keel-pipeline/SKILL.md` — the skill
   is installed into each project and is a first-class customization
   point.

6. **Archive the handoff.**
   Move the handoff file:
     `docs/exec-plans/active/handoffs/F{id}-{slug}.md`
     → `docs/exec-plans/completed/handoffs/F{id}-{slug}.md`

   Archive is the LAST sub-step. Only runs when commit, push, and PR
   creation (or the "no gh" print-manual-instructions path) all succeed.
   If any earlier sub-step halts, the handoff stays in `active/` and the
   human can re-run the pipeline after resolving the issue.

   Amend this move into the feature commit with `git commit --amend
   --no-edit` and re-push (`--force-with-lease`). The PR diff now reflects
   the archived path, matching what a reviewer sees. Skip the amend if the
   forge rejects force-with-lease — the handoff move lands in a
   follow-up commit on the same branch.

## Rules

- **Never skip steps.** Every agent in the pipeline runs.
- **Handoff file is the thread.** Each agent reads and appends to it.
- **Pre-check decides optionals.** Only skip designer/researcher/safety-auditor if pre-check says so.
- **Spec-reviewer and safety-auditor are gates.** If they find issues, loop back to implementer.
- **You don't write code.** Agents write code. You orchestrate.
- **Docs drive code.** If there's no spec, there's no pipeline. Write the spec first.
- **Structured verdicts.** Gate agents output `**Verdict:**` in their
  sections. The orchestrator copies verdicts and attempt counts to the
  YAML frontmatter. Branch on frontmatter, not on parsing agent prose.
- **Max 2 spec-review loops.** After 2 DEVIATION verdicts, escalate.
  Don't try harder — decompose or fix upstream.
- **Downstream reads upstream.** Each agent reads upstream Decisions and
  Constraints FIRST before starting its own work.
- **Stage 4 auto-landing.** After VERIFIED/READY-TO-LAND, the orchestrator runs Step 9
  end-to-end without asking. The human's review surface is the PR on
  GitHub, not a per-step prompt. To run the pipeline without auto-landing
  (e.g., for debugging), interrupt before Step 9 — the orchestrator will
  stop at the landing boundary.
- **Clean tree, then branch, then build.** "Before Starting" refuses a
  dirty working tree and auto-branches from main/master BEFORE any agent
  runs. This is the only automatic branch creation the pipeline performs.
  Once inside a feature branch, intermediate pipeline writes cannot
  pollute main even on a halt.
- **gh is optional.** The pipeline prints manual PR instructions if gh
  is missing or not authed. It does not fail the run.
- **doc-gardener is unconditional.** Step 9 sub-step 1 always dispatches
  doc-gardener; no more "if the feature was substantial" judgment call.
  Drift fixes are applied to the working tree BEFORE the commit, so the
  PR diff is stable from the moment it opens.
- **Roundtable is advisory.** It never directly blocks landing. Findings
  feed back through authoritative gates (spec-reviewer, safety-auditor) on
  re-run. If roundtable has concerns after max attempts, proceed anyway.
- **Re-check MCP before each call.** Don't rely on the roundtable_enabled
  flag from Step 0.5. Probe availability immediately before each roundtable
  tool call. Timeout: 120s. On failure: skip, log reason, continue.
- **PR-only landing.** Every feature lands by pushing the feature branch
  and opening a PR. No merge-to-base, no strategy selection. To change the
  landing flow, edit Step 9 in the installed skill file.
- **VERIFIED → READY-TO-LAND → LANDED.** Landing-verifier emits VERIFIED.
  Roundtable review (if enabled) transitions to READY-TO-LAND. Step 9
  transitions to LANDED after commit+push+PR+archive succeed. When
  roundtable is disabled, VERIFIED triggers Step 9 directly (skip
  READY-TO-LAND).
- **Archive is the last step.** Step 9 moves the handoff to `completed/`
  only after commit, push, and PR-creation (or the no-gh fallback) all
  succeed. Any earlier halt leaves the handoff in `active/` so re-running
  the pipeline picks up from where it stopped.
