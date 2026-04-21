# OpenAI-Compatible HTTP Providers — Requirements Specification

**Document type:** Software Requirements Specification (SRS). Captures *what* the system must do and the constraints it must honor. **Explicitly does not prescribe a solution** — no code shape, library choice, file layout, or implementation strategy appears here. A separate design document will follow once requirements are locked.

**Status:** Draft. Supersedes the Ollama-specific backend scope once accepted.
**Date:** 2026-04-21
**Authors:** Tej (product owner), Claude Opus 4.7 1M (drafting support)
**Prerequisite reading:** `docs/plans/2026-04-20-ollama-cloud-provider.md` (the Ollama-specific architecture this generalizes), PR #11 as the current baseline.

---

## 1. Summary

Roundtable today has one HTTP-native backend (`ollama`) bound to a single provider (Ollama Cloud) and model-picks-via-agent-spec. Real-world use surfaced that (a) Ollama Cloud Pro-tier reliability is fragile on multiple models, and (b) the same model families (kimi, qwen, glm) are available via other OpenAI-compatible endpoints with different reliability, pricing, and privacy profiles. The system must evolve so callers can address specific `(provider, model)` pairs, and operators can register multiple providers independently.

## 2. Goals (business outcomes)

This refactor is accepted if, after the change:

- **G1.** A caller can dispatch to a specific model *on a specific provider* in a single agent-spec entry, without writing any Go code.
- **G2.** An operator can register multiple OpenAI-compatible providers in one Roundtable process; each with its own credentials, base URL, concurrency cap, and timeout.
- **G3.** The same model family (e.g., `kimi-k2.6`) can be served from two or more different providers in the same hivemind dispatch and be distinguishable in the results.
- **G4.** Adding support for a newly-discovered OpenAI-compatible provider does not require changes to the dispatcher, the `Backend` interface, the tool schemas, or the `Result` shape — only provider-specific configuration.
- **G5.** Existing invariants from PR #11 (offline `Healthy()`, `Result.Metadata` propagation, opt-in-only defaults, per-backend metrics, PII no-log rule) remain enforced without regression.

## 3. Non-goals (explicitly out of scope)

These are acknowledged as valuable but will not be solved by this work:

- **NG1.** Streaming responses (`stream: true`). The system remains single-`Result` per dispatch.
- **NG2.** Retry logic on transient failures. Decision C from `docs/plans/2026-04-20-ollama-cloud-provider.md` ("surface rate limits; no retry") still holds.
- **NG3.** Circuit-breaker / adaptive concurrency. Same reasoning.
- **NG4.** Cost tracking or token-budget enforcement.
- **NG5.** Response caching.
- **NG6.** Unification of non-OpenAI-compatible backends (Anthropic native, Gemini native via subprocess CLIs) with the HTTP-native path. Those stay as subprocess backends.
- **NG7.** Auto-routing ("pick the best available provider for a given model"). A caller specifying an agent must continue to name the provider + model explicitly.
- **NG8.** A model-capability catalog (context windows, tool support, vision support) — deferred to a future effort aligned with pal-extraction §2.1.
- **NG9.** Distributed (cross-process) concurrency limiting.

## 4. Glossary

- **Provider:** a distinct HTTP endpoint + authentication scheme + credentials serving one or more models (e.g., "Ollama Cloud," "Moonshot," "z.ai / Zhipu," "DeepSeek," "Groq").
- **Model ID:** the string a provider accepts in the `model` field of a chat-completions request (e.g., `kimi-k2.6:cloud` on Ollama, `kimi-k2-0711-preview` on Moonshot).
- **OpenAI-compatible:** a provider that accepts a request shape matching OpenAI's `/v1/chat/completions` contract (`messages` array, Bearer auth, `choices[].message.content` response).
- **Agent spec:** the JSON object in a `ToolRequest.Agents` entry that identifies one dispatch unit.
- **Dispatch unit:** the `(provider, model, name)` tuple that produces exactly one `Result` per `Run` call.
- **Default agents:** the set dispatched when a caller supplies no explicit `agents` and no `ROUNDTABLE_DEFAULT_AGENTS` override.

## 5. Functional Requirements

### 5.1 Provider configuration

- **FR-1.** The system SHALL allow the operator to enable one or more OpenAI-compatible providers independently, each identified by a unique provider identifier.
- **FR-2.** Each provider's configuration SHALL include at minimum: base URL, authentication credential, concurrency cap, response-header timeout, and an optional default model ID.
- **FR-3.** Absence of a provider's credentials SHALL result in the provider being unregistered — not a runtime failure. The provider must simply not appear to callers.
- **FR-4.** Provider configuration SHALL be readable from environment variables so it can be set without code changes.
- **FR-5.** Provider identifiers SHALL be operator-controlled strings (not hardcoded). The operator chooses how to name a provider (e.g., `moonshot`, `moonshot-eu`, `zai-trial`).
- **FR-6.** Two providers with identical base URLs but different credentials (e.g., trial key vs production key) SHALL be separately registerable under distinct identifiers.

### 5.2 Dispatch addressability

- **FR-7.** An agent spec SHALL be able to name both a provider identifier and a model ID, separately.
- **FR-8.** The same hivemind dispatch SHALL be able to include multiple agents targeting the same model on different providers (e.g., `kimi-k2.6` via Ollama and `kimi-k2` via Moonshot) and return distinct results.
- **FR-9.** An agent spec SHALL continue to support a human-readable display `name` distinct from provider and model.
- **FR-10.** When an agent spec references an unknown provider identifier, the dispatcher SHALL return a per-agent `not_found`-class result (matching the existing contract for missing backends), never a dispatch-wide failure.
- **FR-11.** When an agent spec references a registered provider but omits the model, the provider's configured default model SHALL be used. If no default is configured, a per-agent error-class result SHALL be returned with a message identifying the missing model.

### 5.3 Per-provider behavior

- **FR-12.** Each provider SHALL enforce its own concurrency cap (per-process bulkhead), sized independently of other providers.
- **FR-13.** Each provider SHALL enforce its own response-header timeout, tunable without code change.
- **FR-14.** A slow or failing call on one provider SHALL NOT degrade the latency or availability of any other provider.
- **FR-15.** A rate-limit (HTTP 429) or overload (HTTP 503) response from any provider SHALL produce `status: "rate_limited"` on the resulting `Result` and SHALL NOT trigger any automatic retry.
- **FR-16.** When a provider's upstream sends a `Retry-After` header, its value SHALL be surfaced on `Result.Metadata["retry_after"]`.
- **FR-17.** When a provider's response indicates output was truncated (by whatever field that provider uses — `finish_reason: "length"`, `done_reason: "length"`, etc.), this SHALL be surfaced on `Result.Metadata` in a form that callers can query generically without knowing the provider.

### 5.4 Result contract (preserved invariants)

- **FR-18.** Every successful call SHALL populate `Result.Metadata` with at least: the resolved model ID used, token counts (input and output) where the provider returns them, and any provider-specific termination signal that indicates truncation.
- **FR-19.** `BuildResult` SHALL continue to propagate `ParsedOutput.Metadata` unchanged.
- **FR-20.** Errors SHALL classify into the existing status values: `ok`, `rate_limited`, `timeout`, `error`. No new status values SHALL be introduced by this refactor.
- **FR-21.** Context-deadline expiry while waiting in a provider's concurrency gate SHALL map to `status: "timeout"` via `BuildResult`, matching current behavior. Context cancellation SHALL map to `status: "error"`.

### 5.5 Defaults and discovery

- **FR-22.** No provider registered by this refactor SHALL appear in the `defaultAgents()` set. All OpenAI-compatible providers SHALL remain opt-in only. The regression test `TestDefaultAgents_ExcludesOllama` SHALL be generalized to assert no HTTP-native provider is ever a default.
- **FR-23.** The `ROUNDTABLE_DEFAULT_AGENTS` operator-level override SHALL continue to accept agent-spec JSON referencing any registered provider.
- **FR-24.** The system SHALL provide a mechanism for operators to enumerate registered providers (e.g., via `/metricsz`, an informational log line at startup, or a read-only endpoint). Callers SHALL NOT be required to guess which providers are available.

### 5.6 Health and readiness

- **FR-25.** `Healthy()` for any HTTP-native provider SHALL remain offline — credential/config validation only. No network probe. The existing invariant from `docs/plans/2026-04-20-ollama-cloud-provider.md` §5.8 applies to every provider added by this refactor.
- **FR-26.** A single provider's upstream being degraded SHALL NOT cause `/readyz` to report unready.

### 5.7 Metrics

- **FR-27.** Per-provider and per-model metrics SHALL be emitted to the existing `/metricsz` endpoint under Prometheus-convention names. At minimum: request count labeled by provider + model + status, duration labeled by provider + model.
- **FR-28.** Metric label cardinality SHALL remain bounded — provider identifiers are operator-controlled strings and are expected to be low-cardinality (single-digit count). Model IDs are higher-cardinality but bounded by the set of models a given provider serves.

### 5.8 Observability

- **FR-29.** When a provider's concurrency gate blocks a request past a configurable wait threshold, a debug log SHALL be emitted. This preserves the observability rationale from §4.6 of the Ollama architectural plan.
- **FR-30.** Prompts and response bodies SHALL continue to never be logged at any level. This PII invariant applies to every provider added by this refactor without exception.

### 5.9 File handling

- **FR-31.** When `req.Files` is non-empty, file contents SHALL be inlined into the user message before the HTTP call, for every OpenAI-compatible provider. The per-file and aggregate caps (`ollamaMaxFileBytes`, `ollamaMaxTotalFileBytes`) SHALL be preserved or generalized, not removed.
- **FR-32.** Unreadable files, oversized files, and over-budget files SHALL be surfaced in the inlined blob in a form the model can see (not silently skipped).

## 6. Non-functional Requirements

### 6.1 Compatibility

- **NFR-1.** The `Backend` interface defined in `internal/roundtable/backend.go` SHALL NOT change shape. Any new provider SHALL satisfy the existing interface.
- **NFR-2.** The `Result` and `ParsedOutput` types SHALL NOT gain required fields. Additive optional fields are acceptable; removals and renames are not.
- **NFR-3.** MCP tool input schemas (hivemind, deepdive, architect, etc.) SHALL NOT gain new required fields. Additive optional fields are acceptable.

### 6.2 Testability

- **NFR-4.** Every provider registered by this refactor SHALL be testable in isolation using `httptest.Server` without requiring a real credential or real upstream.
- **NFR-5.** Tests SHALL NOT make real network calls against upstream providers during `go test ./...`. Live integration tests requiring credentials MAY exist but SHALL be gated behind a build tag or environment check.
- **NFR-6.** The existing test suite SHALL continue to pass unmodified for assertions that are genuinely implementation-independent. Implementation-tied assertions MAY change if the underlying behavior changes, but MUST still verify the same contractual properties.

### 6.3 Dependencies

- **NFR-7.** This refactor SHALL NOT introduce a new external Go module dependency unless the dependency is (a) from `golang.org/x/*` or (b) maintained by a provider whose SDK we're adopting, with a written justification.
- **NFR-8.** Specifically: adopting a vendor SDK (e.g., `openai-go`, `anthropic-sdk-go`) is PERMITTED only if the requirements above cannot be met with stdlib + the existing `golang.org/x/sync` dependency. Justification required if adopted.

### 6.4 Operational

- **NFR-9.** Adding, changing, or removing a provider SHALL take effect on process restart. Hot-reload of provider configuration is out of scope.
- **NFR-10.** Existing operators of the `ollama` backend SHALL have a clear migration path documented in `INSTALL.md` (either their existing env vars are honored, or the deprecation is explicit and the new equivalents are enumerated).

### 6.5 Documentation

- **NFR-11.** `INSTALL.md` SHALL list every provider shipped with the refactor, each provider's required environment variables, and at least one example agent spec targeting that provider.
- **NFR-12.** The docstring on `defaultAgents()` SHALL be updated to reference the generalized invariant (no HTTP-native provider is ever default), not just Ollama.

## 7. Constraints (hard boundaries)

- **C-1.** This refactor SHALL be contained in a single branch and shipped as a single reviewable change. Incremental provider additions after the refactor are separate PRs.
- **C-2.** Decision C ("no retry in the backend") is preserved.
- **C-3.** Decision F ("API key + base URL read per-Run, not cached") is preserved for credentials and endpoints. Concurrency caps and timeouts remain construction-time (consistent with the existing Ollama behavior since PR #11).
- **C-4.** The `/readyz` offline-Healthy invariant is preserved.
- **C-5.** No provider introduced by this refactor SHALL invoke tool-calling loops inside the backend. Each provider returns exactly one `Result` per `Run`, and agent-level coordination remains the dispatcher's responsibility.

## 8. Migration Requirements

- **MR-1.** Existing Roundtable deployments that use only `OLLAMA_API_KEY` SHALL continue to function after upgrade without any configuration change, OR the upgrade release notes SHALL document the exact env-var mapping required.
- **MR-2.** Existing agent-spec JSON that uses `{"cli":"ollama","model":"<model>:cloud"}` SHALL either continue to dispatch identically or SHALL fail with a migration message that points the caller at the new agent-spec shape. Silent behavior changes are not acceptable.
- **MR-3.** All tests from PR #11 that verify Ollama-specific behavior SHALL either pass unchanged or be re-expressed as provider-agnostic equivalents covering the same property.

## 9. Acceptance Criteria

A proposed implementation satisfies these requirements if:

- **AC-1.** All functional requirements above pass explicit tests — for each FR-N there SHALL exist at least one test (unit or integration) that asserts the required property.
- **AC-2.** A hivemind dispatch with 5 agents targeting 5 distinct `(provider, model)` pairs, where at least 2 providers are represented, returns 5 `Result`s with the expected per-agent status values, and the slow failures of one provider do not block the rest.
- **AC-3.** Removing all provider credential environment variables leaves Roundtable operating normally with only the subprocess CLI backends — no crashes, no startup errors, no confusing logs.
- **AC-4.** `go vet ./...`, `go test ./...`, and `go build ./...` all succeed cleanly.
- **AC-5.** `/metricsz` emits per-provider-per-model counters consistent with FR-27's label shape.
- **AC-6.** A new provider can be added to an already-deployed binary via environment variables only (no rebuild, no new code), IF that provider is OpenAI-compatible and its identifier doesn't collide with an existing one. **This is the core "granular control" property** the refactor exists to achieve.

## 10. Open Questions

These are decision points the design document will need to resolve. Listed here to make the design-document author's scope explicit, not to dictate answers:

- **OQ-1.** What is the agent-spec JSON shape that expresses `(provider, model)` without breaking backward compatibility with `{"cli":"ollama","model":"..."}`?
- **OQ-2.** How are per-provider env-var names structured to avoid naming collisions as the provider count grows?
- **OQ-3.** What is the granularity of `validCLIs`-equivalent validation — do we validate provider identifiers at `ParseAgents` time, or defer to dispatch time?
- **OQ-4.** How does `resolveModel` (or its equivalent) interact with per-provider default models?
- **OQ-5.** Is there a registry pattern (matching pal-extraction §2.2) that makes provider enumeration cleaner, or does a flat map suffice?
- **OQ-6.** Does the concurrency gate remain per-provider, or is there value in a cross-provider "total HTTP calls in flight" cap?

## 11. Success Signals (how we'll know this was worth doing)

After shipping:

- **S-1.** A caller can say "review this diff with kimi via Moonshot AND kimi via Ollama" in one hivemind call and compare the two responses side by side.
- **S-2.** When Ollama Cloud has a 503 storm (per the ongoing `ollama/ollama#15453` issue), operators can steer traffic to direct providers without Roundtable code changes.
- **S-3.** The first new provider added after this refactor lands requires zero changes to `internal/roundtable/run.go`, `internal/roundtable/backend.go`, or any MCP tool schema — purely configuration and possibly one provider-specific file.

---

## Appendix A: Requirements traceability

| Source of requirement | Pointer |
|-|-|
| Existing Ollama architectural decisions (Decision A–K, §4.6 gate, §5.8 offline Healthy) | `docs/plans/2026-04-20-ollama-cloud-provider.md` |
| Post-PR-#11 invariants (opt-in defaults, Name→CLI fix, Metadata propagation) | PR #11 commits `c68d396`, `ef9c285`, `ffdc5f4`, `273f862` |
| Reliability evidence motivating multi-provider support | Stress tests in session 2026-04-21: `mcp__roundtable__hivemind` runs 2, 3, and 4 |
| Hallucination evidence motivating opt-in defaults invariant | Same session, run 4 (PR-review dispatch) |

## Appendix B: What this document deliberately omits

Readers expecting a design document will find these topics missing *on purpose*:

- Whether a single generic backend struct or multiple per-provider structs implement the contract.
- Whether the solution uses `openai-go` SDK, stdlib `net/http`, or anything else.
- File layout, naming conventions for new files, or refactor strategy for existing files.
- The exact JSON shape chosen for agent specs (see OQ-1).
- The env-var naming scheme (see OQ-2).
- Timeline or phasing.

These belong in a separate design document authored after these requirements are accepted.
