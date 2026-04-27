# North Star: KEEL



KEEL — Knowledge-Encoded Engineering Lifecycle.
Adapted from [OpenAI's harness engineering article](https://openai.com/index/harness-engineering/).
This document defines where we're heading — not where we are today.

## The Principle

Humans steer. Claude executes. The repo is Claude's workspace, context, and
system of record. Everything Claude needs to make decisions lives here.

## Framework Principles

KEEL operates under seven principles anchored at
[`docs/process/KEEL-PRINCIPLES.md`](process/KEEL-PRINCIPLES.md)
(copied into this project by `install.py`). Every agent and skill
KEEL ships references these:

- **P1. Agent Legibility** — repo optimized for agent comprehension, not human aesthetics
- **P2. Progressive Disclosure** — small stable entry points, navigate deeper on demand
- **P3. Self-Sufficient Snapshot** — repo's current state is enough to reconstruct any view
- **P4. No Redundant Storage** — files author unique content; derivable data isn't stored
- **P5. Snapshot, Not Timeline** — `git log` has evolution; repo reflects what is
- **P6. Code/Specs/Backlog Win** — when artifacts disagree, lower-level wins
- **P7. Halt with Call-to-Action** — gates that block must emit actionable next-step messages

## Three-Tier Process (features and PRDs)

|Tier|Unit|Property|Owner|
|-|-|-|-|
|Product design|**PRD**|Cohesion — why these features belong together|Human (product owner) writes|
|Implementation work|**F##**|Isolation — smallest testable unit|Human accepts the slate once per PRD, authors each spec|
|Execution|**Pipeline**|Per-F## (unchanged)|KEEL runs autonomously|

A PRD bundles cohesive F## entries for one product slice. Each F## retains isolation (independent spec, pipeline, PR). `/keel-refine` is the PRD-scope drafting gate; each F## then pipes independently.

**Invariant 7:** every F## traces to a PRD via `PRD: <slug>` pointing to `docs/exec-plans/prds/<slug>.json` (structured JSON PRD, schema v1), or declares `PRD-exempt: <reason>` where reason ∈ `{legacy, bootstrap, infra, trivial}`.

## What We Adopt (Fully)

**Repository = system of record.** If it's not in the repo, it doesn't exist
to Claude. Slack discussions, verbal decisions, tacit knowledge — all must be
encoded as markdown, code, or config in this repo.

**CLAUDE.md as table of contents.** ~80 lines, pointers to deeper docs. Not an
encyclopedia. Teaches Claude what this project is and where to look next.

**Progressive disclosure.** CLAUDE.md → ARCHITECTURE.md → specs → plans.
Claude reads what it needs when it needs it.

**Plans as first-class artifacts.** Active plans in `exec-plans/active/`,
completed plans in `exec-plans/completed/`. Progress and decisions logged
in the plan itself.

**Agent legibility is the goal.** Docs are written for Claude's comprehension,
not for a human audience. Clear, scannable, with explicit cross-references.

## What We Adapt (Scaled Down)

<!-- CUSTOMIZE: What parts of harness engineering do you adapt for your scale?
     Examples:
     - Mechanical enforcement: start with formatter + tests, add structural tests later
     - Garbage collection: manual review at session boundaries, automated sweeps later
     - Agent review: self-review before presenting to human
     - Observability: start with stdout/stderr, add structured logging later -->

**Mechanical enforcement.** [YOUR APPROACH]

**Garbage collection.** [YOUR APPROACH]

**Agent review loops.** [YOUR APPROACH]

**Observability stack.** [YOUR APPROACH]

## What We Skip (For Now)

<!-- CUSTOMIZE: What do you skip until the project matures? -->

- [THING TO SKIP AND WHY]

## Target Folder Structure (Fully Realized)

```
CLAUDE.md                           # ~80 lines, table of contents
ARCHITECTURE.md                     # Process model, layers, module map
Dockerfile                          # Dev container
docker-compose.yml                  # Orchestration

docs/
├── north-star.md                   # This document
├── product-specs/
│   └── [YOUR-SPEC].md
├── design-docs/
│   ├── core-beliefs.md             # Golden principles + testing strategy
│   └── [DESIGN-DOCS].md
├── exec-plans/
│   ├── active/                     # Plans being executed
│   │   ├── feature-backlog.md      # Canonical F## registry
│   │   └── handoffs/
│   ├── completed/                  # Finished plans
│   │   └── handoffs/
│   ├── prds/                       # Product requirements docs (one per feature set)
│   └── tech-debt-tracker.md        # Known shortcuts
├── references/                     # External docs, llms.txt files
└── process/                        # KEEL process docs (from kit)

[PROJECT_DIR]/                      # Your application source
├── [SOURCE]/                       # Business logic
├── [TESTS]/                        # Tests
└── [CONFIG]/                       # Configuration
```

## Growth Stages

| Stage | Trigger | KEEL Additions |
|-------|---------|----------------|
| **0: Foundation** | Before first code | Folder structure, CLAUDE.md, ARCHITECTURE.md, core-beliefs, Docker |
| **1: First Code** | Core module works | Tech debt updates, formatter checks |
| **2: Working App** | App renders/serves | Quality tracking, structural tests for module coverage |
| **3: MVP Complete** | All success criteria met | Move plans to completed/, garbage collection pass |
| **4: Post-MVP** | New features | New plans, periodic doc review, consider pre-commit hooks |

## The Four Loops

### 1. Validation Loop
```
Claude writes code → runs tests → checks output →
fixes failures → re-runs → repeats until green
```
<!-- CUSTOMIZE: Add your validation tools (e.g., LiveView test helpers, Playwright, Cypress) -->

### 2. Knowledge Boundary
```
┌─────────────────────────────────┐
│    What Claude CAN see          │
│  Code, markdown, schemas,      │
│  exec plans, tests, configs    │
└─────────────────────────────────┘
        ↑ must encode ↑
┌─────────────────────────────────┐
│   What Claude CAN'T see        │
│  Slack, verbal decisions,      │
│  your head, Google Docs        │
└─────────────────────────────────┘
```

### 3. Layered Architecture
<!-- CUSTOMIZE: Replace with your project's layer diagram -->
```
[UI Layer]
      ↓
[Runtime / Service Layer]
      ↓
[Foundation / Core Layer]
```
Dependencies flow strictly downward.

### 4. Garbage Collection
After each implementation chunk:
- Re-read CLAUDE.md — still accurate?
- Re-read ARCHITECTURE.md — still matches code?
- Update tech-debt-tracker with new shortcuts
- Fix any docs that lie
