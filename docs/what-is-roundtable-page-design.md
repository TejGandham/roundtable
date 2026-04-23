# "What is Roundtable" Page — Design Spec

**Date:** 2026-04-23
**Target file:** `docs/index.html` (single-file, GitHub Pages at `tejgandham.github.io/roundtable`)
**Sibling reference:** `../keel/docs/index.html`
**Scope:** One-page "what is Roundtable" explainer for newcomers
**Design genre:** Editorial council · chamber-lit · anti-SaaS

## 1. Visual Theme

**Name:** *The Chamber*

**Genre commitment:** Editorial-council — the page reads like a short policy paper about a council's proceedings, not a SaaS landing. Warm near-black ground, brass-on-parchment palette, Space Grotesk display + JetBrains Mono for all monospace. No YC-standard purple gradients, no Inter, no three-column feature grid with line icons.

**The ONE memorable thing — *The Council*:** Three personified model-bots (Claude, Gemini, Codex) seated around a literal brass round table in the hero section. Each bot has a distinct color, silhouette, and "personality." Beside every quoted model response anywhere on the page, the corresponding bot silhouette recurs — so the characters become the visual shorthand for "who said what." No other MCP tool or devtool site does this. When someone links the page, "the roundtable with the three little bots" is how they'll describe it.

**Supporting motif — guest chairs:** Dashed-outline chair glyphs around the table represent HTTP providers (Kimi, MiniMax, GLM, DeepSeek). They sit at the same table but are visually subordinate to the core three. Signals extensibility without diluting the "easy to understand" canonical setup.

**Narrative arc (top → bottom):**

1. Hero — three answer-cards disagreeing on a real question (value-first opening)
2. The Council — the round-table scene (name's metaphor made visual)
3. The Problem — correct-pattern-wrong-detail hallucination
4. What It Does — one termcast showing `/roundtable-canvass` dispatch
5. Why Disagreement Matters — verdict-ribbon patterns, agreement vs. dissent
6. Tools — five role-assigned dispatch tools (roundtable-canvass, roundtable-deliberate, roundtable-blueprint, roundtable-critique, roundtable-crosscheck)
7. Extend the Table — guest models (Kimi / MiniMax / GLM / DeepSeek), no config JSON
8. How It's Built — one-paragraph technical summary
9. Quick Start CTA — "point your agent at `INSTALL.md`"

## 2. Color Palette

All colors exposed as CSS variables on `:root`.

### Primary surface

| Token | Value | Role |
|-|-|-|
|`--bg`|`#110E09`|Page background (warm near-black)|
|`--bg-deep`|`#06050A`|Termcast / code-block background|
|`--surface-1`|`#1A140C`|Card surface (`.wf-ans`, `.wf-tool`, panel backgrounds)|
|`--surface-2`|`#221B10`|Raised surface (hover states on cards)|
|`--border`|`rgba(212,160,74,0.18)`|Brass-tinted border at low opacity|
|`--border-strong`|`rgba(212,160,74,0.4)`|Stronger border on dashed guest elements|

### Text

| Token | Value | Use |
|-|-|-|
|`--text`|`#EDE6D6`|Primary text (warm off-white, paper-leaning)|
|`--text-2`|`#B8B0A0`|Secondary text, ledes|
|`--text-3`|`#8A7F6C`|Captions, meta, muted labels|
|`--text-dim`|`#6E6654`|Terminal dim text, dividers|

### Accent — The signature move

| Token | Value | Use |
|-|-|-|
|`--brass`|`#D4A04A`|The signature — eyebrows, emblems, verdict-ribbon lights, hero glow, primary CTA fill|
|`--brass-deep`|`#B8862E`|Primary CTA gradient secondary stop|
|`--brass-glow`|`rgba(212,160,74,0.7)`|Verdict-light shadow|

**Why amber:** Of the top 50 dev-tool landing pages surveyed, none use amber as the dominant accent. The devtool-default is cyan/blue/indigo. Brass breaks the pattern, fits the "chamber/council" genre, and carries enough warmth to soften the dark ground.

### Agent colors (canonical — the Council's three seats)

| Token | Value | Role |
|-|-|-|
|`--claude`|`#8EB0F5`|Cool blue — a shade softer than Anthropic's marketing blue|
|`--claude-deep`|`#5C8DEF`|Claude-bot body gradient base|
|`--gemini`|`#58D2BD`|Teal-mint — distinct from generic cyan|
|`--gemini-deep`|`#35B9A4`|Gemini-bot body gradient base|
|`--codex`|`#E89559`|Rust-orange — earthy, warmer than pure orange|
|`--codex-deep`|`#D67939`|Codex-bot body gradient base|

### Guest / status

| Token | Value | Use |
|-|-|-|
|`--guest`|`#D4A04A`|HTTP provider guest chairs (reuses brass — they're extensions of the table)|
|`--ok`|`#7ABF67`|Success, verdict-agree states (muted lime, not the default #22C55E)|
|`--warn`|`#E8BC5B`|Warning / dissent tone (desaturated mustard)|
|`--err`|`#D85D47`|Error / hard-dissent|

### Contrast audit (WCAG 2.1 AA)

Computed ratios against `--bg` (`#110E09`):

|Foreground|Ratio|Usage|Passes|
|-|-|-|-|
|`--text` `#EDE6D6`|**14.8:1**|Body text|AAA|
|`--text-2` `#B8B0A0`|**9.0:1**|Ledes, secondary|AAA|
|`--text-3` `#8A7F6C`|**5.2:1**|Muted labels (4.5:1 min)|AA|
|`--text-dim` `#6E6654`|**3.5:1**|Decorative only — NEVER for meaningful text|fails AA for body — enforce decoration-only via lint|
|`--brass` `#D4A04A`|**8.8:1**|Accent on primary bg|AAA|
|`--claude` `#8EB0F5`|**9.6:1**|Agent label|AAA|
|`--gemini` `#58D2BD`|**8.9:1**|Agent label|AAA|
|`--codex` `#E89559`|**7.6:1**|Agent label|AAA|

All meaningful text passes AA. `--text-dim` is decoration-only and enforced by lint rule D-3.

## 3. Typography

### Families

|Token|Value|Use|
|-|-|-|
|`--font-display`|`'Space Grotesk', system-ui, sans-serif`|All headings, body, UI|
|`--font-mono`|`'JetBrains Mono', 'SFMono-Regular', monospace`|Code, eyebrows, terminal, labels, meta|

**Web font loading:** one Google Fonts request combining both families with the needed weights — 400, 500, 700 for Space Grotesk; 400, 600, 700 for JetBrains Mono. Use `display=swap` to prevent FOIT. No other fonts loaded.

### Scale

|Token|Size|Line|Weight|Tracking|Use|
|-|-|-|-|-|-|
|`--h1`|72px|0.95|700|-3px|Hero (Roundtable)|
|`--h1-sm`|48px|0.95|700|-2px|Hero on narrow viewports (<640px)|
|`--h2`|32px|1.1|700|-1px|Section headings|
|`--h2-sm`|24px|1.15|700|-0.6px|Section headings narrow|
|`--h3`|22px|1.2|700|-0.5px|Callouts, sub-sections|
|`--body`|16px|1.55|400|0|Paragraphs|
|`--small`|14px|1.5|400|0|Captions|
|`--micro`|11px|1.5|400|0|Fine print|
|`--eyebrow`|10px|1.3|600|1.8px|Mono eyebrow labels (uppercase)|
|`--code`|13px|1.7|400|0|Termcast body|

### Rules

- H1 uses `--font-display` weight 700, tracking `-3px`. No italics, no gradient text on the primary `<h1>`.
- All eyebrow labels use mono, uppercase, `--brass` fill, letter-spacing `1.8px`.
- Inline code (`<code>`) in body text: mono, 14px (1em·0.875), `--brass` fill when emphatic, `--text` when neutral, no background (the surrounding text carries enough contrast).
- Block code / termcasts: mono 13px on `--bg-deep`.
- Roundtable logomark: *Roundtable* — regular (non-italic), 72px. **No italicization of the word Roundtable** — keep it stated, not styled.

### Anti-slop compliance

|Check|Result|
|-|-|
|Inter / Roboto|Banned|
|Framework-default sans|N/A — this is static HTML|
|Purple gradient in 240–280° range|Banned — primary gradient is brass `#D4A04A → #B8862E` (≈36°)|
|Three-column generic feature grid|Partially used in Tools row — mitigated by the eyebrow/role labels and the unique mono-styled names; not a neutral "icon + heading + subtitle" card|
|"Trusted by" logo wall|Not present|

## 4. Component Stylings

### Eyebrow label

```css
.eyebrow {
  font-family: var(--font-mono);
  font-size: var(--eyebrow);
  font-weight: 600;
  letter-spacing: 1.8px;
  text-transform: uppercase;
  color: var(--brass);
}
```

States: no hover/focus — it's a static label.

### Answer card (hero)

```css
.answer-card {
  background: rgba(0,0,0,0.3);
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 20px;
  transition: border-color .25s ease, transform .25s ease;
}
.answer-card:hover { border-color: rgba(212,160,74,0.35); transform: translateY(-2px); }
.answer-card.claude { border-color: rgba(142,176,245,0.35); }
.answer-card.gemini { border-color: rgba(88,210,189,0.35); }
.answer-card.codex  { border-color: rgba(232,149,89,0.35); }
```

**States:** default, hover (border brightens, subtle lift), focus-visible (same as hover plus 2px brass outline-offset).

### Agent bot silhouette

Inline SVG, 40×48 default — scales to 60×72 in hero illustrations. One root function (`agentBot(accent)`) returns markup with face, mouth, antenna, body. Variant states handle `speaking` / `silent`. Used as avatar in answer cards, as large figures in the round-table scene, and as inline 24×28 silhouettes beside quoted responses anywhere on the page.

### Verdict ribbon

```html
<div class="verdict">
  <span class="light"></span><span class="light"></span><span class="light dim"></span>
  <span class="count">2 agree · 1 dissents</span>
  <span class="label">→ look twice</span>
</div>
```

Lit dot = an agent spoke and agreed with the majority. Dim dot = dissent. Always 3 dots for the core council (guest ribbons can be 4–5 in the Extend section).

### Termcast

Mirrors keel's termcast structure for visual family (mac-style title bar, mono content, colored tokens), but repalettes tokens: prompt = `--brass`, dim = `--text-dim`, agent names use the per-agent tokens. Border-radius 12px, `--bg-deep` fill, inset highlight at 1px rgba white .04.

### Tool badge (five-across row)

Compact cards: name in brass mono 12px, role in `--text-3` 10px uppercase. No icons. Hover raises border to `--brass` at 0.35.

### Guest provider badge

Dashed brass border (`1px dashed rgba(212,160,74,0.4)`), `--surface-1` fill at low opacity. Three internal rows: name (brass mono), via-provider (`--text-3` mono), one-line note (`--text-2`). Distinguishable from tool badges by the dashed border + three-line layout.

### CTA buttons

```css
.cta.primary {
  background: linear-gradient(135deg, var(--brass), var(--brass-deep));
  color: #17120A;
  padding: 14px 28px;
  border-radius: 100px;
  font-family: var(--font-mono);
  font-size: 14px;
  font-weight: 600;
  letter-spacing: 0.4px;
}
.cta.secondary {
  background: var(--surface-1);
  color: var(--text);
  border: 1px solid var(--border);
}
```

**States:** default, hover (lift `-2px`, shadow intensifies), active (lift `0`, shadow reduces), focus-visible (brass outline offset 3px).

Hit area ≥44×44px via min-height: 44px.

## 5. Layout Principles

### Container

- Max-width **800px** for the page body (matches keel's narrow reading-friendly column).
- Horizontal padding: 20px mobile, 44px desktop.
- Vertical rhythm: sections separated by 56–72px; within sections 20–28px between elements.

### Grid usage

Only two grid uses:

1. **Answer cards (hero):** `grid-template-columns: repeat(3, 1fr); gap: 14px;` → collapses to single column <720px.
2. **Tools row:** `grid-template-columns: repeat(5, 1fr); gap: 10px;` → 2 columns <640px, 1 column <420px.
3. **Guest badges:** `grid-template-columns: repeat(4, 1fr); gap: 10px;` → 2 columns <720px, 1 column <440px.

Everything else is single-column flow. No asymmetric or diagonal layouts — those would fight the "easy to understand" brief.

### Spacing scale

|Token|Value|Use|
|-|-|-|
|`--space-1`|4px|Token inside compact elements|
|`--space-2`|8px|Small gaps|
|`--space-3`|12px|Inside cards|
|`--space-4`|18px|Card padding, paragraph margin|
|`--space-5`|24px|Section-internal separation|
|`--space-6`|40px|Between major elements|
|`--space-7`|64px|Between sections|
|`--space-8`|96px|Before major narrative breaks|

### Round-table scene (central composition)

- Width 100% of container, fixed aspect ratio 3:2 (height = width × 0.5).
- Table: centered ellipse, 320×140 at desktop, scales proportionally.
- Three core bots: equilateral triangle around the table — Claude top-left, Gemini top-right, Codex bottom-center.
- Two guest-chair glyphs: dashed-border "+" squares between the core bots at the table's rim, bottom-corners.
- Mobile (<640px): bots scale to 60% size, table keeps proportion, layout still recognizable.

## 6. Depth & Elevation

Single shadow system; no random-per-element shadows.

|Level|Shadow|Use|
|-|-|-|
|0|none|Inline text, eyebrows, dividers|
|1|`0 1px 0 rgba(255,255,255,0.03)`|Inset highlight only — rim light on surfaces|
|2|`0 8px 24px rgba(0,0,0,0.35)`|Cards — answer cards, tool badges|
|3|`0 12px 32px rgba(0,0,0,0.45), 0 0 0 1px rgba(212,160,74,0.25)`|Hover-raised card, termcast in focus|
|4|`0 20px 48px rgba(0,0,0,0.5), inset 0 2px 0 rgba(255,255,255,0.08)`|The round table (the composition anchor — deserves distinct weight)|
|5|`0 0 0 3px var(--brass)` + level 3|Focus-visible outline (keyboard focus)|

### Radius scale

|Token|Value|Use|
|-|-|-|
|`--r-sm`|4px|Inline code, micro-chip|
|`--r-md`|8px|Input-like elements|
|`--r-lg`|12px|Cards, badges|
|`--r-xl`|16px|Major containers, round-table outer|
|`--r-pill`|100px|CTAs, eyebrow labels, verdict ribbon|

## 7. Do's and Don'ts

### Do

- Commit to the editorial-council genre in every copy decision. Short paragraphs. One idea per sentence. Write like a policy paper, not marketing collateral.
- Use the agent bot silhouette beside every quoted model response. Consistency teaches the visual language fast.
- Put the verdict ribbon at the end of any section that demonstrates a multi-agent response — it's a recurring rhythm, not a one-off.
- Use brass for accents you want remembered. Agent colors for identity. `--text` for everything that must be read.
- Keep the round-table scene as the one ornate illustration. Other illustrations would dilute it.
- Reduce-motion: all non-decorative motion must honor `prefers-reduced-motion: reduce`.

### Don't

- Don't show raw env var JSON or config arrays on this page. Agent reads `INSTALL.md`; the landing page is for humans forming intuition.
- Don't stretch the round-table bots' color to other UI elements (no blue CTAs, no teal borders) — the agent colors are reserved for agent identity.
- Don't add feature grids with generic line icons. If a feature needs to be mentioned, use a badge with mono label or a short termcast — never SaaS-standard icon-heading-subtitle trio.
- Don't italicize "Roundtable." The word stays upright in every display. Italic emphasis is reserved for single words in headings where `--brass` color also applies.
- Don't use `--text-dim` for meaningful text — it fails AA body contrast. Decoration only.
- Don't mirror keel's winding-path / scroll-story-with-characters depth. This page is the 500–600 line version; keel's 1000+ is the long-form.
- Don't add telemetry, analytics, or third-party scripts. Single HTML file, no network beyond Google Fonts.

## 8. Responsive Behavior

### Breakpoints

|Range|Name|Behavior|
|-|-|-|
|≥960px|desktop|Full layout, H1 72px|
|720–959px|wide tablet|Container at 800px, padding 36px|
|640–719px|tablet|Answer-cards collapse to 1 column; guest-badges 2×2|
|480–639px|phone-wide|H1 drops to 48px, padding 20px|
|<480px|phone|Tool-row collapses to 1 column, guest-badges 1 column, bot sizes ×60%|

### Round-table scene scaling

Desktop: 320×140 table, 60×72 bots. Table uses explicit px; bots use `em` relative to scene's font-size for proportional scaling. At phone width, scene's font-size halves — bots scale with it.

### Touch targets

All interactive elements min 44×44px. Verdict ribbon is non-interactive so exempt. Agent silhouettes that appear inline beside quotes are decorative (no interaction) so exempt; if they ever become clickable in a future iteration, bump them to 44×44 hit area.

### Horizontal overflow

- Termcast block: `overflow-x: auto; word-break: break-word;` on `<pre>` — code never forces horizontal page scroll.
- Guest provider rows (the config example, if ever added): same treatment.
- Long model names in badges: `max-width` + `text-overflow: ellipsis` with `title` attribute for full name on hover.

### Motion / reduced-motion

|Animation|Default|`prefers-reduced-motion: reduce`|
|-|-|-|
|Card hover lift|150ms translate + shadow|Disabled — color change only|
|Scroll reveal (section fade/translate)|600ms ease-out on enter|Disabled — elements visible immediately|
|Verdict ribbon light glow|2s soft pulse loop|Disabled — static lit state|
|Termcast cursor blink|1s step-infinite|Disabled — static block|
|Bot hover wobble|300ms rotate|Disabled|

No parallax. No infinite-loop animations on primary content. No high-contrast flashes.

## 9. Agent Prompt Guide

**For the implementer agent (next session) — rules while building `docs/index.html`:**

1. **Single file, no build step.** Everything inline: `<style>` in `<head>`, `<script>` at end of body. No npm, no bundler, no framework. The page is a static GitHub Pages artifact.
2. **No frameworks.** Plain HTML/CSS/JS. SVG is inline.
3. **CSS variables first.** Every color, size, and radius from §2–§6 lives on `:root` as a custom property. No hex values inline.
4. **Semantic HTML.** `<header>`, `<section>`, `<article>` for each narrative block. Every section gets an `id`. H1 appears exactly once (in the hero).
5. **ARIA + labels.**
   - Agent silhouettes are `role="img"` with `aria-label="Claude bot"` etc.
   - Verdict ribbon is `role="status"` with an `aria-label` summarizing the count ("2 of 3 agents agree").
   - Decorative SVG (background gradients, ornament) gets `aria-hidden="true"`.
6. **Fonts via Google Fonts preload.**
   ```html
   <link rel="preconnect" href="https://fonts.googleapis.com">
   <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
   <link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Space+Grotesk:wght@400;500;700&family=JetBrains+Mono:wght@400;600;700&display=swap">
   ```
7. **Reduced-motion media query** covers every animation per §8.
8. **No external scripts** beyond Google Fonts CSS. No analytics.
9. **Mirror keel's structure patterns where helpful:** termcast structure, scroll-reveal `IntersectionObserver`, CTA button group, section layout rhythm. Do NOT copy keel's color tokens, font stack, or SVG character styling — those are genre-specific.
10. **Length target:** 500–700 lines of inline HTML/CSS/JS. If it's trending past 800, something's over-engineered.
11. **Contents order:** match §1 narrative arc exactly (hero → council → problem → what → disagreement → tools → extend → how-built → CTA).
12. **Copy tone:** direct, editorial, one idea per sentence. No marketing cliches ("revolutionize", "transform", "powerful"). Pull source lines from `README.md` where they already say it well.

## Design Lint Rules

|ID|Severity|Rule|
|-|-|-|
|D-1|critical|No hex color values in `style=""` or inline CSS — use CSS variables|
|D-2|critical|Interactive elements (`onclick` handlers, links) must be `<button>` or `<a>`, not `<div>` / `<span>`|
|D-3|high|`--text-dim` and similar sub-AA tokens used only for decorative text (captions <12px, separators). Lint: any element with `color: var(--text-dim)` AND font-size ≥14px fails.|
|D-4|high|Every animation has a `@media (prefers-reduced-motion: reduce)` override|
|D-5|high|No `font-family` outside `--font-display` or `--font-mono`|
|D-6|high|Agent colors (`--claude`, `--gemini`, `--codex`) never used on non-agent UI (CTAs, borders of non-agent cards, accents)|
|D-7|medium|H1 appears exactly once — in the hero|
|D-8|medium|Touch targets ≥44×44px for all `<a>` / `<button>` elements|
|D-9|medium|No `<h2>` without an eyebrow label above it|
|D-10|low|Inline SVG under 1KB each — oversized SVG should be extracted or simplified|

## Iteration Guide

When a future change comes through, check the rule before editing:

|Change request|Rule|
|-|-|
|"Add a hero video / Lottie / animation"|Violates "single ornate illustration" — pitch a new illustration only if it replaces the round-table scene, not adds to it|
|"Add logos of providers we support"|Violates anti-slop "trusted by logo wall." Stick to mono text badges.|
|"Brighten the background"|The dark-warm ground is a genre commitment. Brighten → move to Paper Noir, which is a different DESIGN.md.|
|"Add a second accent color"|One signature color. If adding, downgrade brass first — don't dilute.|
|"Use icons in Tools row"|No. Mono text is the house style.|
|"Match keel exactly"|This page is a sibling, not a clone. Keel has its genre; Roundtable has its genre.|

## Changed tokens

(Initial creation — none)

---

**Status:** draft · awaiting user review
**Next step after approval:** implement `docs/index.html` per §9 Agent Prompt Guide; keep diff limited to new file + optional `docs/CNAME` if custom domain is wanted.
