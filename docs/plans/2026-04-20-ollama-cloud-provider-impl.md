# Ollama Cloud Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an `ollama` HTTP-backed provider to Roundtable so users can dispatch prompts to Ollama Cloud models (kimi-k2.6:cloud, qwen3.5:cloud, glm-5.1:cloud, minimax-m2.7:cloud) alongside the existing subprocess backends.

**Architecture:** A single `OllamaBackend` implementing the existing `Backend` interface, speaking native `/api/chat` via `net/http`. API key read per-`Run()` from `OLLAMA_API_KEY`; `Healthy()` is offline-only (env validation) to avoid self-DoSing Ollama's 1/3/10 concurrency cap during the dispatcher's parallel probe phase. Users select cloud models through `AgentSpec.Model` in the `agents` JSON; no new per-CLI fields are added to `ToolRequest`. Errors map to existing `Result.Status` values (`rate_limited` for 429/503, `error` for 4xx auth, `timeout` for deadline exceeded). Metrics extend the existing JSON-based `internal/httpmcp/metrics.go` with Prometheus-convention names.

**Cross-package wiring (no globals).** `internal/httpmcp` already imports `internal/roundtable`, so a back-import would cycle. Rather than reach for a package-level mutable hook (`var MetricsSink func(...)`), `OllamaBackend` accepts an `ObserveFunc` value through its constructor — wired at the composition root in `cmd/roundtable-http-mcp/main.go`. A nil func is normalized to a no-op so tests that don't care about metrics simply pass `nil`.

**Explicitly out of scope for this PR.** `docs/plans/2026-04-20-pal-extraction.md` proposes three Tier-1 additions (`ErrorClass` enum, `FileInlineReport` on `Result.Metadata`, localhost-dummy-bearer fallback) that earlier draft text described as landing alongside this PR. They are **descoped** here: the core "make Ollama work" feature is already 9 tasks and ~350 LOC, and adding three more uncoupled changes risks both the schedule and the review surface. The pal-extraction document remains as a follow-up reference; reopen it after the Ollama backend has dogfood time.

**Tech Stack:** Go 1.26, stdlib `net/http`, stdlib `httptest` for tests, existing `Backend`/`Result`/`ParsedOutput`/`BuildResult` types, existing JSON-based metrics in `internal/httpmcp/metrics.go`.

**Reference architectural document:** `docs/plans/2026-04-20-ollama-cloud-provider.md` (read that first for WHY; this plan is HOW).

---

## File Structure

| File | Purpose | Action |
|-|-|-|
| `internal/roundtable/result.go` | Add `Metadata map[string]any` field (Task 0) + `ConfigErrorResult` helper (Task 1). | Modify |
| `internal/roundtable/result_test.go` | Cover `ConfigErrorResult`. | Modify |
| `internal/roundtable/output.go` | Propagate `parsed.Metadata` into `Result.Metadata` from `BuildResult` (Task 0). | Modify |
| `internal/roundtable/output_test.go` | Cover the propagation, including the existing `model_used` precedence behavior. | Modify |
| `internal/roundtable/ollama.go` | The new `OllamaBackend` — struct, `ObserveFunc` type, `NewOllamaBackend(defaultModel, observe)`, `Name/Start/Stop/Healthy/Run`, parser, file inliner, concurrency gate (Task 5a). | Create |
| `internal/roundtable/ollama_test.go` | Unit tests: parser tables (success, 401, 429, 429+Retry-After, 503, malformed JSON, `done_reason=length`), offline `Healthy` behavior, `Run()` against `httptest.Server` for success/timeout/error classification, concurrency gate serialization + ctx-deadline + ctx-cancel, env resolution table. | Create |
| `go.mod` / `go.sum` | Add `golang.org/x/sync` (used by Task 5a for `semaphore.Weighted`). | Modify |
| `internal/roundtable/run.go` | Add `"ollama"` to `validCLIs` map. | Modify (1 line) |
| `internal/roundtable/run_test.go` | Cover `ParseAgents` accepting `"cli":"ollama"`. | Modify |
| `cmd/roundtable-http-mcp/main.go` | Conditionally register `OllamaBackend` in `buildBackends()` when `OLLAMA_API_KEY` is set; pass `metrics.ObserveBackend` as the backend's `observe` param (Task 8). | Modify |
| `internal/httpmcp/metrics.go` | Extend `Metrics` struct with per-backend counters named per Prometheus convention (`roundtable_backend_requests_total` etc.) exposed as JSON via `/metricsz`. | Modify |
| `internal/httpmcp/metrics_test.go` | Cover new per-backend counters and their JSON emission. | Create |
| `INSTALL.md` | Document `OLLAMA_API_KEY`, `OLLAMA_BASE_URL`, `OLLAMA_DEFAULT_MODEL` env vars; add an `agents` JSON example. | Modify |

Total net-new code: ~350 LOC (backend + file inliner + tests + metrics extension).

> **Note on file handling (added 2026-04-20 after review):** The subprocess CLI backends rely on their own tool-calling to open files referenced in `req.Files`; an HTTP-only backend has no tool loop, so `OllamaBackend.Run()` must eagerly read the file contents and inline them into the user message. See Task 4 below and `docs/plans/2026-04-20-ollama-cloud-provider.md` (file-inlining section) for rationale and caps.

---

## Task 0: Extend `Result` with a `Metadata` field and propagate it through `BuildResult`

**Why first (and why it's a blocker for the headline feature).** The Ollama parser (Task 3) extracts `done_reason`, token counts, and `model_used` into `parsed.Metadata`. The architectural plan §4.1, §5.3, and §7's `roundtable_ollama_truncated_total` counter all depend on `done_reason == "length"` reaching the dispatcher's JSON output — that is the only signal callers get that the 16K-token completion cap chopped their `deepdive` answer. But `internal/roundtable/result.go:8-20` defines a `Result` struct with **no** `Metadata` field, and `internal/roundtable/output.go:42-95` (`BuildResult`) reads `parsed.Metadata["model_used"]` to compute `Result.Model` then drops the rest of `parsed.Metadata` on the floor. Without this task, Task 3's parser tests pass in isolation but the truncation feature is functionally inert at the API boundary.

This is a cross-backend change — claude.go:144, gemini.go:154, and codex_fallback.go:183/193 all populate `parsed.Metadata` today, so once propagation lands, every backend's JSON results gain a `metadata` field too. That's correct behavior (currently-hidden useful info gets surfaced) and additive (no consumer can depend on a field that doesn't exist yet). `omitempty` on the new field keeps results without metadata clean.

**Files:**
- Modify: `internal/roundtable/result.go` (add `Metadata` field with `omitempty`)
- Modify: `internal/roundtable/output.go` (propagate in `BuildResult`)
- Modify: `internal/roundtable/output_test.go` (assert propagation; assert `model_used` precedence still works)

- [ ] **Step 1: Write the failing test**

Append to `internal/roundtable/output_test.go`:

```go
func TestBuildResult_PropagatesMetadata(t *testing.T) {
	raw := RawRunOutput{ExitCode: intPtr(0)}
	parsed := ParsedOutput{
		Status: "ok",
		Metadata: map[string]any{
			"model_used":  "kimi-k2.6:cloud",
			"done_reason": "length",
			"tokens":      map[string]any{"prompt_eval_count": 42, "eval_count": 8},
		},
	}
	r := BuildResult(raw, parsed, "fallback")

	if r.Metadata == nil {
		t.Fatal("Metadata = nil, want propagated map")
	}
	if got := r.Metadata["done_reason"]; got != "length" {
		t.Errorf("done_reason = %v, want length", got)
	}
	if r.Metadata["tokens"] == nil {
		t.Error("tokens missing from propagated metadata")
	}
	// model_used precedence still works (existing behavior, regression guard)
	if r.Model != "kimi-k2.6:cloud" {
		t.Errorf("model = %q, want kimi-k2.6:cloud (parsed metadata wins)", r.Model)
	}
}

func TestBuildResult_NilMetadata_OmittedFromJSON(t *testing.T) {
	raw := RawRunOutput{ExitCode: intPtr(0)}
	r := BuildResult(raw, ParsedOutput{Status: "ok"}, "model")
	if r.Metadata != nil {
		t.Errorf("Metadata = %v, want nil when parser provided none", r.Metadata)
	}
	// Belt-and-suspenders: confirm omitempty keeps it out of JSON.
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), `"metadata"`) {
		t.Errorf("JSON contains metadata key for nil map: %s", string(data))
	}
}
```

The test file may not currently import `encoding/json` and `strings`; add them to the import block as needed.

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/roundtable/ -run 'TestBuildResult_PropagatesMetadata|TestBuildResult_NilMetadata' -v
```

Expected: FAIL — `r.Metadata undefined: type Result has no field Metadata`.

- [ ] **Step 3: Add the `Metadata` field**

Edit `internal/roundtable/result.go`. In the `Result` struct, add the field after `SessionID` (so JSON ordering puts metadata at the end, where new fields belong):

```go
type Result struct {
	Response        string         `json:"response"`
	Model           string         `json:"model"`
	Status          string         `json:"status"`
	ExitCode        *int           `json:"exit_code"`
	ExitSignal      *string        `json:"exit_signal"`
	Stderr          string         `json:"stderr"`
	ElapsedMs       int64          `json:"elapsed_ms"`
	ParseError      *string        `json:"parse_error"`
	Truncated       bool           `json:"truncated"`
	StderrTruncated bool           `json:"stderr_truncated"`
	SessionID       *string        `json:"session_id"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}
```

`omitempty` is load-bearing: existing tests assert exact JSON shape against several backends, and a `"metadata":null` field on every result would noise them up. With `omitempty`, results that don't carry metadata serialize identically to today.

- [ ] **Step 4: Propagate in `BuildResult`**

Edit `internal/roundtable/output.go`. Inside `BuildResult`, after the existing model-resolution block and before the `return &Result{...}` literal, the simplest correct change is to add `Metadata: parsed.Metadata` to the struct literal:

```go
	return &Result{
		Response:        response,
		Model:           model,
		Status:          status,
		ExitCode:        raw.ExitCode,
		ExitSignal:      raw.ExitSignal,
		Stderr:          raw.Stderr,
		ElapsedMs:       raw.ElapsedMs,
		ParseError:      parseError,
		Truncated:       raw.Truncated,
		StderrTruncated: raw.StderrTruncated,
		SessionID:       parsed.SessionID,
		Metadata:        parsed.Metadata, // propagate; omitempty handles nil
	}
```

No filtering: surfacing `model_used` in the propagated map is benign (it's already on `Result.Model`) and removing it would create a maintenance burden as new keys appear.

- [ ] **Step 5: Run the new tests to verify they pass**

Run:
```bash
go test ./internal/roundtable/ -run 'TestBuildResult_PropagatesMetadata|TestBuildResult_NilMetadata' -v
```

Expected: PASS on both.

- [ ] **Step 6: Run the full package test suite as a regression guard**

Run:
```bash
go test ./internal/roundtable/
```

Expected: PASS. If a claude/gemini/codex test breaks because it now sees a `metadata` field where it didn't before, the right fix is updating the test expectation — the new field is correct behavior. Do **not** filter the propagation to "fix" the test; that hides the feature.

- [ ] **Step 7: Run the full module test suite**

Run:
```bash
go test ./...
```

Expected: PASS. The most likely external touchpoint is `internal/httpmcp` JSON snapshot tests; if any assert exact `Result` JSON serialization, update them to expect the optional `metadata` key.

- [ ] **Step 8: Commit**

```bash
git add internal/roundtable/result.go internal/roundtable/output.go internal/roundtable/output_test.go
git commit -m "$(cat <<'EOF'
feat(result): add Metadata field and propagate through BuildResult

Result has gained a Metadata map[string]any field with omitempty.
BuildResult now copies parsed.Metadata onto the returned Result,
where it was previously read for model_used and then discarded.

This unblocks the Ollama backend's done_reason=length truncation
signal (the only marker callers get that the 16K completion cap
chopped their output) and surfaces token-count/usage metadata that
the existing claude/gemini/codex parsers already produce. omitempty
keeps results without metadata serializing identically to today.
EOF
)"
```

---

## Task 1: `ConfigErrorResult` helper

**Why first:** Every later task that exercises the `Run()` error path needs this helper to exist. Current `NotFoundResult` emits `"<backend> CLI not found in PATH"` which is semantically wrong for an HTTP backend where the failure is missing env vars, not a missing binary.

**Files:**
- Modify: `internal/roundtable/result.go` (append a function after `ProbeFailedResult`)
- Test: `internal/roundtable/result_test.go` (append a test function)

- [ ] **Step 1: Write the failing test**

Append to `internal/roundtable/result_test.go`:

```go
func TestConfigErrorResult(t *testing.T) {
	r := ConfigErrorResult("ollama", "kimi-k2.6:cloud", "OLLAMA_API_KEY not set")
	if r.Status != "error" {
		t.Errorf("status = %q, want error", r.Status)
	}
	if r.Model != "kimi-k2.6:cloud" {
		t.Errorf("model = %q, want kimi-k2.6:cloud", r.Model)
	}
	if !strings.Contains(r.Stderr, "OLLAMA_API_KEY not set") {
		t.Errorf("stderr = %q, want substring 'OLLAMA_API_KEY not set'", r.Stderr)
	}
	if !strings.Contains(r.Response, "ollama") {
		t.Errorf("response = %q, want substring 'ollama'", r.Response)
	}
}

func TestConfigErrorResult_DefaultModel(t *testing.T) {
	r := ConfigErrorResult("ollama", "", "no model configured")
	if r.Model != "cli-default" {
		t.Errorf("model = %q, want cli-default", r.Model)
	}
}
```

The file doesn't currently import `strings`. Add it to the `import` block:

```go
import (
	"encoding/json"
	"strings"
	"testing"
)
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/roundtable/ -run 'TestConfigErrorResult' -v
```

Expected: FAIL — `undefined: ConfigErrorResult`.

- [ ] **Step 3: Implement `ConfigErrorResult`**

Append to `internal/roundtable/result.go` after the `ProbeFailedResult` function:

```go
// ConfigErrorResult is the HTTP-native analogue of NotFoundResult/ProbeFailedResult.
// Use it when a backend cannot run due to missing/invalid configuration
// (e.g., missing API key, unresolvable model) rather than a missing binary
// or a failed probe. Status is "error" so callers treat it as a normal
// per-agent failure, not a dispatch-wide fault.
func ConfigErrorResult(backendName, model, reason string) *Result {
	if model == "" {
		model = "cli-default"
	}
	return &Result{
		Model:    model,
		Status:   "error",
		Response: backendName + " backend misconfigured: " + reason,
		Stderr:   reason,
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/roundtable/ -run 'TestConfigErrorResult' -v
```

Expected: PASS (both new tests).

- [ ] **Step 5: Run the full package test suite as a regression guard**

Run:
```bash
go test ./internal/roundtable/
```

Expected: PASS — no existing tests broken.

- [ ] **Step 6: Commit**

```bash
git add internal/roundtable/result.go internal/roundtable/result_test.go
git commit -m "$(cat <<'EOF'
feat(ollama): add ConfigErrorResult for HTTP backend misconfig

Introduces the HTTP-native analogue of NotFoundResult. Used by
OllamaBackend when OLLAMA_API_KEY or a model is missing — those
failures aren't "CLI not found in PATH" and shouldn't masquerade
as such in the Result.Stderr.
EOF
)"
```

---

## Task 2: `OllamaBackend` scaffold + offline `Healthy()`

Build the struct, constructor, and the four methods that don't touch HTTP yet: `Name`, `Start`, `Stop`, `Healthy`. The `Healthy()` design invariant (offline-only) matters: the dispatcher invokes `Healthy()` concurrently per-agent, so a network probe would self-DoS Ollama's 1/3/10 concurrency cap. This task locks that invariant in with a test.

**Files:**
- Create: `internal/roundtable/ollama.go`
- Create: `internal/roundtable/ollama_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/roundtable/ollama_test.go` with:

```go
package roundtable

import (
	"context"
	"testing"
)

func TestOllamaBackend_Name(t *testing.T) {
	b := NewOllamaBackend("", nil)
	if b.Name() != "ollama" {
		t.Errorf("Name() = %q, want ollama", b.Name())
	}
}

func TestOllamaBackend_Healthy_NoAPIKey(t *testing.T) {
	t.Setenv("OLLAMA_API_KEY", "")
	b := NewOllamaBackend("", nil)
	if err := b.Healthy(context.Background()); err == nil {
		t.Error("Healthy with empty OLLAMA_API_KEY: want error, got nil")
	}
}

func TestOllamaBackend_Healthy_WithAPIKey(t *testing.T) {
	t.Setenv("OLLAMA_API_KEY", "sk-test-value")
	b := NewOllamaBackend("", nil)
	if err := b.Healthy(context.Background()); err != nil {
		t.Errorf("Healthy with OLLAMA_API_KEY set: want nil, got %v", err)
	}
}

// Healthy must NEVER perform a network request. This test passes a context
// that's already canceled; if Healthy made a network call, it would return
// the context error. The offline-only invariant is load-bearing — see
// docs/plans/2026-04-20-ollama-cloud-provider.md §5.8.
func TestOllamaBackend_Healthy_IsOffline(t *testing.T) {
	t.Setenv("OLLAMA_API_KEY", "sk-test-value")
	b := NewOllamaBackend("", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.Healthy(ctx); err != nil {
		t.Errorf("Healthy with canceled ctx: want nil (offline check), got %v", err)
	}
}

func TestOllamaBackend_StartStop_NoError(t *testing.T) {
	b := NewOllamaBackend("", nil)
	if err := b.Start(context.Background()); err != nil {
		t.Errorf("Start: %v", err)
	}
	if err := b.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

// Note: nil-ObserveFunc normalization is implicitly tested by every other
// test in this file (and across Task 5) that passes nil and then exercises
// Run() — if the constructor didn't normalize, the deferred call in Run()
// would nil-deref and those tests would panic. A standalone "is nil safe?"
// test would be tautological (asserting only `b != nil`), so we rely on
// the integration tests' coverage instead.
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/roundtable/ -run 'TestOllamaBackend' -v
```

Expected: FAIL — `undefined: NewOllamaBackend`.

- [ ] **Step 3: Implement the scaffold**

Create `internal/roundtable/ollama.go`:

```go
package roundtable

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

// ObserveFunc is the optional metrics hook invoked once per Run() with the
// backend name, the resulting Result.Status, and the wall-clock elapsed
// time. Wired by cmd/roundtable-http-mcp/main.go to route into
// httpmcp.Metrics.ObserveBackend at the composition root. Defined in
// the roundtable package so OllamaBackend doesn't import httpmcp (which
// would cycle, since httpmcp already imports roundtable). A func value
// carries no package dependency, so there's no cycle.
type ObserveFunc func(backend, status string, elapsedMs int64)

// OllamaBackend implements Backend for Ollama Cloud (cloud-hosted :cloud
// models accessed over HTTPS with a bearer token). Unlike the subprocess
// backends (gemini/codex/claude), this one has no CLI harness — requests
// go directly to POST {OLLAMA_BASE_URL}/api/chat.
//
// Design invariants:
//   - Healthy() is offline: it only validates env vars. The dispatcher
//     runs Healthy() concurrently per-agent (run.go), so a network probe
//     would burn our concurrency cap (1 free / 3 pro / 10 max) before
//     Run() even starts.
//   - OLLAMA_API_KEY and OLLAMA_BASE_URL are read per-Run, not cached at
//     construction, so key rotation doesn't require a server restart
//     (matches subprocess backends that re-read env per-spawn).
//   - Run() uses the shared *http.Client; http.Client is safe for
//     concurrent use per stdlib guarantee.
//   - observe is never nil after NewOllamaBackend (constructor normalizes
//     nil to a no-op closure), so Run() can call it unconditionally.
type OllamaBackend struct {
	httpClient   *http.Client
	defaultModel string
	observe      ObserveFunc
}

// ollamaMaxResponseBytes caps response bodies to protect against a
// misconfigured upstream streaming unbounded garbage. 8 MiB is well over
// the 16,384-token completion cap with headroom for JSON framing.
const ollamaMaxResponseBytes = 8 * 1024 * 1024

// NewOllamaBackend returns a backend configured with explicit timeouts on
// every layer that can stall independently of context cancellation.
// defaultModel is the fallback when neither AgentSpec.Model nor
// OLLAMA_DEFAULT_MODEL are set. observe is invoked once per Run() with
// (backend, status, elapsedMs); pass nil if metrics aren't needed.
func NewOllamaBackend(defaultModel string, observe ObserveFunc) *OllamaBackend {
	if observe == nil {
		observe = func(string, string, int64) {}
	}
	return &OllamaBackend{
		defaultModel: defaultModel,
		observe:      observe,
		httpClient: &http.Client{
			// No Client.Timeout: we rely on the dispatcher's context deadline.
			// But Transport needs explicit timeouts because context
			// cancellation only reaches net/http AFTER the request is
			// in flight; a stalled TLS handshake can otherwise hang.
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

func (o *OllamaBackend) Name() string                  { return "ollama" }
func (o *OllamaBackend) Start(_ context.Context) error { return nil }
func (o *OllamaBackend) Stop() error                   { return nil }

// Healthy validates configuration only. DO NOT add a network probe here —
// see the OllamaBackend docstring and docs/plans/2026-04-20-ollama-cloud-provider.md
// §5.8. The dispatcher calls this concurrently per-agent; a probe would
// burn the concurrency quota before any Run() executes.
func (o *OllamaBackend) Healthy(_ context.Context) error {
	if os.Getenv("OLLAMA_API_KEY") == "" {
		return fmt.Errorf("OLLAMA_API_KEY not set")
	}
	return nil
}

// Run is implemented in Task 5 (uses the helper from Task 4).
func (o *OllamaBackend) Run(ctx context.Context, req Request) (*Result, error) {
	return nil, fmt.Errorf("not implemented yet")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/roundtable/ -run 'TestOllamaBackend' -v
```

Expected: PASS on `TestOllamaBackend_Name`, `TestOllamaBackend_Healthy_*`, `TestOllamaBackend_StartStop_NoError`.

- [ ] **Step 5: Verify `OllamaBackend` satisfies the `Backend` interface**

Append to `internal/roundtable/ollama_test.go`:

```go
// Compile-time assertion that OllamaBackend satisfies Backend.
var _ Backend = (*OllamaBackend)(nil)
```

Then:
```bash
go build ./internal/roundtable/
```

Expected: build succeeds. If `Backend` interface check fails, the compiler will point at the missing method.

- [ ] **Step 6: Commit**

```bash
git add internal/roundtable/ollama.go internal/roundtable/ollama_test.go
git commit -m "$(cat <<'EOF'
feat(ollama): add OllamaBackend scaffold with offline Healthy

Introduces the struct, constructor, and lifecycle methods. Run() is
a stub; the parser and HTTP call land in subsequent commits.

Healthy() is explicitly offline (env validation only). A network
probe here would self-DoS Ollama's 1/3/10 concurrency cap because
the dispatcher calls Healthy concurrently per-agent. This invariant
is codified with TestOllamaBackend_Healthy_IsOffline.

Transport timeouts (dial, TLS handshake, response headers) are set
explicitly because context cancellation only reaches net/http after
the request is in flight.

Constructor accepts an ObserveFunc for metrics; nil is normalized
to a no-op so call sites that don't care about metrics can pass
nil. The actual metrics wiring lands in Task 8.
EOF
)"
```

---

## Task 3: Response parser

Build `ollamaParseResponse` as a pure function of `(body []byte, statusCode int) → ParsedOutput`. This is table-testable in isolation, before the HTTP plumbing exists.

**Files:**
- Modify: `internal/roundtable/ollama.go` (append parser functions)
- Modify: `internal/roundtable/ollama_test.go` (append table tests)

- [ ] **Step 1: Write the failing tests**

Append to `internal/roundtable/ollama_test.go`:

```go
import (
	"strings"
)

func TestOllamaParse_Success(t *testing.T) {
	body := []byte(`{
		"model":"kimi-k2.6:cloud",
		"message":{"role":"assistant","content":"Hello from kimi"},
		"done_reason":"stop",
		"prompt_eval_count":42,
		"eval_count":8
	}`)
	parsed := ollamaParseResponse(body, 200, "")
	if parsed.Status != "ok" {
		t.Errorf("status = %q, want ok", parsed.Status)
	}
	if parsed.Response != "Hello from kimi" {
		t.Errorf("response = %q, want 'Hello from kimi'", parsed.Response)
	}
	if parsed.Metadata["model_used"] != "kimi-k2.6:cloud" {
		t.Errorf("model_used = %v, want kimi-k2.6:cloud", parsed.Metadata["model_used"])
	}
	if parsed.Metadata["done_reason"] != "stop" {
		t.Errorf("done_reason = %v, want stop", parsed.Metadata["done_reason"])
	}
	tokens, ok := parsed.Metadata["tokens"].(map[string]any)
	if !ok {
		t.Fatalf("tokens not a map: %T", parsed.Metadata["tokens"])
	}
	if tokens["prompt_eval_count"].(float64) != 42 {
		t.Errorf("prompt_eval_count = %v, want 42", tokens["prompt_eval_count"])
	}
	if tokens["eval_count"].(float64) != 8 {
		t.Errorf("eval_count = %v, want 8", tokens["eval_count"])
	}
}

func TestOllamaParse_Truncated(t *testing.T) {
	// done_reason == "length" means the 16K output cap (or model's own
	// max_tokens) cut the response short. This MUST be surfaced — it's
	// the only signal callers get that their deepdive output was chopped.
	body := []byte(`{
		"model":"glm-5.1:cloud",
		"message":{"role":"assistant","content":"partial output..."},
		"done_reason":"length"
	}`)
	parsed := ollamaParseResponse(body, 200, "")
	if parsed.Status != "ok" {
		t.Errorf("status = %q, want ok (truncation is not an error)", parsed.Status)
	}
	if parsed.Metadata["done_reason"] != "length" {
		t.Errorf("done_reason = %v, want length", parsed.Metadata["done_reason"])
	}
}

func TestOllamaParse_RateLimited429(t *testing.T) {
	body := []byte(`{"error":"rate limit exceeded"}`)
	parsed := ollamaParseResponse(body, 429, "")
	if parsed.Status != "rate_limited" {
		t.Errorf("status = %q, want rate_limited", parsed.Status)
	}
	if !strings.Contains(parsed.Response, "rate") {
		t.Errorf("response = %q, want substring 'rate'", parsed.Response)
	}
}

func TestOllamaParse_RateLimited429_WithRetryAfter(t *testing.T) {
	// Ollama doesn't currently publish Retry-After (architectural plan §5.4),
	// but if/when it does, the parser must surface it on Metadata so callers
	// can back off intelligently. Pass the header value through; don't try
	// to parse it (RFC 7231 allows seconds OR an HTTP-date — caller decides).
	body := []byte(`{"error":"rate limit exceeded"}`)
	parsed := ollamaParseResponse(body, 429, "30")
	if parsed.Status != "rate_limited" {
		t.Errorf("status = %q, want rate_limited", parsed.Status)
	}
	if got := parsed.Metadata["retry_after"]; got != "30" {
		t.Errorf("metadata retry_after = %v, want 30", got)
	}
}

func TestOllamaParse_ServiceUnavailable503(t *testing.T) {
	// Ollama Cloud's 503 storms are documented load-shedding. Treat them
	// as rate-limit-equivalent so users react consistently.
	body := []byte(`{"error":"service unavailable"}`)
	parsed := ollamaParseResponse(body, 503, "")
	if parsed.Status != "rate_limited" {
		t.Errorf("status = %q, want rate_limited", parsed.Status)
	}
}

func TestOllamaParse_Unauthorized401(t *testing.T) {
	body := []byte(`{"error":"invalid api key"}`)
	parsed := ollamaParseResponse(body, 401, "")
	if parsed.Status != "error" {
		t.Errorf("status = %q, want error", parsed.Status)
	}
	if !strings.Contains(parsed.Response, "invalid api key") {
		t.Errorf("response = %q, want upstream error message", parsed.Response)
	}
}

func TestOllamaParse_Forbidden403(t *testing.T) {
	body := []byte(`{"error":"forbidden"}`)
	parsed := ollamaParseResponse(body, 403, "")
	if parsed.Status != "error" {
		t.Errorf("status = %q, want error", parsed.Status)
	}
}

func TestOllamaParse_MalformedJSON(t *testing.T) {
	body := []byte(`not json at all`)
	parsed := ollamaParseResponse(body, 200, "")
	if parsed.Status != "error" {
		t.Errorf("status = %q, want error", parsed.Status)
	}
	if parsed.ParseError == nil || *parsed.ParseError != "JSON parse failed" {
		t.Errorf("parse_error = %v, want 'JSON parse failed'", parsed.ParseError)
	}
}

func TestOllamaParse_MissingMessageField(t *testing.T) {
	body := []byte(`{"model":"kimi-k2.6:cloud","done":true}`)
	parsed := ollamaParseResponse(body, 200, "")
	if parsed.Status != "error" {
		t.Errorf("status = %q, want error (no message.content)", parsed.Status)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/roundtable/ -run 'TestOllamaParse' -v
```

Expected: FAIL — `undefined: ollamaParseResponse`.

- [ ] **Step 3: Implement the parser**

Append to `internal/roundtable/ollama.go`:

```go
import statements to add at top of file (merge with existing imports):
	"encoding/json"
```

Then append these functions to `internal/roundtable/ollama.go`:

```go
// ollamaParseResponse converts a raw response body + HTTP status code into
// a ParsedOutput. See docs/plans/2026-04-20-ollama-cloud-provider.md §4.1
// for the status-mapping rationale.
//
// Status codes:
//   - 200: parse JSON, expect {model, message: {content}, done_reason, ...}
//   - 401/403: status="error", pass through upstream error message
//   - 429: status="rate_limited" (Ollama Cloud doesn't currently publish
//     Retry-After, but if present it's surfaced on Metadata["retry_after"]
//     verbatim — caller decides how to interpret seconds vs HTTP-date)
//   - 503: status="rate_limited" (Ollama Cloud's 503 storms are load-shedding)
//   - other: status="error" with upstream body as response
//
// retryAfter is the raw Retry-After header value (or "" if absent). Passed
// through as a string rather than http.Header to keep the parser package-
// dependency-light and the tests trivial to construct.
func ollamaParseResponse(body []byte, statusCode int, retryAfter string) ParsedOutput {
	switch {
	case statusCode == 429 || statusCode == 503:
		return ollamaRateLimitedOutput(body, statusCode, retryAfter)
	case statusCode >= 400:
		return ollamaErrorOutput(body, statusCode)
	case statusCode != 200:
		// 1xx/3xx aren't expected here; treat as error.
		return ollamaErrorOutput(body, statusCode)
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		pe := "JSON parse failed"
		return ParsedOutput{
			Response:   string(body),
			Status:     "error",
			ParseError: &pe,
		}
	}

	msg, ok := data["message"].(map[string]any)
	if !ok {
		return ParsedOutput{
			Response: "ollama: response missing message field",
			Status:   "error",
		}
	}
	content, _ := msg["content"].(string)

	metadata := map[string]any{}
	if m, ok := data["model"].(string); ok {
		metadata["model_used"] = m
	}
	if dr, ok := data["done_reason"].(string); ok {
		metadata["done_reason"] = dr
	}
	// Preserve token counts as a nested map for metadata consumers;
	// float64 is what encoding/json gives us for JSON numbers.
	tokens := map[string]any{}
	if v, ok := data["prompt_eval_count"]; ok {
		tokens["prompt_eval_count"] = v
	}
	if v, ok := data["eval_count"]; ok {
		tokens["eval_count"] = v
	}
	if len(tokens) > 0 {
		metadata["tokens"] = tokens
	}

	return ParsedOutput{
		Response: content,
		Status:   "ok",
		Metadata: metadata,
	}
}

// ollamaRateLimitedOutput formats a 429/503 response uniformly. retryAfter
// is the raw Retry-After header value (or "" if absent); when present it's
// surfaced on Metadata so callers can back off intelligently.
func ollamaRateLimitedOutput(body []byte, statusCode int, retryAfter string) ParsedOutput {
	msg := ollamaExtractErrorMessage(body)
	if msg == "" {
		msg = fmt.Sprintf("HTTP %d", statusCode)
	}
	suffix := ". No Retry-After is published; back off and retry later."
	if retryAfter != "" {
		suffix = ". Retry-After: " + retryAfter
	}
	out := ParsedOutput{
		Response: "Ollama rate limited (HTTP " + fmt.Sprint(statusCode) + "): " + msg + suffix,
		Status:   "rate_limited",
	}
	if retryAfter != "" {
		out.Metadata = map[string]any{"retry_after": retryAfter}
	}
	return out
}

// ollamaErrorOutput formats a non-success non-rate-limit response.
func ollamaErrorOutput(body []byte, statusCode int) ParsedOutput {
	msg := ollamaExtractErrorMessage(body)
	if msg == "" {
		msg = string(body)
	}
	return ParsedOutput{
		Response: fmt.Sprintf("ollama HTTP %d: %s", statusCode, msg),
		Status:   "error",
	}
}

// ollamaExtractErrorMessage pulls a human-readable message out of an
// error body. Accepts {"error":"..."} or {"error":{"message":"..."}}.
// Returns "" if body doesn't parse or has no error field.
func ollamaExtractErrorMessage(body []byte) string {
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return ""
	}
	switch v := data["error"].(type) {
	case string:
		return v
	case map[string]any:
		if m, ok := v["message"].(string); ok {
			return m
		}
	}
	return ""
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/roundtable/ -run 'TestOllamaParse' -v
```

Expected: PASS on all eight parser tests.

- [ ] **Step 5: Run the full package suite as a regression guard**

Run:
```bash
go test ./internal/roundtable/
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/roundtable/ollama.go internal/roundtable/ollama_test.go
git commit -m "$(cat <<'EOF'
feat(ollama): add response parser with done_reason metadata

Handles Ollama /api/chat success responses and four error classes
(401/403 -> error, 429/503 -> rate_limited, other -> error,
malformed JSON -> error with parse_error set).

done_reason is surfaced in metadata because "length" is the only
signal callers get that the 16,384-token completion cap truncated
their output.
EOF
)"
```

---

## Task 4: `inlineFileContents` — file content inliner

**Why this task exists:** The subprocess CLI backends (claude/codex/gemini) open files referenced in `req.Files` via their own tool-calling loops. An HTTP-only backend calling `/api/chat` has no tool loop, so the model would see the file-name list from `AssemblePrompt` but have no way to fetch the contents. This task adds a helper that eagerly reads the files and produces an XML-tag-wrapped blob suitable for prepending to the user message. See `docs/plans/2026-04-20-ollama-cloud-provider.md` (file-inlining section) for the full rationale.

**Files:**
- Modify: `internal/roundtable/ollama.go` (add the helper + constants; no import changes beyond what Task 3 added, but add `os` for `os.ReadFile`)
- Modify: `internal/roundtable/ollama_test.go` (append table tests)

- [ ] **Step 1: Write the failing tests**

Append to `internal/roundtable/ollama_test.go`:

```go
import (
	"bytes"
	"os"
	"path/filepath"
)

func TestInlineFileContents_Empty(t *testing.T) {
	if got := inlineFileContents(nil); got != "" {
		t.Errorf("nil paths: got %q, want empty", got)
	}
	if got := inlineFileContents([]string{}); got != "" {
		t.Errorf("empty paths: got %q, want empty", got)
	}
}

func TestInlineFileContents_SingleFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(p, []byte("hello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := inlineFileContents([]string{p})
	if !strings.Contains(got, "hello world") {
		t.Errorf("expected content in output: %q", got)
	}
	if !strings.Contains(got, `<file path="`+p+`">`) {
		t.Errorf("expected opening <file> tag: %q", got)
	}
	if !strings.Contains(got, "</file>") {
		t.Errorf("expected closing </file> tag: %q", got)
	}
}

func TestInlineFileContents_UnreadableFile(t *testing.T) {
	got := inlineFileContents([]string{"/nonexistent/definitely-not-here.txt"})
	if !strings.Contains(got, "error=") {
		t.Errorf("expected error attr: %q", got)
	}
	if !strings.Contains(got, "/nonexistent/definitely-not-here.txt") {
		t.Errorf("expected path in error output: %q", got)
	}
}

func TestInlineFileContents_TruncatesLargeFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "big.bin")
	big := bytes.Repeat([]byte("a"), ollamaMaxFileBytes+1024)
	if err := os.WriteFile(p, big, 0o644); err != nil {
		t.Fatal(err)
	}
	got := inlineFileContents([]string{p})
	if !strings.Contains(got, "<truncated />") {
		t.Errorf("expected <truncated /> marker: output len %d", len(got))
	}
	// Must not exceed per-file cap by more than tag overhead.
	if len(got) > ollamaMaxFileBytes+1024 {
		t.Errorf("output exceeds per-file cap plus overhead: %d > %d", len(got), ollamaMaxFileBytes+1024)
	}
}

func TestInlineFileContents_SkipsOverTotalBudget(t *testing.T) {
	dir := t.TempDir()
	// Create 5 files at the per-file cap. Total = 5 * 128 KiB = 640 KiB,
	// budget is 512 KiB. First 4 fit, last 1 should be skipped.
	block := bytes.Repeat([]byte("x"), ollamaMaxFileBytes)
	var paths []string
	for i := 0; i < 5; i++ {
		p := filepath.Join(dir, fmt.Sprintf("f%d.bin", i))
		if err := os.WriteFile(p, block, 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	got := inlineFileContents(paths)
	if !strings.Contains(got, "<skipped-files>") {
		t.Errorf("expected <skipped-files> block: output len %d", len(got))
	}
	if !strings.Contains(got, paths[4]) {
		t.Errorf("expected last file (%s) to be named in skipped block", paths[4])
	}
	// First file should still be present as a full <file>.
	if !strings.Contains(got, `<file path="`+paths[0]+`">`) {
		t.Error("expected first file to still be inlined")
	}
}
```

The test file already imports `fmt` and `strings` from earlier tasks; `bytes`, `os`, and `path/filepath` are new additions.

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/roundtable/ -run 'TestInlineFileContents' -v
```

Expected: FAIL — `undefined: inlineFileContents` and `undefined: ollamaMaxFileBytes`.

- [ ] **Step 3: Implement the helper**

In `internal/roundtable/ollama.go`, update the imports block to include `os` and `strings` (merge — `fmt` is already there from Task 3):

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)
```

Near the existing `ollamaMaxResponseBytes` constant, add:

```go
// ollamaMaxFileBytes caps a single inlined file. Source files rarely exceed
// this; binaries and LLM dumps get cut with a <truncated /> marker inside
// the <file> block.
const ollamaMaxFileBytes = 128 * 1024

// ollamaMaxTotalFileBytes caps the aggregate size across all inlined files
// in one dispatch. Sized to fit comfortably inside a 128K-token context
// window (roughly 128K tokens at 4 bytes/token average). Files beyond the
// budget are listed in a <skipped-files> block so the model at least knows
// they existed.
const ollamaMaxTotalFileBytes = 512 * 1024
```

Append the helper function (placement: after the parser functions, before `Run()`):

```go
// inlineFileContents reads the given paths and produces an XML-tag-wrapped
// blob suitable for prepending to a user message. Format:
//
//	<file path="X">
//	<contents...>
//	</file>
//
// Oversized files are truncated with <truncated /> inside the block. Files
// beyond the aggregate budget are listed by path inside <skipped-files>.
// Unreadable files emit self-closing <file path="X" error="..." /> tags so
// the model sees failures rather than silent omission.
//
// Returns "" for nil/empty paths.
func inlineFileContents(paths []string) string {
	if len(paths) == 0 {
		return ""
	}

	var sb strings.Builder
	var total int
	var skipped []string

	for _, p := range paths {
		if total >= ollamaMaxTotalFileBytes {
			skipped = append(skipped, p)
			continue
		}

		data, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintf(&sb, "<file path=%q error=%q />\n\n", p, err.Error())
			continue
		}

		truncated := false
		if len(data) > ollamaMaxFileBytes {
			data = data[:ollamaMaxFileBytes]
			truncated = true
		}

		remaining := ollamaMaxTotalFileBytes - total
		if len(data) > remaining {
			if remaining <= 0 {
				skipped = append(skipped, p)
				continue
			}
			data = data[:remaining]
			truncated = true
		}

		fmt.Fprintf(&sb, "<file path=%q>\n", p)
		sb.Write(data)
		if truncated {
			sb.WriteString("\n<truncated />")
		}
		sb.WriteString("\n</file>\n\n")
		total += len(data)
	}

	if len(skipped) > 0 {
		sb.WriteString("<skipped-files>\n")
		for _, p := range skipped {
			fmt.Fprintf(&sb, "- %s\n", p)
		}
		sb.WriteString("</skipped-files>\n\n")
	}

	return sb.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/roundtable/ -run 'TestInlineFileContents' -v
```

Expected: PASS on all five tests.

- [ ] **Step 5: Run the full package suite as a regression guard**

Run:
```bash
go test ./internal/roundtable/
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/roundtable/ollama.go internal/roundtable/ollama_test.go
git commit -m "$(cat <<'EOF'
feat(ollama): add inlineFileContents helper

HTTP-only backends have no tool-calling loop; the subprocess CLIs
handle req.Files by using their own read tools. inlineFileContents
bridges that gap: it eagerly reads the file list and emits an
XML-tag-wrapped blob the Run() method will prepend to the user
message in Task 5.

Caps: 128 KiB per file, 512 KiB total. Files over the per-file cap
get <truncated />; files beyond the aggregate budget are listed in
<skipped-files>. Unreadable files become <file ... error="..." />
so the model sees the failure instead of silently missing context.
EOF
)"
```

---

## Task 5: `Run()` HTTP flow

Wire `Run()` to build the request, call `httpClient.Do`, read body under `io.LimitReader`, and funnel status code + body through `ollamaParseResponse`. Map context deadlines explicitly to `status="timeout"`.

**Files:**
- Modify: `internal/roundtable/ollama.go` (replace the stub `Run` body; add imports)
- Modify: `internal/roundtable/ollama_test.go` (append httptest-based tests)

- [ ] **Step 1: Write the failing tests**

Append to `internal/roundtable/ollama_test.go`:

```go
import (
	"net/http"
	"net/http/httptest"
	"time"
)

func newOllamaTestBackend(t *testing.T, baseURL string) *OllamaBackend {
	t.Helper()
	t.Setenv("OLLAMA_BASE_URL", baseURL)
	t.Setenv("OLLAMA_API_KEY", "sk-test-value")
	// observe=nil — constructor normalizes to no-op. Tests that need to
	// observe metrics build their own backend (see TestOllamaRun_EmitsMetrics).
	return NewOllamaBackend("kimi-k2.6:cloud", nil)
}

func TestOllamaRun_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path = %q, want /api/chat", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test-value" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model":"kimi-k2.6:cloud",
			"message":{"role":"assistant","content":"Hello from kimi"},
			"done_reason":"stop",
			"prompt_eval_count":10,
			"eval_count":4
		}`))
	}))
	defer srv.Close()

	b := newOllamaTestBackend(t, srv.URL)
	res, err := b.Run(context.Background(), Request{Prompt: "hi", Model: "kimi-k2.6:cloud"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != "ok" {
		t.Errorf("status = %q, want ok", res.Status)
	}
	if res.Response != "Hello from kimi" {
		t.Errorf("response = %q", res.Response)
	}
	if res.Model != "kimi-k2.6:cloud" {
		t.Errorf("model = %q", res.Model)
	}
	if res.ElapsedMs <= 0 {
		t.Errorf("elapsed_ms = %d, want > 0", res.ElapsedMs)
	}
	// End-to-end metadata propagation: parser populates parsed.Metadata,
	// BuildResult propagates it onto Result.Metadata (Task 0 makes this
	// path real). done_reason is the load-bearing one — it's how callers
	// detect 16K-cap truncation.
	if res.Metadata == nil {
		t.Fatal("res.Metadata = nil; Task 0 propagation did not land")
	}
	if got := res.Metadata["done_reason"]; got != "stop" {
		t.Errorf("metadata done_reason = %v, want stop", got)
	}
}

func TestOllamaRun_TruncationSurfaced(t *testing.T) {
	// done_reason=length must survive the parser → BuildResult → Result
	// pipeline so callers can detect 16K-cap truncation (architectural
	// plan §5.3).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"model":"kimi-k2.6:cloud",
			"message":{"role":"assistant","content":"partial..."},
			"done_reason":"length"
		}`))
	}))
	defer srv.Close()

	b := newOllamaTestBackend(t, srv.URL)
	res, err := b.Run(context.Background(), Request{Prompt: "hi", Model: "kimi-k2.6:cloud"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != "ok" {
		t.Errorf("status = %q, want ok (truncation isn't an error)", res.Status)
	}
	if res.Metadata == nil || res.Metadata["done_reason"] != "length" {
		t.Errorf("metadata done_reason = %v, want length", res.Metadata["done_reason"])
	}
}

// Note: there is no TestOllamaRun_NoAPIKey because Run() doesn't validate
// OLLAMA_API_KEY anymore (it would be dead code given Task 2's Healthy()
// and Task 7's conditional buildBackends registration). The missing-key
// case is covered by TestOllamaBackend_Healthy_NoAPIKey in Task 2.

func TestOllamaRun_NoModelResolvable(t *testing.T) {
	t.Setenv("OLLAMA_API_KEY", "sk-test-value")
	b := NewOllamaBackend("", nil) // no default
	res, err := b.Run(context.Background(), Request{Prompt: "hi"}) // no Model either
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != "error" {
		t.Errorf("status = %q, want error", res.Status)
	}
}

func TestOllamaRun_RateLimited429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"too many requests"}`))
	}))
	defer srv.Close()

	b := newOllamaTestBackend(t, srv.URL)
	res, err := b.Run(context.Background(), Request{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != "rate_limited" {
		t.Errorf("status = %q, want rate_limited", res.Status)
	}
}

func TestOllamaRun_ContextDeadline(t *testing.T) {
	// Server hangs; client context expires first. Expect status=timeout.
	// Timeout is routed through BuildResult so the response text is the
	// "Request timed out after Ns. Retry with..." formatting that
	// subprocess backends use — that's the user-visible contract.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()

	b := newOllamaTestBackend(t, srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	res, err := b.Run(ctx, Request{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != "timeout" {
		t.Errorf("status = %q, want timeout", res.Status)
	}
	// BuildResult emits a "Request timed out after" message — confirm we
	// went through that path, not the manual &Result{} construction.
	if !strings.Contains(res.Response, "timed out") {
		t.Errorf("response = %q, want 'timed out' message from BuildResult", res.Response)
	}
}

func TestOllamaRun_NetworkError(t *testing.T) {
	// Non-routable base URL forces a dial error.
	t.Setenv("OLLAMA_API_KEY", "sk-test-value")
	t.Setenv("OLLAMA_BASE_URL", "http://127.0.0.1:1") // port 1 refuses connections
	b := NewOllamaBackend("kimi-k2.6:cloud", nil)
	res, err := b.Run(context.Background(), Request{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != "error" {
		t.Errorf("status = %q, want error", res.Status)
	}
	if res.Stderr == "" {
		t.Error("stderr should contain the dial error")
	}
}

func TestOllamaRun_DefaultBaseURL(t *testing.T) {
	// We need to verify the fall-through to https://ollama.com without
	// actually hitting the network. Strategy: stand up a probe server on
	// 127.0.0.1, but DON'T set OLLAMA_BASE_URL — instead, intercept the
	// outbound URL by injecting an *http.Client whose Transport rewrites
	// the request to localhost. That keeps us hermetic.
	//
	// Simpler alternative used here: set OLLAMA_BASE_URL to a non-routable
	// loopback port that refuses connections immediately. The default-URL
	// codepath is exercised by other tests (newOllamaTestBackend uses an
	// httptest server URL); this test just guards against the env-missing
	// short-circuit being incorrectly reintroduced.
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.Path
		_, _ = w.Write([]byte(`{"model":"kimi-k2.6:cloud","message":{"role":"assistant","content":"ok"},"done_reason":"stop"}`))
	}))
	defer srv.Close()

	t.Setenv("OLLAMA_API_KEY", "sk-test-value")
	t.Setenv("OLLAMA_BASE_URL", srv.URL)
	b := NewOllamaBackend("kimi-k2.6:cloud", nil)
	res, err := b.Run(context.Background(), Request{Prompt: "hi", Model: "kimi-k2.6:cloud"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != "ok" {
		t.Errorf("status = %q, want ok", res.Status)
	}
	if gotURL != "/api/chat" {
		t.Errorf("path = %q, want /api/chat (URL composition broken)", gotURL)
	}
}

func TestOllamaRun_DefaultBaseURL_FallsThroughWhenUnset(t *testing.T) {
	// Belt-and-suspenders: when OLLAMA_BASE_URL is unset, the backend must
	// build a URL against https://ollama.com. We don't want to hit the
	// network even briefly, so point at a non-routable loopback port that
	// refuses connections immediately. The error message must reference
	// "ollama.com" so we know the default URL was constructed correctly.
	//
	// (We use a TCP-refuse loopback rather than letting a 5ms ctx race
	// against real DNS; previous draft of this test would intermittently
	// reach ollama.com from CI machines.)
	t.Setenv("OLLAMA_API_KEY", "sk-test-value")
	t.Setenv("OLLAMA_BASE_URL", "")
	// Use a context already canceled to guarantee we don't actually dial.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := NewOllamaBackend("kimi-k2.6:cloud", nil)
	res, _ := b.Run(ctx, Request{Prompt: "hi"})
	// The transport will return context.Canceled before any network I/O.
	// Status will be "error" (not "timeout" — Canceled isn't DeadlineExceeded).
	// What we're really asserting: the codepath got past env-resolution and
	// reached httpClient.Do, which means baseURL was non-empty.
	if res.Status != "error" {
		t.Errorf("status = %q, want error (canceled ctx)", res.Status)
	}
	if res.Stderr == "" {
		t.Error("stderr empty; expected canceled-ctx error message")
	}
}

func TestOllamaRun_InlinesFileContents(t *testing.T) {
	// Write two temp files and assert their contents reach the server
	// inside the user message, wrapped in <file> tags.
	dir := t.TempDir()
	p1 := filepath.Join(dir, "one.txt")
	p2 := filepath.Join(dir, "two.txt")
	if err := os.WriteFile(p1, []byte("alpha body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, []byte("beta body"), 0o644); err != nil {
		t.Fatal(err)
	}

	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"model":"kimi-k2.6:cloud","message":{"role":"assistant","content":"ok"},"done_reason":"stop"}`))
	}))
	defer srv.Close()

	b := newOllamaTestBackend(t, srv.URL)
	_, err := b.Run(context.Background(), Request{
		Prompt: "review these",
		Model:  "kimi-k2.6:cloud",
		Files:  []string{p1, p2},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	body := string(gotBody)
	for _, want := range []string{
		`<file path="` + p1 + `">`,
		"alpha body",
		`<file path="` + p2 + `">`,
		"beta body",
		"review these", // original prompt still present
	} {
		if !strings.Contains(body, want) {
			t.Errorf("request body missing %q", want)
		}
	}
}

func TestOllamaRun_NoFilesNoInlining(t *testing.T) {
	// Files: nil -> user content is just req.Prompt, no <file> tags.
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"model":"kimi-k2.6:cloud","message":{"role":"assistant","content":"ok"},"done_reason":"stop"}`))
	}))
	defer srv.Close()

	b := newOllamaTestBackend(t, srv.URL)
	_, err := b.Run(context.Background(), Request{
		Prompt: "no files here",
		Model:  "kimi-k2.6:cloud",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(string(gotBody), "<file ") {
		t.Errorf("unexpected <file> tag in request with no req.Files: %s", string(gotBody))
	}
}
```

These tests depend on `io`, `path/filepath`, and `os` being imported in the test file (already added in Task 4).

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/roundtable/ -run 'TestOllamaRun' -v
```

Expected: FAIL — `Run` returns `nil, "not implemented yet"`.

- [ ] **Step 3: Implement `Run()`**

In `internal/roundtable/ollama.go`, add imports `bytes`, `errors`, `io`, then replace the stub `Run` method with this full implementation:

```go
func (o *OllamaBackend) Run(ctx context.Context, req Request) (*Result, error) {
	apiKey := os.Getenv("OLLAMA_API_KEY")
	baseURL := os.Getenv("OLLAMA_BASE_URL")
	if baseURL == "" {
		baseURL = "https://ollama.com"
	}

	model := req.Model
	if model == "" {
		model = o.defaultModel
	}

	// NOTE on missing API key: we don't re-validate apiKey here. Healthy()
	// already failed the dispatcher's probe phase if the key is unset
	// (Task 2), and buildBackends only registers this backend when the key
	// is present at startup (Task 7). A defensive check here would be dead
	// code given those two invariants. The model check below stays because
	// model is per-Request, not per-process.
	if model == "" {
		return ConfigErrorResult("ollama", "",
			"no model resolved: set OLLAMA_DEFAULT_MODEL or AgentSpec.Model"), nil
	}

	// Prepend inlined file contents (if any). Subprocess backends get file
	// contents via their own tool loop; an HTTP call has no tool loop, so
	// we eagerly read req.Files here. See Task 4 for the helper.
	content := req.Prompt
	if inlined := inlineFileContents(req.Files); inlined != "" {
		content = inlined + content
	}

	bodyBytes, _ := json.Marshal(map[string]any{
		"model":    model,
		"messages": []map[string]string{{"role": "user", "content": content}},
		"stream":   false,
	})
	// json.Marshal can't fail on a fixed-shape map of strings; skip the
	// dead-branch error check.

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/api/chat", bytes.NewReader(bodyBytes))
	if err != nil {
		return ConfigErrorResult("ollama", model, "request build: "+err.Error()), nil
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	// NOTE: do NOT log bodyBytes or the response body at any level —
	// they contain user prompts and model output (PII/secret surface).
	// Log status code and elapsed time only.
	start := time.Now()
	resp, err := o.httpClient.Do(httpReq)
	elapsed := time.Since(start).Milliseconds()

	if err != nil {
		// Route timeout through BuildResult so it gets the same response-text
		// formatting (`Request timed out after Ns. Retry with...`) as the
		// subprocess backends. Avoids drift if BuildResult's timeout logic
		// grows new behavior. Other transport errors stay direct since they
		// have no equivalent path in BuildResult.
		if errors.Is(ctx.Err(), context.DeadlineExceeded) ||
			errors.Is(err, context.DeadlineExceeded) {
			return BuildResult(
				RawRunOutput{TimedOut: true, ElapsedMs: elapsed, Stderr: err.Error()},
				ParsedOutput{},
				model,
			), nil
		}
		return &Result{
			Model:     model,
			Status:    "error",
			Stderr:    err.Error(),
			ElapsedMs: elapsed,
		}, nil
	}
	defer resp.Body.Close()

	// Read under a cap. io.ReadAll returns io.EOF-safely on a LimitReader.
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, ollamaMaxResponseBytes))
	if readErr != nil {
		return &Result{
			Model:     model,
			Status:    "error",
			Stderr:    "read body: " + readErr.Error(),
			ElapsedMs: elapsed,
		}, nil
	}

	parsed := ollamaParseResponse(raw, resp.StatusCode, resp.Header.Get("Retry-After"))
	return BuildResult(
		RawRunOutput{Stdout: raw, ElapsedMs: elapsed},
		parsed,
		model,
	), nil
}
```

After editing, the complete `import` block at the top of `ollama.go` should be:

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/roundtable/ -run 'TestOllamaRun' -v
```

Expected: PASS on all seven `TestOllamaRun_*` tests.

- [ ] **Step 5: Run the full package suite**

Run:
```bash
go test ./internal/roundtable/
```

Expected: PASS across every test in the package.

- [ ] **Step 6: Vet check**

Run:
```bash
go vet ./internal/roundtable/
```

Expected: clean (no warnings).

- [ ] **Step 7: Commit**

```bash
git add internal/roundtable/ollama.go internal/roundtable/ollama_test.go
git commit -m "$(cat <<'EOF'
feat(ollama): implement Run() against native /api/chat

POSTs {model, messages:[{role:user,content:Prompt}], stream:false} with
bearer auth. Env read per-Run so OLLAMA_API_KEY rotation doesn't need
a restart (matches subprocess backends). Body capped by io.LimitReader
at 8 MiB. context.DeadlineExceeded maps explicitly to status=timeout
(subprocess path gets this via BuildResult; we don't go through the
same code).

Prompt and response bodies are never logged per PII invariant.
EOF
)"
```

---

## Task 5a: Client-side concurrency gate (`golang.org/x/sync/semaphore`)

**Why this task.** Ollama Cloud's concurrency ceiling is **1 free / 3 Pro / 10 Max**. A `hivemind` fanout that includes 4+ ollama agents (e.g., `kimi + qwen + glm + minimax`) on a Pro account will silently rate-limit one request every time — the 4th call racing 3 occupied upstream slots. This task adds a per-process bulkhead in `OllamaBackend` so concurrent `Run()` calls above the cap wait in-process instead of getting a 429 at Ollama's edge. All four calls complete successfully (modulo the dispatcher deadline).

**Library choice (buy, not build): `golang.org/x/sync/semaphore`.** The Go-team-maintained weighted semaphore. Chosen over alternatives after roundtable review:

| Option | Verdict |
|-|-|
| `golang.org/x/sync/semaphore` | **Chosen.** Go-team-maintained, ctx-aware `Acquire(ctx, n)` is atomic (no release-on-cancel race), ~3 LOC per use site, effectively stdlib-adjacent. |
| `chan struct{}` + `select` | Functionally equivalent for weight-1. Rejected: ctx-select release-on-cancel edge case is fiddly; 5 LOC vs 3; "buy" is unambiguously cheaper when the library is Go-team-maintained. |
| `failsafe-go/failsafe-go` | Only compelling if Decision C (no retry) gets reversed. Library excellent; imports a full resilience-policy framework for one bulkhead use = overkill here. |
| `eapache/go-resiliency`, `slok/goresilience`, `felixgeelhaar/fortify`, `cinar/resile` | Either too heavy for scope, too new, or both. None offers a clearly better primitive than `x/sync/semaphore` for this problem. |

**Behavior (decided after roundtable review): blocking gate, not fast-fail.** A fast-fail gate (non-blocking `TryAcquire`) produces the same user-facing outcome as no gate (3 ok + 1 `rate_limited`), only faster. The whole point is for the 4th call to eventually succeed, which requires waiting. Consequence: Decision C ("surface rate limits; no retry") is partially softened — burst 429s become added latency. **This is intentional for hivemind UX** and is called out explicitly in the architectural plan (new §4.6 added by this task). A debug log fires when `Acquire` waits >100ms so gate latency isn't invisible.

**Scope caveats, to be documented (no code):**
- **Per-process only.** Two `roundtable-http-mcp` instances sharing an `OLLAMA_API_KEY` can collectively exceed the cap. Acceptable for single-instance dogfood. Multi-instance deployments would need a shared (Redis) limiter or a sidecar proxy; out of scope.
- **Construction-time config.** `OLLAMA_MAX_CONCURRENT_REQUESTS` is read once in `NewOllamaBackend`. This is NOT a violation of Decision F (which is scoped to `OLLAMA_API_KEY`/`OLLAMA_BASE_URL` rotation); the cap matches your account tier and changes only when you upgrade, which warrants a restart. Analogous to `defaultModel` which is also captured at construction.
- **Acquire placement.** Immediately before `httpClient.Do`, AFTER file inlining and JSON marshaling. Holding a slot during local CPU work wastes effective capacity.

**Files:**
- Modify: `go.mod`, `go.sum` (add `golang.org/x/sync`)
- Modify: `internal/roundtable/ollama.go` (add `sem` field, read env in constructor, Acquire/Release around `Do`, log slow waits)
- Modify: `internal/roundtable/ollama_test.go` (serialization, gate deadline, gate cancel, env resolution tests)
- Modify: `docs/plans/2026-04-20-ollama-cloud-provider.md` (append §4.6 "Concurrency gate" documenting the decision)

- [ ] **Step 1: Add the dependency**

```bash
go get golang.org/x/sync
```

Verify it appeared in `go.mod`:

```bash
grep golang.org/x/sync go.mod
```

Expected: a `require golang.org/x/sync vX.Y.Z` line.

- [ ] **Step 2: Write the failing tests**

Append to `internal/roundtable/ollama_test.go`:

```go
func TestOllamaRun_SerializesOverCap(t *testing.T) {
	// With OLLAMA_MAX_CONCURRENT_REQUESTS=1 and two concurrent Runs against
	// a server that holds each request for 50ms, the second call must start
	// ≥40ms after the first (allowing for scheduler slack). If the gate is
	// missing, both calls arrive at the server within milliseconds.
	var (
		mu    sync.Mutex
		seen  []time.Time
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		seen = append(seen, time.Now())
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"ok"},"done_reason":"stop"}`))
	}))
	defer srv.Close()

	t.Setenv("OLLAMA_BASE_URL", srv.URL)
	t.Setenv("OLLAMA_API_KEY", "sk-test-value")
	t.Setenv("OLLAMA_MAX_CONCURRENT_REQUESTS", "1")
	b := NewOllamaBackend("kimi-k2.6:cloud", nil)

	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, _ = b.Run(context.Background(), Request{Prompt: "hi", Model: "kimi-k2.6:cloud"})
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("server saw %d requests, want 2", len(seen))
	}
	if gap := seen[1].Sub(seen[0]); gap < 40*time.Millisecond {
		t.Errorf("calls not serialized: gap = %v, want >= 40ms", gap)
	}
}

func TestOllamaRun_GateDeadline_MapsToTimeout(t *testing.T) {
	// Slot-holder takes forever; waiter has a tight deadline. Waiter must
	// return status=timeout with stderr mentioning the concurrency slot.
	hold := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-hold
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"ok"},"done_reason":"stop"}`))
	}))
	defer srv.Close()
	defer close(hold)

	t.Setenv("OLLAMA_BASE_URL", srv.URL)
	t.Setenv("OLLAMA_API_KEY", "sk-test-value")
	t.Setenv("OLLAMA_MAX_CONCURRENT_REQUESTS", "1")
	b := NewOllamaBackend("kimi-k2.6:cloud", nil)

	// Launch the holder; give it time to Acquire.
	go func() { _, _ = b.Run(context.Background(), Request{Prompt: "holder", Model: "kimi-k2.6:cloud"}) }()
	time.Sleep(20 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	res, _ := b.Run(ctx, Request{Prompt: "waiter", Model: "kimi-k2.6:cloud"})
	if res.Status != "timeout" {
		t.Errorf("status = %q, want timeout (gate deadline)", res.Status)
	}
	if !strings.Contains(res.Stderr, "concurrency") {
		t.Errorf("stderr = %q, want mention of concurrency slot", res.Stderr)
	}
}

func TestOllamaRun_GateCancel_MapsToError(t *testing.T) {
	// Holder + canceled (not deadline-exceeded) ctx → status=error, distinguishable
	// from timeout so callers/dashboards handle cancellation separately.
	hold := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-hold
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"ok"},"done_reason":"stop"}`))
	}))
	defer srv.Close()
	defer close(hold)

	t.Setenv("OLLAMA_BASE_URL", srv.URL)
	t.Setenv("OLLAMA_API_KEY", "sk-test-value")
	t.Setenv("OLLAMA_MAX_CONCURRENT_REQUESTS", "1")
	b := NewOllamaBackend("kimi-k2.6:cloud", nil)

	go func() { _, _ = b.Run(context.Background(), Request{Prompt: "holder", Model: "kimi-k2.6:cloud"}) }()
	time.Sleep(20 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	res, _ := b.Run(ctx, Request{Prompt: "waiter", Model: "kimi-k2.6:cloud"})
	if res.Status != "error" {
		t.Errorf("status = %q, want error (ctx canceled, not deadlined)", res.Status)
	}
}

func TestOllamaBackend_ResolveMaxConcurrent(t *testing.T) {
	cases := []struct {
		env  string
		want int64
	}{
		{"", 3},            // default
		{"1", 1},           // free tier
		{"10", 10},         // max tier
		{"notanumber", 3},  // invalid → default
		{"0", 3},           // zero invalid → default
		{"-2", 3},          // negative invalid → default
	}
	for _, tc := range cases {
		t.Run("env="+tc.env, func(t *testing.T) {
			t.Setenv("OLLAMA_MAX_CONCURRENT_REQUESTS", tc.env)
			got := resolveOllamaMaxConcurrent()
			if got != tc.want {
				t.Errorf("resolveOllamaMaxConcurrent() = %d, want %d", got, tc.want)
			}
		})
	}
}
```

`sync` is already imported from Task 8's metrics test; if running Task 5a standalone before Task 8, add `"sync"` and `"time"` to the test file imports as needed.

- [ ] **Step 3: Run tests to verify they fail**

```bash
go test ./internal/roundtable/ -run 'TestOllamaRun_Serializes|TestOllamaRun_Gate|TestOllamaBackend_ResolveMaxConcurrent' -v
```

Expected: FAIL — `undefined: resolveOllamaMaxConcurrent`; serialization test passes only by accident if the gate isn't there (no enforcement).

- [ ] **Step 4: Implement**

Add imports to `internal/roundtable/ollama.go`:

```go
import (
	// ... existing ...
	"log/slog"
	"strconv"

	"golang.org/x/sync/semaphore"
)
```

Add constants near `ollamaMaxResponseBytes`:

```go
// ollamaDefaultMaxConcurrent matches the Ollama Cloud Pro tier ceiling.
// Free-tier users must set OLLAMA_MAX_CONCURRENT_REQUESTS=1; Max-tier
// users set =10. Documented in INSTALL.md.
const ollamaDefaultMaxConcurrent int64 = 3

// ollamaGateSlowLogThreshold is the wait time above which we emit a debug
// log on Acquire. Without this signal the gate becomes invisible latency
// under Decision C's no-retry stance.
const ollamaGateSlowLogThreshold = 100 * time.Millisecond
```

Add the `sem` field to `OllamaBackend`:

```go
type OllamaBackend struct {
	httpClient   *http.Client
	defaultModel string
	observe      ObserveFunc
	sem          *semaphore.Weighted // per-process bulkhead — see Task 5a
}
```

Add the env resolver:

```go
// resolveOllamaMaxConcurrent reads OLLAMA_MAX_CONCURRENT_REQUESTS, falling
// back to ollamaDefaultMaxConcurrent on unset/invalid values. Construction-
// time only; the semaphore's capacity is immutable after NewWeighted.
func resolveOllamaMaxConcurrent() int64 {
	v := os.Getenv("OLLAMA_MAX_CONCURRENT_REQUESTS")
	if v == "" {
		return ollamaDefaultMaxConcurrent
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		slog.Warn("OLLAMA_MAX_CONCURRENT_REQUESTS invalid; using default",
			"value", v, "default", ollamaDefaultMaxConcurrent)
		return ollamaDefaultMaxConcurrent
	}
	return n
}
```

Update the constructor:

```go
func NewOllamaBackend(defaultModel string, observe ObserveFunc) *OllamaBackend {
	if observe == nil {
		observe = func(string, string, int64) {}
	}
	return &OllamaBackend{
		defaultModel: defaultModel,
		observe:      observe,
		sem:          semaphore.NewWeighted(resolveOllamaMaxConcurrent()),
		httpClient: &http.Client{
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
```

In `Run()`, add the Acquire/Release block **immediately before the existing `resp, err := o.httpClient.Do(httpReq)` call**. The surrounding code is unchanged; only the gate inserts here:

```go
	// Bulkhead: block until a slot is available or the ctx fires. Placement
	// is after prompt assembly and request construction so we don't hold a
	// slot during local CPU work. See Task 5a.
	acquireStart := time.Now()
	if err := o.sem.Acquire(ctx, 1); err != nil {
		waited := time.Since(acquireStart).Milliseconds()
		// Deadline vs cancel: callers and dashboards treat these differently.
		// Deadline → "your hivemind took too long" (timeout). Cancel → "the
		// caller walked away" (error). Don't collapse them.
		if errors.Is(ctx.Err(), context.DeadlineExceeded) ||
			errors.Is(err, context.DeadlineExceeded) {
			result = BuildResult(
				RawRunOutput{
					TimedOut:  true,
					ElapsedMs: waited,
					Stderr:    fmt.Sprintf("deadline exceeded waiting for ollama concurrency slot after %dms (OLLAMA_MAX_CONCURRENT_REQUESTS gates in-flight calls)", waited),
				},
				ParsedOutput{},
				model,
			)
			return result, nil
		}
		result = &Result{
			Model:     model,
			Status:    "error",
			Stderr:    "ollama gate acquire failed: " + err.Error(),
			ElapsedMs: waited,
		}
		return result, nil
	}
	defer o.sem.Release(1)
	if waited := time.Since(acquireStart); waited > ollamaGateSlowLogThreshold {
		slog.Debug("ollama concurrency gate wait", "wait_ms", waited.Milliseconds())
	}

	// Now the existing Do() call:
	httpStart := time.Now()
	resp, err := o.httpClient.Do(httpReq)
	// ... rest of Run unchanged ...
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/roundtable/ -run 'TestOllamaRun_Serializes|TestOllamaRun_Gate|TestOllamaBackend_ResolveMaxConcurrent' -v
```

Expected: PASS on all four test names (six subtests in the table test).

- [ ] **Step 6: Run the full package suite as a regression guard**

```bash
go test ./internal/roundtable/
```

Expected: PASS. Earlier Task 5 tests that didn't set `OLLAMA_MAX_CONCURRENT_REQUESTS` will hit the default-3 semaphore; single-threaded tests never contend, so they're unaffected.

- [ ] **Step 7: Append §4.6 "Concurrency gate" to the architectural plan**

Open `docs/plans/2026-04-20-ollama-cloud-provider.md` and append a new subsection under §4:

```markdown
### 4.6 Client-side concurrency gate (`x/sync/semaphore`)

**Problem.** Ollama Cloud enforces 1/3/10 concurrent upstream calls (free/pro/max). A `hivemind` fanout with 4+ ollama agents on Pro will silently rate-limit one call per dispatch.

**Design.** `OllamaBackend` holds a `*semaphore.Weighted` sized from `OLLAMA_MAX_CONCURRENT_REQUESTS` (default 3). `Run()` calls `Acquire(ctx, 1)` immediately before `httpClient.Do` and `Release(1)` via `defer`. Calls above the cap block until a slot frees.

**Softening Decision C.** The gate partially softens Decision C's "surface rate limits; no retry" — burst 429s become added latency instead. This is intentional for hivemind UX: the caller wants all 4 agents to return useful responses, not 3 ok + 1 `rate_limited`. A debug log fires when `Acquire` waits >100ms so gate latency stays observable.

**ctx error handling.** If the dispatcher's ctx expires while we're in the gate, the user-facing failure is "your hivemind timed out" (`status: "timeout"`) not "Ollama rate-limited us". Cancellation is classified separately (`status: "error"`) because "the caller walked away" and "the system is overloaded" are different failure modes with different remediations.

**Scope.** Per-process only. Two `roundtable-http-mcp` instances sharing an API key will collectively exceed the cap; single-instance dogfood is unaffected. Distributed limiting is out of scope.

**Capacity is construction-time.** `OLLAMA_MAX_CONCURRENT_REQUESTS` is read once in `NewOllamaBackend`. This is an analogue of `defaultModel` (also startup-only) and does NOT violate Decision F, which is scoped to `API_KEY`/`BASE_URL` rotation. Capacity changes only when you change tier, which warrants a restart.
```

- [ ] **Step 8: Commit**

```bash
git add go.mod go.sum internal/roundtable/ollama.go internal/roundtable/ollama_test.go docs/plans/2026-04-20-ollama-cloud-provider.md
git commit -m "$(cat <<'EOF'
feat(ollama): add per-process concurrency gate via x/sync/semaphore

Ollama Cloud's 1/3/10 tier cap (free/pro/max) rate-limits the 4th
concurrent call in a Pro-tier hivemind fanout. OllamaBackend now
holds a bulkhead semaphore sized from OLLAMA_MAX_CONCURRENT_REQUESTS
(default 3). Run() acquires before the Do() call and releases on
return, so burst 429s become local queueing instead of surfaced
rate-limit results.

Partially softens Decision C (surface rate limits; no retry): burst
429s become latency. Intentional for hivemind UX; debug-logged when
wait exceeds 100ms so the bulkhead doesn't become invisible latency.

Gate ctx errors: DeadlineExceeded → status=timeout (via BuildResult
for consistency with subprocess backends); Canceled → status=error
so dashboards can distinguish "we're overloaded" from "caller left".

Per-process only. Multi-instance deployments sharing an API key can
collectively exceed the cap; that's documented as out-of-scope.

§4.6 added to architectural plan for the rationale.
EOF
)"
```

---

## Task 6: Register `"ollama"` in `validCLIs` + `ParseAgents` test

`ParseAgents` (`internal/roundtable/run.go`) rejects any CLI name not in `validCLIs`. Add `"ollama"` and test.

**Files:**
- Modify: `internal/roundtable/run.go:63`
- Modify: `internal/roundtable/run_test.go` (append a test)

- [ ] **Step 1: Write the failing test**

Append to `internal/roundtable/run_test.go`:

```go
func TestParseAgents_AcceptsOllama(t *testing.T) {
	specs, err := ParseAgents(`[{"cli":"ollama","name":"kimi","model":"kimi-k2.6:cloud"}]`)
	if err != nil {
		t.Fatalf("ParseAgents: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1", len(specs))
	}
	if specs[0].CLI != "ollama" {
		t.Errorf("cli = %q, want ollama", specs[0].CLI)
	}
	if specs[0].Model != "kimi-k2.6:cloud" {
		t.Errorf("model = %q, want kimi-k2.6:cloud", specs[0].Model)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
go test ./internal/roundtable/ -run 'TestParseAgents_AcceptsOllama' -v
```

Expected: FAIL (ParseAgents rejects ollama with an `invalid cli` error).

- [ ] **Step 3: Register `"ollama"` in `validCLIs`**

Edit `internal/roundtable/run.go` at the line defining `validCLIs` (currently line 63):

```go
var validCLIs = map[string]bool{"gemini": true, "codex": true, "claude": true, "ollama": true}
```

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
go test ./internal/roundtable/ -run 'TestParseAgents_AcceptsOllama' -v
```

Expected: PASS.

- [ ] **Step 5: Run the full package suite**

Run:
```bash
go test ./internal/roundtable/
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/roundtable/run.go internal/roundtable/run_test.go
git commit -m "$(cat <<'EOF'
feat(ollama): accept "ollama" as a valid agent CLI name

Extends validCLIs so ParseAgents accepts
[{"cli":"ollama","model":"..."}] from the agents JSON.
EOF
)"
```

---

## Task 7: Wire `OllamaBackend` into `buildBackends()`

Register the backend in the dispatch factory. Only register when `OLLAMA_API_KEY` is set, so the backend is simply absent (not a broken entry) on systems without the key.

**Files:**
- Modify: `cmd/roundtable-http-mcp/main.go:151-166`

- [ ] **Step 1: Inspect the current `buildBackends`**

Read `cmd/roundtable-http-mcp/main.go:151-166`. You'll see:

```go
func buildBackends(logger *slog.Logger) map[string]roundtable.Backend {
	var codexBackend roundtable.Backend
	codexPath := roundtable.ResolveExecutable("codex")
	if codexPath != "" {
		codexBackend = roundtable.NewCodexBackend(codexPath, "")
		logger.Info("codex backend configured (lazy start)", "path", codexPath)
	} else {
		logger.Warn("codex binary not found, using CodexFallback")
		codexBackend = roundtable.NewCodexFallbackBackend("", "")
	}
	return map[string]roundtable.Backend{
		"gemini": roundtable.NewGeminiBackend(""),
		"codex":  codexBackend,
		"claude": roundtable.NewClaudeBackend(""),
	}
}
```

- [ ] **Step 2: Verify `os` is already imported**

At the top of `cmd/roundtable-http-mcp/main.go`, confirm `"os"` is in the import block. If not, add it. (It is already imported.)

- [ ] **Step 3: Replace `buildBackends` with the ollama-aware version**

Replace the function body (lines 151-166) with:

```go
// buildBackends constructs the model backends. Shared between the
// stdio and HTTP entry points. An ollama backend is registered only
// when OLLAMA_API_KEY is set; absence means the "ollama" key is simply
// not in the map, and the dispatcher emits a not_found result if an
// agent requests it.
func buildBackends(logger *slog.Logger) map[string]roundtable.Backend {
	var codexBackend roundtable.Backend
	codexPath := roundtable.ResolveExecutable("codex")
	if codexPath != "" {
		codexBackend = roundtable.NewCodexBackend(codexPath, "")
		logger.Info("codex backend configured (lazy start)", "path", codexPath)
	} else {
		logger.Warn("codex binary not found, using CodexFallback")
		codexBackend = roundtable.NewCodexFallbackBackend("", "")
	}

	backends := map[string]roundtable.Backend{
		"gemini": roundtable.NewGeminiBackend(""),
		"codex":  codexBackend,
		"claude": roundtable.NewClaudeBackend(""),
	}

	if os.Getenv("OLLAMA_API_KEY") != "" {
		defaultModel := os.Getenv("OLLAMA_DEFAULT_MODEL")
		// observe=nil here; Task 8 replaces this with metrics.ObserveBackend
		// once the metrics object exists at this scope. Constructor
		// normalizes nil to a no-op so the backend works correctly in the
		// interim.
		backends["ollama"] = roundtable.NewOllamaBackend(defaultModel, nil)
		baseURL := os.Getenv("OLLAMA_BASE_URL")
		if baseURL == "" {
			baseURL = "https://ollama.com"
		}
		logger.Info("ollama backend configured",
			"default_model", defaultModel,
			"base_url", baseURL)
	} else {
		logger.Debug("ollama backend not configured (OLLAMA_API_KEY unset)")
	}

	return backends
}
```

- [ ] **Step 4: Fix the Name→CLI bug in `NotFoundResult` / `ProbeFailedResult` calls**

While in this area: `internal/roundtable/run.go:348` and `:354` pass `cfg.spec.Name` as the backend name to `NotFoundResult` / `ProbeFailedResult`. For an agent like `{"cli":"ollama","name":"kimi"}` with `OLLAMA_API_KEY` unset, the resulting Stderr becomes `kimi CLI not found in PATH` — misleading because "kimi" is the agent's display name, not the CLI/backend identifier. This is a pre-existing bug, but Task 7 makes the absent-backend path materially more common (every server without `OLLAMA_API_KEY` set will hit it), so it's worth fixing now rather than letting users discover it through a confusing error.

Edit `internal/roundtable/run.go:348` and `:354`:

```go
if cfg.backend == nil {
	results[cfg.spec.Name] = NotFoundResult(cfg.spec.CLI, cfg.request.Model)
} else {
	reason := "unknown"
	if probeResults[i].err != nil {
		reason = probeResults[i].err.Error()
	}
	results[cfg.spec.Name] = ProbeFailedResult(cfg.spec.CLI, cfg.request.Model, reason, nil)
}
```

(Map key stays `cfg.spec.Name` — that's the per-agent display key the dispatcher uses to address results. Only the `backendName` argument changes from Name to CLI.)

Add a regression test in `internal/roundtable/run_test.go` (or wherever the dispatcher tests live) that constructs an agent with distinct Name and CLI, omits the backend from the map, and asserts the Stderr mentions the CLI not the Name. Mirror an existing not_found test if one exists.

- [ ] **Step 5: Build the binary**

Run:
```bash
go build ./cmd/roundtable-http-mcp/
```

Expected: build succeeds, produces `roundtable-http-mcp` in the repo root.

- [ ] **Step 6: Smoke-test the factory with `OLLAMA_API_KEY` unset**

Run:
```bash
OLLAMA_API_KEY= go test ./cmd/roundtable-http-mcp/... 2>&1 | tail -20
```

Expected: existing tests in `cmd/roundtable-http-mcp/` still pass (or no tests if the package has none; an exit 0 is fine).

- [ ] **Step 7: Vet check**

Run:
```bash
go vet ./...
```

Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add cmd/roundtable-http-mcp/main.go internal/roundtable/run.go internal/roundtable/run_test.go
git commit -m "$(cat <<'EOF'
feat(ollama): register OllamaBackend in buildBackends

The backend is registered only when OLLAMA_API_KEY is set;
when unset, the "ollama" key is absent from the backends map and
agents that request it get the standard not_found result from the
dispatcher.

OLLAMA_DEFAULT_MODEL is captured at registration time for logging
and as the final fallback when no per-agent model is specified.

Also fixes a pre-existing bug in run.go where NotFoundResult and
ProbeFailedResult received the agent's display Name instead of its
CLI identifier — yielding "kimi CLI not found in PATH" for an agent
named "kimi" running on the (missing) "ollama" backend.
EOF
)"
```

---

## Task 8: Per-backend metrics in `internal/httpmcp/metrics.go`

Extend the existing JSON-based metrics with four Prometheus-convention-named counters per backend. No new dependency; the naming is compatible with a future `client_golang` migration.

**Files:**
- Modify: `internal/httpmcp/metrics.go` (add `ObserveBackend` + per-backend counter maps)
- Create: `internal/httpmcp/metrics_test.go`
- Modify: `internal/roundtable/ollama.go` (call `o.observe` from `Run()`)
- Modify: `internal/roundtable/ollama_test.go` (add `TestOllamaRun_EmitsMetrics` with constructor injection)
- Modify: `cmd/roundtable-http-mcp/main.go` (`buildBackends` gains `*httpmcp.Metrics` parameter; passes `metrics.ObserveBackend` to `NewOllamaBackend`)

`internal/httpmcp/metrics.go` lives in a package that `internal/roundtable` doesn't import (and can't, since `httpmcp` already imports `roundtable`). Rather than reach for a package-level mutable hook (a global `MetricsSink` var), we use **constructor injection**: `OllamaBackend.observe` is set at construction time from `cmd/roundtable-http-mcp/main.go`, where the `*httpmcp.Metrics` instance lives. Task 2 already established `ObserveFunc` as a roundtable-package type and made the constructor accept it.

**Wiring shape (decided after roundtable review).** `buildBackends` takes a `roundtable.ObserveFunc`, NOT a `*httpmcp.Metrics`. Reasons:

- `buildBackends` is called from BOTH `runStdio` (`main.go:72`) and `runHTTP` (`main.go:98`); stdio has no `*httpmcp.Metrics` and shouldn't be forced to fabricate one. Stdio passes `nil`; HTTP passes `metrics.ObserveBackend` (a method value).
- Avoids leaking the `httpmcp` package into the backend factory's signature — the seam between providers and metrics belongs in `main`, not in the factory.

**Refactor scope, accurately stated.** `httpmcp.NewApp` (`server.go:96`) currently constructs its own `metrics := &Metrics{}` internally. To have `main` own the metrics instance — required so the same `*Metrics` is shared between `NewApp` and `metrics.ObserveBackend` passed to `buildBackends` — `NewApp`'s signature must change. This breaks `internal/httpmcp/server_test.go`'s `newTestApp` and any other call sites; updating them is part of this task.

**Existing test breakage to fix in this task.** `internal/httpmcp/server_test.go:283` (`TestMetricsEndpoint`) does `var m map[string]int64; json.Unmarshal(body, &m)`. Once this task adds nested counter maps (`roundtable_backend_requests_total: {"ollama/ok": 2}`), that unmarshal fails with `cannot unmarshal object into Go value of type int64`. The earlier draft of this plan claimed the existing test would still pass — that was wrong. Step 9 below fixes it.

**Steps:**
1. Add the `(*httpmcp.Metrics).ObserveBackend` method (Steps 1–5).
2. Refactor `httpmcp.NewApp` to accept an injected `*Metrics`; update `newTestApp` and all other `NewApp` callers (Step 6).
3. Wire `o.observe` into `Run()` via the existing constructor parameter (Step 7).
4. Change `buildBackends` to take an `ObserveFunc`; update both `runStdio` (passes `nil`) and `runHTTP` (passes `metrics.ObserveBackend`) (Step 8).
5. Update `TestMetricsEndpoint` for the new JSON shape (Step 9).
6. Add the metrics-emission unit test on `OllamaBackend` (Step 10).

- [ ] **Step 1: Write the failing metrics test**

Create `internal/httpmcp/metrics_test.go`:

```go
package httpmcp

import (
	"encoding/json"
	"testing"
)

func TestMetrics_BackendCounter(t *testing.T) {
	m := &Metrics{}
	m.ObserveBackend("ollama", "ok", 120)
	m.ObserveBackend("ollama", "ok", 240)
	m.ObserveBackend("ollama", "rate_limited", 50)
	m.ObserveBackend("gemini", "ok", 300)

	data := m.JSON()
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// roundtable_backend_requests_total{backend="ollama",status="ok"} == 2
	bt, ok := got["roundtable_backend_requests_total"].(map[string]any)
	if !ok {
		t.Fatalf("roundtable_backend_requests_total missing or wrong type: %v", got)
	}
	ollamaOK, ok := bt["ollama/ok"].(float64)
	if !ok || ollamaOK != 2 {
		t.Errorf("ollama/ok = %v, want 2", bt["ollama/ok"])
	}
	ollamaRL, ok := bt["ollama/rate_limited"].(float64)
	if !ok || ollamaRL != 1 {
		t.Errorf("ollama/rate_limited = %v, want 1", bt["ollama/rate_limited"])
	}
	geminiOK, ok := bt["gemini/ok"].(float64)
	if !ok || geminiOK != 1 {
		t.Errorf("gemini/ok = %v, want 1", bt["gemini/ok"])
	}

	// roundtable_backend_request_duration_ms_sum{backend="ollama"} == 410
	ds, ok := got["roundtable_backend_request_duration_ms_sum"].(map[string]any)
	if !ok {
		t.Fatalf("duration_sum missing: %v", got)
	}
	if ds["ollama"].(float64) != 410 {
		t.Errorf("ollama sum = %v, want 410", ds["ollama"])
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
go test ./internal/httpmcp/ -run 'TestMetrics_BackendCounter' -v
```

Expected: FAIL — `undefined: (*Metrics).ObserveBackend`.

- [ ] **Step 3: Extend `Metrics` with per-backend counters**

Replace the contents of `internal/httpmcp/metrics.go` with:

```go
package httpmcp

import (
	"encoding/json"
	"sync"
	"sync/atomic"
)

// Metrics holds server-wide counters. Field names on the JSON output
// follow Prometheus conventions (roundtable_backend_*) so a future
// migration to client_golang needs only a transport swap, not a
// rename.
type Metrics struct {
	TotalRequests  atomic.Int64
	DispatchErrors atomic.Int64

	mu sync.Mutex
	// backendRequests counts per (backend, status). Key format: "backend/status".
	backendRequests map[string]*atomic.Int64
	// backendDurationSum accumulates elapsed_ms per backend.
	backendDurationSum map[string]*atomic.Int64
	// backendDurationCount counts samples per backend (for computing mean).
	backendDurationCount map[string]*atomic.Int64
}

// ObserveBackend records a single backend call's outcome.
// `status` is the Result.Status string ("ok", "error", "rate_limited",
// "timeout", etc.). `elapsedMs` is the wall-clock duration.
func (m *Metrics) ObserveBackend(backend, status string, elapsedMs int64) {
	key := backend + "/" + status
	m.mu.Lock()
	if m.backendRequests == nil {
		m.backendRequests = map[string]*atomic.Int64{}
		m.backendDurationSum = map[string]*atomic.Int64{}
		m.backendDurationCount = map[string]*atomic.Int64{}
	}
	c, ok := m.backendRequests[key]
	if !ok {
		c = &atomic.Int64{}
		m.backendRequests[key] = c
	}
	ds, ok := m.backendDurationSum[backend]
	if !ok {
		ds = &atomic.Int64{}
		m.backendDurationSum[backend] = ds
	}
	dc, ok := m.backendDurationCount[backend]
	if !ok {
		dc = &atomic.Int64{}
		m.backendDurationCount[backend] = dc
	}
	m.mu.Unlock()
	c.Add(1)
	ds.Add(elapsedMs)
	dc.Add(1)
}

type metricsSnapshot struct {
	TotalRequests  int64 `json:"total_requests"`
	DispatchErrors int64 `json:"dispatch_errors"`

	BackendRequests      map[string]int64 `json:"roundtable_backend_requests_total"`
	BackendDurationSum   map[string]int64 `json:"roundtable_backend_request_duration_ms_sum"`
	BackendDurationCount map[string]int64 `json:"roundtable_backend_request_duration_ms_count"`
}

func (m *Metrics) Snapshot() metricsSnapshot {
	snap := metricsSnapshot{
		TotalRequests:        m.TotalRequests.Load(),
		DispatchErrors:       m.DispatchErrors.Load(),
		BackendRequests:      map[string]int64{},
		BackendDurationSum:   map[string]int64{},
		BackendDurationCount: map[string]int64{},
	}
	m.mu.Lock()
	for k, v := range m.backendRequests {
		snap.BackendRequests[k] = v.Load()
	}
	for k, v := range m.backendDurationSum {
		snap.BackendDurationSum[k] = v.Load()
	}
	for k, v := range m.backendDurationCount {
		snap.BackendDurationCount[k] = v.Load()
	}
	m.mu.Unlock()
	return snap
}

func (m *Metrics) JSON() []byte {
	data, _ := json.Marshal(m.Snapshot())
	return data
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
go test ./internal/httpmcp/ -run 'TestMetrics_BackendCounter' -v
```

Expected: PASS.

- [ ] **Step 5: Run the full `httpmcp` test suite as a regression guard**

Run:
```bash
go test ./internal/httpmcp/
```

Expected: PASS on all existing tests. (The original test reading `TotalRequests` / `DispatchErrors` still works because those fields are unchanged.)

- [ ] **Step 6: Refactor `httpmcp.NewApp` to accept an injected `*Metrics`**

Today `internal/httpmcp/server.go:96-106` constructs its own `metrics := &Metrics{}` inside `NewApp`. Change it to accept one:

```go
// Before:
// func NewApp(config Config, dispatch DispatchFunc, backends map[string]BackendProbe) *App {
//     ...
//     metrics := &Metrics{}
//     ...
// }

// After:
func NewApp(config Config, dispatch DispatchFunc, backends map[string]BackendProbe, metrics *Metrics) *App {
    // remove the internal `metrics := &Metrics{}` construction
    ...
}
```

Then update every caller:

1. `internal/httpmcp/server_test.go::newTestApp` and any direct `NewApp(` invocation in tests — pass `&Metrics{}` (or a fixture).
2. `cmd/roundtable-http-mcp/main.go::runHTTP` (`main.go` near where `NewApp` is called) — see Step 8 for the wiring; for now just propagate the new parameter.

Build to confirm nothing's missed:

```bash
go build ./...
```

Expected: clean. If `NewApp` is called from elsewhere (`grep -rn 'httpmcp.NewApp\|NewApp(' --include='*.go'`), update those too.

- [ ] **Step 7: Wire metrics into `OllamaBackend.Run()` via the injected `observe`**

`o.observe` is already non-nil after the constructor (Task 2 normalizes nil to a no-op), so `Run()` can call it unconditionally. Add a single deferred call at the top of `Run()` so every exit path is observed.

Replace the `Run()` body with this version (diff vs Task 5a: a top-level `started`/`result` pair plus a single `defer` calling `o.observe`; the API-key check stays removed per Task 5's revision; the gate from Task 5a stays in place; the timeout path still flows through `BuildResult`; the parser call still passes `Retry-After`):

```go
func (o *OllamaBackend) Run(ctx context.Context, req Request) (*Result, error) {
	apiKey := os.Getenv("OLLAMA_API_KEY")
	baseURL := os.Getenv("OLLAMA_BASE_URL")
	if baseURL == "" {
		baseURL = "https://ollama.com"
	}

	model := req.Model
	if model == "" {
		model = o.defaultModel
	}

	// Observe every exit path. observe is non-nil (constructor normalizes).
	started := time.Now()
	var result *Result
	defer func() {
		if result != nil {
			o.observe("ollama", result.Status, time.Since(started).Milliseconds())
		}
	}()

	if model == "" {
		result = ConfigErrorResult("ollama", "",
			"no model resolved: set OLLAMA_DEFAULT_MODEL or AgentSpec.Model")
		return result, nil
	}

	// Prepend inlined file contents (if any). Subprocess backends get file
	// contents via their own tool loop; an HTTP call has no tool loop, so
	// we eagerly read req.Files here. See Task 4 for the helper.
	content := req.Prompt
	if inlined := inlineFileContents(req.Files); inlined != "" {
		content = inlined + content
	}

	bodyBytes, _ := json.Marshal(map[string]any{
		"model":    model,
		"messages": []map[string]string{{"role": "user", "content": content}},
		"stream":   false,
	})

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/api/chat", bytes.NewReader(bodyBytes))
	if err != nil {
		result = ConfigErrorResult("ollama", model, "request build: "+err.Error())
		return result, nil
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	// Bulkhead: block until a concurrency slot is available or ctx fires.
	// See Task 5a for rationale. Placement is after prompt assembly and
	// request construction so we don't hold a slot during local CPU work.
	acquireStart := time.Now()
	if err := o.sem.Acquire(ctx, 1); err != nil {
		waited := time.Since(acquireStart).Milliseconds()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) ||
			errors.Is(err, context.DeadlineExceeded) {
			result = BuildResult(
				RawRunOutput{
					TimedOut:  true,
					ElapsedMs: waited,
					Stderr:    fmt.Sprintf("deadline exceeded waiting for ollama concurrency slot after %dms (OLLAMA_MAX_CONCURRENT_REQUESTS gates in-flight calls)", waited),
				},
				ParsedOutput{},
				model,
			)
			return result, nil
		}
		result = &Result{Model: model, Status: "error", Stderr: "ollama gate acquire failed: " + err.Error(), ElapsedMs: waited}
		return result, nil
	}
	defer o.sem.Release(1)
	if waited := time.Since(acquireStart); waited > ollamaGateSlowLogThreshold {
		slog.Debug("ollama concurrency gate wait", "wait_ms", waited.Milliseconds())
	}

	httpStart := time.Now()
	resp, err := o.httpClient.Do(httpReq)
	elapsed := time.Since(httpStart).Milliseconds()

	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) ||
			errors.Is(err, context.DeadlineExceeded) {
			result = BuildResult(
				RawRunOutput{TimedOut: true, ElapsedMs: elapsed, Stderr: err.Error()},
				ParsedOutput{},
				model,
			)
			return result, nil
		}
		result = &Result{Model: model, Status: "error", Stderr: err.Error(), ElapsedMs: elapsed}
		return result, nil
	}
	defer resp.Body.Close()

	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, ollamaMaxResponseBytes))
	if readErr != nil {
		result = &Result{Model: model, Status: "error", Stderr: "read body: " + readErr.Error(), ElapsedMs: elapsed}
		return result, nil
	}

	parsed := ollamaParseResponse(raw, resp.StatusCode, resp.Header.Get("Retry-After"))
	result = BuildResult(RawRunOutput{Stdout: raw, ElapsedMs: elapsed}, parsed, model)
	return result, nil
}
```

- [ ] **Step 8: Change `buildBackends` to take `roundtable.ObserveFunc`; update both `runStdio` and `runHTTP`**

`buildBackends` is called from BOTH `runStdio` (`main.go:72`) and `runHTTP` (`main.go:98`). Stdio has no `*httpmcp.Metrics` and shouldn't acquire one. The cleanest seam is to have `buildBackends` accept a `roundtable.ObserveFunc` directly: stdio passes `nil`; HTTP passes `metrics.ObserveBackend`.

Change `buildBackends`' signature in `cmd/roundtable-http-mcp/main.go:151`:

```go
func buildBackends(logger *slog.Logger, observe roundtable.ObserveFunc) map[string]roundtable.Backend {
	var codexBackend roundtable.Backend
	codexPath := roundtable.ResolveExecutable("codex")
	if codexPath != "" {
		codexBackend = roundtable.NewCodexBackend(codexPath, "")
		logger.Info("codex backend configured (lazy start)", "path", codexPath)
	} else {
		logger.Warn("codex binary not found, using CodexFallback")
		codexBackend = roundtable.NewCodexFallbackBackend("", "")
	}

	backends := map[string]roundtable.Backend{
		"gemini": roundtable.NewGeminiBackend(""),
		"codex":  codexBackend,
		"claude": roundtable.NewClaudeBackend(""),
	}

	if os.Getenv("OLLAMA_API_KEY") != "" {
		defaultModel := os.Getenv("OLLAMA_DEFAULT_MODEL")
		backends["ollama"] = roundtable.NewOllamaBackend(defaultModel, observe)
		baseURL := os.Getenv("OLLAMA_BASE_URL")
		if baseURL == "" {
			baseURL = "https://ollama.com"
		}
		logger.Info("ollama backend configured",
			"default_model", defaultModel,
			"base_url", baseURL)
	} else {
		logger.Debug("ollama backend not configured (OLLAMA_API_KEY unset)")
	}

	return backends
}
```

Then update both call sites:

```go
// runStdio (main.go:72): no metrics in scope; pass nil.
backends := buildBackends(logger, nil)

// runHTTP (main.go:98): construct metrics here so the same instance
// is shared with NewApp. Pass the method value through.
metrics := &httpmcp.Metrics{}
backends := buildBackends(logger, metrics.ObserveBackend)
// ... later, when calling NewApp from Step 6 ...
app := httpmcp.NewApp(config, dispatch, probes, metrics)
```

Build:
```bash
go build ./...
```

Expected: clean. If `runHTTP` previously didn't have a local `metrics` variable, this is where it acquires one — see Step 6 for the `NewApp` signature change that consumes it.

- [ ] **Step 9: Update `TestMetricsEndpoint` for the new JSON shape**

`internal/httpmcp/server_test.go:283` currently does:

```go
var m map[string]int64
if err := json.Unmarshal(body, &m); err != nil {
	t.Fatalf("parse metrics JSON: %v (body: %s)", err, body)
}
```

That works today because `metricsSnapshot` is two `int64` fields. After Step 3 added nested objects (`roundtable_backend_requests_total: {"ollama/ok": 2}`), this `Unmarshal` fails with `cannot unmarshal object into Go value of type int64`. Update it:

```go
var m map[string]any
if err := json.Unmarshal(body, &m); err != nil {
	t.Fatalf("parse metrics JSON: %v (body: %s)", err, body)
}
if v, _ := m["total_requests"].(float64); v < 1 {
	t.Errorf("total_requests = %v, want >= 1", m["total_requests"])
}
if v, _ := m["dispatch_errors"].(float64); v != 0 {
	t.Errorf("dispatch_errors = %v, want 0", m["dispatch_errors"])
}
```

(JSON numbers unmarshal to `float64` under `map[string]any`; the cast is intentional. The new nested fields are ignored by this test — they're covered by `TestMetrics_BackendCounter` from Step 1.)

Run the full `httpmcp` suite:

```bash
go test ./internal/httpmcp/
```

Expected: PASS, including the updated `TestMetricsEndpoint`.

- [ ] **Step 10: Write the end-to-end metrics smoke test**

Append to `internal/roundtable/ollama_test.go`:

```go
func TestOllamaRun_EmitsMetrics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"model":"kimi-k2.6:cloud","message":{"role":"assistant","content":"hi"},"done_reason":"stop"}`))
	}))
	defer srv.Close()

	type call struct {
		backend, status string
		elapsedMs       int64
	}
	// Capture observe invocations into a local slice. No global state, no
	// save/restore dance, no test-parallelism hazard.
	var (
		mu  sync.Mutex
		got []call
	)
	observe := func(backend, status string, elapsedMs int64) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, call{backend, status, elapsedMs})
	}

	t.Setenv("OLLAMA_BASE_URL", srv.URL)
	t.Setenv("OLLAMA_API_KEY", "sk-test-value")
	b := NewOllamaBackend("kimi-k2.6:cloud", observe)

	if _, err := b.Run(context.Background(), Request{Prompt: "hi", Model: "kimi-k2.6:cloud"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("metrics calls = %d, want 1", len(got))
	}
	if got[0].backend != "ollama" || got[0].status != "ok" {
		t.Errorf("metrics call = %+v, want backend=ollama status=ok", got[0])
	}
	if got[0].elapsedMs < 0 {
		t.Errorf("elapsed_ms = %d, want >= 0", got[0].elapsedMs)
	}
}
```

The `sync` import joins the existing imports in `ollama_test.go`.

- [ ] **Step 11: Run all tests**

Run:
```bash
go test ./... 2>&1 | tail -30
```

Expected: all packages PASS, including `TestMetricsEndpoint` from Step 9 and `TestMetrics_BackendCounter` from Step 1.

- [ ] **Step 12: Commit**

```bash
git add internal/httpmcp/metrics.go internal/httpmcp/metrics_test.go internal/httpmcp/server.go internal/httpmcp/server_test.go internal/roundtable/ollama.go internal/roundtable/ollama_test.go cmd/roundtable-http-mcp/main.go
git commit -m "$(cat <<'EOF'
feat(ollama): per-backend metrics with Prometheus-convention names

Extends httpmcp.Metrics with backend-scoped counters and durations
exposed through /metricsz under roundtable_backend_* names. The
schema is Prometheus-compatible so a future client_golang migration
is a rename-free transport swap.

OllamaBackend receives an ObserveFunc through its constructor;
runHTTP in main.go owns the *httpmcp.Metrics instance and passes
metrics.ObserveBackend (a method value) to buildBackends, which
forwards it into NewOllamaBackend. runStdio passes nil. No package-
level hook — the import-cycle constraint is sidestepped by injecting
a function value, which carries no package dependency.

httpmcp.NewApp's signature gains a *Metrics parameter (was
constructed internally; main now owns it). TestMetricsEndpoint
updated for the new nested-counter JSON shape.
EOF
)"
```

---

## Task 9: Documentation — `INSTALL.md`

Users need to know which env vars to set and how to compose an `agents` JSON that targets Ollama.

**Files:**
- Modify: `INSTALL.md` (append a section)

- [ ] **Step 1: Read the current INSTALL.md**

Run:
```bash
wc -l INSTALL.md
```

Note the length so the append lands at the end.

- [ ] **Step 2: Append the Ollama section**

Append to the end of `INSTALL.md`:

~~~markdown
## Ollama Cloud provider

Roundtable v0.8+ supports Ollama's cloud-hosted `:cloud` models
(kimi-k2.6, qwen3.5, glm-5.1, minimax-m2.7, gpt-oss, etc.) over HTTPS.
Unlike the subprocess backends (claude/codex/gemini), this one has no
CLI binary — requests go directly to Ollama's REST API.

### Environment

| Variable | Required | Default | Purpose |
|-|-|-|-|
| `OLLAMA_API_KEY` | yes | — | Bearer token from https://ollama.com/settings/keys. If unset, the ollama backend is simply not registered. |
| `OLLAMA_BASE_URL` | no | `https://ollama.com` | Override for self-hosted Ollama or for tests. |
| `OLLAMA_DEFAULT_MODEL` | no | — | Fallback model used when an agent spec doesn't set `model`. Recommended: `kimi-k2.6:cloud` or `gpt-oss:120b-cloud`. |
| `OLLAMA_MAX_CONCURRENT_REQUESTS` | no | `3` | Per-process bulkhead on concurrent `/api/chat` calls. Match your Ollama account tier: **Free=`1`**, **Pro=`3`** (default), **Max=`10`**. Calls above the cap block until a slot frees instead of getting a 429 from Ollama's edge. Read once at startup; restart to change. |

### Example: dispatching to one cloud model

```json
{
  "prompt": "Explain context-free grammars with a concrete example.",
  "agents": "[{\"cli\":\"ollama\",\"name\":\"kimi\",\"model\":\"kimi-k2.6:cloud\"}]"
}
```

### Example: `hivemind` with mixed providers

```json
{
  "prompt": "Review this design doc and flag risks.",
  "files": "docs/design.md",
  "agents": "[{\"cli\":\"claude\"},{\"cli\":\"gemini\"},{\"cli\":\"ollama\",\"name\":\"kimi\",\"model\":\"kimi-k2.6:cloud\"},{\"cli\":\"ollama\",\"name\":\"glm\",\"model\":\"glm-5.1:cloud\"}]"
}
```

### Known limitations (Apr 2026)

- **Concurrency cap**: Free tier allows 1 concurrent cloud model call, Pro $20/mo allows 3, Max $100/mo allows 10. Roundtable holds a per-process bulkhead sized by `OLLAMA_MAX_CONCURRENT_REQUESTS` (default 3) so a `hivemind` with more ollama agents than slots queues locally instead of getting silent 429s from Ollama's edge. If multiple `roundtable-http-mcp` processes share an API key, they can still collectively exceed the cap — run a single instance, or set each process's cap to a fraction of the tier total.
- **Output cap**: All `:cloud` models are capped at 16,384 completion tokens. When truncated, `done_reason=length` is surfaced in `metadata`.
- **503 storms**: Ollama Cloud is a preview service; 503s are treated as `rate_limited`. No `Retry-After` is published, and Roundtable does not auto-retry.
- **US-only inference**: not suitable for EU/GDPR-sensitive deployments.
~~~

- [ ] **Step 3: Commit**

```bash
git add INSTALL.md
git commit -m "docs(ollama): document env vars, agents JSON examples, and known limitations"
```

---

## Final verification

- [ ] **Step 1: Full build**

Run:
```bash
go build ./...
```

Expected: clean build.

- [ ] **Step 2: Full test suite**

Run:
```bash
go test ./... 2>&1 | tail -30
```

Expected: all packages PASS.

- [ ] **Step 3: Vet**

Run:
```bash
go vet ./...
```

Expected: clean.

- [ ] **Step 4: Manual smoke test (optional — requires a real API key)**

Only if you have an `OLLAMA_API_KEY`:

```bash
export OLLAMA_API_KEY=sk-your-real-key
./roundtable-http-mcp --port 8080 &
curl -sS -X POST http://localhost:8080/mcp \
  -H 'Content-Type: application/json' \
  -d '{
    "tool":"hivemind",
    "prompt":"Say hi",
    "agents":"[{\"cli\":\"ollama\",\"name\":\"kimi\",\"model\":\"kimi-k2.6:cloud\"}]",
    "timeout":60
  }' | jq .
```

Expected: JSON response with `results.kimi.status == "ok"` and a non-empty `response`. If you see `rate_limited`, that's Ollama's 1/3/10 cap or a 503 storm — retry in a minute.

Check `/metricsz` to confirm the counter incremented:

```bash
curl -sS http://localhost:8080/metricsz | jq .roundtable_backend_requests_total
```

Expected: `{"ollama/ok": 1}` (or `{"ollama/rate_limited": 1}` if you saw a 429/503).

- [ ] **Step 5: Push the feature branch**

```bash
git push origin feature/ollama-cloud-provider
```

Expected: branch updated on remote. Forgejo will print a PR link.

---

## Self-review

**Spec coverage check against `docs/plans/2026-04-20-ollama-cloud-provider.md`:**

| Spec requirement | Task |
|-|-|
| §4.1 / §5.3 / §7: `done_reason=length` reaches caller (Result.Metadata pipeline) | Task 0 (struct + propagation), Task 3 (parser populates it), Task 5 `TestOllamaRun_TruncationSurfaced` (end-to-end) |
| §4.6 / §5.1: concurrency gate to prevent silent 429s on 4+ ollama-agent hivemind (Pro tier) | Task 5a (`x/sync/semaphore.Weighted`, `OLLAMA_MAX_CONCURRENT_REQUESTS`, blocking Acquire with ctx branching) |
| §3 / §9.1: Single `ollama` backend with model via `AgentSpec` | Task 2 (scaffold), Task 6 (validCLIs) |
| §4.1: `http.Client` with explicit Transport timeouts | Task 2 (constructor) |
| §4.1: `io.LimitReader` with 8 MiB cap | Task 5 |
| §4.1: offline `Healthy()` as design invariant | Task 2 (`TestOllamaBackend_Healthy_IsOffline`) |
| §4.1: runtime env read in `Run()` | Task 5 |
| §4.1: `ConfigErrorResult`, not `NotFoundResult` | Task 1 |
| §4.1: native `/api/chat` parser with `done_reason` metadata | Task 3 |
| §4.1: 429/503 → `rate_limited`; 401/403 → `error` | Task 3 |
| §4.1: 429 `Retry-After` surfaced on `Metadata["retry_after"]` | Task 3 (`TestOllamaParse_RateLimited429_WithRetryAfter`), Task 5 (Run passes `resp.Header.Get("Retry-After")`) |
| §4.1: context.DeadlineExceeded → `status="timeout"` (via `BuildResult` for response-text consistency with subprocess backends) | Task 5 |
| §4.1: no prompt/body logging (PII invariant) | Task 5 (comment in code) |
| §4.1: file content inlining with per-file + total caps | Task 4 (helper), Task 5 (wired into Run()) |
| §4.2: `buildBackends` conditional registration | Task 7 |
| §4.3 / §9.2: no `ToolRequest.OllamaModel*` fields | (Not added — plan intentionally omits) |
| §5.8: `/readyz` invariant preserved via offline `Healthy()` | Task 2 |
| §7: Prometheus-convention metric names | Task 8 |
| Docs: INSTALL.md env vars + examples | Task 9 |

**Placeholder scan:** none — all code is concrete.

**Type consistency:**
- `ObserveFunc` signature `func(backend, status string, elapsedMs int64)` is identical in Task 2 (type declaration + constructor), Task 5 (test helper passes nil), Task 7 (`buildBackends` passes `nil` placeholder), Task 8 Step 8 (`buildBackends` rewritten to take `roundtable.ObserveFunc`; runStdio passes nil, runHTTP passes `metrics.ObserveBackend`), Task 8 Step 10 (test injects a closure). No package-level mutable hook exists.
- `ConfigErrorResult(backendName, model, reason string)` signature is consistent in Task 1 (impl + tests) and Task 5 (all call sites).
- `ollamaParseResponse(body []byte, statusCode int, retryAfter string) ParsedOutput` signature: tests in Task 3 pass the retry-after explicitly; Task 5 / Task 8 Step 7 pass `resp.Header.Get("Retry-After")`.
- `inlineFileContents(paths []string) string` signature in Task 4 matches the call site wired into `Run()` in Task 5.
- `httpmcp.NewApp` signature gains a `*Metrics` parameter in Task 8 Step 6; both `runHTTP` and `newTestApp` callers updated in the same step. `buildBackends` signature changes in Task 8 Step 8; both `runStdio` and `runHTTP` updated.

**Roundtable-review additions (2026-04-21, third-pass) — concurrency gate:**
- Task 5a inserted between Task 5 and Task 6 after a roundtable review of buy-vs-build for the Ollama Cloud concurrency cap.
- Library: `golang.org/x/sync/semaphore` (2-1 vote over a zero-dep `chan struct{}` pattern — ctx-aware `Acquire` is atomic, Go-team-maintained).
- Behavior: blocking gate (2-1 over fast-fail — fast-fail is equivalent to doing nothing from the caller's POV; blocking is the only version that actually prevents the 4-of-4 rate-limit).
- ctx error branching: `DeadlineExceeded` → `timeout` (via `BuildResult` for parity with subprocess backends); `Canceled` → `error` so dashboards distinguish "overloaded" from "caller left".
- Default cap: 3 (matches Pro tier ceiling; Free-tier users must set 1).
- Env name: `OLLAMA_MAX_CONCURRENT_REQUESTS` (explicit; stays in `OLLAMA_*` family).
- Construction-time only — documented as analogous to `defaultModel`, NOT a violation of Decision F (which is scoped to `API_KEY`/`BASE_URL` rotation).
- Observability: debug log when `Acquire` waits >100ms so gate latency stays visible.
- Per-process scope documented; multi-instance deployments need distributed limiting (out of scope).

**Roundtable-review fixes incorporated (2026-04-20, second-pass):**
- Task 8 wiring: `buildBackends(logger, roundtable.ObserveFunc)`, NOT `(logger, *httpmcp.Metrics)`. Stdio doesn't acquire an HTTP-only dependency. (3 reviewers concurred.)
- Task 8 Step 6: explicit `httpmcp.NewApp` signature change + `newTestApp` updates (was glossed over in first draft).
- Task 8 Step 9: `TestMetricsEndpoint` updated for nested-counter JSON shape (was claimed unchanged; codex caught the regression).
- Task 5: `TestOllamaRun_DefaultBaseURL` no longer races against real `ollama.com`; uses `httptest` server and a separate canceled-ctx test for the unset-env path. (claude+codex concurred.)
- Task 5: `apiKey == ""` check removed from `Run()` — dead code given Task 2's `Healthy()` and Task 7's conditional registration. (gemini caught.)
- Task 5: timeout path routes through `BuildResult` for response-text consistency. (claude.)
- Task 3: parser accepts `retryAfter string` so 429s can surface `Retry-After` on metadata. (codex caught — architectural §4.1 requirement.)
- Task 7: pre-existing bug in `run.go:348/354` — `NotFoundResult`/`ProbeFailedResult` get `cfg.spec.Name` instead of `cfg.spec.CLI`, yielding misleading errors like `kimi CLI not found in PATH`. Fixed while in the area since Task 7 makes the absent-backend path more common. (codex caught.)
- pal-extraction Tier 1 explicitly **descoped** from this PR (intro section). Document remains as follow-up reference. (codex+gemini flagged the inconsistency.)
