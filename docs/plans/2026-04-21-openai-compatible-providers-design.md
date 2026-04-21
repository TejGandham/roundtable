# OpenAI-Compatible HTTP Providers — Design Document

**Document type:** Design / architectural decision record. Maps every requirement in the companion spec to a concrete code-level decision. **Does not re-litigate requirements** — where the spec fixes a behavior, this document only records *how* it's implemented.

**Status:** Draft.
**Date:** 2026-04-21
**Authors:** Tej (review + decisions), Claude Opus 4.7 1M (drafting)
**Companion spec:** `docs/plans/2026-04-21-openai-compatible-providers-requirements.md`
**Prerequisite reading:** `docs/plans/2026-04-20-ollama-cloud-provider.md` (Ollama-specific invariants preserved by this design), PR #11 (the baseline this refactor builds on).

---

## 1. Summary

Introduce a generic `OpenAIHTTPBackend` targeting the OpenAI `/v1/chat/completions` contract, sharing the machinery (HTTP client, per-process semaphore bulkhead, file inlining, metrics hook, error mapping) that `OllamaBackend` already proves in production. The existing `OllamaBackend` stays on Ollama's native `/api/chat` endpoint unchanged — it ships a known-good metadata shape (`done_reason`, `prompt_eval_count`, `eval_count`) that callers and dashboards rely on, and MR-2's "dispatch identically" invariant is cheaper to meet by leaving it alone than by rewriting it.

Provider configuration is data, not code. Operators declare a JSON array in `ROUNDTABLE_PROVIDERS`; each entry names a provider id, base URL, credential env-var name, default model, and tuning knobs. Adding a new OpenAI-compatible provider after this refactor ships requires zero Go code — purely a config edit. Existing `OLLAMA_API_KEY`-based deployments continue to work via a legacy auto-registration path.

The `Backend` interface, `Result` shape, and MCP tool schemas are untouched (NFR-1/2/3).

## 2. Decisions at a glance

| Question | Decision |
|-|-|
| OQ-1 agent-spec shape | Unify on `provider`; `cli` stays as a deprecated alias accepted by ParseAgents |
| OQ-2 env-var scheme | `ROUNDTABLE_PROVIDERS` JSON blob + `api_key_env` indirection to a separate env var |
| OQ-3 validation timing | Dispatch-time; drop the hardcoded `validCLIs` whitelist |
| OQ-4 model resolution | Per-provider default lives in the backend (existing pattern) |
| OQ-5 registry shape | Flat `map[id]Backend` + sidecar `[]ProviderInfo` for enumeration |
| OQ-6 concurrency | Per-provider semaphore only; no global cap |
| Wire-format scope | Generic backend for OpenAI-compat; `OllamaBackend` stays on native `/api/chat` |
| SDK adoption | Stdlib only; revisit when scope grows to streaming or tool-calling |

## 3. Agent-spec schema (OQ-1)

### 3.1 Canonical JSON shape

```json
{
  "name":     "kimi-moonshot",
  "provider": "moonshot",
  "model":    "kimi-k2-0711-preview",
  "role":     "reviewer",
  "resume":   ""
}
```

- `provider` is the canonical field. Its value is the key the dispatcher uses to look up a `Backend`; values not present in the registered set dispatch to a per-agent `not_found`-class result (FR-10), *not* a ParseAgents-time rejection. Registered ids are the built-in `gemini`/`codex`/`claude`, every id declared in `ROUNDTABLE_PROVIDERS`, and the auto-registered `ollama` legacy provider (when active).
- `cli` is a deprecated alias. If only `cli` is set, `ParseAgents` copies its value into `Provider` and emits no warning (silent compatibility per MR-2). If **both** `cli` and `provider` are set, the entry is rejected at `ParseAgents` time with a clear error: *"specify `provider` or `cli` — not both"*.
- `name`, `model`, `role`, `resume` are unchanged.

### 3.2 In-memory shape

```go
type AgentSpec struct {
    Name     string
    Provider string   // was: CLI
    Model    string
    Role     string
    Resume   string
}
```

The Go field is renamed `CLI` → `Provider`. Every current caller of `agent.CLI` is updated (`resolveRole`, `resolveModel`, `resolveResume`, the dispatcher lookup in `run.go`). Internal rename; no external Go consumers (`internal/` package).

### 3.3 Dispatcher behavior

`Run` in `run.go` already looks up `backends[agent.CLI]` and emits `NotFoundResult` on miss (`run.go:286-290, 361-370`). Renaming the field to `Provider` keeps that logic intact — FR-10 is satisfied without new branches.

## 4. Provider configuration (OQ-2)

### 4.1 `ROUNDTABLE_PROVIDERS` JSON schema

One env var, one JSON array, one entry per provider:

```json
[
  {
    "id": "moonshot",
    "base_url": "https://api.moonshot.cn/v1",
    "api_key_env": "MOONSHOT_API_KEY",
    "default_model": "kimi-k2-0711-preview",
    "max_concurrent": 5,
    "response_header_timeout": "60s"
  },
  {
    "id": "zai",
    "base_url": "https://api.z.ai/v1",
    "api_key_env": "ZAI_API_KEY",
    "default_model": "glm-4.6",
    "max_concurrent": 3,
    "response_header_timeout": "60s"
  }
]
```

Fields:

| Field | Required | Type | Default | Notes |
|-|-|-|-|-|
| `id` | yes | string | — | Operator-chosen identifier. Non-empty, no collision with built-in ids (`gemini`, `codex`, `claude`). Colliding with `ollama` is permitted and suppresses legacy auto-registration (§4.3). |
| `base_url` | yes | string | — | Root URL; `/chat/completions` is appended at request time. |
| `api_key_env` | yes | string | — | Name of the env var holding the secret. Indirection per §4.2. |
| `default_model` | no | string | `""` | Used when `AgentSpec.Model` is empty. |
| `max_concurrent` | no | int | 3 | Per-process semaphore capacity. |
| `response_header_timeout` | no | string (time.Duration) | `"60s"` | `http.Transport.ResponseHeaderTimeout`. |

Unknown/misspelled JSON keys are reported as a parse error at startup. Missing required fields produce a parse error. Id collisions within the array produce a parse error.

### 4.2 Secret indirection

`api_key_env` names an env var; its value is read via `os.Getenv` at **request time**, matching Decision F from the Ollama plan (key rotation without restart).

Rationale: (a) the JSON blob becomes a config document, not a secret — safe to paste in bug reports, loggable at debug level; (b) operators can rotate a single secret without re-encoding JSON; (c) the indirection is explicit (no discovery, no scanning) — matches the "no magic, everything deterministic" principle.

`Healthy(ctx)` reports failure when `os.Getenv(apiKeyEnv) == ""`. `Run` does **not** re-validate the key — matching the pattern `ollama.go:210-214` establishes: the dispatcher's probe has already gated entry, and if the env var is cleared between probe and Run, the HTTP call surfaces a 401 that `openAIParseResponse` maps to `status: "error"`. Avoids a dead defensive check and keeps behavior uniform across backends.

### 4.3 Legacy auto-registration (MR-1)

When **no entry in `ROUNDTABLE_PROVIDERS` claims the id `ollama`** AND `OLLAMA_API_KEY` is set, the composition root auto-registers an `ollama` provider bound to `OllamaBackend` (native `/api/chat`, not the generic OpenAI backend). Config sources:

| Config field | Env var | Default |
|-|-|-|
| `base_url` | `OLLAMA_BASE_URL` | `https://ollama.com` |
| `api_key_env` | (fixed) `OLLAMA_API_KEY` | — |
| `default_model` | `OLLAMA_DEFAULT_MODEL` | `""` |
| `max_concurrent` | `OLLAMA_MAX_CONCURRENT_REQUESTS` | 3 |
| `response_header_timeout` | `OLLAMA_RESPONSE_HEADER_TIMEOUT` | 60s |

When `ROUNDTABLE_PROVIDERS` contains an entry with `id: "ollama"`, that entry wins (and is routed to the generic `OpenAIHTTPBackend` against Ollama Cloud's `/v1/chat/completions`). This gives operators an explicit migration path: *"register ollama yourself in JSON, and you opt into the OpenAI-compat dialect."* Default (no action) keeps native behavior.

### 4.4 Startup visibility (FR-24)

On startup, the composition root emits one structured log line per registered provider:

```
INFO provider registered  id=moonshot  base_url=https://api.moonshot.cn/v1  default_model=kimi-k2-0711-preview  max_concurrent=5  dialect=openai
```

`dialect` is `openai` for generic-backend providers and `ollama-native` for the legacy-registered Ollama entry.

The same information is exposed on `/metricsz` under `roundtable_providers_registered` (FR-24 machine-readable enumeration).

## 5. Components

### 5.1 `internal/roundtable/openai_http.go` (new)

Implements `Backend` for any OpenAI-compatible provider.

```go
type OpenAIHTTPBackend struct {
    id           string
    baseURL      string
    apiKeyEnv    string
    defaultModel string
    httpClient   *http.Client
    observe      ObserveFunc
    sem          *semaphore.Weighted
}

func NewOpenAIHTTPBackend(cfg ProviderConfig, observe ObserveFunc) *OpenAIHTTPBackend
```

- `Name()` → returns `id`.
- `Start()` / `Stop()` → no-ops.
- `Healthy(_)` → `os.Getenv(apiKeyEnv) != ""`. Offline only (FR-25).
- `Run(ctx, req)`:
  1. Deferred `observe(id, model, result.Status, elapsedMs)` (new signature; §5.6).
  2. Resolve `model`: `req.Model` else `defaultModel` else `ConfigErrorResult` (FR-11).
  3. `inlineFileContents(req.Files)` (shared helper; §5.4).
  4. Encode body: `{"model":X, "messages":[{"role":"user","content":Y}], "stream":false}`.
  5. `sem.Acquire(ctx, 1)` with the existing deadline-vs-cancel distinction from `ollama.go:258-285` (FR-21).
  6. POST `{baseURL}/chat/completions`, `Authorization: Bearer <os.Getenv(apiKeyEnv)>`.
  7. Parse via `openAIParseResponse` (§5.1.1).
  8. `BuildResult(...)`.

### 5.1.1 `openAIParseResponse`

Dual of `ollamaParseResponse`. Status mapping:

| HTTP status | `Result.Status` | Notes |
|-|-|-|
| 200 | `ok` (parse failure → `error`) | Extract `choices[0].message.content`. |
| 429 | `rate_limited` | Surface `Retry-After` on `Metadata["retry_after"]` (FR-15, FR-16). |
| 503 | `rate_limited` | Same. |
| other ≥400 | `error` | Upstream error body passed through. |
| 1xx/3xx/other | `error` | Not expected. |

Metadata populated on success (FR-18):

| Key | Source |
|-|-|
| `model_used` | `response.model` |
| `finish_reason` | `response.choices[0].finish_reason` |
| `tokens.prompt_tokens` | `response.usage.prompt_tokens` |
| `tokens.completion_tokens` | `response.usage.completion_tokens` |

On 429/503: `retry_after` on Metadata when the header is present; otherwise absent.

### 5.2 `internal/roundtable/ollama.go` (minimal changes)

Preserved intact except for two mechanical updates:

- Package-level constants `ollamaMaxFileBytes` / `ollamaMaxTotalFileBytes` are **renamed** to `defaultMaxFileBytes` / `defaultMaxTotalFileBytes` (both files in `internal/roundtable` share them; FR-31 generalization).
- The `ObserveFunc` call changes from `o.observe("ollama", result.Status, elapsedMs)` to `o.observe("ollama", model, result.Status, elapsedMs)` (§5.6).

The native-dialect response parser (`ollamaParseResponse`, emitting `done_reason` + `prompt_eval_count` + `eval_count`) is untouched. FR-17 explicitly accepts per-provider truncation-signal variance, so the asymmetry between `finish_reason` (generic) and `done_reason` (Ollama native) is by design.

`inlineFileContents` is already package-private; both backends call it directly without a "lift" — same package.

### 5.3 `internal/roundtable/run.go` changes

- `AgentSpec.CLI` → `AgentSpec.Provider` (field rename + JSON tag accepting both `provider` and `cli`).
- `ParseAgents`: delete the `validCLIs` whitelist; keep structural checks (non-empty `provider`, no duplicate `name`, no reserved `name`, reject the `cli`+`provider` collision).
- `resolveRole`, `resolveModel`, `resolveResume`: switch on `agent.Provider` instead of `agent.CLI`. Existing per-CLI request-level overrides (`req.GeminiModel`, `req.CodexModel`, `req.ClaudeModel`, matching role fields) remain keyed on the four built-in ids; HTTP providers read their defaults from their own backend config and never consult the per-CLI request overrides.
- `defaultAgents()` docstring rewritten to reference *"no HTTP-native provider is ever default"* as the generalized invariant (NFR-12).

### 5.4 Shared HTTP helpers

No new file. Both backends live in `internal/roundtable` and share package-private helpers:

- `inlineFileContents([]string) string` (unchanged location: `ollama.go`).
- `defaultMaxFileBytes`, `defaultMaxTotalFileBytes` constants (renamed from ollama-prefixed).
- A new small helper `newHTTPTransport(responseHeaderTimeout time.Duration) *http.Transport` extracted from `ollama.go:142-159` — used by both `NewOllamaBackend` and `NewOpenAIHTTPBackend`.
- A new small helper `buildRateLimitedResult(body []byte, statusCode int, retryAfter, providerLabel string) ParsedOutput` — generalizes `ollamaRateLimitedOutput`. Ollama's existing string wording (`"Ollama rate limited (HTTP %d): ..."`) is preserved by passing `providerLabel = "Ollama"`; new providers pass their id.

### 5.5 `cmd/roundtable-http-mcp/main.go` changes

`buildBackends` is restructured:

```go
func buildBackends(
    logger *slog.Logger,
    observe roundtable.ObserveFunc,
) (map[string]roundtable.Backend, []roundtable.ProviderInfo) {

    backends := map[string]roundtable.Backend{
        "gemini": roundtable.NewGeminiBackend(""),
        "codex":  codexBackend, // existing construction above
        "claude": roundtable.NewClaudeBackend(""),
    }
    var infos []roundtable.ProviderInfo

    configs, err := roundtable.LoadProviderRegistry(os.Getenv)
    if err != nil {
        logger.Error("ROUNDTABLE_PROVIDERS parse failed — no HTTP providers registered", "error", err)
        // intentionally proceed; subprocess backends still work
    }
    for _, c := range configs {
        backends[c.ID] = roundtable.NewOpenAIHTTPBackend(c, observe)
        infos = append(infos, roundtable.ProviderInfo{ID: c.ID, BaseURL: c.BaseURL, DefaultModel: c.DefaultModel, Dialect: "openai"})
        logger.Info("provider registered", "id", c.ID, "base_url", c.BaseURL, "default_model", c.DefaultModel, "max_concurrent", c.MaxConcurrent, "dialect", "openai")
    }

    // Legacy auto-registration (MR-1). Runs only if JSON didn't claim "ollama".
    if _, claimed := backends["ollama"]; !claimed && os.Getenv("OLLAMA_API_KEY") != "" {
        defaultModel := os.Getenv("OLLAMA_DEFAULT_MODEL")
        backends["ollama"] = roundtable.NewOllamaBackend(defaultModel, observe)
        baseURL := os.Getenv("OLLAMA_BASE_URL")
        if baseURL == "" { baseURL = "https://ollama.com" }
        infos = append(infos, roundtable.ProviderInfo{ID: "ollama", BaseURL: baseURL, DefaultModel: defaultModel, Dialect: "ollama-native"})
        logger.Info("provider registered", "id", "ollama", "base_url", baseURL, "default_model", defaultModel, "dialect", "ollama-native")
    }

    return backends, infos
}
```

`infos` threads to `httpmcp.NewApp` for `/metricsz` exposure.

### 5.6 `internal/httpmcp/metrics.go` changes

Label shape: `(backend, status)` → `(provider, model, status)` per FR-27.

```go
// internal/roundtable/backend_observe.go (or co-located in backend.go)
type ObserveFunc func(provider, model, status string, elapsedMs int64)

// internal/httpmcp/metrics.go
func (m *Metrics) ObserveProvider(provider, model, status string, elapsedMs int64)
```

- Counter map key format: `provider/model/status` (was `backend/status`).
- Duration sum/count maps keyed by `provider/model`.
- JSON output keys use Prometheus convention: `roundtable_provider_requests_total`, `roundtable_provider_request_duration_ms_sum`, `roundtable_provider_request_duration_ms_count`.
- The old `ObserveBackend` / `roundtable_backend_*` names are removed (single-PR refactor, no deprecation window — consumers are internal to this repo).

`metrics_test.go` updated to assert the new key shape (MR-3).

Cardinality (FR-28): `provider` is bounded by operator config (single-digit); `model` is bounded by `default_model` per provider plus whatever agent specs request (low double digits at most in any real deployment). No unbounded label.

## 6. Provider registry (OQ-5)

```go
// internal/roundtable/providers.go (new)

type ProviderConfig struct {
    ID                    string
    BaseURL               string
    APIKeyEnv             string
    DefaultModel          string
    MaxConcurrent         int
    ResponseHeaderTimeout time.Duration
}

type ProviderInfo struct {
    ID           string
    BaseURL      string
    DefaultModel string
    Dialect      string // "openai" or "ollama-native"
}

// LoadProviderRegistry parses ROUNDTABLE_PROVIDERS via the given getenv
// function (injected for testability). Returns (nil, nil) when unset.
// Returns (nil, error) on parse or validation failure; caller logs and
// proceeds without HTTP providers.
func LoadProviderRegistry(getenv func(string) string) ([]ProviderConfig, error)
```

`LoadProviderRegistry` applies defaults, validates required fields, rejects id collisions with built-in ids (`gemini`/`codex`/`claude`) and within the array itself. Registry is immutable after startup (NFR-9).

## 7. Concurrency model (OQ-6)

Each provider owns its `semaphore.Weighted` sized by its `MaxConcurrent`. No cross-provider coordination — a stall in one provider's Acquire queue is invisible to every other provider (FR-14).

The existing `ollamaGateSlowLogThreshold` (100ms) is renamed `httpGateSlowLogThreshold` and used by both backends (FR-29). Value unchanged.

## 8. Data flow (success path)

```
Caller (agents JSON)
  → ParseAgents                      (structural validation; cli→provider alias)
  → resolveAgents
  → per-agent: resolveRole / resolveModel / resolveResume
  → dispatcher looks up backends[agent.Provider]
  → OpenAIHTTPBackend.Healthy        (offline; env(apiKeyEnv) present)
  → OpenAIHTTPBackend.Run:
        inlineFileContents(req.Files)
        sem.Acquire(ctx)
        POST {baseURL}/chat/completions  (Bearer env(apiKeyEnv))
        openAIParseResponse(body, status, retryAfter)
        BuildResult(..., ParsedOutput, model)
        observe(provider, model, status, elapsedMs)
  → Result into DispatchResult.Results[agent.Name]
```

Unknown `agent.Provider` → `backends[agent.Provider] == nil` → existing `NotFoundResult` path (FR-10). Nothing new in the dispatcher.

## 9. Testing strategy (NFR-4, NFR-5, NFR-6)

New test files:

- `openai_http_test.go` — mirrors `ollama_test.go`: `httptest.Server` fixtures for 200, 401, 429 (with and without `Retry-After`), 503, malformed JSON, slow-response (ctx-deadline), ctx-cancel mid-flight, concurrency-gate deadline.
- `providers_test.go` — `LoadProviderRegistry`: well-formed JSON, malformed JSON, missing required fields, id collisions with built-ins, id collisions within the array, default application, `time.Duration` parsing, empty env, nil getenv result.

Modified:

- `run_test.go`:
  - `TestDefaultAgents_ExcludesOllama` → `TestDefaultAgents_ExcludesAllHTTPProviders` (FR-22 generalization).
  - New: `TestParseAgents_AcceptsCLIAsDeprecatedAlias` (MR-2).
  - New: `TestParseAgents_RejectsBothCLIAndProvider`.
  - New: `TestParseAgents_AcceptsUnknownProvider` (validation deferred to dispatch, FR-10).
- `metrics_test.go` — new label shape.

AC-2 test: fan-out against two `httptest.Server` instances (one fast, one slow) under one dispatch; assert both results arrive and the fast result completes in a bound that excludes the slow one's delay (isolation per FR-14).

No real network calls in `go test ./...`. Any live-credential integration test gated behind `//go:build integration` (NFR-5).

## 10. Migration (MR-1, MR-2, MR-3)

- **MR-1 (`OLLAMA_API_KEY`-only deployments):** no action required. Legacy auto-registration path (§4.3) handles them. `INSTALL.md` documents both paths (legacy and new).
- **MR-2 (existing agent specs):** `{"cli":"ollama","model":"kimi-k2.6:cloud"}` → `ParseAgents` accepts `cli` as alias, dispatches against the auto-registered `OllamaBackend`, which hits the same native `/api/chat` endpoint as before. Bit-for-bit identical Result (including `done_reason` metadata). Same for `{"cli":"gemini"}` / `{"cli":"codex"}` / `{"cli":"claude"}` against subprocess backends.
- **MR-3 (tests):** Ollama-specific assertions preserved where they test contracts (FR-15/16/17/18). Tests asserting now-renamed symbols (`ollamaMaxFileBytes` → `defaultMaxFileBytes`) update their references; the underlying property tested is unchanged.

## 11. Dependencies (NFR-7, NFR-8)

No new external modules. `net/http`, `encoding/json`, `time`, `strings`, `os`, `log/slog`, `golang.org/x/sync/semaphore` — all already in `go.mod`.

SDK adoption explicitly deferred. The OpenAI wire-shape work for one dialect is ~150 lines of stdlib, which is less code than adapting to `openai-go`'s typed request/response structs would be. Revisit when scope grows to streaming (NG1) or tool-calling (C-5) — then an SDK earns its keep.

## 12. Documentation (NFR-10, NFR-11, NFR-12)

`INSTALL.md` gains:

- New "Providers" section replacing the Ollama-specific block.
- One canonical `ROUNDTABLE_PROVIDERS` example with three entries (moonshot, zai, ollama-as-openai-compat).
- For each shipped provider id in the example: required env vars and one agent-spec JSON snippet.
- A migration subsection: *"if you only have `OLLAMA_API_KEY` set, nothing changes for you."*

`defaultAgents()` docstring updated to reference the generalized invariant (NFR-12).

## 13. Explicit non-changes

Preserving these unchanged is a design feature, not an oversight:

- `Backend` interface (`internal/roundtable/backend.go`): signature identical (NFR-1).
- `Result` and `ParsedOutput` types: no new required fields (NFR-2).
- MCP tool schemas (`hivemind`, `deepdive`, `architect`, `challenge`, `xray`): no new required fields (NFR-3).
- `/readyz` behavior: unchanged; no provider contributes to readiness (FR-26, C-4).
- Retry policy: still none (C-2, Decision C).
- Status values: only `ok` / `rate_limited` / `timeout` / `error` (FR-20).

## 14. Known risks

- **Label cardinality growth.** If an operator registers many providers with many model ids each, `/metricsz` labels multiply. FR-28 notes expected cardinality is low; no enforcement. Accepted risk.
- **JSON-embedded config is fragile.** A single missing comma in `ROUNDTABLE_PROVIDERS` disables every HTTP provider for that process. Mitigated by (a) the startup log line per registered provider making absence visible and (b) a loud `ERROR` log on parse failure. Still: a failure mode to keep in mind — flagged in `INSTALL.md`.
- **Silent rename of `OllamaBackend`'s metric label.** The metric key format changes from `ollama/<status>` to `ollama/<model>/<status>`. Any existing Grafana dashboard reading `roundtable_backend_requests_total` breaks. This is a single-PR refactor with no deprecation window; operators update dashboards alongside the upgrade. Documented in release notes.
- **Ollama's native vs OpenAI-compat endpoints.** This design deliberately keeps Ollama on native. If Ollama deprecates `/api/chat` (not currently signaled), migration is: register `ollama` in JSON with the generic backend and remove `OllamaBackend`. Path is clear; design doesn't take it today.

---

## Appendix A: File map

| Path | Change |
|-|-|
| `internal/roundtable/openai_http.go` | NEW — `OpenAIHTTPBackend` + `openAIParseResponse` |
| `internal/roundtable/providers.go` | NEW — `ProviderConfig`, `ProviderInfo`, `LoadProviderRegistry` |
| `internal/roundtable/ollama.go` | MODIFIED — rename shared constants; update `observe` signature; extract shared transport helper |
| `internal/roundtable/run.go` | MODIFIED — `CLI`→`Provider`; drop `validCLIs`; JSON alias; error on `cli`+`provider` collision |
| `internal/roundtable/backend.go` | UNCHANGED |
| `internal/roundtable/result.go` | UNCHANGED |
| `cmd/roundtable-http-mcp/main.go` | MODIFIED — `buildBackends` returns `(map, []ProviderInfo)`; registry wiring; legacy fallback |
| `internal/httpmcp/metrics.go` | MODIFIED — `(provider, model, status)` label shape; rename `ObserveBackend` → `ObserveProvider` |
| `internal/httpmcp/server.go` | MODIFIED — expose `ProviderInfo` list on `/metricsz` |
| `INSTALL.md` | MODIFIED — new Providers section; migration note |
| `openai_http_test.go`, `providers_test.go` | NEW |
| `ollama_test.go`, `run_test.go`, `metrics_test.go` | MODIFIED per §9 |

## Appendix B: Requirements traceability (partial — interesting mappings only)

| Requirement | Where realized |
|-|-|
| FR-7 provider + model in spec | §3.1 |
| FR-8 same model, multiple providers, same dispatch | §8 (no coupling) |
| FR-10 unknown provider → not_found | §3.3 (existing dispatcher path) |
| FR-11 missing model | §5.1 step 2 |
| FR-14 provider isolation | §7 (per-provider semaphore only) |
| FR-16 Retry-After | §5.1.1 |
| FR-17 truncation signal | §5.1.1 (`finish_reason`) + §5.2 (Ollama `done_reason`) |
| FR-21 deadline vs cancel | §5.1 step 5 (existing pattern) |
| FR-22 defaults exclude HTTP | §5.3 (generalized test) |
| FR-24 enumeration | §4.4, §5.5, §5.6 |
| FR-25 offline Healthy | §5.1 |
| FR-27 metrics labels | §5.6 |
| FR-31/32 file inlining + visibility | §5.4 (shared helpers, existing semantics) |
| MR-1 legacy OLLAMA_API_KEY path | §4.3 |
| MR-2 existing agent specs | §3.1, §10 |
| NFR-1/2/3 interface/Result/schema unchanged | §13 |
