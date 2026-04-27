# Roundtable feedback from a KEEL framework session

Source: KEEL framework dev session, 2026-04-27. Single roundtable
deliberate dispatch on a real design question (KEEL adherence-enforcing
language for client repos), 7 panelists answered.

This is operator feedback on dispatch *technique* and panel *output
shape* — what made this dispatch effective, where the surface could
help orchestrators converge faster, and what's still left to manual
synthesis.

## What worked — keep doing this

### 1. Neutral framing, no anchor

Prompt opened with "operator feedback says X; design Y." It did **not**
say "I think placement should be option (c)." Result: 7 panelists, 4
distinct placement votes (a, b, c, d), genuine disagreement.

Anchor-free framing is what turns roundtable into a divergence-then-
synthesis tool instead of an echo chamber. If the orchestrator already
has an answer, roundtable is the wrong tool — use it when there are
genuine open sub-questions.

> Suggestion: surface this in `roles/`/the dispatch prompt — "do not
> name your preferred answer in the prompt; the panel's value is in
> the divergence."

### 2. Numbered sub-questions

Five labeled sub-questions (placement, mechanism, pressure handling,
halt-with-CTA, failure modes). Each panelist addressed each one.
Without them, panels often diverge into different aspects of a question
and become hard to compare.

> Suggestion: add a built-in convention in the dispatch prompt
> template — "if your question has multiple aspects, number them. The
> panel will return parallel structured answers, which is what makes
> synthesis tractable."

### 3. Bounded deliverable shape

"Each panelist: produce (placement choice from a-d), ready-to-paste
markdown, confidence rating, strongest counter-argument against your
own recommendation." This made comparison mechanical — read seven
"placement choices" first, count votes, then read the deliverables
of the consensus winners.

The "strongest counter-argument against your own recommendation"
clause is the most underrated dispatch technique I've found. It
forces calibrated confidence and surfaces the exact failure mode the
orchestrator should plan for.

> Suggestion: bake "strongest counter-argument against your own
> recommendation" into the default `planner` / `analyst` role prompt.

### 4. Explicit invitation to disagree

"Disagree with each other where you genuinely disagree — that's the
value of this canvass." Two panelists explicitly cited each other's
positions and refuted them. Without that line, panelists tend to
converge to the median preemptively.

## Friction points — where the surface could help

### A. Output volume

Single deliberate dispatch returned ~60KB across 7 panelists. For a
single open design question, that's 7 × ~8KB of opinion. Orchestrator
spends 5-10 minutes reading. Some friction:

1. No way to ask the panel to *converge* in a follow-up. After reading
   the seven, the orchestrator manually identifies majority placement,
   then manually picks best-of-class language from each. A
   `roundtable-converge` mode that ingests the prior dispatch's output
   and asks each panelist "given your peers' positions, would you
   change your vote? what's the single best draft to converge on?"
   would offload synthesis.

2. No structured output by default. Each panelist returned freeform
   markdown — different headings, different ordering, different
   verbosity. A `--schema` parameter ("each panelist returns
   `{placement: a|b|c|d, deliverable: markdown, confidence: low|med|
   high, counter_argument: text}`") would make `jq`-style extraction
   trivial.

> Suggestion: add a `--schema` param accepting JSON-Schema-lite, plus
> a `roundtable-converge` tool that takes the prior dispatch ID and
> asks the panel to produce a single recommended answer.

### B. Token budget per panelist not visible

Some panelists wrote long multi-section essays; others wrote tight
3-section answers. No way for the orchestrator to ask "tighter" or
"longer" per panelist. Adding `max_tokens_per_panelist` would let
orchestrators dial output volume to the question's stakes.

### C. Cross-panelist citation is rare

Two panelists referenced each other's positions; five did not. With
parallel dispatch, panelists run independently and can't see each
other's work. A two-pass mode (canvass first → each panelist sees
peers' answers and writes a final stance) would produce richer
synthesis with one extra round-trip. Some questions warrant this;
most don't. Make it opt-in.

> Suggestion: `--mode=two-pass` parameter. Pass 1 = independent
> answers (current behavior). Pass 2 = each panelist receives the
> redacted peer answers and produces "final stance + which peers I
> agree with" in one short response.

### D. No way to lock in panelist roles per dispatch

The default `analyst`/`planner`/`codereviewer` roles are fine, but on
adversarial design questions I've wanted heterogeneous roles in one
dispatch — e.g., one panelist as "skeptic," one as "advocate," one as
"synthesizer." Currently all panelists share a role.

> Suggestion: `--roles={"claude": "skeptic", "codex": "advocate", ...}`

### E. The "strongest counter-argument" prompt isn't surfaced

The technique is invisible to a first-time orchestrator. It would help
if the dispatch prompt template had a comment block like:

```
# Effective dispatch patterns:
# - Do not anchor with your preferred answer.
# - Number sub-questions for parallel comparison.
# - Ask for "strongest counter-argument against your own recommendation".
# - Invite disagreement explicitly.
```

…rendered as an in-tool hint when the orchestrator dispatches without
those patterns present.

## Concrete observation — KEEL-shaped framing

This dispatch ran in the KEEL framework repo. The prompt referenced
KEEL's seven principles (P1-P7) as constraints. Panelists understood
the constraints and respected them in their drafts (no panelist
proposed a placement that violated P4-no-redundant-storage; one
flagged that "belt-and-braces" violates P4 even though they recommended
it). Suggesting that domain-constraint-aware dispatch is feasible —
adding a `--constraints` param that orchestrators populate from their
project's principles file would make panel output more usable in
constrained projects.

## Summary for roundtable maintainers

- **Keep:** anchor-free framing, numbered sub-questions, bounded
  deliverable shape, "strongest counter-argument" technique.
- **Add:** structured-output schema param, two-pass mode for
  cross-panelist visibility, `roundtable-converge` follow-up tool,
  per-panelist role override, in-tool hints surfacing effective
  dispatch patterns.
- **Lower priority:** per-panelist token budget, constraints param.
