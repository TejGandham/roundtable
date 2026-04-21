package roundtable

import (
	"context"
	"strings"
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

// Compile-time assertion that OllamaBackend satisfies Backend.
var _ Backend = (*OllamaBackend)(nil)

// -----------------------------------------------------------------------
// Task 3: parser tests
// -----------------------------------------------------------------------

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
	// max_tokens) cut the response short. MUST be surfaced as metadata.
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
	// Ollama doesn't currently publish Retry-After (arch plan §5.4), but
	// if/when it does, the parser must surface it on Metadata so callers
	// can back off intelligently.
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
