package roundtable

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/semaphore"
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
	sem          *semaphore.Weighted // per-process bulkhead — see §4.6
}

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

// ollamaMaxResponseBytes caps response bodies to protect against a
// misconfigured upstream streaming unbounded garbage. 8 MiB is well over
// the 16,384-token completion cap with headroom for JSON framing.
const ollamaMaxResponseBytes = 8 * 1024 * 1024

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

// ollamaDefaultMaxConcurrent matches the Ollama Cloud Pro tier ceiling.
// Free-tier users must set OLLAMA_MAX_CONCURRENT_REQUESTS=1; Max-tier
// users set =10. Documented in INSTALL.md.
const ollamaDefaultMaxConcurrent int64 = 3

// ollamaGateSlowLogThreshold is the wait time above which we emit a debug
// log on Acquire. Without this signal the gate becomes invisible latency
// under Decision C's no-retry stance.
const ollamaGateSlowLogThreshold = 100 * time.Millisecond

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
		sem:          semaphore.NewWeighted(resolveOllamaMaxConcurrent()),
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

// Run dispatches a single prompt to Ollama Cloud's /api/chat endpoint.
// See docs/plans/2026-04-20-ollama-cloud-provider.md §4.1 for rationale.
//
// Env is read per-call (see Decision F). File contents in req.Files are
// eagerly inlined because this HTTP path has no tool-calling loop. The
// context deadline is routed through BuildResult so the timeout response
// text matches the subprocess backends' formatting.
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

	// Observe every exit path. observe is non-nil after the constructor
	// (nil normalized to a no-op), so the call is unconditional. Using a
	// named return-bound variable is tempting but awkward given we already
	// have multi-return paths; capture the last *Result produced before
	// each return via a closure-scoped variable.
	runStart := time.Now()
	var result *Result
	defer func() {
		if result != nil {
			o.observe("ollama", result.Status, time.Since(runStart).Milliseconds())
		}
	}()

	// NOTE: we don't re-validate apiKey here. Healthy() already failed the
	// dispatcher's probe if unset, and buildBackends only registers this
	// backend when the key is present at startup. A defensive check here
	// would be dead code given those invariants. Model is per-Request so
	// that check stays.
	if model == "" {
		result = ConfigErrorResult("ollama", "",
			"no model resolved: set OLLAMA_DEFAULT_MODEL or AgentSpec.Model")
		return result, nil
	}

	// Prepend inlined file contents (if any). Subprocess backends get file
	// contents via their own tool loop; an HTTP call has no tool loop, so
	// we eagerly read req.Files here. See inlineFileContents for caps.
	content := req.Prompt
	if inlined := inlineFileContents(req.Files); inlined != "" {
		content = inlined + content
	}

	// Use Encoder with HTML escaping disabled so <file path="...">
	// blocks travel the wire verbatim instead of </>-encoded.
	// Smaller bytes, and easier to eyeball in telemetry dumps (if we ever
	// enable them — today we don't log the body per the PII invariant).
	var bodyBuf bytes.Buffer
	enc := json.NewEncoder(&bodyBuf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(map[string]any{
		"model":    model,
		"messages": []map[string]string{{"role": "user", "content": content}},
		"stream":   false,
	})
	bodyBytes := bodyBuf.Bytes()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/api/chat", bytes.NewReader(bodyBytes))
	if err != nil {
		result = ConfigErrorResult("ollama", model, "request build: "+err.Error())
		return result, nil
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	// Bulkhead: block until a concurrency slot is available or the ctx
	// fires. Placement is after prompt assembly and request construction
	// so we don't hold a slot during local CPU work. Deadline vs cancel:
	// callers and dashboards treat these differently. Deadline → "your
	// hivemind took too long" (timeout). Cancel → "the caller walked away"
	// (error). Don't collapse them. See §4.6.
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

	// NOTE: do NOT log bodyBytes or the response body at any level — they
	// contain user prompts and model output (PII/secret surface). Log
	// status code and elapsed time only.
	start := time.Now()
	resp, err := o.httpClient.Do(httpReq)
	elapsed := time.Since(start).Milliseconds()

	if err != nil {
		// Route timeout through BuildResult so the response-text formatting
		// matches subprocess backends. Other transport errors stay direct.
		if errors.Is(ctx.Err(), context.DeadlineExceeded) ||
			errors.Is(err, context.DeadlineExceeded) {
			result = BuildResult(
				RawRunOutput{TimedOut: true, ElapsedMs: elapsed, Stderr: err.Error()},
				ParsedOutput{},
				model,
			)
			return result, nil
		}
		result = &Result{
			Model:     model,
			Status:    "error",
			Stderr:    err.Error(),
			ElapsedMs: elapsed,
		}
		return result, nil
	}
	defer resp.Body.Close()

	// Cap response under LimitReader to protect against runaway upstream.
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, ollamaMaxResponseBytes))
	if readErr != nil {
		result = &Result{
			Model:     model,
			Status:    "error",
			Stderr:    "read body: " + readErr.Error(),
			ElapsedMs: elapsed,
		}
		return result, nil
	}

	parsed := ollamaParseResponse(raw, resp.StatusCode, resp.Header.Get("Retry-After"))
	result = BuildResult(
		RawRunOutput{Stdout: raw, ElapsedMs: elapsed},
		parsed,
		model,
	)
	return result, nil
}

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
