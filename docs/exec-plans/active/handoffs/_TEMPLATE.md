# [Feature Name]

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
status: IN-PROGRESS
pipeline:       # bootstrap | backend | frontend | cross-cutting
spec_ref:       # e.g., mvp-spec:4.2

# Pre-check routing (set by pre-check, read by orchestrator)
intent:         # refactoring | build | mid-sized | architecture | research
complexity:     # trivial | standard | complex | architecture-tier
designer_needed:       # YES | NO
researcher_needed:     # YES | NO
safety_auditor_needed: # YES | NO
arch_advisor_needed:         # YES | NO
implementer_needed:          # YES | NO

# Gate verdicts (set by orchestrator after each gate agent)
spec_review_verdict:   # CONFORMANT | DEVIATION
spec_review_attempt: 0
safety_verdict:        # PASS | VIOLATION
safety_attempt: 0
code_review_verdict:         # APPROVED | CHANGES NEEDED
code_review_attempt: 0
arch_advisor_verdict:        # SOUND | UNSOUND (verify mode only)

# Arch-advisor re-run counters (separate from initial gate passes)
# Used when arch-advisor UNSOUND triggers a re-run of gates
arch_retry_spec_review_attempt: 0
arch_retry_safety_attempt: 0

# Pipeline configuration
remote_name:                         # resolved at Step 0 item 6 (e.g., origin, upstream)
roundtable_enabled:                  # true | false (set at Step 0.5)
pr_url:                              # set after gh pr create at Step 9

# Roundtable pre-check review (Step 1.3)
roundtable_precheck_attempt: 0
roundtable_precheck_verdict:         # APPROVED | CONCERNS
roundtable_precheck_skipped:         # true (with reason) if MCP unavailable

# Roundtable design review (Step 2.5)
roundtable_design_attempt: 0
roundtable_design_verdict:           # APPROVED | CONCERNS
roundtable_skipped:                  # true (with reason) if MCP unavailable

# Roundtable landing review (Step 8.5)
roundtable_landing_attempt: 0
roundtable_landing_verdict:          # APPROVED | CONCERNS

# Roundtable-triggered gate re-run counters (separate from initial passes)
roundtable_retry_code_review_attempt: 0
roundtable_retry_spec_review_attempt: 0
roundtable_retry_safety_attempt: 0
---

## pre-check
<!-- Execution brief appended here by pre-check agent -->

### Constraints for downstream
<!-- MUST/MUST NOT directives for downstream agents. Max 5 bullets. -->

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
<!-- Test report appended here -->

### Decisions (optional)
<!-- Key choices made and why. Max 5 bullets. -->

## implementer
<!-- Implementation report appended here -->

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
<!-- Landing report appended here -->

## roundtable-landing-review
<!-- Multi-model advisory review of implementation (Step 8.5, if roundtable enabled).
     Orchestrator calls roundtable-crosscheck + roundtable-critique tools. Output appended here. -->
