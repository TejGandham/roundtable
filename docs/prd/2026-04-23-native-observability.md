# PRD: Native Observability for the Roundtable MCP Server

Status: Draft
Author: Product Owner
Date: 2026-04-23
Target delivery: TBD (see Phasing)

---

## 1. TL;DR

Roundtable's current observability signals are inherited from a legacy shell-script wrapper that predates the Go MCP server. Two consequences follow: (a) the HTTP-backed providers we ship today (Fireworks Kimi, Fireworks MiniMax, and any future OpenAI-compatible HTTP provider) emit **no telemetry at all**, so they are invisible on the operations dashboard; and (b) once the legacy wrapper is fully retired, the telemetry from the three CLI backends (Claude, Codex, Gemini) will also disappear. This PRD specifies the product we need so that every invocation of every provider is measurable, attributable, and alertable from the Go binary itself, independent of how it was launched.

## 2. Background and Problem Statement

### 2.1 What we have today

- A production Grafana dashboard ("Roundtable Skill", UID `roundtable-skill`) showing invocation counts, success rate, latency quantiles, and per-backend breakdowns for four span names: `roundtable.invoke`, `roundtable.gemini`, `roundtable.codex`, `roundtable.claude`.
- A Prometheus + Alloy + Loki observability stack with a spanmetrics connector in the `roundtable` namespace that converts OTEL traces into the metrics the dashboard consumes.
- A `roundtable_calls_total` counter and `roundtable_duration_milliseconds` histogram, enriched with provider status, provider model, role, host, project, and session labels.

### 2.2 What is broken or about to break

- **HTTP providers are dark.** The Go binary defines an `ObserveFunc` hook for HTTP backends but the CLI entry point passes `nil` for it. Every Fireworks Kimi or MiniMax call completes without recording a single metric. Operators cannot tell how often HTTP providers are invoked, which ones are timing out, which models are in use, or what their latency distribution looks like.
- **Legacy dependency.** The only reason the four existing spans exist is that the old shell-based `/roundtable` skill wraps the Go binary and emits OTEL traces itself. When the wrapper is removed, all four panels on the dashboard will go flat.
- **Invisible failure modes.** Today, a Fireworks rate-limit storm, a carrier outage on `api.fireworks.ai`, or a regression in our HTTP request serialization would only be detected by someone noticing anecdotally that "roundtable feels off". There is no alerting surface.
- **Inconsistent attribution.** Even for the three CLI providers that do emit spans, the emission happens outside the Go binary, which means data about provider version, retries, timeouts, token usage, and internal error classes is not captured because the wrapper does not have access to it.

### 2.3 Why now

Roundtable has moved from "a couple of CLI wrappers" to "a panel of N models with HTTP providers expected to be the growth vector." HTTP providers will outnumber CLI providers within the next release cycle. Shipping new providers with zero observability is a production risk and blocks our ability to set SLOs or prioritize reliability work. We also risk a silent regression window between "legacy wrapper retired" and "native telemetry shipped" during which the entire dashboard goes blank.

## 3. Goals

1. **Universal coverage.** Every provider registered in roundtable — CLI-based, HTTP-based, current, or future — emits telemetry automatically once it is configured.
2. **Zero dashboard regression.** When the legacy shell wrapper is removed, the existing `Roundtable Skill` dashboard continues to render meaningful data with no manual rework by the operator.
3. **Diagnosable failures.** When a provider fails, the signal carries enough context to answer "which provider, what model, what class of error, on whose machine, in which project, how long did it take" without tailing logs.
4. **Operator self-service.** Adding a new provider to the config should produce a visible surface on the dashboard within one release cycle, without touching dashboard JSON, Alloy config, or Prometheus relabeling rules.
5. **No breaking changes to existing label names or metric names** that the current dashboard and any downstream alerts depend on.

### 3.1 Non-goals

- Replacing the existing Alloy → Prometheus → Grafana stack.
- Introducing a trace-storage backend (Tempo, Jaeger). The spanmetrics pattern stays.
- Instrumenting internals of provider SDKs (for example, instrumenting the Claude Code CLI's own tool calls).
- Request/response payload capture. Only structural and outcome metadata is collected.
- Cost telemetry (tokens consumed, dollar cost), recommendation acceptance rate, and hallucination rate. All three are deferred to a **future "Backend Value" PRD** whose motivating question is "is this backend economical?" Answering that requires cost, acceptance, and quality signals as a bundle — splitting them across separate PRDs would fragment the analysis. This PRD remains scoped to operational stability and visibility; the economic/quality question is its own product workstream.
- Per-user identity. The existing `session_tag` / `host_name` / `project_name` attribution is sufficient.

## 4. Users and User Stories

### 4.1 Primary personas

- **Homelab operator (owner of the Brahma k3s cluster).** Monitors the dashboard, receives ntfy alerts, decides when to upgrade Alloy/Prometheus, pays the cost of bad data (storage, alert fatigue).
- **Roundtable contributor / maintainer.** Adds providers, investigates regressions, fields "roundtable is slow" reports, triages bug reports against specific providers.
- **End user of the roundtable skill.** Invokes roundtable from Claude Code, Copilot CLI, or Gemini CLI. Does not interact with the dashboard directly, but benefits when the operator can keep providers healthy.

### 4.2 User stories

- As an **operator**, I can open the dashboard and immediately see which providers were invoked in the last 7 days and how often each one failed, without knowing in advance which providers exist.
- As an **operator**, I get a ntfy alert when any provider's error rate exceeds a configurable threshold over a configurable window.
- As a **maintainer**, when a user reports "kimi is timing out," I can filter the dashboard to kimi-only and see the timeout rate, model distribution, and which hosts are affected in under 30 seconds.
- As a **maintainer**, when I add a new HTTP provider to the config and ship a release, I can confirm the next invocation appears on the dashboard within one minute without any dashboard edits.
- As a **contributor**, when I introduce a regression in request serialization, the failure shows up as a new error classification on the dashboard so I notice in CI/dogfood rather than in production.

## 5. Functional Requirements

### 5.1 Coverage (MUST)

- F1. Every provider invocation — regardless of transport (local subprocess, HTTP, stdio MCP, future transports) — produces exactly one **root record** per user-visible invocation, plus exactly one **child record per provider fanned out**.
- F2. The root record represents the full "roundtable call" as seen by the MCP client. The child records represent individual backend calls.
- F3. Child records exist even when the parent invocation fails early, provided at least one provider was attempted. (This allows differentiating "no providers reached" from "all providers failed".)
- F4. Telemetry is emitted from the Go binary directly and does not depend on any external wrapper, skill script, or sidecar being present.
- F5. If `ENABLE_ROUNDTABLE_TELEMETRY` is off (the default), roundtable runs with zero observability code paths exercised. If the flag is on but the configured OTLP endpoint is unreachable, telemetry is silently dropped at the client. Neither absent configuration nor collector failures ever break a roundtable invocation.

### 5.2 Attribution (MUST)

Every record carries enough context to answer the following five questions without log correlation:

- **Who invoked it?** Host identity, project identity, session identity.
- **Which provider?** Stable provider identifier (matching the config key the operator set).
- **Which model?** Model identifier as reported by the provider, or a declared fallback string when the provider does not return one.
- **What was the outcome?** A small, bounded set of outcome classifications: success, transport error, provider error, timeout, rate-limited, not-found, user-cancelled, and unknown. The classification set must be documented and stable; additions require a version bump on the dashboard.
- **How long did it take?** Wall-clock latency of the backend call, measured inside the Go binary.

### 5.3 Cardinality discipline (MUST)

- F6. No free-form user input appears as a label value. This explicitly includes: prompt text, file paths provided by the user, raw error messages returned by providers.
- F7. Provider model strings are passed through but must be validated against a documented allow-pattern (lowercase, bounded length, limited character set). Values failing validation are bucketed to `invalid`.
- F8. Session identifiers are bounded to a hash prefix of known length (matching the current `session_tag` convention, for example 4 hex characters).
- F9. The total label cardinality per metric must be forecastable. The PRD acceptance includes a documented upper bound for each label, signed off by the operator.

### 5.4 Configurability (MUST)

- F10. Telemetry is **opt-in**. The master switch is the environment variable `ENABLE_ROUNDTABLE_TELEMETRY`. Default value is off. No collector is contacted, no goroutines are started, and no attributes are assembled when the flag is off.
- F11. When `ENABLE_ROUNDTABLE_TELEMETRY` is on, the operator additionally controls: OTLP endpoint URL, service name, deployment environment tag, and a static set of resource attributes (host, project, session). OTEL-standard environment variables are accepted where they exist (for example `OTEL_EXPORTER_OTLP_ENDPOINT`); roundtable-specific variables take precedence for roundtable-specific attributes.
- F12. The configuration surface is an environment-variable-first interface. No per-invocation flags required in the common path.
- F13. Configuration changes do not require rebuilding the binary.

### 5.5 Compatibility with existing dashboard (MUST)

- F14. The four existing span names (`roundtable.invoke`, `roundtable.gemini`, `roundtable.codex`, `roundtable.claude`) continue to exist and behave the same as today.
- F15. Existing label names (`roundtable_role`, `roundtable_gemini_status`, `roundtable_gemini_model`, `roundtable_codex_status`, `roundtable_codex_model`, `host_name`, `project_name`, `session_tag`, `status_code`) continue to be populated with equivalent semantics.
- F16. New providers use a **generalized naming convention** (see 5.6) that the existing dashboard's "Invocations by span_name" panel automatically surfaces without being edited.

### 5.6 Extensibility (MUST)

- F17. Adding a new provider in config causes a new child record with span name `roundtable.<provider_id>` to appear automatically. No code changes to the observability layer.
- F18. A generalized per-backend status and per-backend model label pair exists for all providers uniformly (for example, `roundtable_backend_status` and `roundtable_backend_model`), in addition to the legacy per-provider labels retained for backward compatibility.
- F19. The dashboard acquires panels for new providers via templating variables that are populated from live label values, not hardcoded. A follow-up dashboard refresh is allowed but must not be required.

### 5.7 Alerting readiness (SHOULD)

- F20. The schema supports the following alert classes without further schema changes: per-provider error rate over threshold, per-provider P95 latency over threshold, absence of any roundtable activity for a sustained period (dead-man switch), and per-host outage.
- F21. Alert rules themselves are out of scope for this PRD but the schema design must be reviewed against the alert classes above before sign-off.

### 5.8 Developer ergonomics (SHOULD)

- F22. A local development mode exists that prints telemetry to standard error in a human-readable format, for debugging new providers without needing the full pipeline. Activated by setting `ENABLE_ROUNDTABLE_TELEMETRY=stderr` (or equivalent documented value), distinct from the OTLP-collector path.
- F23. Unit tests can assert against emitted telemetry without spinning up an OTLP collector.

## 6. Non-Functional Requirements

- N1. **Performance overhead.** Emitting telemetry must add no more than a fixed, documented budget (proposed: 20 ms) to the end-to-end wall-clock of an invocation in the happy path, and zero measurable overhead when telemetry is disabled by config.
- N2. **Failure isolation.** An unreachable OTLP endpoint, a malformed response from the collector, or a clock skew must never propagate as an error to the MCP client or change the exit status of a roundtable invocation.
- N3. **Binary size.** The additional dependencies must not increase the release binary size by more than a documented budget (proposed: 15%).
- N4. **Security.** No secrets (API keys, tokens, OAuth material) are permitted in any attribute, resource, or log line. This is enforced by a allowlist on emission, not by review.
- N5. **Privacy.** User prompts, provider responses, and file contents never appear in any telemetry record. Enforced by construction (the emission site has no access to those payloads).
- N6. **Backwards compatibility.** A user running a pre-release build of the Go binary against the current Alloy/Prometheus/Grafana stack must not break either side. Specifically, new labels must not collide with existing ones, and new span names must not be sinks for spanmetrics configured against the old names.
- N7. **Operability.** The observability layer is itself observable: the binary logs at least one line per startup indicating whether telemetry was enabled, where it is being sent, and any initial connection success or failure.
- N8. **Cross-platform.** Telemetry works on the three supported platforms (macOS arm64/amd64, Linux arm64/amd64, Windows amd64). No Linux-only code paths.

## 7. Success Metrics

Measured 30 days post-launch:

- S1. **Coverage.** ≥ 99% of invocations across all configured providers in the operator's environment appear on the dashboard. Measured by sampling client-side invocation count vs dashboard-surfaced count over a rolling window.
- S2. **Attribution completeness.** ≥ 99% of invocation records have non-null values for all five attribution fields (host, project, session, provider, model). The remaining ≤ 1% must fall into an explicit `unknown` bucket, not be missing.
- S3. **Legacy parity.** On the day the legacy shell wrapper is removed, the dashboard shows no measurable drop (no panel flips to "No data" for more than one scrape interval).
- S4. **MTTR improvement.** For any single regression that makes it to production post-launch, time-to-detection from the operator's first invocation of the bad build is ≤ 5 minutes, where today it is bounded only by "when someone notices." (Tracked qualitatively via post-mortems.)
- S5. **Operator overhead.** Adding a new provider to the config surfaces panels within one release cycle with zero dashboard edits. Binary success criterion.
- S6. **Overhead budget.** P50 added latency per invocation is within the documented budget in N1. Binary success criterion.

## 8. Scope and Phasing

The work is substantial enough to be split into three shippable phases.

### Phase 1 — Native Emission, CLI Parity

Objective: the Go binary emits telemetry natively, matching the legacy wrapper's signal surface for the three CLI providers. Operators can retire the legacy wrapper without losing any dashboard content.

**Phase 1 is a stability-measurement phase, not a quality-measurement phase.** The questions Phase 1 must be able to answer are:

- How often is roundtable invoked, and by whom?
- Which provider backends are most stable (lowest error rate)?
- What are the latency distributions — p50, p95, p99 — per backend?
- Are there host-specific or project-specific patterns in failure?
- When a regression lands, does the failure signal surface quickly enough to catch it?

The questions Phase 1 **does not** attempt to answer, by design:

- How often are a backend's recommendations accepted by the orchestrator?
- What fraction of responses are hallucinations or low-quality?
- What is the per-invocation dollar or token cost?
- Which MCP client is driving the traffic?

Those belong to later phases once the stability baseline is established and trusted.

Exit criteria:
- All four legacy span names emit from the Go binary.
- All legacy labels retain their current meaning and population rate.
- Dashboard renders identically (within statistical noise) before and after switching from wrapper-emitted to Go-emitted traces on a dogfood host.
- Legacy wrapper retirement is unblocked.
- The five Phase 1 stability questions above are answerable from the dashboard without leaving it.

### Phase 2 — HTTP Provider Coverage

Objective: HTTP providers (Fireworks Kimi, Fireworks MiniMax, future OpenAI-compatible providers) emit first-class telemetry using the generalized schema.

Exit criteria:
- Every HTTP provider produces records with the generalized `roundtable.<provider_id>` span name.
- Dashboard auto-surfaces HTTP providers on the existing "Invocations by span_name" panel and via templating variables, without dashboard JSON edits.
- Provider-specific failure modes (HTTP 429, HTTP 5xx, body parse errors, tool-call serialization errors) are distinguishable via the `status` classification.

### Phase 3 — Operator Alerts and Dashboard Refresh

Objective: the operator moves from "dashboard exists" to "I get paged on the right things."

Exit criteria:
- Documented alert rules covering the four classes listed in F19 are deployed.
- A refreshed dashboard layout uses generalized labels as the primary breakdown, with legacy per-provider panels retained for compatibility but de-emphasized.
- An "Observability" section is added to the project README and AGENTS.md covering: how to enable, how to verify, how to add a provider.

Phases must ship in order. Phase 1 is the critical path; Phase 2 unblocks the pending HTTP-provider growth; Phase 3 is the polish that makes the investment durable.

## 9. Dependencies

- Alloy configuration on the Brahma cluster currently defines the spanmetrics connector, OTLP receiver, histogram buckets, and the resource-to-telemetry promotion. Any schema change requires coordinated edits to `homelab-docs/brahma/manifests/monitoring/alloy.yaml`.
- Prometheus retention is 15 days and Loki is 30 days. This constrains the longest practical dashboard time range.
- The OTLP receiver on Alloy is available only from inside the Tailscale network. Clients running outside the tailnet (for example, a contributor's laptop without Tailscale) will have telemetry silently dropped — this is acceptable and aligns with N2.
- The homelab documentation at `homelab-docs/brahma/monitoring.md` is the canonical reference for how Alloy, Prometheus, Grafana, and spanmetrics fit together. Any architectural change must be mirrored there.
- No new third-party service is required. No new cost center is introduced.

## 10. Risks and Mitigations

- **R1. Cardinality explosion from misuse.** A future provider could emit unbounded model strings or free-form statuses. *Mitigation:* F7 validation at the emission boundary, plus a regression test that enumerates the expected label value set.
- **R2. Dual emission during transition.** Both the legacy wrapper and the Go binary might emit telemetry simultaneously for one release window, double-counting invocations. *Mitigation:* Phase 1 exit criteria explicitly require a clean cutover plan with a documented "wrapper off" signal; this PRD calls it out but leaves the specific switch to the implementation plan.
- **R3. Overhead surprise on cold invocations.** First-call OTLP connection setup could add a perceptible latency spike for short, interactive uses. *Mitigation:* N1 sets the budget; Phase 1 must include a benchmark that exercises the cold path.
- **R4. Operator friction.** If the default configuration requires every user to set environment variables or Tailscale is not running, roundtable becomes harder to use. *Mitigation:* F5 — telemetry is best-effort and never blocks, and the default config must "just work" on the two primary operator hosts (coding-agent1, vader) with zero user-visible friction.
- **R5. Schema lock-in.** If we ship Phase 1 with a label schema we later regret, migrating is expensive (dashboard edits, alert edits, operator communication). *Mitigation:* this PRD reserves Phase 1 for legacy parity specifically to delay generalized-schema commitments until Phase 2, when we have more information.
- **R6. Cross-platform flakiness.** OTLP SDKs have historically had Windows quirks. *Mitigation:* N8 requires cross-platform CI coverage for the observability path, not just the happy-path code.
- **R7. Dashboard drift.** Two people could edit the dashboard JSON via the Grafana UI during the rollout and lose changes. *Mitigation:* existing convention (source of truth in `grafana/roundtable-dashboard.json`, push via API) is reinforced in Phase 3 documentation.

## 11. Rollout Plan

- **Private dogfood.** First ship to one host (vader) behind an explicit opt-in env var. Compare dashboard output against legacy wrapper output for at least seven days. Require hand-verified parity for every existing panel.
- **Default-on internal.** Enable by default on both operator hosts. Keep the opt-out env var for one release.
- **External default-on.** Ship to any downstream users after one full release cycle of internal default-on with no reported regressions.
- **Legacy removal.** Only remove the shell wrapper's OTEL emission after external default-on has been stable for one release cycle.

At no point during rollout should the dashboard go blank. If it does, that is an S3 failure and rollout halts.

## 12. Open Questions

- Q1. **[RESOLVED 2026-04-23]** Out-of-the-box mode without an OTLP collector: **No.** Telemetry is explicitly opt-in via `ENABLE_ROUNDTABLE_TELEMETRY` (default: off). Roundtable does not attempt to make observability "just work" for every use case. Operators who want telemetry configure it deliberately; everyone else runs with zero telemetry overhead and zero surprises. This narrows G1 — "universal coverage" applies to *every provider*, not *every user*. Contributors without infrastructure get nothing by default; the stderr debug mode in F22 remains available for local development.
- Q2. **[RESOLVED 2026-04-23]** Normalize provider model strings client-side: **No, defer.** Phases 1 and 2 record model identifiers verbatim as returned by each provider. Current cardinality (4 values for gemini, 2 for codex across full history) does not justify the lock-in cost of a normalization scheme. Revisit in Phase 3 only if observed cardinality degrades dashboard readability. Accepted risk: if a provider with messy model IDs ships before Phase 3, pie charts may look noisy for a release cycle.
- Q3. **[RESOLVED 2026-04-23]** Per-invocation correlation ID for log/trace stitching: **Deferred to Phase 3.** Phases 1 and 2 ship without any dedicated correlation identifier. The decision of whether to add one, and if so, what form it takes (span attribute, log field, dashboard link surface, or none), is explicitly left open until Phase 3 is in scope. Implementation plans for Phases 1 and 2 must not presuppose a direction. Rationale: current invocation density (approximately 22 calls over 15 days in the reference environment) allows existing attribution fields — host, project, session, timestamp — to serve as an adequate correlation key. A decision made today would be speculative; the Phase 3 decision will benefit from real alerting data and debugging experience.
- Q4. **[RESOLVED 2026-04-23]** MCP client identity capture: **Deferred.** Phases 1 and 2 do not capture MCP client name or version. Implementation plans must not presuppose direction. Revisit when (a) production traffic demonstrably comes from multiple distinct MCP clients, or (b) a specific bug report would have been materially easier to triage with client identity. Rationale: no evidence yet that traffic is multi-client; `clientInfo` parsing adds MCP-lifecycle coupling to the telemetry emitter that is not justified by current data.
- Q5. **[RESOLVED 2026-04-23]** Cost/token telemetry: **Out of this PRD. Belongs to a future "Backend Value" PRD that bundles cost and quality signals together.** The motivating product question for that future PRD: *is this backend economical?* — which cannot be answered by token cost alone or acceptance rate alone. It requires both: cost per accepted recommendation, rejection rate vs spend, and hallucination-adjusted effective cost. Bundling token usage, dollar cost, orchestrator acceptance rate, and hallucination rate into one coherent PRD keeps that economic analysis intact. Splitting them (token cost in one PRD, quality in another) would fragment the decision. This native-observability PRD remains scoped to stability and operational visibility.
- Q6. **[RESOLVED 2026-04-23]** Separate health surface vs usage surface: **Deferred to the roundtable project.** This PRD does not prescribe whether or how a health surface (startup spans, probe results, heartbeats, or any other form) should exist. The roundtable maintainers make that call based on their operational experience once native telemetry is in production. Phases 1 and 2 ship a usage-only surface; Phase 3 dashboard-refresh scope may revisit if operators raise a specific blind spot. Implementation plans must not presuppose direction on this question.

## 13. Acceptance

This PRD is considered accepted when:

- The operator and the roundtable maintainers sign off on the goals, non-goals, phasing, and success metrics.
- The open questions in Section 12 are resolved in writing, or explicitly deferred with owner and target date.
- An implementation plan (separate document) is linked from this PRD, covering the schema in detail, the specific labels and span names, the config surface, and the dogfood plan.

Until those three conditions are met, implementation should not begin.
