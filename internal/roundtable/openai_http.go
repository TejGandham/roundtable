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
	"strings"
	"time"

	"golang.org/x/sync/semaphore"
)

// defaultMaxResponseBytes caps response bodies to protect against a
// misbehaving upstream streaming unbounded garbage. 8 MiB is well over any
// reasonable completion size with headroom for JSON framing.
// MEMORY-SAFETY INVARIANT — required by §5.2 of the design doc.
const defaultMaxResponseBytes = 8 * 1024 * 1024

// OpenAIHTTPBackend implements Backend for any provider speaking the
// OpenAI /v1/chat/completions contract.
//
// Design invariants:
//   - Healthy() is offline: it only checks whether os.Getenv(apiKeyEnv) is
//     non-empty. The dispatcher runs Healthy concurrently per-agent; a
//     network probe would burn the provider's concurrency quota before
//     Run() even starts.
//   - API key is read per-Run via os.Getenv(apiKeyEnv) so rotation
//     doesn't require a restart.
//   - httpClient is safe for concurrent use.
//   - observe is never nil after NewOpenAIHTTPBackend (nil normalized to no-op).
type OpenAIHTTPBackend struct {
	id           string
	baseURL      string
	apiKeyEnv    string
	defaultModel string
	httpClient   *http.Client
	observe      ObserveFunc
	sem          *semaphore.Weighted
	gateSlowLog  time.Duration
}

// NewOpenAIHTTPBackend constructs a backend from one registered provider.
// observe may be nil (will be normalized to a no-op).
func NewOpenAIHTTPBackend(cfg ProviderConfig, observe ObserveFunc) *OpenAIHTTPBackend {
	if observe == nil {
		observe = func(string, string, string, int64) {}
	}
	return &OpenAIHTTPBackend{
		id:           cfg.ID,
		baseURL:      cfg.BaseURL,
		apiKeyEnv:    cfg.APIKeyEnv,
		defaultModel: cfg.DefaultModel,
		observe:      observe,
		sem:          semaphore.NewWeighted(int64(cfg.MaxConcurrent)),
		gateSlowLog:  cfg.GateSlowLogThreshold,
		httpClient: &http.Client{
			// No Client.Timeout — we rely on the dispatcher's ctx deadline.
			// But Transport needs explicit timeouts because context
			// cancellation only reaches net/http AFTER the request is in
			// flight; a stalled TLS handshake can otherwise hang.
			Transport: newHTTPTransport(cfg.ResponseHeaderTimeout, cfg.MaxConcurrent),
		},
	}
}

// newHTTPTransport scales MaxIdleConnsPerHost with MaxConcurrent so a
// provider configured for, say, 10 concurrent calls doesn't churn TCP/TLS
// on every other request under burst traffic. Minimum floor of 4 keeps
// behavior unchanged for small configs.
func newHTTPTransport(responseHeaderTimeout time.Duration, maxConcurrent int) *http.Transport {
	idlePool := maxConcurrent
	if idlePool < 4 {
		idlePool = 4
	}
	return &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: responseHeaderTimeout,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   idlePool,
	}
}

func (o *OpenAIHTTPBackend) Name() string                  { return o.id }
func (o *OpenAIHTTPBackend) Start(_ context.Context) error { return nil }
func (o *OpenAIHTTPBackend) Stop() error                   { return nil }

// Healthy validates configuration only. DO NOT add a network probe —
// the dispatcher calls this concurrently per-agent; a probe would burn
// the provider's concurrency quota before any Run() executes.
func (o *OpenAIHTTPBackend) Healthy(_ context.Context) error {
	if os.Getenv(o.apiKeyEnv) == "" {
		return fmt.Errorf("%s: %s not set", o.id, o.apiKeyEnv)
	}
	return nil
}

// Run dispatches a single chat-completion request. Env is read per-call.
// File contents in req.Files are eagerly inlined because this HTTP path
// has no tool-calling loop. Context deadline routes through BuildResult
// so the timeout-response formatting matches subprocess backends.
func (o *OpenAIHTTPBackend) Run(ctx context.Context, req Request) (*Result, error) {
	apiKey := os.Getenv(o.apiKeyEnv)

	model := req.Model
	if model == "" {
		model = o.defaultModel
	}

	runStart := time.Now()
	var result *Result
	defer func() {
		if result != nil {
			o.observe(o.id, model, result.Status, time.Since(runStart).Milliseconds())
		}
	}()

	if model == "" {
		result = ConfigErrorResult(o.id, "",
			"no model resolved: set provider default_model or AgentSpec.Model")
		return result, nil
	}

	// Fail fast on an empty credential at dispatch time, before we burn a
	// semaphore slot and a network round-trip on a guaranteed 401. Healthy()
	// validates the same env var, but it's checked once per probe cycle and
	// can drift (rotate/unset) between probe and Run.
	if apiKey == "" {
		result = ConfigErrorResult(o.id, model,
			o.apiKeyEnv+" not set at dispatch time")
		return result, nil
	}

	content := req.Prompt
	if inlined := inlineFileContents(req.Files); inlined != "" {
		content = inlined + content
	}

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
		strings.TrimSuffix(o.baseURL, "/")+"/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		result = ConfigErrorResult(o.id, model, "request build: "+err.Error())
		return result, nil
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	// Bulkhead: block until a concurrency slot is available or ctx fires.
	acquireStart := time.Now()
	if err := o.sem.Acquire(ctx, 1); err != nil {
		waited := time.Since(acquireStart).Milliseconds()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) ||
			errors.Is(err, context.DeadlineExceeded) {
			result = BuildResult(
				RawRunOutput{
					TimedOut:  true,
					ElapsedMs: waited,
					Stderr:    fmt.Sprintf("deadline exceeded waiting for %s concurrency slot after %dms", o.id, waited),
				},
				ParsedOutput{},
				model,
			)
			return result, nil
		}
		result = &Result{
			Model:     model,
			Status:    "error",
			Stderr:    o.id + " gate acquire failed: " + err.Error(),
			ElapsedMs: waited,
		}
		return result, nil
	}
	defer o.sem.Release(1)
	if waited := time.Since(acquireStart); waited > o.gateSlowLog {
		slog.Debug("http gate wait", "provider", o.id, "wait_ms", waited.Milliseconds())
	}

	// SECURITY: never log bodyBytes or response body at any level — they
	// contain user prompts and model output (PII/secret surface). Only
	// status code and elapsed time are safe to log.
	start := time.Now()
	resp, err := o.httpClient.Do(httpReq)
	elapsed := time.Since(start).Milliseconds()

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
		result = &Result{
			Model:     model,
			Status:    "error",
			Stderr:    err.Error(),
			ElapsedMs: elapsed,
		}
		return result, nil
	}
	defer resp.Body.Close()

	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, defaultMaxResponseBytes))
	if readErr != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) ||
			errors.Is(readErr, context.DeadlineExceeded) {
			result = BuildResult(
				RawRunOutput{TimedOut: true, ElapsedMs: elapsed, Stderr: "read body: " + readErr.Error()},
				ParsedOutput{},
				model,
			)
			return result, nil
		}
		result = &Result{
			Model:     model,
			Status:    "error",
			Stderr:    "read body: " + readErr.Error(),
			ElapsedMs: elapsed,
		}
		return result, nil
	}

	parsed := openAIParseResponse(raw, resp.StatusCode, resp.Header.Get("Retry-After"), o.id)
	result = BuildResult(
		RawRunOutput{Stdout: raw, ElapsedMs: elapsed},
		parsed,
		model,
	)
	return result, nil
}

// openAIParseResponse converts a raw /v1/chat/completions response body
// plus HTTP status into a ParsedOutput. See design doc §5.1.1.
//
// Arguments:
//   - body: raw response bytes (already length-limited by caller)
//   - statusCode: HTTP status
//   - retryAfter: raw Retry-After header value ("" if absent)
//   - providerLabel: string prefix for error messages (e.g., "moonshot")
func openAIParseResponse(body []byte, statusCode int, retryAfter, providerLabel string) ParsedOutput {
	switch {
	case statusCode == 429 || statusCode == 503:
		return openAIRateLimitedOutput(body, statusCode, retryAfter, providerLabel)
	case statusCode >= 400:
		return openAIErrorOutput(body, statusCode, providerLabel)
	case statusCode != 200:
		return openAIErrorOutput(body, statusCode, providerLabel)
	}

	var data struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     json.Number `json:"prompt_tokens"`
			CompletionTokens json.Number `json:"completion_tokens"`
		} `json:"usage"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&data); err != nil {
		pe := "JSON parse failed"
		return ParsedOutput{
			Response:   string(body),
			Status:     "error",
			ParseError: &pe,
		}
	}
	if len(data.Choices) == 0 {
		return ParsedOutput{
			Response: providerLabel + ": response missing choices",
			Status:   "error",
		}
	}

	metadata := map[string]any{}
	if data.Model != "" {
		metadata["model_used"] = data.Model
	}
	finish := data.Choices[0].FinishReason
	if finish != "" {
		metadata["finish_reason"] = finish
	}
	if finish == "length" {
		metadata["output_truncated"] = true
	}

	tokens := map[string]any{}
	if s := data.Usage.PromptTokens.String(); s != "" {
		if f, err := data.Usage.PromptTokens.Float64(); err == nil {
			tokens["prompt_tokens"] = f
		}
	}
	if s := data.Usage.CompletionTokens.String(); s != "" {
		if f, err := data.Usage.CompletionTokens.Float64(); err == nil {
			tokens["completion_tokens"] = f
		}
	}
	if len(tokens) > 0 {
		metadata["tokens"] = tokens
	}

	content, contentErr := extractOpenAIContent(data.Choices[0].Message.Content)
	if contentErr != nil {
		return ParsedOutput{
			Response: providerLabel + ": " + contentErr.Error(),
			Status:   "error",
			Metadata: metadata,
		}
	}

	return ParsedOutput{
		Response: content,
		Status:   "ok",
		Metadata: metadata,
	}
}

// extractOpenAIContent accepts the OpenAI-compat message.content field as
// either a plain string (classic shape used by DeepSeek, Groq, most
// legacy deployments) or an array of content parts (the OpenAI-current
// shape used for multi-modal, tool-use, and some provider refusal
// payloads). Text parts are concatenated; other part types are skipped.
// Returns an error when neither shape parses — that signals a
// non-compliant upstream, not an empty assistant message (an assistant
// that legitimately replies with "" still parses fine).
func extractOpenAIContent(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var sb strings.Builder
		for _, p := range parts {
			if p.Type == "text" && p.Text != "" {
				sb.WriteString(p.Text)
			}
		}
		return sb.String(), nil
	}
	return "", fmt.Errorf("unrecognized message.content shape (neither string nor []part)")
}

func openAIRateLimitedOutput(body []byte, statusCode int, retryAfter, providerLabel string) ParsedOutput {
	msg := openAIExtractErrorMessage(body)
	if msg == "" {
		msg = fmt.Sprintf("HTTP %d", statusCode)
	}
	suffix := ". No Retry-After is published; back off and retry later."
	if retryAfter != "" {
		suffix = ". Retry-After: " + retryAfter
	}
	out := ParsedOutput{
		Response: fmt.Sprintf("%s rate limited (HTTP %d): %s%s", providerLabel, statusCode, msg, suffix),
		Status:   "rate_limited",
	}
	if retryAfter != "" {
		out.Metadata = map[string]any{"retry_after": retryAfter}
	}
	return out
}

func openAIErrorOutput(body []byte, statusCode int, providerLabel string) ParsedOutput {
	msg := openAIExtractErrorMessage(body)
	if msg == "" {
		msg = string(body)
	}
	return ParsedOutput{
		Response: fmt.Sprintf("%s HTTP %d: %s", providerLabel, statusCode, msg),
		Status:   "error",
	}
}

// openAIExtractErrorMessage pulls a human-readable message out of an error
// body. OpenAI-compat servers conventionally return {"error":{"message":...}};
// some legacy shims use {"error":"..."}. Accept both.
func openAIExtractErrorMessage(body []byte) string {
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
