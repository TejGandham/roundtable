# Quick Start: Your First Afternoon with KEEL

KEEL — Knowledge-Encoded Engineering Lifecycle. From install to first feature through the pipeline.

## Prerequisites

- [Claude Code](https://claude.com/claude-code) — the only supported agent runtime today
- Docker installed (or your stack's runtime)
- Python 3.14+ and [`uv`](https://docs.astral.sh/uv/) on PATH — KEEL's installer halts with a CTA if either is missing; `uv` can install 3.14 for you (`uv python install 3.14`)
- A product idea (even rough is fine)

## How KEEL Grows With Your Project

You don't need everything on day one. KEEL is designed to match the weight
of your project at each stage:

| Stage | What you add | What it unlocks |
|-|-|-|
| **Day 1** | CLAUDE.md + core-beliefs.md | Agent has project context and safety rules |
| **Day 2** | Product spec + ARCHITECTURE.md | Agent can reason about what to build and where it fits |
| **Week 1** | Feature backlog + first handoff files | Structured pipeline execution begins |
| **Week 2+** | Safety-auditor config + domain invariants | Mechanical enforcement of non-negotiable rules |
| **Month 2+** | Full pipeline + garbage collection | Institutional knowledge compounds across features |

Start with whatever you need now. Add the rest as complexity demands it.
The framework catches you when ad-hoc prompting stops scaling.

## The 3 Steps

### 1. Install KEEL

```bash
# New project
mkdir my-project && cd my-project && git init

# Existing project
cd my-project

# Install KEEL
git clone --depth 1 https://github.com/TejGandham/keel.git /tmp/keel
python3 /tmp/keel/scripts/install.py
rm -rf /tmp/keel
```

The installer prompts for project name, stack, and description. It copies
agents, skills, doc structure, and template files into your project,
replaces placeholders, and cleans up instruction comments. It never
overwrites existing files.

### 2. Configure with Claude Code

Open Claude Code in your project directory and run the command the
installer printed:

- **New project (no existing code):** `/keel-setup`
- **Existing codebase:** `/keel-adopt`

Both skills walk you through everything interactively:
- CLAUDE.md refinement (project identity, safety rules, commands)
- North star (vision, growth stages, principles)
- Architecture (layers, modules, data flow)
- Domain invariants (per-item confirmation)
- Safety enforcement and agent configuration

Every phase drafts from context first, then asks you to review.
You confirm at every gate.

### 3. Your first feature via a PRD

1. **Draft the PRD.**

   ```
   /keel-refine "short description of what you're building"
   ```

   Or point at any non-JSON input material (legacy markdown spec,
   bundle directory with wireframes, image, etc.) — `/keel-refine`
   is the conversion hub that turns these into a structured JSON PRD:

   ```
   /keel-refine docs/prds/my-feature.md
   ```

   The skill will draft F## entries, walk them with you card-by-card,
   and commit both the structured JSON PRD file (at
   `docs/exec-plans/prds/<slug>.json`) and the backlog entries when you
   type `commit`.

2. **Pipe each F## independently.**

   ```
   /keel-pipeline F## docs/exec-plans/prds/<slug>.json
   ```

   The pipeline reads the structured JSON PRD only; a `.md` path is a
   routing error (halt with CTA back to `/keel-refine`).

   Repeat for each F## in dependency order.

3. **Review the PRs.**
   One PR per F##. Merge in dependency order.

## What Happens Next

After bootstrap lands, the pipeline becomes your daily workflow:
1. Pick next feature from backlog
2. Run `/keel-pipeline F{id} docs/exec-plans/prds/<slug>.json`
3. Watch the pipeline — it runs end-to-end, self-corrects at gates, and stops in-session only on escalation
4. Review the resulting PR — the pipeline archives the handoff, commits, pushes the feature branch, and opens a PR on your forge for you to review and merge

When the pipeline stalls, see [FAILURE-PLAYBOOK.md](FAILURE-PLAYBOOK.md) for the decision tree.

See [THE-KEEL-PROCESS.md](THE-KEEL-PROCESS.md) for the comprehensive guide.
