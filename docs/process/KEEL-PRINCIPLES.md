# KEEL Framework Principles

Canonical source. These principles govern every framework-level
decision in KEEL — agent prompts, skill semantics, file layouts,
validator rules, and everything that ships to user installs. A
proposed change that violates any of these is the change to revisit,
not the principle.

User projects will inherit these principles as operating constraints
for their own KEEL-driven development.

---

## The two foundational principles

From OpenAI's harness-engineering article, ported directly. See
`docs/process/OPENAI-FOUNDATIONS.md` for the source experiment and
how KEEL adapts these.

### P1. Agent Legibility is the Goal

**The repo is optimized for agent comprehension, not human
aesthetics.**

Every tree layout, every file naming choice, every comment, every
metadata field is judged on: *can an agent, landing cold with no
prior session context, understand this?* Human ergonomics are a
welcome side-effect — not the decision axis. When the two conflict,
agent legibility wins.

### P2. Progressive disclosure

**Agents start with a small, stable entry point and navigate to
deeper sources as needed.**

The entry point (CLAUDE.md for user projects, AGENTS.md for
framework contributors) is a table of contents, not an encyclopedia.
Every deeper source is reachable in O(1) navigation from the entry
point or from a stable predecessor. Agents should never have to
guess where to look next.

---

## The four corollaries

These extend P1 and P2 into specific operational rules. Violating
any of them violates agent legibility or progressive disclosure.

### P3. The repo is self-sufficient

**The repo's current state is always sufficient to reconstruct any
view — PRD, architecture doc, design summary, dependency graph, or
anything else derivable from authored state.**

A reconstruction tool (synthesizer) must be *possible* at any
moment. It does not have to be *implemented* on day 1. The design
must leave all the raw material in the repo so that year-3 Claude,
landing cold, can reconstruct any view needed.

### P4. No redundant storage

**Stored files are authoritative ONLY for content they uniquely
author. Anything derivable from other repo state shouldn't be stored
redundantly — if it is, it's a cache that can stale.**

Concrete examples:
- PRD files do not carry `feature_ids: [...]` manifests. That list
  is derivable from the backlog via `grep "PRD: <slug>"`.
- PRD files do not declare `state: accepted` or lifecycle fields.
  State is emergent from whether F## entries reference the PRD and
  whether they're `[ ]` or `[x]`.
- Directory structure does not encode completion status. A
  `completed/` folder that mirrors `active/` is a cache of `[x]`
  markers in the backlog.

When in doubt, ask: *can this be derived from other repo state?* If
yes, don't store it.

### P5. The repo is a snapshot, not a timeline

**History isn't a concern of the repo. `git log` has the evolution;
the repo-as-snapshot doesn't need to narrate how it got here.**

No "changelog" field on artifacts. No "last modified" timestamps
baked into content. No "was originally X, then Y, now Z" commentary
in docs. The repo reflects *what is*, not *how it got here*. If the
reader needs history, they run `git log`.

This principle unlocks principle P4 — much of what contributors
might otherwise want to store redundantly is actually history
masquerading as state.

**Concretely forbidden patterns.** Any of these in a tracked
markdown file is a P5 violation and must not land:

- Strikethrough-landed lists: `~~**item**~~ (landed abc1234)`, or
  a "Remaining work" list where completed items are struck through
  with commit SHAs appended. When an item lands, it leaves the
  list — git log holds the landing record.
- Retroactive notes: "Note (YYYY-MM-DD): this has since been
  closed", "as of <date>", "since the initial write-up". If the
  doc's content is stale, fix the content; don't bolt a patch on
  top narrating the drift.
- Commit SHAs in prose: "fixed in commit abc1234", "see f00b4r7
  for the rationale". The doc should describe what IS; readers
  who need rationale use `git log` / `git blame` against the
  lines in question.
- Timestamped status lines: "**Status:** direction accepted
  2026-MM-DD", "Done 2026-MM-DD", "Landed 2026-MM-DD". If the
  direction is current, just state the direction; if the direction
  was superseded, update the doc to the current direction.
- Progress-log sections: "## Resolved", "## Done", "## Changelog",
  "## History". Resolved items disappear; they don't accumulate
  in a section.
- "Forthcoming" / "will land" / "pending" references to work
  already in the repo. When the work lands, remove the promise.

**Archival docs use dated filenames.** Historical design reviews,
snapshots, and retrospectives that are intrinsically dated go
under `docs/design-docs/YYYY-MM-DD-<slug>.md` (or the equivalent).
The dated filename IS the snapshot timestamp — readers know the
doc is an archival record from its filename alone, and no
in-content dating or retroactive annotation is needed. Internal
references to other archival docs by their dated filenames are
fine; they are pointers to identified artifacts, not timeline
annotations.

### P6. Code, specs, and backlog win

**What delivered code, committed specs, and the backlog say is true
trumps any document that says otherwise. If a PRD claims F12 ships
something but F12's spec/code shipped something different, the
spec/code win — the PRD is stale or wrong.**

This establishes the authority hierarchy when artifacts disagree.
The validator and agents resolve conflicts by trusting the
lowest-level artifact that carries the disputed fact:

| Conflict | Winner |
|-|-|
| PRD narrative vs. backlog state | Backlog |
| Backlog entry vs. spec content | Spec |
| Spec vs. delivered code | Code |
| Delivered code vs. test outcomes | Test outcomes |

A PRD claiming a feature that was never implemented is a stale PRD,
not a missing feature. The validator and agents flag this but do
not "correct" the upstream artifact by inventing work.

---

## The execution principle

### P7. Halt with call-to-action

**Pipeline halts are features, not bugs, when paired with specific
actionable next-step messages. Silent halts and blind continuation
are the failure modes to avoid.**

When any gate, validator, agent, or skill cannot proceed, it must:

- **Halt explicitly.** Not silently exit. Not fall through to the
  next stage. Not invent a recovery path.
- **Emit a specific, actionable message** telling the next actor
  (usually the human) exactly what to do to unblock. Not just
  *"validation failed"* — a *concrete* next step.
- **Name the specific cause.** Not *"invalid state"* — the exact
  field, entry, or condition.

Examples of correct halts:

> *"Feature F11 has no `PRD:` or `PRD-exempt:` field, and the
> KEEL-INVARIANT-7 cutoff is F10. Add `PRD: <slug>` pointing to an
> existing PRD at `docs/exec-plans/prds/<slug>.json`, or add
> `PRD-exempt: <reason>` where reason is one of legacy, bootstrap,
> infra, or trivial."*

> *"Walk complete. 8 cards drafted for PRD: user-password-auth. Are
> all F08-F15 from this PRD ready? Verbs: commit, revisit F##,
> abort."*

> *"F26 has 2 unresolved HUMAN markers blocking the pipeline.
> Resolve them in `feature-backlog.md` by editing the entry, then
> re-run `/keel-pipeline F26`. See marker-resolution workflow in
> CLAUDE.md."*

Examples of failures the principle forbids:

- Exiting a skill without a final message because "the user can
  figure out what to do."
- Continuing past a failed validation "to be helpful."
- Retrying a failing operation in a loop without new information.
- Guessing at a recovery path (e.g., auto-creating a missing PRD
  file with a placeholder).

The design precedent: `testbed-feedback/post-walk-state-skipped.md`
documented the ParchMark `/keel-refine` orchestrator silently
skipping Phase 5 Step 3 — fallthrough instead of halt. The fix is
structural: Step 3 is a required verb, and halting there is the
correct behavior.

---

## Using these principles

### In framework-level decisions

Every design conversation around KEEL edits — new agents, new
skills, new validator rules, new file conventions — must weigh the
proposed change against these principles. If the change violates
one:

1. Restate the problem. Often the original frame is the issue, not
   the design.
2. Check if a sibling design satisfies the principle without losing
   the motivation.
3. If no sibling satisfies the principle, the principle wins.
   Principles override convenience.

Maintainers running roundtable deliberations should paste the
principles into the prompt so the panel attacks proposals under the
same framing.

### In agent and skill prompts

Agents that make decisions about storage, derivation, or halt
behavior should reference this doc in their prompt headers:

- `pre-check` — applies P6 (authority hierarchy) and P7 (halt-with-CTA)
  on every gate.
- `backlog-drafter` — applies P4 (no redundant storage) when deciding
  what goes in drafted entries vs. derived.
- `safety-auditor` — applies P6 (artifact authority) when reconciling
  drift between specs and code.
- `doc-gardener` — applies P4 and P5 when sweeping for drift.

Skills that halt conditionally — `/keel-refine`, `/keel-pipeline`,
`/keel-adopt` — should reference P7 in their halt semantics: every
exit path must produce an actionable message.

The canonical reference path in every user install is
`docs/process/KEEL-PRINCIPLES.md`. It ships via `PROCESS_DOCS` in
`scripts/keel_manifest.py` and is copied by `scripts/install.py`.

### When principles conflict

They are designed to be orthogonal. If a real conflict arises:

- **P1 (legibility) + P4 (no redundant storage) conflict** when
  making a fact self-legible would require storing it redundantly.
  **Resolution:** prefer the form that is legible *given P2
  (progressive disclosure)*. Agents can follow a pointer cheaply;
  caches stale expensively. Pointers over inline redundancy.
- **P5 (snapshot, not timeline) + P3 (self-sufficient) conflict**
  when a view requires knowing when something happened.
  **Resolution:** the view is a timeline view, not a snapshot
  view, and belongs in `git log` output, not in the repo snapshot.
- **P6 (artifact authority) + P4 (no redundant storage) conflict**
  when an artifact encodes a fact that's derivable from a
  lower-authority one. **Resolution:** the lower-authority
  derivation wins, per P6 itself. The higher-authority artifact is
  storing a cached assertion.

Conflicts are rare. Most apparent conflicts are false — one frame
satisfies all principles; finding it is the design work.

---

## Framework fidelity

These principles are the framework's own contract. If a KEEL
maintainer proposes a change to the framework itself (to agents,
skills, validators, scripts, or this file), that change goes through
the same principles-governed design process. The framework
dogfoods its own rules.

This doc is stable. Major edits to the principles require roundtable
review (per `NORTH-STAR.md` §"Principles for Framework Development"
item 2) and a note in `git log` explaining the shift. Minor edits
(clarifying examples, fixing typos) can land directly.
