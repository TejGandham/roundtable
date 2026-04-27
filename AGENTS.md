# Agent Instructions
# This is a production app which mean directly working on main git branch is prohibited. You are allowed to merge to main with permission after work is complete but never directly work on main, always branch and cleanup when done.

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **Run quality gates** (if code changed) - Tests, linters, builds
2. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   git push
   git status  # MUST show "up to date with origin"
   ```
3. **Clean up** - Clear stashes, prune remote branches
4. **Verify** - All changes committed AND pushed
5. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds

## Doc conventions

- **`docs/superpowers/` is transitional.** Anything under that path is in-flight scaffolding for an active design or workflow — not durable architectural truth. Treat it as scratch: acceptable to delete once the design has landed and the rationale lives in code/ARCHITECTURE.md or in a permanent spec elsewhere. Do not point load-bearing code at files under this path; if you must, log it in `docs/exec-plans/tech-debt-tracker.md` with the migration plan.
- **Snapshot, not timeline.** `docs/` describes current state and active future intent. Phase plans, dogfood results, post-mortems, completed reviews → belong in `git log`, not in `docs/`. KEEL principle P5.
