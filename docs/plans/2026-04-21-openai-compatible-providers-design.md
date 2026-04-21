# OpenAI-Compatible HTTP Providers — Design Document

**Document type:** Design / architectural decision record. Maps every active requirement in the companion spec to a concrete code-level decision.

**Status:** Draft.
**Date:** 2026-04-21
**Authors:** Tej (review + decisions), Claude Opus 4.7 1M (drafting)
**Companion spec:** `docs/plans/2026-04-21-openai-compatible-providers-requirements.md`
**Prerequisite reading:** `docs/plans/2026-04-20-ollama-cloud-provider.md` (operational invariants — offline `Healthy`, per-process bulkhead, PII no-log — preserved by this design), PR #11 (the baseline this refactor supersedes).

---

## 0. Requirements amendments (2026-04-21)

Per product-owner decision during the design session, **Migration Requirements MR-1/MR-2/MR-3 are waived**. Roundtable has a single operator today; backward compatibility with PR-#11 Ollama-specific configuration buys nothing. The consequences propagate through this design:

- No `cli` field alias in `ParseAgents`.
- No legacy `OLLAMA_API_KEY`-based auto-registration.
- `OllamaBackend` and its native `/api/chat` dialect are **deleted**, not preserved. Ollama becomes an entry in `ROUNDTABLE_PROVIDERS` against `https://ollama.com/v1/chat/completions` like every other provider.
- Metrics are cleanly renamed from `roundtable_backend_*` to `roundtable_provider_*` with no deprecation window.
- PR #11's Ollama-specific tests are replaced by tests against the generic OpenAI HTTP backend; the underlying contractual properties (offline `Healthy`, 429/503→`rate_limited`, `Retry-After` surfaced on `Metadata`, file inlining) carry over.

All functional requirements (FR-1..FR-32), non-functional requirements (NFR-1..NFR-12), and constraints (C-1..C-5) in the companion spec remain in force.

## 1. Summary

One generic `OpenAIHTTPBackend` implements the `Backend` interface against any provider that speaks OpenAI `/v1/chat/completions`. Operators declare providers as a JSON array in `ROUNDTABLE_PROVIDERS`; each entry names an id, base URL, credential env-var name, default model, concurrency cap, and response-header timeout. The composition root loops over the array and registers one backend per entry, keyed by id.

The `Backend` interface, `Result` shape, and MCP tool schemas are untouched (NFR-1/2/3).

## 2. Decisions at a glance

| Question | Decision |
|-|-|
| OQ-1 agent-spec shape | Single `provider` field. No aliases. |
| OQ-2 env-var scheme | `ROUNDTABLE_PROVIDERS` JSON blob + `api_key_env` indirection to a separate env var |
| OQ-3 validation timing | Dispatch-time; unknown `provider` → per-agent `not_found`. No `validCLIs` whitelist. |
| OQ-4 model resolution | Per-provider default lives in the backend |
| OQ-5 registry shape | Flat `map[id]Backend` + sidecar `[]ProviderInfo` for enumeration |
| OQ-6 concurrency | Per-provider semaphore only; no global cap |
| Wire-format scope | OpenAI `/v1/chat/completions` for every HTTP provider, Ollama included |
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

- `provider` is the only field naming the backend. Subprocess backends (`gemini`, `codex`, `claude`) use it too: e.g. `{"provider":"gemini"}`.
- `cli` is **not accepted**. Specs using the old `cli` field fail `ParseAgents` with: *"unknown field `cli`; use `provider`"*.
- `name`, `model`, `role`, `resume` are unchanged from the pre-refactor `AgentSpec`.

### 3.2 In-memory shape

```go
type AgentSpec struct {
    Name     string
    Provider string
    Model    string
    Role     string
    Resume   string
}
```

`AgentSpec.CLI` renamed to `AgentSpec.Provider`. Every caller updated (`resolveRole`, `resolveModel`, `resolveResume`, the dispatcher lookup in `run.go`). Internal package; no external Go consumers.

### 3.3 Dispatcher behavior

`Run` in `run.go` looks up `backends[agent.Provider]` and emits `NotFoundResult` on miss (existing path at `run.go:286-290, 361-370`, updated for the renamed field). FR-10 satisfied without new branches.

## 4. Provider configuration (OQ-2)

### 4.1 `ROUNDTABLE_PROVIDERS` JSON schema

One env var, one JSON array, one entry per provider. Example (canonical shape for `INSTALL.md`):

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
  },
  {
    "id": "ollama",
    "base_url": "https://ollama.com/v1",
    "api_key_env": "OLLAMA_API_KEY",
    "default_model": "kimi-k2.6:cloud",
    "max_concurrent": 3,
    "response_header_timeout": "60s"
  }
]
```

Fields:

| Field | Required | Type | Default | Notes |
|-|-|-|-|-|
| `id` | yes | string | — | Operator-chosen identifier. Non-empty, no collision with built-in subprocess ids (`gemini`, `codex`, `claude`), no duplicates within the array. |
| `base_url` | yes | string | — | Root URL; `/chat/completions` is appended at request time. |
| `api_key_env` | yes | string | — | Name of the env var holding the secret. Indirection per §4.2. |
| `default_model` | no | string | `""` | Used when `AgentSpec.Model` is empty. |
| `max_concurrent` | no | int | 3 | Per-process semaphore capacity. |
| `response_header_timeout` | no | string (time.Duration) | `"60s"` | `http.Transport.ResponseHeaderTimeout`. |

Unknown JSON keys, missing required fields, id collisions, and unparseable durations all produce a parse error at startup. The composition root logs the error and proceeds with subprocess-only backends — the process does not exit (FR-3).

### 4.2 Secret indirection

`api_key_env` names an env var; its value is read via `os.Getenv` at **request time**, matching Decision F from the Ollama plan (key rotation without restart).

Rationale: (a) the JSON blob becomes a config document, not a secret — safe to paste in bug reports, loggable at debug level; (b) operators can rotate a single secret without re-encoding JSON; (c) the indirection is explicit (no discovery, no scanning) — matches the "no magic, everything deterministic" principle.

`Healthy(ctx)` reports failure when `os.Getenv(apiKeyEnv) == ""`. `Run` does **not** re-validate — matching the pattern at `ollama.go:210-214`: the dispatcher's probe gates entry, and if the env var is cleared between probe and Run, the HTTP call surfaces a 401 that `openAIParseResponse` maps to `status: "error"`. No dead defensive check.

### 4.3 Startup visibility (FR-24)

On startup, the composition root emits one structured log line per registered provider:

```
INFO provider registered  id=moonshot  base_url=https://api.moonshot.cn/v1  default_model=kimi-k2-0711-preview  max_concurrent=5
```

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
  1. Deferred `observe(id, model, result.Status, elapsedMs)` (new signature; §5.5).
  2. Resolve `model`: `req.Model` else `defaultModel` else `ConfigErrorResult` (FR-11).
  3. `inlineFileContents(req.Files)` (existing helper, moved from `ollama.go` intact).
  4. Encode body: `{"model":X, "messages":[{"role":"user","content":Y}], "stream":false}`.
  5. `sem.Acquire(ctx, 1)` with the existing deadline-vs-cancel distinction from `ollama.go:258-285` (FR-21). Logic copied verbatim; error messages generalize the provider label.
  6. POST `{baseURL}/chat/completions`, `Authorization: Bearer <os.Getenv(apiKeyEnv)>`.
  7. Parse via `openAIParseResponse` (§5.1.1).
  8. `BuildResult(...)`.

### 5.1.1 `openAIParseResponse`

Status mapping:

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
| `finish_reason` | `response.choices[0].finish_reason` (FR-17 truncation signal) |
| `tokens.prompt_tokens` | `response.usage.prompt_tokens` |
| `tokens.completion_tokens` | `response.usage.completion_tokens` |

On 429/503: `retry_after` on Metadata when the header is present; otherwise absent.

### 5.2 `internal/roundtable/ollama.go` — DELETED

`OllamaBackend`, `ollamaParseResponse`, `ollamaRateLimitedOutput`, `ollamaErrorOutput`, `ollamaExtractErrorMessage`, and every `ollama*` constant are removed. Salvaged helpers:

- `inlineFileContents([]string) string` moves to **`internal/roundtable/files.go`** (new file, same package; no behavior change).
- `defaultMaxFileBytes` / `defaultMaxTotalFileBytes` constants move to `files.go` (renamed from their ollama-prefixed names; FR-31 generalization).
- The `http.Transport` construction (`ollama.go:142-159`) lifts to a small helper `newHTTPTransport(responseHeaderTimeout time.Duration) *http.Transport` in **`internal/roundtable/openai_http.go`** — used by `NewOpenAIHTTPBackend`.
- `httpGateSlowLogThreshold = 100 * time.Millisecond` (renamed from `ollamaGateSlowLogThreshold`) lives alongside the backend.

### 5.3 `internal/roundtable/run.go` changes

- Rename `AgentSpec.CLI` → `AgentSpec.Provider`. JSON tag is `"provider"` (no alias).
- `ParseAgents`:
  - Accept only the `provider` field from input. A present-but-unknown JSON key `cli` produces an error: *`"unknown field \"cli\"; use \"provider\" instead"`*. (Implementation note: use `json.Decoder.DisallowUnknownFields` on a typed struct, or explicit key-set checking on the existing `map[string]any` path.)
  - Empty `provider` → error.
  - Delete `validCLIs` map entirely.
  - Structural checks retained: non-duplicate `name`, non-reserved `name`.
- `resolveRole`, `resolveModel`, `resolveResume`: switch on `agent.Provider` instead of `agent.CLI`. The switch arms for `"gemini"` / `"codex"` / `"claude"` stay; every HTTP provider falls through to the default branch (returns `""`), which lets the backend's own default take over. Per-CLI request-level overrides (`req.GeminiModel` etc.) continue to apply **only** to the three subprocess backends.
- `defaultAgents()` docstring rewritten to generalize: *"no HTTP-native provider is ever default — opt in explicitly via agents JSON or `ROUNDTABLE_DEFAULT_AGENTS`"* (NFR-12).

### 5.4 `cmd/roundtable-http-mcp/main.go` changes

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
        return backends, infos
    }
    for _, c := range configs {
        backends[c.ID] = roundtable.NewOpenAIHTTPBackend(c, observe)
        infos = append(infos, roundtable.ProviderInfo{
            ID: c.ID, BaseURL: c.BaseURL, DefaultModel: c.DefaultModel,
        })
        logger.Info("provider registered",
            "id", c.ID, "base_url", c.BaseURL, "default_model", c.DefaultModel,
            "max_concurrent", c.MaxConcurrent)
    }
    return backends, infos
}
```

`infos` threads to `httpmcp.NewApp` for `/metricsz` exposure.

Legacy env-var branch is removed. Any operator previously on `OLLAMA_API_KEY` sets `ROUNDTABLE_PROVIDERS` once — documented in `INSTALL.md`.

### 5.5 `internal/httpmcp/metrics.go` changes

Label shape: `(backend, status)` → `(provider, model, status)` per FR-27.

```go
// internal/roundtable/backend.go (or a sibling)
type ObserveFunc func(provider, model, status string, elapsedMs int64)

// internal/httpmcp/metrics.go
func (m *Metrics) ObserveProvider(provider, model, status string, elapsedMs int64)
```

- Counter map key: `provider/model/status`.
- Duration sum/count maps keyed by `provider/model`.
- JSON output keys: `roundtable_provider_requests_total`, `roundtable_provider_request_duration_ms_sum`, `roundtable_provider_request_duration_ms_count`.
- Old `ObserveBackend` / `roundtable_backend_*` removed outright.

`metrics_test.go` updated accordingly.

Cardinality (FR-28): `provider` is bounded by operator config (single-digit); `model` is bounded by `default_model` per provider plus what agent specs request (low double digits at most). No unbounded label.

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
}

// LoadProviderRegistry parses ROUNDTABLE_PROVIDERS via the given getenv
// function (injected for testability). Returns (nil, nil) when unset.
// Returns (nil, error) on parse or validation failure.
func LoadProviderRegistry(getenv func(string) string) ([]ProviderConfig, error)
```

Validates required fields, applies defaults, rejects id collisions with built-in subprocess ids and within the array. Registry is immutable after startup (NFR-9).

## 7. Concurrency model (OQ-6)

Each provider owns its `semaphore.Weighted` sized by `MaxConcurrent`. No cross-provider coordination (FR-14). `httpGateSlowLogThreshold` (100ms) applied uniformly on Acquire (FR-29).

## 8. Data flow (success path)

```
Caller (agents JSON)
  → ParseAgents                      (structural validation; unknown `cli` key rejected)
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

Unknown `agent.Provider` → `backends[agent.Provider] == nil` → existing `NotFoundResult` path (FR-10).

## 9. Testing strategy (NFR-4, NFR-5, NFR-6)

New test files:

- `openai_http_test.go` — `httptest.Server` fixtures for 200, 401, 429 (with and without `Retry-After`), 503, malformed JSON, slow-response (ctx-deadline), ctx-cancel mid-flight, concurrency-gate deadline. Covers the contractual properties PR #11's `ollama_test.go` tested, re-expressed for the generic backend.
- `providers_test.go` — `LoadProviderRegistry`: well-formed JSON, malformed JSON, missing required fields, id collisions with built-ins, id collisions within the array, default application, `time.Duration` parsing, empty env.
- `files_test.go` — lifted file-inlining tests (from the deleted `ollama_test.go`) for `inlineFileContents` in its new home.

Modified:

- `run_test.go`:
  - `TestDefaultAgents_ExcludesOllama` → `TestDefaultAgents_ExcludesAllHTTPProviders` (FR-22 generalization).
  - New: `TestParseAgents_RejectsCLIField`.
  - New: `TestParseAgents_AcceptsUnknownProvider` (validation deferred to dispatch, FR-10).
- `metrics_test.go` — new label shape.

Deleted:

- `ollama_test.go` — every assertion is either migrated to `openai_http_test.go` (contractual) or retired (Ollama-native-dialect specifics).

AC-2 test: fan-out against two `httptest.Server` instances (one fast, one slow) under one dispatch; assert both results arrive and the fast result completes in a bound that excludes the slow one's delay (isolation per FR-14).

No real network calls in `go test ./...`. Any live-credential integration test gated behind `//go:build integration` (NFR-5).

## 10. Dependencies (NFR-7, NFR-8)

No new external modules. `net/http`, `encoding/json`, `time`, `strings`, `os`, `log/slog`, `golang.org/x/sync/semaphore` — all already in `go.mod`.

SDK adoption explicitly deferred. The OpenAI wire-shape work is ~150 lines of stdlib, less code than adapting to `openai-go`'s typed request/response structs would be. Revisit when scope grows to streaming (NG1) or tool-calling (C-5).

## 11. Documentation (NFR-10, NFR-11)

`INSTALL.md` gains a new "Providers" section replacing the Ollama-specific block:

- The canonical `ROUNDTABLE_PROVIDERS` example with three entries (moonshot, zai, ollama).
- For each provider id in the example: the required secret env var and one agent-spec JSON snippet.
- One paragraph noting the legacy `OLLAMA_API_KEY`/`OLLAMA_BASE_URL`/etc. vars are no longer read; the operator sets `ROUNDTABLE_PROVIDERS` as the single source of truth.

`defaultAgents()` docstring updated to reference the generalized invariant (NFR-12).

## 12. Explicit non-changes

Preserving these unchanged is a design feature:

- `Backend` interface (`internal/roundtable/backend.go`): signature identical (NFR-1).
- `Result` and `ParsedOutput` types: no new required fields (NFR-2).
- MCP tool schemas (`hivemind`, `deepdive`, `architect`, `challenge`, `xray`): no new required fields (NFR-3).
- `/readyz` behavior: unchanged; no provider contributes to readiness (FR-26, C-4).
- Retry policy: still none (C-2, Decision C).
- Status values: only `ok` / `rate_limited` / `timeout` / `error` (FR-20).

## 13. Known risks

- **Ollama OpenAI-compat dialect acceptance.** This design assumes Ollama Cloud's `/v1/chat/completions` accepts the `:cloud`-suffix model IDs (e.g. `kimi-k2.6:cloud`) and returns tokens in OpenAI `usage.*` shape. Ollama does ship an OpenAI-compat shim; the exact `:cloud` suffix behavior on that endpoint is a **verification item for the implementation plan's first step** (smoke test with a live key before committing to the new backend). If the compat shim rejects `:cloud` suffixes, the fallback is to use the non-suffixed model id and rely on Ollama Cloud's default routing — or, worst case, restore a minimal Ollama-native path. The risk is visible, bounded, and discoverable early.
- **JSON-embedded config is fragile.** A single missing comma in `ROUNDTABLE_PROVIDERS` disables every HTTP provider for that process. Mitigated by (a) the startup log line per registered provider making absence visible, (b) a loud `ERROR` log on parse failure with the JSON error message included, (c) `providers_test.go` covering common malformations. A failure mode to keep in mind — flagged in `INSTALL.md`.
- **Label cardinality growth.** If an operator registers many providers with many model ids each, `/metricsz` labels multiply. FR-28 notes expected cardinality is low; no enforcement. Accepted risk.

---

## Appendix A: File map

| Path | Change |
|-|-|
| `internal/roundtable/openai_http.go` | NEW — `OpenAIHTTPBackend`, `openAIParseResponse`, `newHTTPTransport`, `httpGateSlowLogThreshold` |
| `internal/roundtable/providers.go` | NEW — `ProviderConfig`, `ProviderInfo`, `LoadProviderRegistry` |
| `internal/roundtable/files.go` | NEW — `inlineFileContents`, `defaultMaxFileBytes`, `defaultMaxTotalFileBytes` (lifted from `ollama.go`) |
| `internal/roundtable/ollama.go` | DELETED |
| `internal/roundtable/ollama_test.go` | DELETED (contracts migrated to `openai_http_test.go`; file-inlining tests to `files_test.go`) |
| `internal/roundtable/run.go` | MODIFIED — `CLI`→`Provider`; drop `validCLIs`; reject `cli` JSON key |
| `internal/roundtable/backend.go` | UNCHANGED |
| `internal/roundtable/result.go` | UNCHANGED |
| `cmd/roundtable-http-mcp/main.go` | MODIFIED — `buildBackends` returns `(map, []ProviderInfo)`; registry wiring; legacy branch removed |
| `internal/httpmcp/metrics.go` | MODIFIED — `(provider, model, status)` label shape; `ObserveBackend` → `ObserveProvider` |
| `internal/httpmcp/server.go` | MODIFIED — expose `ProviderInfo` list on `/metricsz` |
| `INSTALL.md` | MODIFIED — new Providers section; legacy env vars removed from docs |
| `openai_http_test.go`, `providers_test.go`, `files_test.go` | NEW |
| `run_test.go`, `metrics_test.go` | MODIFIED per §9 |

## Appendix B: Requirements traceability (interesting mappings)

| Requirement | Where realized |
|-|-|
| FR-3 absent credentials → unregistered | §4.1 (parse error path) + §5.4 (composition root) |
| FR-7 provider + model in spec | §3.1 |
| FR-8 same model, multiple providers, same dispatch | §8 (no coupling) |
| FR-10 unknown provider → not_found | §3.3 (existing dispatcher path) |
| FR-11 missing model | §5.1 step 2 |
| FR-14 provider isolation | §7 (per-provider semaphore only) |
| FR-16 Retry-After | §5.1.1 |
| FR-17 truncation signal | §5.1.1 (`finish_reason`) |
| FR-21 deadline vs cancel | §5.1 step 5 (existing pattern) |
| FR-22 defaults exclude HTTP | §5.3 (generalized test) |
| FR-24 enumeration | §4.3, §5.4, §5.5 |
| FR-25 offline Healthy | §5.1 |
| FR-27 metrics labels | §5.5 |
| FR-31/32 file inlining + visibility | §5.2 (moved helper, semantics preserved) |
| NFR-1/2/3 interface / Result / schema unchanged | §12 |
