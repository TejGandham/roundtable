# pal-mcp-server pattern extraction for Roundtable

**Source:** `/mnt/agent-storage/vader/src/pal-mcp-server` (Python, unmaintained).
**Purpose:** Mine the most valuable architectural designs from pal before it goes cold, and decide which to land alongside the Ollama Cloud backend work on `feature/ollama-cloud-provider`.
**Date:** 2026-04-20

pal solves almost exactly our problem: parallel dispatch to multiple AI providers over HTTP APIs, response normalization, error classification, composite tools. It has years of provider-specific scar tissue we can skip re-learning. But it's Python + heavy Pydantic + MCP-transport-aware + unmaintained; we're Go + stdlib + HTTP-native + active. We take the shapes, not the code.

## Patterns ranked by value for the current Ollama PR

### Tier 1 — take now, in this PR

#### 1.1 Error-retryability classification hook

**pal:** `providers/base.py::_is_error_retryable()` overridden by `providers/openai_compatible.py` which *parses structured error codes*:
- **429 (Rate Limit):** inspect error type/code — **token-budget 429s are NOT retryable, request-per-minute 429s ARE.**
- **502/503/504/500:** retryable transient.
- **408 / TLS handshake / connection reset:** retryable.
- **4xx other (400/401/403):** not retryable.

**Why this matters for Ollama:** our current plan says "surface 429/503 as `rate_limited`, don't retry at all" (locked decision §2 Row C). That's defensible (no `Retry-After` from Ollama, and retries risk self-DoSing the 1/3/10 concurrency cap). But pal's finer-grained classification is genuinely better: a *request-rate* 429 from Ollama during a fanout is worth one retry with backoff; a *token-budget* 429 (over plan quota for the day) is never retryable. A 503 during the documented storm pattern is retryable if we budget it.

**Proposal:** keep no-retry in Phase 1 as planned, but **land the classification type now** so it's in place for the backoff work later:

```go
// internal/roundtable/errors.go (new)
type ErrorClass int

const (
    ErrorClassOK           ErrorClass = iota
    ErrorClassInvalid                  // 400/401/403 — auth, malformed
    ErrorClassRateRequest              // 429 request-rate — retryable with backoff
    ErrorClassRateQuota                // 429 quota exhausted — NOT retryable until reset
    ErrorClassOverloaded               // 503/502/504 — retryable
    ErrorClassTransient                // TLS/connection reset — retryable
    ErrorClassTimeout                  // context deadline — not retryable (caller bounds us)
)

// ClassifyOllamaError maps a response to an ErrorClass without
// actually retrying. Phase 1: used for metrics + Result.Status mapping.
// Phase 2: used for the optional retry loop.
func ClassifyOllamaError(statusCode int, body []byte) ErrorClass { ... }
```

Keeps our "no retries" promise for this PR AND makes the next step cheap.

**Copy vs adapt:** adapt. pal's regex-based body inspection (line 770) is fragile; we use HTTP status + structured JSON parsing.

#### 1.2 File-content inlining with a reported truncation result

**pal:** `tools/workflow/workflow_mixin.py::_prepare_file_content_for_prompt(files, max_tokens) → (formatted, truncation_info)`.

Key detail my current §4 design misses: **pal returns a structured truncation-info report alongside the formatted content.** If files got cut, the *tool* can tell the user — not just the model.

**Why this matters:** my current plan emits `<truncated />` and `<skipped-files>` markers inside the prompt. The model sees them. But the `Result` returned to the caller has no field saying "we skipped 3 files." A user running `hivemind` with 20 files wouldn't know 6 got dropped unless they parse the response.

**Proposal:** have `inlineFileContents` return `(content string, report FileInlineReport)` instead of just the content string. Surface `report` as metadata on the `Result`:

```go
type FileInlineReport struct {
    IncludedPaths  []string
    TruncatedPaths []string   // included but cut at 128 KiB per-file
    SkippedPaths   []string   // not included — total budget blown
    UnreadablePaths map[string]string // path → error message
}

// Attach to Result.Metadata["file_inlining"] when non-empty.
```

**Copy vs adapt:** adapt. pal reports via tuple return; Go's multiple return values are the clean match. Adds ~20 LOC and one test.

#### 1.3 Dummy API key for unauthenticated endpoints

**pal:** `providers/custom.py:72` — `"dummy-key-for-unauthenticated-endpoint"` when no key is provided, because the OpenAI-compat SDK requires *some* Authorization header.

**Why this matters:** Ollama has a local-daemon mode at `http://localhost:11434` where users might run the same models without a bearer token. If a dev sets `OLLAMA_BASE_URL=http://localhost:11434` with no `OLLAMA_API_KEY`, our current plan fails closed with `ConfigErrorResult`. For local Ollama that's annoying.

**Proposal:** if `OLLAMA_BASE_URL` points at localhost / 127.0.0.1, treat a missing API key as "send Bearer dummy" instead of erroring. Log a debug message explaining the fallback. Public `ollama.com` still requires the key.

```go
func (o *OllamaBackend) resolveAuth(baseURL string) (string, error) {
    if key := os.Getenv("OLLAMA_API_KEY"); key != "" {
        return key, nil
    }
    if isLocalBaseURL(baseURL) {
        return "dummy", nil // local daemon ignores the header
    }
    return "", fmt.Errorf("OLLAMA_API_KEY not set (required for non-local base URL)")
}
```

**Copy vs adapt:** copy the idea verbatim; simpler in Go.

### Tier 2 — take soon, NOT this PR

#### 2.1 `ModelCapabilities` metadata struct

**pal:** `providers/shared/model_capabilities.py` — immutable dataclass per model with `context_window`, `max_output_tokens`, `supports_images`, `supports_system_prompts`, `supports_function_calling`, `supports_json_mode`, `temperature_constraint`, `intelligence_score` (1–20), `aliases []string`.

**Why this matters (eventually):** Roundtable currently has no model registry. Model names are free-text; `resolveModel` picks whatever the user passed. That's fine for 3 subprocess backends with known models, but with Ollama we're fanning out to 4+ model names per provider, each with different output caps, tool-call support, and context windows. A structured catalog lets us auto-dispatch ("give me the best available under 128K context") and warn users about mismatches (asking `kimi-k2.6:cloud` to do vision → error at dispatch, not at the API).

**Why NOT this PR:** introducing a capability system touches *every* backend, not just Ollama. Scope explosion. And our current ship is "make Ollama work"; capability ranking is an auto-dispatch feature that nobody's asked for yet.

**Proposal:** defer. File as a follow-up (issue, not code). If we build it, start with a read-only `ollama_models.json` analogue of pal's `custom_models.json` — declarative, editable without code changes, scoped to Ollama only for v1.

#### 2.2 Provider registry with priority order

**pal:** `providers/registry.py::ModelProviderRegistry` — singleton, routes unknown model names through `PROVIDER_PRIORITY_ORDER` (Google → OpenAI → Azure → XAI → DIAL → Custom → OpenRouter).

**Why this matters (eventually):** if Roundtable ever gets a second HTTP backend (OpenRouter was suggested, then rejected in §2 Row H), the question "which backend gets a given model name" becomes real. Today we route by `AgentSpec.CLI` field, which is effectively explicit provider selection. That's fine. But the moment we have two HTTP providers that can both serve `kimi-k2.6` (Ollama Cloud + OpenRouter), the registry pattern is what breaks the tie.

**Why NOT this PR:** Row H already said no OpenRouter. Premature.

**Proposal:** defer. When a second HTTP provider lands, pal's registry pattern is the shape to adopt — priority list + validation cascade, not singleton.

### Tier 3 — do NOT take

These looked tempting but are wrong for us:

#### 3.1 Temperature constraint classes

**pal:** `RangeTemperatureConstraint` / `DiscreteTemperatureConstraint` per model, with `get_effective_temperature(requested)` clamping.

**Why not:** Roundtable's `Request` struct has no `Temperature` field today. We never expose temperature control. Our MCP tools are opinionated — `deepdive` and `architect` don't take a temperature. Adding a constraint system for a parameter we don't plumb is pure speculation.

**If someone asks for temperature control later, this is the shape.** But today, skip.

#### 3.2 In-memory continuation storage for multi-turn

**pal:** `utils/conversation_memory.py` + `continuation_id` threading across tool invocations, stored server-side in memory.

**Why not:** Roundtable already has per-backend resume semantics (`GeminiResume`, `CodexResume`, `ClaudeResume` in `ToolRequest`), and each CLI manages its own session state. Ollama Cloud's `/api/chat` is stateless; we'd have to replay full conversation history every call. Fine — that's just client-side history assembly, no server-side store needed. pal's in-memory storage is a hazard (lost on restart, doesn't scale across processes) we don't want.

#### 3.3 Regex-based error body parsing

**pal:** `openai_compatible.py:770` extracts JSON from error strings with regex.

**Why not:** fragile. Ollama's native `/api/chat` returns clean JSON errors; we use `json.Unmarshal` directly. Already in the plan.

#### 3.4 Singleton registry as a package-level global

**pal:** `ModelProviderRegistry` is a Python singleton.

**Why not:** hides dependencies, breaks tests, not idiomatic Go. When we do land a registry (Tier 2.2), it should be an explicit parameter through `buildBackends`, not a package-level `var`.

#### 3.5 MCP 25K token transport cap handling

**pal:** `config.py:78-115` documents a 25K token limit on the CLI-side MCP transport boundary and works around it.

**Why not:** our MCP transport is HTTP/stdio with no such limit in the SDK we use. Don't cargo-cult the workaround.

## What actually lands in this PR

Three concrete additions to the Ollama plan, all tier 1:

| # | Item | Est. LOC | Files touched |
|-|-|-|-|
| 1 | `ClassifyOllamaError` helper + `ErrorClass` enum (used by parser for mapping + by future retry loop) | ~50 | `internal/roundtable/ollama.go`, `ollama_test.go` |
| 2 | `FileInlineReport` return from `inlineFileContents`; surfaced on `Result.Metadata["file_inlining"]` when non-empty | ~25 | `internal/roundtable/ollama.go`, `ollama_test.go` |
| 3 | Local-base-URL detection + dummy bearer fallback | ~15 | `internal/roundtable/ollama.go`, `ollama_test.go` |

**Net:** +90 LOC on top of the existing 650 LOC plan. No new dependencies, no scope change to the feature itself — these are hardening and reporting improvements that fall naturally into Task 3 (parser), Task 4 (inliner), and Task 5 (Run).

## Open questions for the roundtable

1. Is the `ErrorClass` enum worth building in Phase 1 if we're not retrying yet, or is that YAGNI until the retry loop actually ships?
2. Does surfacing a `FileInlineReport` on `Result.Metadata` clash with the existing metadata shape (which is flat `map[string]any`)?
3. Is the local-dummy-key fallback a footgun (silent auth bypass against a mis-pointed URL) or a convenience win?
4. Any tier-3 "do not take" items that are actually worth reconsidering?
5. Anything I missed from pal that should be tier 1 or 2?
