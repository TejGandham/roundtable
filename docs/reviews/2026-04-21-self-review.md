# Self-review: feat/openai-compatible-providers

_Dogfooded via the refactor's own `challenge` tool; 3 of 4 agents returned substantive reviews, kimi (ollama HTTP) timed out at the provider's 240s response_header_timeout — a live demo of FR-14 isolation._

---
## Claude (codereviewer, 155489ms)

# Critical Review — ROUNDTABLE_PROVIDERS refactor

## 1. Unbounded-cardinality metrics — memory leak vector
**SEVERITY:** P1
**WHERE:** `internal/httpmcp/metrics.go:35-55` (`ObserveProvider`)
**WHAT:** `providerRequests` key is `provider/model/status`; `providerDurationSum/Count` keyed by `provider/model`. `model` is attacker-controlled through `AgentSpec.Model` (MCP tool input → `ToolRequest.*Model` → `resolveModel`). A client that sends N distinct model strings allocates N map entries + atomics for the process lifetime. Long-running HTTP server → unbounded memory.
**FIX:** Either (a) drop `model` from metric keys and keep only `(provider, status)`, surfacing per-model breakdown on a separate bounded LRU, or (b) validate `model` against a per-provider allow-list at dispatch time. Cardinality bound is non-negotiable for a Prometheus-shaped metric.

## 2. Non-deadline ctx cancel during `sem.Acquire` loses "timeout" status
**SEVERITY:** P1
**WHERE:** `internal/roundtable/openai_http.go:~178-200`
**WHAT:** Only `context.DeadlineExceeded` is mapped to a `timeout`-shaped result via `BuildResult{TimedOut:true}`. If the parent ctx is `Canceled` (caller aborts, shutdown), the gate-acquire path emits a generic `Status:"error"` with a raw error string. Breaks contract-consistency with the in-flight request path (which also only special-cases DeadlineExceeded → ditto bug there).
**FIX:** Treat both `context.DeadlineExceeded` and `context.Canceled` as a terminal cancellation; emit a stable status (either reuse `timeout` or add `canceled`). Match the subprocess backend's cancel semantics.

## 3. Empty API key silently sent at Run time
**SEVERITY:** P2
**WHERE:** `internal/roundtable/openai_http.go:~122` (`apiKey := os.Getenv(o.apiKeyEnv)`)
**WHAT:** No check that `apiKey != ""` in `Run`. If env is unset between `Healthy()` (dispatcher may skip in some paths) and `Run()`, the backend sends `Authorization: Bearer ` and gets a 401 parsed as opaque "error", not a clean config error. Regression vs deleted Ollama if it validated inline.
**FIX:** Early-exit with `ConfigErrorResult` when `apiKey == ""` before building the request.

## 4. Swallowed JSON encode error
**SEVERITY:** P2
**WHERE:** `internal/roundtable/openai_http.go:~150` (`_ = enc.Encode(...)`)
**WHAT:** Encoding `map[string]any` with string values won't fail in practice, but ignoring the return is sloppy and hides future regressions (e.g., someone adds a `chan` or cyclic structure in metadata).
**FIX:** `if err := enc.Encode(...); err != nil { return ConfigErrorResult(...) }`.

## 5. Fragile string-matching for legacy `"cli"` field
**SEVERITY:** P2
**WHERE:** `internal/roundtable/run.go:~55-70` (`ParseAgents`)
**WHAT:** `strings.Contains(msg, `"cli"`)` depends on Go's json error text (`json: unknown field "cli"`). Go stdlib could change the format — the "help operators migrate" message silently degrades to generic "unknown field" error. Tests don't pin this.
**FIX:** Pre-parse into `[]map[string]json.RawMessage`, explicitly check for a `cli` key and emit the migration hint. Then strict-decode into `agentSpecJSON`.

## 6. Dead branch in `openAIParseResponse`
**SEVERITY:** P2
**WHERE:** `internal/roundtable/openai_http.go:~238-245`
**WHAT:** After `statusCode >= 400` the `statusCode != 200` case is unreachable for 2xx≠200 (201/204) — they fall into the error path. Not a 2xx success tolerance. If any OpenAI-compat upstream returns 200 with an async 202 ack shape, you'd misreport. Minor, but the current switch is confusing.
**FIX:** Collapse to `switch { case 429/503: … ; case >=400: … ; case >=300 || <200: … ; default success }`. Decide deliberately whether 2xx-non-200 is success or error.

## 7. No `BaseURL` validation at load time
**SEVERITY:** P2
**WHERE:** `internal/roundtable/providers.go:~95`
**WHAT:** `base_url` is accepted as-is. A typo ("htps://...", missing scheme, path with trailing slash collisions on `+"/chat/completions"`) only surfaces per-request with a generic error. Operators will struggle to diagnose.
**FIX:** `url.Parse` + scheme-in-{http,https} check + reject trailing slash (or normalize). Fail fast at startup.

## 8. `inlineFileContents`: no escaping of `</file>` in contents
**SEVERITY:** P2
**WHERE:** `internal/roundtable/files.go:~60-80`
**WHAT:** A file that legitimately contains `</file>` or `<file path="…">` (e.g., a fixture, a dumped prompt) can forge a synthetic boundary — prompt-injection style confusion for the downstream model. Not a memory-safety issue; a content-integrity one.
**FIX:** Either CDATA-wrap or base64 the contents, or escape `<` → `&lt;` at minimum. Document the chosen boundary guarantee.

## 9. Test flakiness — hard-coded 100 ms ctx in in-flight timeout test
**SEVERITY:** P2
**WHERE:** `openai_http_test.go:TestOpenAIHTTPBackend_Run_CtxDeadlineDuringRequest`
**WHAT:** `context.WithTimeout(ctx, 100*time.Millisecond)` combined with loopback TCP — tight under loaded CI runners. Historically a flake generator.
**FIX:** Use a mutex-blocked handler that only releases on `cancel`; assert status via channel race, not wall-clock. Or synchronize via a `sync.WaitGroup` and cancel explicitly.

## 10. Test coverage gaps vs deleted Ollama
**SEVERITY:** P2
**WHERE:** `openai_http_test.go`
**WHAT:** Missing negative cases: (a) empty API key observed at Run time after Healthy; (b) HTTP-date Retry-After format (`Wed, 21 Oct 2015 …`) — real providers send this; (c) response with 200 + non-JSON body; (d) malformed URL from config; (e) ctx `Canceled` (not just `DeadlineExceeded`) during acquire and during Do; (f) partial JSON cut by the 8 MiB LimitReader at exactly 8 MiB. `httptest.Server` gives you loopback-only — no TLS handshake timeout, no DNS, no IPv6 fallback — but that's acceptable unit-scope; just call it out.

## 11. `IdleConnTimeout: 90s` + `MaxIdleConnsPerHost: 4` not configurable
**SEVERITY:** P3
**WHERE:** `openai_http.go:newHTTPTransport`
**WHAT:** Hard-coded. Per-provider tuning may be required for providers with aggressive idle-socket teardown (causes unexpected connection resets on low-traffic agents).
**FIX:** Move into `ProviderConfig` as optional fields with current defaults. Not a ship-blocker.

## 12. `providerJSON` allows `""` `DefaultModel` — silently accepted
**SEVERITY:** P3
**WHERE:** `providers.go:~100`
**WHAT:** Fine when agents always pass `model`, but combined with FR-3 ("silently skip if api_key_env empty") creates a class of configs that look registered but fail every call with `"no model resolved"`. Would be nice to warn at load if `default_model == ""` so the operator knows they need per-agent model overrides.

---

<SUMMARY>
Main concerns:
(1) Unbounded metric cardinality — `model` is in the map key and is attacker-controllable via MCP input. Memory leak over time. Must bound.
(2) `context.Canceled` on sem.Acquire / http.Do is emitted as generic "error" instead of timeout/canceled — contract inconsistency with deadline path.
(3) Empty API key not checked in Run(); relies on Healthy being called first.
(4) Swallowed JSON encode error; fragile string-match for legacy "cli" field detection; dead branch in status switch.
(5) `BaseURL` has no validation at load — operators lose startup-time feedback.
(6) `inlineFileContents` doesn't escape `</file>` in content → potential prompt-boundary confusion.
(7) Test suite: 100 ms timeout test is a flake magnet; missing cases for HTTP-date Retry-After, ctx.Canceled variant, empty-key-at-Run, malformed URL.
None are P0 ship-blockers; (1) and (2) should be fixed before merge.
</SUMMARY>

VERDICT: HOLD
---
## Codex (codereviewer, 184235ms)

Reviewing the refactor against the existing contracts first. I’m checking the surrounding `roundtable` code and any remaining Ollama-era behavior so the findings are about real regressions, not guesses.

I’ve narrowed this to contract and behavior regressions now: prompt assembly, result shaping, and what `/metricsz` and readiness probes expose. Next step is reading the surrounding implementations to verify whether this refactor changed runtime semantics, not just names.

There’s a likely behavior mismatch around prompt/file handling and how result metadata is surfaced. I’m checking whether the new HTTP path still matches what the dispatcher and existing tests expect from the old backend contract.

I need the deleted Ollama implementation to judge regression claims accurately. I’m pulling the previous version from git history now so I can compare rate-limit handling, file inlining, and metadata behavior line for line.

I’ve got two concrete regressions already: one availability bug in provider registration semantics, and one new memory-DoS surface in metrics cardinality. I’m checking the design docs and existing test intent now to make sure the review cites the contract they were trying to preserve.

1. **high (P1)**  
   **WHERE:** `internal/httpmcp/metrics.go:45-74`, `internal/roundtable/openai_http.go:103-112`  
   **WHAT:** Metrics keys now include the raw `model` string, and that value is user-controlled via tool input / agent JSON. A caller can send unbounded unique model names and force unbounded growth of `providerRequests`, `providerDurationSum`, and `providerDurationCount`, which is a straightforward memory-DoS against the HTTP server.  
   **FIX:** Do not key long-lived maps by raw user input. Normalize model labels to a bounded set: prefer parsed `model_used`, cap length, reject control chars, and bucket unknown/custom values into a small number of bins like `_custom` / `_invalid`. Add a test that spams many unique model strings and asserts bounded cardinality.

2. **high (P1)**  
   **WHERE:** `internal/roundtable/providers.go:52-89`, `cmd/roundtable-http-mcp/main.go:170-198`  
   **WHAT:** FR-3 says a provider with missing credentials should be skipped, but `LoadProviderRegistry` hard-fails the entire registry when one entry has empty `api_key_env`. Result: one malformed provider entry drops *all* HTTP providers, not just the bad one.  
   **FIX:** Validate per-entry and continue. Either allow empty `api_key_env` through parsing and let `buildBackends` skip it, or return partial configs plus per-entry errors. Add a mixed-registry test: one bad entry, one good entry, good provider must still register.

3. **medium (P1)**  
   **WHERE:** `internal/roundtable/openai_http.go:251-304`  
   **WHAT:** The parser assumes `choices[0].message.content` is always a string. That is not true for many OpenAI-compatible responses (`content` arrays, tool-call-only assistant messages, refusal payloads). Today those responses can be emitted as `status:"ok"` with an empty `response`, which is silent corruption.  
   **FIX:** Parse `message.content` as `string | []part`, extract text parts, and fail closed when no textual assistant content exists. Add tests for array content, tool-call-only responses, and refusal/error-shaped 200 responses.

4. **medium (P1)**  
   **WHERE:** `cmd/roundtable-http-mcp/main.go:194-203`, `internal/httpmcp/metrics.go:30-38,88-108`  
   **WHAT:** `base_url` is logged at startup and exposed on `/metricsz` with no sanitization. If an operator pastes a URL containing embedded credentials or query tokens, this refactor will publish them verbatim to logs and an unauthenticated endpoint.  
   **FIX:** Parse `base_url` with `net/url`, reject or redact `User`, `RawQuery`, and `Fragment` before storing/logging/exposing it. Add a test with `https://user:secret@example.com/v1?token=x` and assert the secret never appears.

5. **low (P2)**  
   **WHERE:** `internal/roundtable/openai_http_test.go:273-319`  
   **WHAT:** The semaphore test does not actually prove serialization. Both goroutines share one deadline context and the handler blocks until timeout, so the test still passes if the gate is broken and both requests run concurrently.  
   **FIX:** Measure max in-flight requests with an atomic counter and separate contexts; assert it never exceeds `MaxConcurrent`, and add a second test that a canceled waiter does not consume a permit.

**VERDICT:** HOLD

<SUMMARY>Ship is blocked by two concrete issues: the new per-model metrics use raw user-supplied model strings as map keys, which creates an easy memory-DoS path, and the FR-3 implementation is wrong because one bad `ROUNDTABLE_PROVIDERS` entry can disable every HTTP provider. The parser is also not actually generic OpenAI-compatible yet; valid responses with content arrays or tool-call-only messages can be returned as `ok` with empty output. Finally, `base_url` is exposed unsafely in logs and `/metricsz`, and the semaphore test is too weak to catch gate breakage.</SUMMARY>
---
## Gemini (codereviewer, 258286ms)

**Finding 1: Map Cardinality Leak / Memory Exhaustion**
SEVERITY: P0 ship-blocker
WHERE: `internal/httpmcp/metrics.go` (approx line 53, `ObserveProvider`)
WHAT: The `reqKey` concatenates the user-controlled `model` string into the global `providerRequests` map. A buggy or malicious payload querying rotating, unique model names (e.g., `model-1`, `model-2`) will continuously allocate `*atomic.Int64` pointers, permanently leaking memory until an OOM crash.
FIX: Validate `model` against a bounded subset, implement an LRU cache for metric keys, or limit observation to `provider.DefaultModel`.

**Finding 2: Connection Pool Starvation / Socket Churn**
SEVERITY: P1 fix before merge
WHERE: `internal/roundtable/openai_http.go` (approx line 78, `newHTTPTransport`)
WHAT: `MaxIdleConnsPerHost` is hardcoded to `4`, but a provider's `MaxConcurrent` can be configured much higher. Any burst of requests exceeding 4 will exhaust the idle pool, forcing the `http.Client` to aggressively close and renegotiate TCP/TLS connections for every subsequent request, devastating performance.
FIX: Scale the transport dynamically by setting `MaxIdleConnsPerHost: cfg.MaxConcurrent`.

**Finding 3: TOCTOU on API Key Evaluation**
SEVERITY: P1 fix before merge
WHERE: `internal/roundtable/openai_http.go` (approx line 103, `Run`)
WHAT: `Run` dynamically retrieves `apiKey := os.Getenv(o.apiKeyEnv)` but fails to verify if it is empty. If the key is unset after `Healthy()` validated it, the backend acquires a semaphore slot and blindly fires an `Authorization: Bearer ` request, burning a concurrency slot and network bandwidth for a guaranteed 401 error.
FIX: Fast-fail by adding `if apiKey == "" { return ConfigErrorResult(...), nil }` before attempting `o.sem.Acquire`.

**Finding 4: Missing Cancellation Path on Body Streaming**
SEVERITY: P1 fix before merge
WHERE: `internal/roundtable/openai_http.go` (approx lines 205-214, `Run`)
WHAT: If the context deadline fires *while* `io.ReadAll` is reading a slow response, `readErr` is populated, but the error handler unconditionally assigns `Status: "error"`. It fails to detect context timeouts (unlike the `httpClient.Do` handler above it), which breaks downstream fallback/retry mechanisms that expect `Status: "timeout"`.
FIX: Check `errors.Is(ctx.Err(), context.DeadlineExceeded)` in the `readErr != nil` block to correctly return a timeout `Result`.

**Finding 5: Silent Body Truncation Masking Capacity Errors**
SEVERITY: P2 nice-to-have
WHERE: `internal/roundtable/openai_http.go` (approx line 205, `Run`)
WHAT: `io.LimitReader` silently stops reading when `defaultMaxResponseBytes` is hit, yielding a truncated payload without an explicit error. `openAIParseResponse` then fails on the truncated payload, returning a cryptic `"JSON parse failed"` error to the user rather than clearly stating the response exceeded limits.
FIX: Check `if len(raw) == defaultMaxResponseBytes` post-read and explicitly return a size-limit-exceeded error.

**Finding 6: Test Gap / httptest Server Masks Body Timeouts**
SEVERITY: P2 nice-to-have
WHERE: `internal/roundtable/openai_http_test.go` (approx line 260, `TestOpenAIHTTPBackend_Run_CtxDeadlineDuringRequest`)
WHAT: The test pauses *before* writing headers to trigger the timeout. This exclusively exercises the `httpClient.Do` phase deadline logic. It completely misses the edge case of timeouts occurring mid-body stream, which masks the flaw identified in Finding 4.
FIX: Force the headers via `w.WriteHeader(200)`, call `w.(http.Flusher).Flush()`, and *then* block on the channel to verify deadline behavior during `io.ReadAll`.

VERDICT: HOLD
---
## Kimi / Ollama Cloud (codereviewer, 240039ms — timeout)

Request timed out after 241s. Retry with a longer timeout or resume the session.
