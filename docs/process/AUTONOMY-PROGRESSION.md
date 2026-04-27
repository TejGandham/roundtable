# Increasing Agent Autonomy Over Time

How to evolve your KEEL process from "human reviews everything" to increasing levels of agent self-sufficiency.

## The Progression

| Stage | Human Role | Agent Role | Typical Timeline |
|---|---|---|---|
| **1. Full Oversight** | Reviews every agent output, every PR | Executes single steps, waits for approval | First 5-10 features |
| **2. Pipeline Trust** | Reviews landed features, spot-checks handoffs | Runs full pipeline without step-by-step approval | Features 10-20 |
| **3. Agent Review** | Reviews agent review summaries | Agents review each other's work (spec-reviewer, safety-auditor) | Features 20-30 |
| **4. Self-Validation** | Reviews exceptions and escalations only | Agents validate their own work by driving the application | After MVP |
| **5. End-to-End** | Steers priorities, resolves judgment calls | Agent reproduces bug → fixes → validates → opens PR → responds to feedback → merges | Mature codebase |

## Stage 1: Full Oversight (Start Here)

Every KEEL project starts here. The human:
- Reads every execution brief from pre-check
- Reviews every design brief
- Reads every test before implementer runs
- Reviews every implementation
- Reads every spec conformance report
- Manually runs landing-verifier verification

This is intentional. You're calibrating: learning what the agents do well, where they struggle, and what capabilities are missing.

### When to move to Stage 2
- You've stopped finding issues in execution briefs
- Test-writer consistently produces the right tests
- Implementer consistently passes tests on first run
- You trust the handoff mechanism

## Stage 2: Pipeline Trust

Stop reviewing intermediate steps. Run the full pipeline and review the landed result:
- Read the handoff file after landing-verifier reports VERIFIED
- Review the git diff (what changed)
- Run the app and verify behavior
- Commit if satisfied

### When to move to Stage 3
- Spec-reviewer consistently catches deviations you would have caught
- Safety-auditor has never missed a violation you found manually
- You trust the gate agents

## Stage 3: Agent Review

Let agents review each other. The human only reviews:
- Spec-reviewer output (not the code directly)
- Safety-auditor output (not scanning for violations manually)
- The final landing report

### When to move to Stage 4
- You've built validation infrastructure (browser automation, observability)
- Agents can drive the app and verify behavior programmatically
- Test coverage is comprehensive enough that "tests pass" means "it works"

## Stage 4: Self-Validation

Agents validate their own work by driving the application:
- Browser automation / DevTools integration for UI verification
- Observability stack queries for runtime behavior verification
- Performance benchmarks for non-functional requirements

From OpenAI: "We made the app bootable per git worktree, so Codex could launch and drive one instance per change."

### When to move to Stage 5
- Agent validation catches issues before humans do
- The codebase has comprehensive golden principles
- Garbage collection runs automatically
- You trust the system enough to sleep while it works

## Stage 5: End-to-End

The agent drives a feature from bug report to merged PR:
1. Validate current codebase state
2. Reproduce reported bug
3. Record evidence of failure
4. Implement fix
5. Validate fix by driving the application
6. Commit, push the feature branch, open a PR
7. Respond to agent and human feedback
8. Detect and remediate build failures
9. Escalate only when judgment is required
10. Confirm PR merged on the forge

From OpenAI: "We regularly see single Codex runs work on a single task for upwards of six hours (often while the humans are sleeping)."

## What Enables Each Stage

| Capability | Enables |
|---|---|
| Handoff files | Stage 2 (pipeline trust without step reviews) |
| spec-reviewer + safety-auditor | Stage 3 (agent-to-agent review) |
| Browser/app automation | Stage 4 (self-validation) |
| Observability stack | Stage 4 (runtime verification) |
| Golden principles + garbage collection | Stage 5 (autonomous quality) |
| Comprehensive test coverage | All stages (the foundation) |

## The Key Insight

Autonomy isn't given — it's earned. Each stage builds on infrastructure from the previous one. Skip a stage and the foundation crumbles.

"When something failed, the fix was almost never 'try harder.' The primary job became: identify what capability is missing and make it legible and enforceable for the agent." — OpenAI Harness Engineering
