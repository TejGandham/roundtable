# The KEEL Process Guide

**KEEL -- Knowledge-Encoded Engineering Lifecycle**

A structured process for agent-driven software development where humans steer
and agents execute through specialized pipelines.

Adapted from OpenAI's "Harness Engineering" article (February 2026, Ryan
Lopopolo), where a team shipped a product with zero manually-written code
using Codex agents. KEEL adapts those principles into concrete, repeatable
mechanics on top of Claude Code — the only supported agent runtime today.

> All decisions below are anchored to the seven KEEL framework
> principles at [`KEEL-PRINCIPLES.md`](KEEL-PRINCIPLES.md).

---

## What is KEEL?

KEEL is a lifecycle for building software with AI agents as the primary
implementers. The three words in the name each carry weight:

**Knowledge-Encoded.** The repository is truth. Docs drive code. Everything the
agent needs is committed as versioned artifacts -- specs, plans, architecture
docs, testing strategies, handoff files. If it is not in the repo, it does not
exist to the agent. External knowledge (Google Docs, Slack threads, verbal
decisions, tacit expertise) is invisible until someone encodes it as a markdown
file and commits it.

**Engineering.** This is not prompt engineering. It is a structured engineering
process with specialized agents, pipelines, testing doctrine, and mechanical
enforcement. Agents have defined roles, bounded responsibilities, and explicit
inputs and outputs. Quality is enforced by structure, not by hoping the model
gets it right.

**Lifecycle.** KEEL covers the build arc from vision to landed feature:

```
north star --> spec --> backlog --> pipeline --> landed feature --> garbage collection
```

Every phase produces versioned artifacts. Every artifact feeds the next phase.
The repo accumulates institutional knowledge that compounds over time.

**Scope boundary:** KEEL's coverage ends at the git commit. It ensures the code
entering your CI/CD pipeline is spec-conformant, tested, and safe. It does not
cover deployment, infrastructure, monitoring, or incident response — those are
downstream concerns that take over where KEEL leaves off.

### Where KEEL Came From

The name comes from a ship's [keel](https://en.wikipedia.org/wiki/Keel) —
the structural spine laid down first, before any hull goes on top. Lay it
straight and the ship holds. Same idea here: encode the structure, and
every feature built on top inherits it.

In February 2026, Ryan Lopopolo's team at OpenAI published "Harness
Engineering" describing how they shipped a product with Codex agents writing
all the code. Their key insight: the repository must be the complete system of
record for agent work. Docs written for agent legibility. Mechanical
enforcement of invariants. Progressive disclosure of context. Background agents
for garbage collection.

KEEL takes those principles and makes them operational for Claude Code: concrete
file structures, named agent roles, pipeline definitions, handoff formats,
testing layers, and a commit ritual that keeps the repo honest.

---

## 1. Philosophy and Principles

Six core beliefs govern every decision in a KEEL project. Each one
operationalizes a principle from the OpenAI article.

| KEEL Belief | OpenAI Principle | What It Means |
|---|---|---|
| Repo is truth | Repository as system of record | If it is not committed, it does not exist to agents |
| Docs drive code | Agent legibility is the goal | Specs written for agent comprehension, not humans |
| Coding comes last | Spec-first workflow | Spec, then test, then code, then verify. Always. |
| Progressive disclosure | Map not manual | CLAUDE.md is ~80 lines as table of contents; knowledge lives in docs/ |
| Smallest testable units | Depth-first bootstrap | Break goals into building blocks; use them to unlock complexity |
| Garbage collect | Entropy management | Golden principles, recurring cleanup, fix doc lies immediately |

### The Knowledge Boundary Principle

Anything the agent cannot see does not exist. Claude Code reads files from
the repository. It does not read Google Docs, hear Slack conversations, or
know what you decided over lunch. The only way to make external knowledge
visible: encode it as a versioned markdown artifact in the repo.

This is the foundational principle. Everything else in KEEL follows from it.

### Who KEEL Is For

- Solo developers or small teams (1-3 people) using an AI agent as primary implementer
- Projects that grow organically — where today's 3 features become next month's 30
- Long-lived projects where institutional knowledge must compound, not evaporate
- Projects where safety invariants must be mechanically enforced
- Claude Code as the agent runtime (the only platform KEEL supports today)

### When KEEL Is Right

- Multi-feature projects that will grow in scope over time
- Agent-driven development (agent writes the code, human steers)
- Projects where a single rules file (AGENTS.md, .cursorrules) has stopped scaling
- Projects where correctness, spec conformance, and safety matter

### When KEEL Is Overkill

- One-off scripts or quick fixes (just write the code)
- Tight human feedback loops where you are pair-programming with the agent
- Throwaway prototypes you will discard next week
- Projects with fewer than 5 planned features

### The Human-Steers / Agent-Executes Contract

The human decides what to build, in what order, and whether the result is
acceptable. The human writes the north star, approves specs, kicks off
pipeline stages, reviews output, commits code, and updates the backlog.

The agent reads specs, writes tests, writes code, reviews its own work, and
reports results. The agent never decides what to build next.

**Before Stage 4 Phase 1 (historical):** the agent executed repo mutations
(commits, doc moves, backlog updates) only after presenting the action and
receiving human approval at each step.

**Stage 4 Phase 1 and later:** after `landing-verifier` reports VERIFIED, the
orchestrator runs roundtable review (if enabled) and a deterministic post-landing
procedure — doc-gardener, handoff archive, commit, push the feature branch, and
open a PR — without per-step approval. Every feature becomes a PR on the forge;
the human reviews and merges there. Escalations (gate ceilings tripped) still
halt in-session and surface to the human immediately, as before.

The human provides taste, judgment, and strategic direction. The agent provides
speed, consistency, and tireless attention to spec conformance.

---

## 2. The Knowledge Boundary

The "K" in KEEL deserves its own section because it is the hardest principle
to internalize and the easiest to violate.

```
+-------------------------------+
|    What the agent CAN see     |
|  Code, markdown, schemas,     |
|  exec plans, tests, configs,  |
|  handoff files, CLAUDE.md     |
+-------------------------------+
        ^ must encode ^
+-------------------------------+
|   What the agent CAN'T see    |
|  Slack, verbal decisions,     |
|  your head, Google Docs,      |
|  meeting notes, email threads |
+-------------------------------+
```

### Common Knowledge Boundary Violations

| Violation | Symptom | Fix |
|---|---|---|
| Decision made in conversation, not committed | Agent contradicts it next session | Add to relevant spec or design doc |
| Architectural preference in your head | Agent makes a different choice | Write it in core-beliefs.md |
| "Everyone knows" convention | Agent does not know it | Add to CLAUDE.md or a referenced doc |
| Spec change discussed but not written down | Agent implements the old spec | Update the spec file first |
| External API behavior learned from docs site | Agent guesses wrong | Add API contract to a reference doc |

Every time you catch yourself saying "the agent should know that," stop and
ask: is it in the repo? If not, it does not exist. Knowledge compounds when
committed. Knowledge evaporates when verbal.

---

## 3. Creating Your North Star

The north star document (`docs/north-star.md`) encodes the project's vision,
growth stages, operating principles, and definition of success.

### What Goes in the North Star

- **Vision.** What you are building and why. Concrete enough that an agent can
  evaluate whether a design decision aligns with it.
- **Operating principles.** What you adopt fully, what you adapt, what you skip.
  "What we skip" prevents the agent from gold-plating.
- **Growth stages.** A table mapping project maturity to KEEL additions.
- **Target folder structure.** The fully realized directory tree, annotated.
- **The four loops.** Validation (write/test/fix), knowledge boundary, layered
  architecture, garbage collection.

### North Star and CLAUDE.md

The north star encodes taste before it becomes linters. It answers "how should
this project feel?" while CLAUDE.md answers "how is this project structured?"

CLAUDE.md points to the north star. Agents read CLAUDE.md first, then follow
the pointer when they need to make judgment calls.

---

## 4. Bootstrapping the Repo

From empty directory to first passing test.

### Step-by-Step Bootstrap

```
1. Create directory structure
2. Write north star               (docs/north-star.md)
3. Write CLAUDE.md                (~80-100 lines, table of contents)
4. Produce product PRDs via       (/keel-refine → docs/exec-plans/prds/<slug>.json)
   /keel-refine                   (conversion hub — accepts prose, markdown, bundles, images; emits JSON)
5. Write core beliefs             (docs/design-docs/core-beliefs.md)
6. Write testing strategy         (in core-beliefs or standalone)
7. Define architecture layers     (ARCHITECTURE.md)
8. Configure Docker/container     (Dockerfile + docker-compose.yml)
9. Run bootstrap features:
   F01: docker-builder -> landing-verifier
   F02: scaffolder -> landing-verifier
   F03: config-writer -> landing-verifier
10. Verify: tests pass in container
```

Steps 1-8 produce documentation. Step 9 produces code. This ratio is
deliberate -- by the time code is written, the agent has comprehensive context.

### The Document Hierarchy

**CLAUDE.md** (~80-100 lines) -- The table of contents. Quick facts, safety
rules, workflow overview, pointers to deeper docs. "Map not manual."

```
# Project Name
## Quick Facts       - Stack, runtime, key constraints
## Safety Rules      - Non-negotiable invariants (3-6 bullets)
## Workflow — Mandatory Pipelines  - Pipeline definitions (compact)
## Architecture      - Link to ARCHITECTURE.md
## PRDs and specs    - Links to exec-plans/prds/, design-docs/
## Plans             - Links to exec-plans/active/, completed/
## Development       - How to build, run, test (4-6 lines)
```

**ARCHITECTURE.md** -- Process model, module map with dependencies, layer
diagram, key design decisions. Updated as modules are added.

**Structured PRDs** (`docs/exec-plans/prds/<slug>.json`) -- What to build.
Schema-validated JSON with `features[].contract` + `features[].oracle` that
IS the spec. Authored by `/keel-refine`'s card walk.

**Design docs** (`docs/design-docs/`) -- How it looks, core beliefs, testing
strategy. Principles that apply across all features.

**Execution plans** (`docs/exec-plans/`) -- Feature backlog, handoff files,
tech debt tracker. The operational layer.

```
docs/
  north-star.md
  design-docs/
    core-beliefs.md
    ui-design.md
  exec-plans/
    prds/
      <slug>.json
    active/
      feature-backlog.md
      handoffs/
    completed/
      handoffs/
    tech-debt-tracker.md
  references/
```

---

## 5. Writing Specs That Agents Can Execute

A spec exists to eliminate ambiguity. If the agent has to guess, the spec
failed.

### Spec Structure

Every spec answers these questions in order:

1. **What is this?** One paragraph of context.
2. **What does it do?** Concrete behavior as assertions.
3. **What are the inputs and outputs?** Data structures, types, examples.
4. **What are the constraints?** Safety rules, edge cases.
5. **What is explicitly excluded?** Scope boundaries.

### Agent Legibility

| Human-readable | Agent-legible |
|---|---|
| "The button should feel responsive" | "Button click triggers fetch within 100ms; UI shows :fetching state" |
| "Handle errors gracefully" | "On non-zero exit: set operation to :error, store stderr in last_error, broadcast" |
| "Support common git workflows" | Table of 6 states with exact field values for each |

### Spec Consistency Checks

Before writing tests, verify specs agree:

- [ ] Product spec and design doc describe the same behavior
- [ ] Architecture doc lists the modules the feature will touch
- [ ] Feature backlog entry references correct spec sections
- [ ] Safety rules in core-beliefs match constraints in product spec

Spec drift in an agent-built project is as dangerous as a code bug.

### Anti-Pattern: Vague Specs

```
BAD:  "Display repo status with appropriate colors"
GOOD: "Status priority: error > diverged > dirty > topic > behind > clean.
       Each state maps to exactly one color. See table in Section 4.6."

BAD:  "Pull should be safe"
GOOD: "Pull preconditions (ALL must be true):
       - On default branch, clean tree, not diverged, behind > 0
       If any fails: button disabled, tooltip shows reason."
```

---

## 6. The Feature Backlog

The feature backlog decomposes the product spec into ordered, independently
testable features.

### Decomposition Principles

- **Smallest testable unit.** If a feature touches more than two system layers,
  split it.
- **Dependency-aware ordering.** Execute top-to-bottom, never cherry-pick.
- **Layer isolation.** Foundation first, then service, then UI.

### Backlog Format

```markdown
- [x] **F04 Git.repo?/1**
  Needs: F02, F03
  PRD: repo-man-mvp
  <!-- DRAFTED: <ISO-date> by backlog-drafter; 0 markers remain -->
  <!-- SOURCE: docs/prds/repo-man-mvp.md -->

- [ ] **F05 Git branch detection**
  Needs: F04
  PRD: repo-man-mvp
  <!-- DRAFTED: <ISO-date> by backlog-drafter; 0 markers remain -->
  <!-- SOURCE: docs/prds/repo-man-mvp.md -->
```

Each entry carries: feature ID + name, optional `Needs:` line,
optional `Design:` line for UI-bearing features, mandatory
`PRD: <slug>` (or `PRD-exempt: <reason>`) anchoring it to the JSON
PRD at `docs/exec-plans/prds/<slug>.json`, plus the drafter's
`<!-- DRAFTED: ... -->` and `<!-- SOURCE: ... -->` provenance
comments. The contract and acceptance oracle live in the JSON PRD's
per-feature `contract` + `oracle.assertions[]`, not on the backlog
entry.

### Feature Sizing

A well-sized feature adds 1-3 modules, has 3-8 tests, completes in one
pipeline run, and has a clear "done" state.

### Anti-Pattern: Cross-Layer Features

Example (from the Repo Man project):
```
BAD:  "F12 Fetch with UI feedback"  (service + UI — crosses layers)

GOOD: "F12 RepoServer init"         (service only)
      "F13 RepoServer fetch"        (service only)
      "F17 Dashboard LiveView"      (UI, consumes service)
```
Your feature names will reflect your domain, but the principle is the same:
each feature lives in one architectural layer.

### Refining the Backlog from a PRD — `/keel-refine`

The backlog is human-owned, but KEEL ships a drafting aid for the step
before the pipeline runs: turning a PRD, a paragraph of intent, or a
set of wireframes/comps into candidate `F##` entries.

```
/keel-refine docs/prds/auth-redesign.md       # from a PRD file
/keel-refine docs/prds/auth-redesign/          # from a bundle directory
                                               # (README.md + sibling images/PDFs)
/keel-refine "let users edit profile inline"  # from prose
/keel-refine                                   # interactive interview
```

You can also paste screenshots, comps, or flow diagrams directly in
chat alongside any of the above invocations. The skill stages pasted
images in `.keel-refine-session/<id>/` (gitignored) and passes them to
the drafter.

The `backlog-drafter` agent reads the PRD, `ARCHITECTURE.md`,
`CLAUDE.md`, the current `feature-backlog.md`, and any UI design assets
(via Claude vision, shallow-read only — for mapping which asset belongs
to which F##, not for transcribing colors or copy). It returns a
structured proposal: new entries with `F##` ids, layer sections,
`Needs:` edges, one-line acceptance criteria, optional `Design:` fields
on UI entries, and `<!-- HUMAN: ... -->` markers everywhere it couldn't
derive a field unambiguously.

**Per-card walkthrough.** The skill first prints an orientation summary
of the full slate, then walks each drafted entry individually before
accepting `commit`. The walk is mandatory — even zero-marker cards are
acknowledged — because NORTH-STAR §Autonomy Ceiling requires per-card
conversational review.

```
Card 1 of 3:

F12 Login screen with validation      → service
  Needs (intra-PRD):  F08
  Needs (cross-PRD):  —
  Design:             login-flow.png

  Contract:
    route: /login
    request_fields: [email, password]

  Oracle:
    type:       integration
    assertions:
      [1] Valid credentials redirect to /home with a session cookie set.
      [2] Invalid credentials return 401 and surface "Email or password incorrect".

  Open markers:
    [1] Should rate-limit thresholds live in this contract or a shared `auth-policy`?

Verbs:
  accept                              — commit this card, advance
  edit title: <text>                  — set/replace title
  edit layer: <enum>                  — service|ui|cross-cutting|foundation
  edit needs: <comma-joined F##>      — full list replace
  set contract.<key>: <value>         — set/replace a contract key
  drop contract.<key>                 — remove a contract key
  edit oracle.type: <enum>            — unit|integration|e2e|smoke
  add oracle.assertion: <text>        — append an assertion
  edit oracle.assertion <n>: <text>   — replace assertion n
  drop oracle.assertion <n>           — remove assertion n
  answer marker <n>: <text>           — record answer (apply via follow-up edit)
  skip marker <n>                     — ship marker as-is (pre-check blocks)
  drop F##                            — remove card, advance
  back                                — revisit prior card
```

The full verb set lives in `.claude/skills/keel-refine/SKILL.md` §"Phase 5 Step 2".

You walk one card at a time. Advancing verbs (`accept`, `drop`, `back`)
move between cards; field edits and marker answers stay on the active
card. After every card has been advanced at least once, the skill
enters post-walk state:

```
Walk complete. 3 cards ready to commit.

Verbs:
  commit        — materialize + git commit
  revisit F##   — re-open a card for editing
  abort         — discard session
```

`commit` is only valid after the walk completes. Attempting it earlier
re-points to the current unwalked card. `commit` then materializes the
backlog entries, moves pasted images to `docs/exec-plans/prds/<slug>/assets/`,
and runs `git add` + `git commit` with a deterministic message.
No confirmation prompt; feature-branch commits are trivially reversible
(`git commit --amend`). `abort` deletes the session workspace — zero
pollution of tracked territory.

After the commit lands, run
`/keel-pipeline F## docs/exec-plans/prds/<slug>.json` when ready —
the JSON PRD written at commit time IS the spec (its `contract` +
`oracle` are what `pre-check` and `test-writer` consume). Nothing
auto-chains. You still choose the order.

**What the drafter will not do:**
- Emit bootstrap-pipeline tasks (F01-F03 are `/keel-setup`'s territory).
  Instead, it returns `status: bootstrap_gap` and routes the human to
  `/keel-adopt` to extend architecture first.
- Write separate spec files. The JSON PRD's per-feature `contract` and
  `oracle` are the spec; no markdown spec stub is created.
- Pick priority. Drafted entries appear in PRD-encounter order; the human
  decides what ships first.
- Modify existing entries. Strict append-only.
- Run the pipeline.
- Transcribe visual tokens (colors, exact spacing, typography) from
  UI design assets. That's `frontend-designer`'s job later in the pipeline.

**Why the marker convention matters.** Pre-check refuses to enter the
pipeline on any entry that still contains `<!-- HUMAN: -->`. The marker
IS the gate between "drafted" and "ready." Pre-check also verifies that
every `Design:` path on a UI entry resolves to a real file (no live
Figma/Miro URLs — committed assets only). Doc-gardener sweeps stale
`<!-- DRAFTED: -->` comments off shipped (`[x]`) entries during the
post-landing pass.

**Format and size caps for pasted/bundled assets:** PNG, JPG, GIF, SVG,
PDF (max 20 pages). Per-file cap 20 MB. Other formats rejected at paste
time with "export as PNG/SVG/PDF and re-paste."

`backlog-drafter` is the first KEEL agent that returns structured YAML
to a skill instead of appending to a handoff file, and `/keel-refine`
is the first KEEL skill that commits on the user's behalf (on explicit
`commit` verb). Both are deliberate — the drafting phase is iterative
and benefits from a chat-based review surface, while the pipeline
retains its PR-based review surface for landed work. See
`.claude/agents/backlog-drafter.md` and
`.claude/skills/keel-refine/SKILL.md` for the full I/O contracts.

## The PRD lifecycle (informal, derived from artifacts)

KEEL does not declare PRD states machine-readably — state is
emergent from artifacts (principle P4: no redundant storage). In
narrative terms:

- **Draft.** A PRD file exists at `docs/exec-plans/prds/<slug>.json`;
  the human is iterating on it. No F## in the backlog reference
  this slug yet.
- **Accepted.** `/keel-refine` committed an F## slate referencing
  the PRD. Phase 5 Step 3 is the explicit human-confirmation
  moment.
- **In-flight.** At least one F## from the slate is in pipeline
  (handoff exists in `docs/exec-plans/active/handoffs/`), HUMAN
  markers are being resolved, specs are being authored.
- **Complete.** Every F## with `PRD: <slug>` is `[x]` in the
  backlog. The PRD file stays at its canonical path (no
  active/completed split — completion is derivable from backlog
  state, not directory location).

No frontmatter field declares these states. Tooling infers them
when needed (`uv run scripts/keel-prd-view.py docs/exec-plans/prds/<slug>.json`
renders a JSON PRD as deterministic markdown for human read-access).

---

## 7. Defining Architecture and Invariants

### The Layered Architecture Pattern

Dependencies flow in one direction only: downward.

```
Layer 5: UI (views, components, templates)
    |
Layer 4: Runtime (supervisors, coordinators, event bus)
    |
Layer 3: Service (stateful processes, orchestration)
    |
Layer 2: Domain (pure logic, derived fields)
    |
Layer 1: Foundation (external interfaces, types, structs)
    |
Layer 0: Config (application config, environment)
```

Enforced by convention early, by structural tests as the project matures.

### Domain-Specific Invariants

Every project has invariants -- correctness constraints, not style preferences.
Define them in `core-beliefs.md`.

**Git operations:**
```
- No git command uses --force
- Pull always uses --ff-only
- Pull rejected when dirty, diverged, not on default branch, or not behind
- Fetch is always safe (no preconditions)
```

**REST API:**
```
- All endpoints return JSON; errors include an "error" key
- No state modification on GET
- Auth required on all endpoints except /health
```

**Data pipeline:**
```
- No step mutates its input
- Failed steps produce dead-letter records, never silent drops
- Schema validation at ingestion, not mid-pipeline
```

### Mechanical Enforcement

| Level | When to add | Example |
|---|---|---|
| Convention | Day one | "Dependencies flow downward" |
| Formatter/linter | After scaffold | Auto-format check (e.g., `prettier --check`, `mix format --check-formatted`) |
| Structural tests | Module layout stabilizes | "No module in src/ui/ imports src/data/" |
| Pre-commit hooks | Patterns emerge | "Tests must pass before commit" |

**Enforce invariants, not implementations.** "Pull must use --ff-only" is an
invariant. "Use this exact shell command with these exact arguments" is an
implementation detail. The agent has freedom in how, not whether.

### Anti-Pattern: Style as Invariants

```
BAD:  "All modules must have a docstring"   (style)
GOOD: "All data-access modules go through the repository interface"  (architecture)
```

---

## 8. Agent Roles and Pipelines

Sixteen specialized agents, each with bounded responsibility.

### Agent Roster

| Agent | Purpose | Input | Output | Never Does |
|---|---|---|---|---|
| **docker-builder** | Build + verify container | core-beliefs, north-star | Docker build report (pass/fail, tool versions) | Modify Dockerfile or compose files |
| **scaffolder** | Project skeleton | Stack, structure spec | Scaffolded project | Write business logic |
| **config-writer** | Test infra, configs, behaviours | Architecture, testing strategy | Config files, helpers | Write feature code |
| **backlog-drafter** | Draft backlog entries from a PRD/prose/bundle (upstream of the pipeline, invoked via `/keel-refine`) | PRD, prose, or pasted images + repo context | Structured YAML proposal with candidate `F##` entries, `Design:` refs, and HUMAN markers | Write specs, emit bootstrap tasks, auto-run the pipeline |
| **pre-check** | Classify intent, evaluate readiness, route pipeline | Backlog entry, PRD JSON | Execution brief with intent, complexity, constraints | Write code or tests |
| **researcher** | Investigate unknowns | Pre-check questions | Research findings | Make design decisions |
| **backend-designer** | Module interfaces, data flow | Resolved JSON, architecture | Signatures, integration points | Write implementation |
| **frontend-designer** | Component hierarchy | Resolved JSON, UI design doc | Component tree, event flow | Write backend code |
| **test-writer** | Failing tests from oracle | Resolved JSON, designer output | Test files (all RED) | Write implementation |
| **implementer** | Code to pass tests | Failing tests, resolved JSON | Implementation (all GREEN) | Modify tests |
| **code-reviewer** | Review code quality | Git diff, architecture | Verdict: APPROVED or CHANGES NEEDED | Modify code |
| **spec-reviewer** | Verify contract + oracle match | Resolved JSON, implementation | Verdict: CONFORMANT or DEVIATION | Modify code |
| **safety-auditor** | Verify invariants | Core-beliefs, implementation | Verdict: PASS or VIOLATION | Modify code |
| **arch-advisor** | Architecture consultation + verification | Resolved JSON, handoff, ARCHITECTURE.md | Guidance (CONSULT) or Verdict: SOUND/UNSOUND (VERIFY) | Modify code |
| **landing-verifier** | Verify completeness | All handoff entries | VERIFIED or BLOCKED | Write code or tests |
| **doc-gardener** | Fix doc drift | All docs, codebase | Updated docs, drift report | Write feature code |

### Reasoning Tiers

Each agent is assigned a reasoning tier based on the cognitive demands of
its task. Tiers map to Claude Code model names in agent frontmatter
(`model:` field).

| Tier | Intent | Claude Code |
|-|-|-|
| **high** | Design decisions, novel work, deep analysis | opus |
| **high, lighter model** | Gate verdicts — pattern matching against existing code or contract/oracle | sonnet (`reasoning: high`) |
| **standard** | Routing, pattern-following, verification | sonnet |

**High reasoning (opus):** pre-check, arch-advisor, implementer, safety-auditor,
backend-designer, frontend-designer, researcher, backlog-drafter (8 agents)

**High reasoning, sonnet:** code-reviewer, spec-reviewer (2 agents) — the
reasoning depth matters, but generation cost does not, because these agents
compare existing code against a contract and oracle rather than authoring
new code.

**Standard reasoning (sonnet):** test-writer, docker-builder, scaffolder,
config-writer, landing-verifier, doc-gardener (6 agents)

### The Four Pipeline Variants

**Bootstrap** (project setup)

```
docker-builder --> landing-verifier --> roundtable-review? --> post-landing
scaffolder     --> landing-verifier --> roundtable-review? --> post-landing
config-writer  --> landing-verifier --> roundtable-review? --> post-landing
```

**Backend** (foundation and service features)

```
pre-check --> roundtable-precheck? --> researcher? --> arch-advisor? --> backend-designer? --> roundtable-review? --> test-writer --> implementer --> code-reviewer --> spec-reviewer --> safety-auditor? --> arch-advisor-verify? --> landing-verifier --> roundtable-review? --> post-landing
```

`?` = conditionally included. Pre-check classifies intent and complexity,
then decides which optional agents run. Designer skipped for trivial features.
Safety-auditor only for domain-critical modules. Arch-advisor runs for
architecture-tier complexity (consultation before design, verification before
landing).

**Frontend** (UI features)

```
pre-check --> roundtable-precheck? --> researcher? --> arch-advisor? --> frontend-designer --> roundtable-review? --> test-writer --> implementer --> code-reviewer --> spec-reviewer --> arch-advisor-verify? --> landing-verifier --> roundtable-review? --> post-landing
```

Frontend-designer always included. No safety-auditor (UI does not execute
domain-critical operations directly). Arch-advisor for architecture-tier only.

**Cross-cutting** (test infrastructure, fixtures)

```
pre-check --> roundtable-precheck? --> test-writer --> implementer --> code-reviewer --> landing-verifier --> roundtable-review? --> post-landing
```

### The Handoff Mechanism

Each feature gets `docs/exec-plans/active/handoffs/F{id}-{feature-name}.md`. The file is
append-only during pipeline execution -- each agent reads the full file, then
appends its output. This preserves context across agent sessions.

Example handoff (from Repo Man):
```markdown
# F13 RepoServer fetch
status: IN_PROGRESS
pipeline: backend
spec_ref: mvp-spec:4.3.1

## pre-check
Intent: build
Complexity: standard
Designer needed: NO
Safety auditor needed: YES
Arch-advisor needed: NO

### Constraints for downstream
- MUST: use Task.Supervisor for async operations (pattern in repo_server.ex)
- MUST NOT: add --force or --rebase flags to any git command

## test-writer
Tests: 5 cases in repo_server_test.exs (all RED)

## implementer
Files: repo_server.ex, application.ex -- Tests: 5/5 GREEN

### Decisions
- Used Task.Supervisor.async_nolink for crash isolation
- Broadcast via Phoenix.PubSub after fetch completes

## spec-reviewer
**Verdict:** CONFORMANT
spec-review-attempt: 1

## safety-auditor
**Verdict:** PASS
No --force, no --rebase. Task.Supervisor isolates crashes.

## landing-verifier
Status: VERIFIED
```

Each agent reads upstream **Decisions** and **Constraints** before starting
its own work. This wisdom accumulation prevents downstream agents from
repeating upstream mistakes or violating upstream design choices.

On completion, move from `active/handoffs/` to `completed/handoffs/`.

### The Orchestrator Role

The human kicks off features. The `keel-pipeline` skill handles the
mechanics: dispatching agents, calling roundtable MCP tools when available,
reading gate verdicts, looping on spec-review/safety/arch-advisor findings
within their ceilings, and running the post-landing procedure automatically —
roundtable review, doc-gardener, handoff archive, commit, push the feature
branch, open a PR. The orchestrator steers; the pipeline executes on their
behalf end-to-end without per-step approval, escalating to the human only
on gate ceilings or via the resulting PR.

**The orchestrator does not write code.** When code quality issues are found
(Step 5), findings are sent back to the implementer agent for fixing. When
spec-reviewer returns DEVIATION or safety-auditor returns VIOLATION, the
orchestrator routes findings to the implementer — it does not fix code itself.

### The Missing Capability Principle

From the OpenAI article: "When something failed, the fix was never 'try
harder.'"

| Failure | Root cause | Fix |
|---|---|---|
| Wrong module structure | No architecture doc | Write ARCHITECTURE.md |
| Misunderstands scope | Vague spec | Rewrite spec with assertions |
| Violates safety rule | Rule not in core-beliefs | Add the rule |
| Inconsistent design | No designer ran | Add designer to pipeline |
| Cannot test its work | No test infrastructure | Run config-writer first |

Build the missing capability. Do not retry the same prompt.

---

## 9. Testing Doctrine

Tests enforce spec conformance, not discover design. Every spec assertion has
a corresponding test. When specs change, tests change first.

### The Six-Layer Model

| Layer | Name | I/O | Speed | Mocking | What It Tests |
|---|---|---|---|---|---|
| 0 | Spec consistency | None | Instant | N/A | Docs agree with each other |
| 1 | Safety invariants | Real | Slow | None | Non-negotiable constraints under real conditions |
| 2a | Integration | Real | Slow | None | External interfaces work correctly |
| 2b | Pure logic | None | Fast | None | Domain computations, derived fields |
| 3 | Service behavior | Mocked | Fast | External deps | Stateful processes, supervisors, orchestration |
| 4 | UI behavior | Mocked | Fast | Service layer | Components render, events dispatch |

**Layer 0: Spec Consistency.** Before writing tests, verify specs agree.
Product spec, design doc, architecture doc, and backlog must describe the same
system.

**Layer 1: Safety Invariants.** First tests written, last deleted. Real I/O
against real systems -- no mocking. Mocking safety tests means testing your
mock, not your safety. Tag `@tag :integration` for conditional execution.

```
Example: create temp repo, set up diverged state, attempt pull, verify:
  - Pull fails cleanly
  - No --force flag used
  - Working tree unchanged
```

**Layer 2a: Integration (Slow).** Real I/O against controlled environments.
Temp dirs, temp repos, real shell commands. Tag `@tag :integration`.

**Layer 2b: Pure Domain Logic (Fast).** No I/O. Data structures, derived
fields, state machines. Millisecond execution. The fast feedback loop.

**Layer 3: Service Behavior.** External deps mocked.
Process serialization, event broadcasts, crash isolation.

**Layer 4: UI Behavior.** Service layer mocked. Component rendering, event
handling, live updates.

### The RED to GREEN Flow

```
test-writer writes tests  -->  all RED
implementer writes code   -->  all GREEN
spec-reviewer verifies    -->  matches spec
```

Test-writer never writes implementation. Implementer never modifies tests.
Tests come from the spec, not the implementation.

### Anti-Pattern: Testing Implementation Details

```
BAD:  "Verify fetch() calls the async helper with exact internal arguments"
GOOD: "Verify fetch() sets state to :fetching, completes async, broadcasts update"
```

Tests should break when behavior changes, not when implementation details
change.

---

## 10. Operating the Pipeline Day-to-Day

### Kicking Off a Feature

1. Identify next unchecked feature in backlog.
2. Verify dependencies are complete.
3. Read referenced spec sections.
4. Create handoff file: `docs/exec-plans/active/handoffs/F{id}-{feature-name}.md`.
5. Run pipeline stages sequentially, reviewing each output.
6. Continue until landing-verifier reports VERIFIED.

### What the Human Does at Each Stage

| Stage | Human action |
|---|---|
| Pre-check | Does intent/complexity classification make sense? Routing correct? |
| Researcher | Are findings relevant? Missing context? |
| Arch-advisor (consult) | Does architecture guidance align with your vision? |
| Designer | Does the design match your mental model? |
| Test-writer | Do tests cover spec assertions? |
| Implementer | Do tests pass? Is code reasonable? |
| Spec-reviewer | CONFORMANT or DEVIATION — do you agree? |
| Safety-auditor | PASS or VIOLATION — comfortable with the analysis? |
| Arch-advisor (verify) | SOUND or UNSOUND — architecture still solid after implementation? |
| Landing-verifier | Is the feature complete? |

At each stage: **proceed**, **redo** (with more context), **fix** (update
spec), or **abort** (rare).

### Structured Verdicts and Loop Control

Gate agents (spec-reviewer, safety-auditor, Arch-advisor) output a structured
`**Verdict:**` as their first line. The pipeline branches on this:

- **Spec-reviewer:** max 2 loops. DEVIATION sends findings to implementer.
  After 2 attempts, escalate to human — decompose the feature or fix the spec.
- **Safety-auditor:** max 3 loops. VIOLATION is never negotiable — fix the code.
  After 3 attempts, escalate — the invariant rule itself may need review.
- **Arch-advisor (verify):** max 1 retry. UNSOUND sends architecture findings to
  implementer, then reruns the full gate sequence (spec-reviewer → safety →
  Arch-advisor). If still UNSOUND, escalate.

See `docs/process/FAILURE-PLAYBOOK.md` for the full decision tree.

### The Commit Ritual

After landing-verifier reports VERIFIED (and roundtable review completes if enabled):

```
1. doc-gardener sweep: apply drift fixes in the working tree
2. Log new shortcuts / check off resolved items in tech-debt-tracker.md
3. Stage files: git add -A (clean tree enforced at pipeline start)
4. Commit: feat(F{id}): {feature name} with verdict table
5. Push the feature branch: git push -u <remote_name> HEAD
6. Open a PR: gh pr create --fill (manual instructions if forge CLI unavailable)
7. Check off feature in backlog
8. Move handoff: active/handoffs/ -> completed/handoffs/ (amended into the feature commit)
```

### Garbage Collection

**When:** After every 5-10 features, after refactoring, at session start, when
an agent produces surprising output.

**Checklist:**

- [ ] CLAUDE.md still accurate?
- [ ] ARCHITECTURE.md matches code?
- [ ] Product spec describes what was built (not what was planned)?
- [ ] Core beliefs still hold?
- [ ] Tech debt tracker updated?
- [ ] Backlog accurate?
- [ ] Completed handoffs archived (active → completed)?

Docs that lie are worse than no docs. A missing doc causes the agent to ask.
A lying doc causes the agent to act on false information with confidence.

### Tech Debt Tracking

Three sections in `docs/exec-plans/tech-debt-tracker.md`:

- **Pre-Implementation** -- spec drift, open questions
- **During Implementation** -- shortcuts, workarounds, deferred bugs
- **Post-MVP** -- improvement opportunities

Each entry: checkbox, date, source, enough context for a future agent.

---

## 11. Increasing Agent Autonomy Over Time

### The Autonomy Progression

| Stage | Human involvement | Agent capability | What enables it |
|---|---|---|---|
| **1. Full review** | Reviews every output | Single pipeline stages | Base pipeline |
| **2. Agent-to-agent review** | Reviews final output only | Spec-reviewer + safety-auditor catch issues | Reliable gate agents over 5-10 features |
| **3. Self-correcting pipeline** | Reviews escalations only | Pipeline diagnoses failures and reroutes | Structured rejection, wisdom accumulation, intent classification |
| **4. Agent end-to-end** | Reviews and merges the resulting PR | Drives features pre-check to PR open, roundtable integration | Mechanical enforcement + Arch-advisor + roundtable review |

### What Enables Each Transition

**1 to 2:** Spec-reviewer and safety-auditor produce reliable verdicts over
5-10 features. Human trusts review agents.

**2 to 3:** The pipeline gains self-correction through three OMA-derived
patterns:
- **Structured rejection:** Gate agents output machine-readable verdicts
  (CONFORMANT/DEVIATION, PASS/VIOLATION). The pipeline branches on these and
  routes failures back to the implementer with specific findings. Bounded
  loops prevent infinite thrashing (max 2 spec-review, max 3 safety).
- **Wisdom accumulation:** Each agent propagates Decisions and Constraints
  downstream through the handoff file. Downstream agents read upstream context
  before starting — preventing repeated mistakes and contradictory choices.
- **Intent classification:** Pre-check classifies work intent (refactoring,
  build, mid-sized, architecture, research) and complexity tier (trivial →
  architecture-tier). This drives routing: trivial features skip the designer,
  architecture-tier features invoke Arch-advisor.

**3 to 4:** The agent orchestrates its own pipeline. Arch-advisor provides
architecture-level verification for complex features. Reads backlog, identifies
next feature, runs stages, reports results. Human reviews in batch.

From the OpenAI article: "single Codex runs work on a task for upwards of six
hours while humans sleep." This is the end state -- but it requires the
mechanical enforcement built in stages 1-3.

### When to Promote Manual Checks to Automated

A check is ready when: performed consistently 5+ times, criteria are objective,
testable without false positives, failure mode understood.

```
Manual:     "No UI module calls external APIs directly"
Automated:  Structural test that fails if API calls found in src/ui/

Manual:     "ARCHITECTURE.md lists all modules"
Automated:  Test comparing module map to actual source files
```

### Guardrails That Enable Autonomy

- [ ] Safety invariants have Layer 1 tests (real I/O)
- [ ] Structural tests enforce layer boundaries
- [ ] Formatter/linter on every test run
- [ ] Spec-reviewer has track record
- [ ] Garbage collection scheduled
- [ ] Tech debt tracker maintained

The goal: move human oversight from "review every line" to "review every
decision." Mechanical enforcement handles correctness. The human handles taste.

---

## Summary

KEEL is a lifecycle, not a checklist. The principles compound: knowledge
boundary feeds spec quality, spec quality feeds test quality, test quality
feeds agent autonomy, agent autonomy feeds development speed.

The minimum viable KEEL setup:

```
1. Write CLAUDE.md (~80 lines, table of contents)
2. Write docs/north-star.md (vision, principles, growth stages)
3. Write one product spec (what to build)
4. Write core-beliefs.md (invariants and testing strategy)
5. Write ARCHITECTURE.md (layers and module map)
6. Define the feature backlog (ordered, dependency-aware)
7. Run the first pipeline
```

Everything else -- handoffs, garbage collection, safety audits, autonomy
progression -- builds on this foundation as the project grows.

The repo is truth. Docs drive code. Coding comes last.
