# KEEL Anti-Patterns

Things that break the process. Each anti-pattern includes what goes wrong and what to do instead.

## The 1000-Page Manual

**What:** Putting everything in CLAUDE.md / AGENTS.md — all rules, all context, all instructions in one massive file.

**Why it fails:** Context is a scarce resource. A giant file crowds out the task, the code, and the relevant docs. Agents pattern-match locally instead of navigating intentionally. The file rots instantly because humans stop maintaining it.

**Instead:** CLAUDE.md is ~80-100 lines. It's a table of contents with pointers to docs/. Progressive disclosure — agents read what they need when they need it.

## Vague Specs

**What:** Specs that say "handle errors gracefully" or "make it fast" or "good UX."

**Why it fails:** Test-writer can't derive concrete test assertions from vague language. Agents guess, and guessing is where hallucination lives.

**Instead:** "Return {:error, :timeout} after 60 seconds." "Respond within 200ms at p99." "Show error message in red monospace with retry button." Specific enough to test.

## Features That Span Layers

**What:** A single feature that touches foundation, service, AND UI.

**Why it fails:** Too many moving parts for one pipeline run. Tests become integration tests by accident. Failures are hard to isolate.

**Instead:** Decompose into layer-specific features with explicit dependencies. F04 (foundation) → F12 (service, needs F04) → F17 (UI, needs F12).

## Testing Implementation Details

**What:** Tests that assert on internal state, private function calls, or specific code structure rather than observable behavior.

**Why it fails:** Implementation changes break tests even when behavior is correct. Implementer is constrained unnecessarily.

**Instead:** Test the spec contract. "Given X input, expect Y output." "When event Z occurs, state changes to W." Test what, not how.

## "Try Harder"

**What:** When an agent fails, re-running with the same prompt hoping for a different result.

**Why it fails:** The agent lacked a capability — a tool, a doc, a guardrail. Retrying doesn't add the missing capability.

**Instead:** Ask: "What is missing?" Build the missing tool/doc/guardrail and feed it back into the repo. Then re-run.

## Enforcing Style as Invariants

**What:** Making code style preferences (naming conventions, comment format, blank line placement) into safety-auditor rules.

**Why it fails:** Safety-auditor should catch things that BREAK the system, not things that offend aesthetics. Style noise drowns out real violations.

**Instead:** Use formatters and linters for style. Reserve safety-auditor for domain invariants that, if violated, cause data loss, security holes, or system failures.

## Knowledge Outside the Repo

**What:** Making decisions in Slack, documenting in Google Docs, keeping architectural knowledge in someone's head.

**Why it fails:** Agents can't see it. It's as invisible as it would be to a new hire joining three months later. Decisions get lost, contradicted, or forgotten.

**Instead:** Encode as markdown in the repo. If it was worth discussing, it's worth committing.

## Manual Friday Cleanup

**What:** Spending 20% of the week manually cleaning up "AI slop" — inconsistent patterns, redundant code, style drift.

**Why it fails:** Doesn't scale. Humans burn out. Drift accumulates faster than cleanup.

**Instead:** Encode golden principles in the repo. Run doc-gardener and cleanup agents on a recurring cadence. Capture taste once, enforce continuously.

## Mocking Safety-Critical Paths

**What:** Using mocks for testing safety invariants (e.g., mocking the git interface when testing pull guards).

**Why it fails:** You're testing your mock, not your safety. The real implementation could violate every rule and your tests would still pass.

**Instead:** Layer 1 tests (safety invariants) MUST use real I/O against temp directories/environments. Mock is for Layers 3-4 only.

## Skipping Pre-Check

**What:** Jumping straight to test-writer because "I know what to build."

**Why it fails:** Pre-check catches spec inconsistencies, unmet dependencies, and routing decisions. Skipping it means test-writer works from assumptions, not verified context.

**Instead:** Always run pre-check. It's fast and it produces the execution brief that every downstream agent reads.
