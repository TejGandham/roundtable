# Ollama Cloud Provider — Architectural Report (Draft)

**Status:** Final (post-roundtable review + user decisions on open questions)
**Date:** 2026-04-20
**Author:** `architect` agent (Opus 4.7, 1M ctx) + external research + hivemind (claude-opus-4-7, codex, gemini-3.1-pro)

> **Revision notes:** §4.1 `Healthy()` tightened to offline-only after all three reviewers flagged concurrent probes self-DoSing on Ollama's 1/3/10 concurrency cap. §4.1 also gains explicit `http.Client` transport config, `io.LimitReader` body cap, runtime env read (no factory-time key freeze), and timeout→status mapping. §4.3 switched from recommending (b) symmetric to (a) minimal; churn count revised upward. §5 gains `done_reason`-as-metadata, `/readyz` impact, and a new config-error result type (not `NotFoundResult`). §6 (OpenRouter alternative) removed — out of scope. §9 decisions locked by user: offline-`Healthy()` invariant, Prometheus-idiomatic metric naming, no Phase 3.

## 1. Problem

Roundtable currently dispatches to three CLI-harness-backed providers (`gemini`, `codex`, `claude`) via subprocess. The user wants to add Ollama's cloud-hosted `:cloud` models:

- `kimi-k2.5:cloud` — reasoning, multimodal, subagent-friendly
- `qwen3.5:cloud` — reasoning + coding + agentic tool use
- `glm-5.1:cloud` — reasoning + code generation (top SWE-Bench Pro in April 2026)
- `minimax-m2.7:cloud` — fast coding / productivity

These models have **no console harness**. They're reached via HTTPS with a bearer token. That makes them the first HTTP-native provider in Roundtable.

## 2. What Roundtable expects from a provider today

Verified from source (`internal/roundtable/backend.go:15-39`):

```go
type Backend interface {
    Name() string
    Start(ctx) error
    Stop() error
    Healthy(ctx) error
    Run(ctx, Request) (*Result, error)
}
```

Key properties of the contract that matter here:

- Backends are **stateless across calls** from the dispatcher's POV (concurrent `Healthy()`/`Run()` must be safe; `run.go:233`).
- The dispatcher does a probe phase (5s `Healthy`) then a run phase (`timeout + 30s` grace). Good news for HTTP: no session to pre-warm.
- `Run()` returns a single final `Result` — streaming isn't plumbed end-to-end anywhere.
- Keys in `buildBackends()` (`cmd/roundtable-http-mcp/main.go:151`) equal the `CLI` field on `AgentSpec`. That's how `{cli: "gemini"}` routes to the Gemini backend.
- `resolveModel` / `resolveRole` / `resolveResume` in `run.go:186-222` each have a **hardcoded `switch agent.CLI`** over `gemini|codex|claude`, plumbed from per-CLI fields on `ToolRequest` (`run.go:29-53`).
- Secrets flow **through environment variables** to child processes. There's no key store.

## 3. The awkward shape: one provider, many models

Every existing backend is a 1:1 CLI-to-model-family mapping. Ollama cloud breaks that — one HTTP endpoint, one API key, but five+ distinct `:cloud` models that users will want to dispatch to in parallel ("have kimi and glm both answer this").

Two shapes are possible:

### Option A — Single `ollama` backend, model via agent spec

```json
{
  "agents": "[
    {\"cli\":\"ollama\",\"name\":\"kimi\",\"model\":\"kimi-k2.5:cloud\"},
    {\"cli\":\"ollama\",\"name\":\"glm\", \"model\":\"glm-5.1:cloud\"},
    {\"cli\":\"claude\",\"name\":\"claude\"}
  ]"
}
```

**Pro:** smallest change — one backend, one factory line, one `ToolRequest.OllamaModel` field.
**Con:** default-agents UX is worse. `cli: "ollama"` with no model requires a `OLLAMA_DEFAULT_MODEL` env var or it's ambiguous.

### Option B — One backend key per model (thin wrappers)

```go
"kimi":     NewOllamaCloudBackend("kimi-k2.5:cloud"),
"qwen":     NewOllamaCloudBackend("qwen3.5:cloud"),
"glm":      NewOllamaCloudBackend("glm-5.1:cloud"),
"minimax":  NewOllamaCloudBackend("minimax-m2.7:cloud"),
```

**Pro:** matches the existing 1:1 convention; `defaultAgents()` can include any subset; no new `ToolRequest` fields needed if users always use the agent's default model.
**Con:** Every new cloud model is a code change. `resolveModel`/`resolveRole` switches grow. Proliferates CLI names that aren't really CLIs.

### Recommendation: Option A, with a registered default.

Rationale:

1. The "CLI name == provider + model family" convention already strained when Codex added a model picker. Ollama would break it further.
2. A single `ollama` backend centralizes auth, retry, rate-limit detection, and (later) tool-call translation.
3. Per-model preferences are naturally expressed through the existing `AgentSpec.Model` field — no new concept.
4. Default model lives in env (`OLLAMA_DEFAULT_MODEL`), keeping it out of code.

If we ever want named shortcuts (`cli: "kimi"`), they can be a thin alias map (`"kimi" → {cli: "ollama", model: "kimi-k2.5:cloud"}`) later. Start without it.

## 4. Concrete insertion plan

### 4.1 New file: `internal/roundtable/ollama.go` (~150 LOC)

```go
type OllamaBackend struct {
    httpClient *http.Client  // shared, explicitly configured
    // No apiKey / baseURL cached: read per-Run so env updates take effect
    // without binary restart (matches subprocess backends that re-read env
    // on every spawn). defaultModel is captured at construction for UX only.
    defaultModel string  // OLLAMA_DEFAULT_MODEL at construction time
}

// MaxResponseBytes caps the response body to protect against a misconfigured
// upstream streaming garbage. 8 MiB covers the 16K-token completion cap with
// comfortable headroom for JSON overhead.
const ollamaMaxResponseBytes = 8 * 1024 * 1024

func NewOllamaBackend(defaultModel string) *OllamaBackend {
    return &OllamaBackend{
        defaultModel: defaultModel,
        httpClient: &http.Client{
            // No total timeout — we rely on ctx from the dispatcher.
            // But the transport needs explicit connect/TLS/header timeouts
            // so a stalled TLS handshake can't hold the connection
            // indefinitely (ctx cancellation reaches net/http only after
            // the request is in flight).
            Transport: &http.Transport{
                DialContext: (&net.Dialer{
                    Timeout:   10 * time.Second,
                    KeepAlive: 30 * time.Second,
                }).DialContext,
                TLSHandshakeTimeout:   10 * time.Second,
                ResponseHeaderTimeout: 30 * time.Second,
                IdleConnTimeout:       90 * time.Second,
                MaxIdleConnsPerHost:   4,
            },
        },
    }
}

func (o *OllamaBackend) Name() string                    { return "ollama" }
func (o *OllamaBackend) Start(_ context.Context) error   { return nil }
func (o *OllamaBackend) Stop() error                     { return nil }

// Healthy is offline by design. A network probe here is dangerous:
// the dispatcher invokes Healthy() concurrently per-agent (run.go:309-320),
// so a 4-agent ollama hivemind fires 4 parallel probes against a service
// whose concurrency cap may be 1 (Free) or 3 (Pro). We would self-DoS
// our own quota before the first Run(). Instead, validate config only;
// let Run() experience and classify real 429/503 responses.
func (o *OllamaBackend) Healthy(_ context.Context) error {
    if os.Getenv("OLLAMA_API_KEY") == "" {
        return fmt.Errorf("OLLAMA_API_KEY not set")
    }
    return nil
}

func (o *OllamaBackend) Run(ctx context.Context, req Request) (*Result, error) {
    // Read env per-Run so key/URL updates take effect without restart.
    apiKey := os.Getenv("OLLAMA_API_KEY")
    baseURL := os.Getenv("OLLAMA_BASE_URL")
    if baseURL == "" {
        baseURL = "https://ollama.com"
    }
    if apiKey == "" {
        return ollamaConfigErrorResult("OLLAMA_API_KEY not set"), nil
    }

    model := req.Model
    if model == "" {
        model = o.defaultModel
    }
    if model == "" {
        return ollamaConfigErrorResult(
            "no model: set OLLAMA_DEFAULT_MODEL or AgentSpec.Model"), nil
    }

    body, _ := json.Marshal(map[string]any{
        "model":    model,
        "messages": []map[string]string{{"role": "user", "content": req.Prompt}},
        "stream":   false,
    })
    httpReq, _ := http.NewRequestWithContext(ctx, "POST",
        baseURL+"/api/chat", bytes.NewReader(body))
    httpReq.Header.Set("Authorization", "Bearer "+apiKey)
    httpReq.Header.Set("Content-Type", "application/json")

    start := time.Now()
    resp, err := o.httpClient.Do(httpReq)
    elapsed := time.Since(start).Milliseconds()
    if err != nil {
        // Map deadline-exceeded explicitly; subprocess path does this via
        // BuildResult but we don't go through the same code.
        if errors.Is(ctx.Err(), context.DeadlineExceeded) ||
            errors.Is(err, context.DeadlineExceeded) {
            return &Result{Model: model, Status: "timeout", ElapsedMs: elapsed}, nil
        }
        return &Result{Model: model, Status: "error",
            Stderr: err.Error(), ElapsedMs: elapsed}, nil
    }
    defer resp.Body.Close()
    // Drain body before close to allow connection reuse. LimitReader
    // caps garbage/streamed-forever responses.
    raw, _ := io.ReadAll(io.LimitReader(resp.Body, ollamaMaxResponseBytes))

    // NOTE: do NOT log `body` or `raw` at any level — they contain user
    // prompts and model output (PII/secret surface). Log status code and
    // elapsed only.

    parsed := ollamaParseResponse(raw, resp.StatusCode)
    return BuildResult(
        RawRunOutput{Stdout: raw, ElapsedMs: elapsed}, parsed, model), nil
}
```

Response parsing mirrors `geminiDecodeJSON` (`gemini.go:108-157`):

- Extract `message.content` on 200.
- Surface `prompt_eval_count` / `eval_count` as `metadata.tokens`.
- **Surface `done_reason` as metadata.** When `done_reason == "length"`, the 16K completion cap (§5.3) truncated the response — callers need to know their `deepdive` output got chopped. This is the HTTP analogue of Gemini's `finish_reason` handling.
- On 429: `status: "rate_limited"` (don't retry; see §5.4). Capture `Retry-After` header if present, surface in metadata.
- On 503: `status: "rate_limited"` (Ollama Cloud's 503 storms are effectively load-shedding; treating as rate-limit lets users react consistently). Reviewer consensus: don't retry in the backend.
- On 401/403: `status: "error"` with auth message.
- On malformed JSON: `status: "error"` with `parse_error`.

Endpoint choice: **native `/api/chat`** over `/v1/chat/completions`. Concrete reasons:
1. Native surfaces `prompt_eval_count`/`eval_count` as top-level fields; OpenAI-compat buries them in `usage` and some Ollama versions drop them.
2. Native exposes `done_reason` (length/stop/load); OpenAI-compat's `finish_reason` is reported-buggy on Ollama (returns `"stop"` when actually truncated at the 16K cap — critical given §5.3).
3. Ollama's OpenAI-compat tool-call path is flakier than native; irrelevant today (we don't plumb tool use) but a latent risk.

**Tradeoff acknowledged:** this parser is NOT reusable for OpenRouter (which is `/v1`-only). §6 updated to reflect that Phase 3 OpenRouter work needs a second parser. We're trading reuse for correctness on the four target models.

**Config-error result type.** `NotFoundResult` returns `ollama CLI not found in PATH` — semantically wrong for an HTTP backend where the failure is missing env, not missing binary. Introduce:

```go
// In internal/roundtable/result.go
func ConfigErrorResult(backend, model, reason string) *Result {
    return &Result{
        Model:  model,
        Status: "error",
        Response: fmt.Sprintf("%s backend misconfigured: %s", backend, reason),
        Stderr:  reason,
    }
}
```

Use this when `OLLAMA_API_KEY` is missing at `Run()` time or no model is resolvable. (Reviewer-caught bug — all three flagged the NotFoundResult misuse.)

### 4.2 `cmd/roundtable-http-mcp/main.go:buildBackends()` (+4 lines)

```go
backends := map[string]roundtable.Backend{
    "gemini": roundtable.NewGeminiBackend(""),
    "codex":  codexBackend,
    "claude": roundtable.NewClaudeBackend(""),
}
if os.Getenv("OLLAMA_API_KEY") != "" {
    backends["ollama"] = roundtable.NewOllamaBackend(os.Getenv("OLLAMA_DEFAULT_MODEL"))
    logger.Info("ollama backend configured")
}
return backends
```

Backend is absent (not nil) when no key is set — consistent with how Codex degrades. The dispatcher already emits `not_found` for unknown CLIs.

### 4.3 `ToolRequest` and resolvers

Two sub-options:

- **(a) Minimal:** don't add `OllamaModel`/`OllamaRole`/`OllamaResume` to `ToolRequest`. Force users to use the `agents` JSON with explicit `model`. Cleanest, no schema churn, no per-CLI switch growth.
- **(b) Symmetric:** add `OllamaModel` / `OllamaRole` / `OllamaResume` to `ToolRequest` and extend `resolveRole` / `resolveModel` / `resolveResume` switches by one case. Matches existing pattern.

**Revised recommendation: (a) minimal.** On the original draft I recommended (b), underestimating the churn. Reviewer count of actual touchpoints:

- `ToolRequest` struct: +3 fields (`run.go:29-61`)
- `run.go` resolvers: +3 switch cases (`run.go:186-222`)
- `httpmcp.ToolInput`: +3 fields + schema entries (`internal/httpmcp/backend.go`, `internal/httpmcp/server.go:48-64`)
- `stdiomcp.ToolInput`: +3 fields + schema entries (`internal/stdiomcp/types.go`, `internal/stdiomcp/server.go:46-62`)
- `buildDispatchFunc` (`main.go:178-225`) and `buildStdioDispatch` (`main.go:230-274`): +3 field copies each — and these two adapters are known-duplicated with a planned Phase C1 collapse (`main.go:229`).

Total ~18–24 touchpoints, widening a duplication that's about to be collapsed. Force users to use the `agents` JSON with explicit `model`; it's what custom agent specs are for. After Phase C1 collapses the two dispatch adapters, if symmetric convenience fields are still wanted, add them to a single adapter.

`validCLIs` (`run.go:63`) must still gain `"ollama": true`.

### 4.4 Roles

No new role file needed. `default.txt` / `planner.txt` / `codereviewer.txt` apply. But: some of these models have stricter system-prompt adherence than others (qwen3.5 is strict; minimax has been observed to partly ignore). Roles are already text-only, so this is a "users will iterate" situation, not a code concern.

### 4.5 Tests

- Unit test the parser with canned `/api/chat` success, error, 429, 503, malformed-JSON bodies (mirror `claude_test.go`).
- Use `httptest.Server` for end-to-end — the HTTP indirection is enough that a mock is simpler than intercepting at the `Backend` level.

### 4.6 Docs

- `INSTALL.md` — env vars (`OLLAMA_API_KEY`, `OLLAMA_BASE_URL`, `OLLAMA_DEFAULT_MODEL`).
- `docs/ARCHITECTURE.md` — add the fourth provider under the backend table.

## 5. Shortcomings of Ollama Cloud (the honest section)

Ollama Cloud launched in **public preview Jan 15 2026** ([docs.ollama.com/cloud](https://docs.ollama.com/cloud)). The integration works, but the provider itself has production-impairing limitations that users running fan-out agent patterns will hit immediately.

### 5.1 Concurrency ceiling is hostile to multi-agent patterns

- Free: **1** concurrent cloud model request
- Pro ($20/mo): **3**
- Max ($100/mo): **10**

Requests over the cap queue, then reject. ([ollama.com/pricing](https://ollama.com/pricing))

Roundtable's `hivemind` tool fans out to every registered backend in parallel. If a user registers kimi/qwen/glm/minimax as four `ollama` agents plus claude+gemini+codex, a Pro account is oversubscribed on every call.

**Mitigation in our code:** detect 429 with no `Retry-After`, surface as `status: "rate_limited"`, don't retry (consistent with Gemini's behavior, `gemini.go:184-192`). Users on Pro will learn to fan out to ≤3 Ollama agents at once.

### 5.2 Reliability is preview-grade

Three open GitHub issues document this:
- [#15419](https://github.com/ollama/ollama/issues/15419) — chronic 503s on kimi/glm/minimax, no `Retry-After`, no status page.
- [#15453](https://github.com/ollama/ollama/issues/15453) — "95% failure rate across all cloud models" report on Pro tier.
- [#14673](https://github.com/ollama/ollama/issues/14673) — full degradation 2026-03-06 from API overload.

No uptime SLA applies to shared inference; the 99.9% SLA on Ollama's pricing page is for **paid dedicated endpoints only** ([checkthat.ai/brands/ollama/pricing](https://checkthat.ai/brands/ollama/pricing)).

**Mitigation:** treat ollama results as best-effort. Roundtable's existing pattern of returning a per-agent `Result` with `status` (rather than failing the whole dispatch) already handles this correctly — a 503 from ollama doesn't block claude/gemini/codex results. Nothing to change in the dispatcher.

### 5.3 16,384-token output cap

All cloud models are capped at 16,384 completion tokens regardless of the model's native context/output limits ([ollama/ollama#13089](https://github.com/ollama/ollama/issues/13089)). That's roughly a third of what Roundtable's `deepdive`/`architect` tools can produce from Claude or Gemini today.

**Mitigation:** document in INSTALL.md. For long-form tools (`deepdive`, `architect`), warn users that ollama agents will likely truncate.

### 5.4 No `Retry-After` header

Ollama's error docs don't specify one, and users have explicitly asked for it ([#15419](https://github.com/ollama/ollama/issues/15419)). Combined with tiered reset windows (5-hour session, 7-day weekly), a 429 could mean "wait 30s" or "wait until Monday" with no signal to distinguish.

**Implication:** we can't do smart backoff. Same answer: surface as `rate_limited`, let the user decide.

### 5.5 Opaque usage accounting

Pricing is qualitative ("light / day-to-day / heavy"). No published tokens/min or requests/day. Metered per-token billing is promised but unshipped as of April 2026.

**Implication:** you can't budget for roundtable runs. For a CI-integrated usage, this is a blocker; for interactive dev work, acceptable.

### 5.6 US-only inference

All cloud hardware is US-based. EU/GDPR deployments can't use it. Not a Roundtable concern; flag in docs.

### 5.7 Tool-call reliability on `/v1` is worse than native

Not relevant today (Roundtable doesn't plumb tool use to external models), but if/when we do, we'd want `/api/chat` which is what I've already recommended.

### 5.8 `/readyz` coupling (operational blocker)

The HTTP server's `/readyz` marks the **whole** MCP server unready if any backend's probe fails (`internal/httpmcp/server.go:231-245`). If Ollama Cloud has one of its 503 storms, a Roundtable instance with ollama registered will report unready to whatever orchestrator is watching (systemd, k8s, Tailscale health), even though claude/gemini/codex are fine.

**Mitigation options, pick one before merging:**
- (i) Exclude ollama from the `/readyz` check set — treat it as optional.
- (ii) Keep the check, but since our `Healthy()` is offline-only after this revision (just env validation), it will never actually fail in response to upstream weather. So this is a non-issue *if and only if* we hold the offline-Healthy line.
- (iii) Add a separate /readyz scope for "core" vs "optional" backends.

Recommend **(ii)**: offline `Healthy()` eliminates the concern by construction. Document it as a design invariant so future changes don't silently reintroduce a network probe.

### 5.9 `/api/tags` may not be a cloud endpoint

Reviewer caveat: `/api/tags` is traditionally a local daemon endpoint; its availability on `ollama.com` under the cloud API is not confirmed by docs. Since the revised `Healthy()` is offline, this is now moot for health probes. Noted here so future "list available models" features know to verify before relying on it.

## 6. Proposed rollout

| Phase | Scope |
|-|-|
| 1 | `OllamaBackend` behind `OLLAMA_API_KEY` (offline `Healthy()`, runtime env read, explicit `Transport` + body cap, `ConfigErrorResult`, native `/api/chat` parser with `done_reason` surfacing, Prometheus metrics per §7); `buildBackends` wiring; **no** `ToolRequest` field additions — users compose via `agents` JSON; unit tests with `httptest` covering 200/401/429/503/malformed-JSON/`done_reason=length`. |
| 2 | Dogfood: run `hivemind` with mixed `[claude, gemini, codex, ollama(kimi), ollama(glm)]` on a real prompt; record failure modes; watch for the concurrency-cap ceiling in practice. |

No schema migration, no MCP tool-input additions in Phase 1, no new CLI subcommand. Phase 1 is ~250 LOC (backend + new result helper + tests) plus ~4 lines in `buildBackends` and 1 line in `validCLIs`.

## 7. Telemetry

### Naming scheme

Prometheus-idiomatic with a `backend` label, per decision §9. The existing `grafana/roundtable-dashboard.json` uses OTel span-style names (`span_name="roundtable.gemini"`, per-backend attributes `roundtable_gemini_status`) — but no Go code in the current rewrite emits those; the dashboard is stale from the Elixir era. We're not going to perpetuate the per-backend-named-attribute anti-pattern (it doesn't scale: every new backend forces a dashboard edit). Instead:

| Metric | Type | Labels | Purpose |
|-|-|-|-|
| `roundtable_backend_requests_total` | counter | `backend`, `status` (ok/rate_limited/error/timeout), `model` | Per-status request count. |
| `roundtable_backend_request_duration_seconds` | histogram | `backend`, `model` | Per-call latency. |
| `roundtable_backend_response_tokens` | histogram | `backend`, `model` | Completion token count (`eval_count` from Ollama; equivalent from other backends). |
| `roundtable_ollama_truncated_total` | counter | `model` | `done_reason == "length"` — the 16K-cap truncation signal (§5.3). Ollama-specific since `done_reason` is Ollama's native field. |
| `roundtable_backend_http_status_total` | counter | `backend`, `code` (200/401/429/503/other) | HTTP status breakdown; only meaningful for HTTP-native backends. |

### Implementation note

No OTel/Prometheus wiring exists in Go yet (verified: zero `StartSpan`/`tracer`/`promhttp` references across the Go source). Phase 1 will need to introduce one. Keep it minimal: one `metrics.go` in `internal/roundtable/` exposing registered counters/histograms, called from `OllamaBackend.Run()` on entry/exit. Don't retrofit the subprocess backends in this PR — they can adopt the same pattern when touched next.

Dashboard update (`grafana/roundtable-dashboard.json`) is a follow-up; the stale OTel queries there should be rewritten against these names whenever someone revives the dashboard.

## 8. Resolved after roundtable review

1. ✅ **Option A** (single `ollama` backend, model via `AgentSpec`) — all three reviewers concurred; `ParseAgents` already supports repeated `cli` with different `name/model` (`run.go:92-125`).
2. ✅ **Minimal ToolRequest extension** — reviewer churn count (~18-24 touchpoints across two duplicated dispatch adapters pending Phase C1 collapse) flipped the recommendation.
3. ✅ **Don't retry 503** — no `Retry-After` to guide backoff, retries risk self-DoS against 1/3/10 concurrency caps, dispatcher degrades gracefully per-agent already.
4. ✅ **Fail-closed when no model resolvable** — return `ConfigErrorResult`, not `NotFoundResult` (HTTP backends don't have a PATH).
5. ✅ **Native `/api/chat`** — better token/finish-reason metadata; `done_reason=length` is the truthful truncation signal the 16K cap makes essential.
6. ✅ **Offline `Healthy()`** — non-negotiable design invariant; concurrent probes would hit the concurrency cap before `Run()` even starts.
7. ✅ **Runtime env read in `Run()`** — subprocess backends read env per-spawn; match that so `OLLAMA_API_KEY` rotation doesn't require restart.

## 9. Locked decisions (user, 2026-04-20)

1. ✅ **`/readyz` invariant (Q1 → A):** offline `Healthy()` is the design invariant. `/readyz` will not reflect Ollama upstream reachability. Codified as a comment on `OllamaBackend.Healthy()` so future changes don't silently reintroduce a network probe.
2. ✅ **Scope: Ollama only (Q2):** OpenRouter is explicitly out of scope. Phase 3 removed from the rollout. If Ollama reliability proves insufficient later, that's a separate conversation, not a pre-staged phase in this plan.
3. ✅ **Telemetry naming (Q3 → B):** audit of `grafana/roundtable-dashboard.json` confirmed no live Go emission exists (dashboard is stale Elixir-era); going with Prometheus-idiomatic `roundtable_backend_*` scheme with `backend` label. See §7 for the five metrics.
