---
name: keel-safety-check
description: "Quick safety audit — scans current changes against domain invariant rules. Use after editing critical modules."
---

# Safety Check

Run an immediate safety audit on the current working tree changes. Dispatches the `safety-auditor` agent against the unstaged/staged diff.

## When to Use

- After editing files that touch domain-critical operations
  <!-- CUSTOMIZE: e.g., git.ex, auth.py, transforms.ts, queries.sql -->
- Before committing changes that touch invariant-protected code
- When the PreToolUse hook reminds you to

## What It Does

This is an **ad-hoc check** for use outside the pipeline (e.g., during manual editing).
Inside the pipeline, `safety-auditor` is dispatched by `keel-pipeline` with handoff context.

1. Runs `git diff` to identify changed files
2. Filters for files that touch domain-critical operations
3. Scans those files against the domain invariant rules below
4. Reports PASS or VIOLATIONS

## Domain Invariant Rules

<!-- CUSTOMIZE: Replace with your project's invariant rules from core-beliefs.md.
     See examples/domain-invariants/ for templates. -->

From CLAUDE.md and `docs/design-docs/core-beliefs.md`:

1. [YOUR INVARIANT RULE 1]
2. [YOUR INVARIANT RULE 2]
3. [YOUR INVARIANT RULE 3]

## Execution

Dispatch the `safety-auditor` agent. It is read-only and reports findings.

If violations are found, fix them before committing.
