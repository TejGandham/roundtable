package roundtable

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var _ Backend = (*OpenAIHTTPBackend)(nil)

func testConfig() ProviderConfig {
	return ProviderConfig{
		ID:                    "moonshot",
		BaseURL:               "https://api.moonshot.cn/v1",
		APIKeyEnv:             "MOONSHOT_API_KEY",
		DefaultModel:          "kimi-k2-0711-preview",
		MaxConcurrent:         3,
		ResponseHeaderTimeout: 60 * time.Second,
		GateSlowLogThreshold:  100 * time.Millisecond,
	}
}

func TestOpenAIHTTPBackend_Name(t *testing.T) {
	b := NewOpenAIHTTPBackend(testConfig(), nil)
	if b.Name() != "moonshot" {
		t.Errorf("Name() = %q, want moonshot", b.Name())
	}
}

func TestOpenAIHTTPBackend_StartStop(t *testing.T) {
	b := NewOpenAIHTTPBackend(testConfig(), nil)
	if err := b.Start(context.Background()); err != nil {
		t.Errorf("Start: %v", err)
	}
	if err := b.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

func TestOpenAIHTTPBackend_Healthy_NoKey(t *testing.T) {
	t.Setenv("MOONSHOT_API_KEY", "")
	b := NewOpenAIHTTPBackend(testConfig(), nil)
	if err := b.Healthy(context.Background()); err == nil {
		t.Error("want error when MOONSHOT_API_KEY unset")
	}
}

func TestOpenAIHTTPBackend_Healthy_WithKey(t *testing.T) {
	t.Setenv("MOONSHOT_API_KEY", "sk-test")
	b := NewOpenAIHTTPBackend(testConfig(), nil)
	if err := b.Healthy(context.Background()); err != nil {
		t.Errorf("want nil, got %v", err)
	}
}

func TestOpenAIHTTPBackend_Healthy_IsOffline(t *testing.T) {
	t.Setenv("MOONSHOT_API_KEY", "sk-test")
	b := NewOpenAIHTTPBackend(testConfig(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.Healthy(ctx); err != nil {
		t.Errorf("canceled ctx should not affect offline Healthy: %v", err)
	}
}

func TestOpenAIParse_Success(t *testing.T) {
	body := []byte(`{
		"id":"chat-xyz",
		"model":"kimi-k2-0711-preview",
		"choices":[{"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":42,"completion_tokens":8}
	}`)
	parsed := openAIParseResponse(body, 200, "", "moonshot")
	if parsed.Status != "ok" {
		t.Errorf("status = %q, want ok", parsed.Status)
	}
	if parsed.Response != "Hello" {
		t.Errorf("response = %q, want Hello", parsed.Response)
	}
	if parsed.Metadata["model_used"] != "kimi-k2-0711-preview" {
		t.Errorf("model_used = %v", parsed.Metadata["model_used"])
	}
	if parsed.Metadata["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v, want stop", parsed.Metadata["finish_reason"])
	}
	if _, present := parsed.Metadata["output_truncated"]; present {
		t.Errorf("output_truncated must be absent when finish_reason != length; got %v", parsed.Metadata["output_truncated"])
	}
	tokens, ok := parsed.Metadata["tokens"].(map[string]any)
	if !ok {
		t.Fatalf("tokens not a map: %T", parsed.Metadata["tokens"])
	}
	if tokens["prompt_tokens"].(float64) != 42 || tokens["completion_tokens"].(float64) != 8 {
		t.Errorf("tokens = %v", tokens)
	}
}

func TestOpenAIParse_Truncated(t *testing.T) {
	body := []byte(`{"model":"glm-4.6","choices":[{"message":{"content":"cut..."},"finish_reason":"length"}]}`)
	parsed := openAIParseResponse(body, 200, "", "zai")
	if parsed.Status != "ok" {
		t.Errorf("status = %q", parsed.Status)
	}
	if parsed.Metadata["finish_reason"] != "length" {
		t.Error("finish_reason missing")
	}
	truncated, ok := parsed.Metadata["output_truncated"].(bool)
	if !ok || !truncated {
		t.Errorf("output_truncated = %v, want true", parsed.Metadata["output_truncated"])
	}
}

func TestOpenAIParse_429NoRetryAfter(t *testing.T) {
	body := []byte(`{"error":{"message":"rate limit exceeded"}}`)
	parsed := openAIParseResponse(body, 429, "", "moonshot")
	if parsed.Status != "rate_limited" {
		t.Errorf("status = %q", parsed.Status)
	}
	if parsed.Metadata["retry_after"] != nil {
		t.Error("retry_after should be absent")
	}
}

func TestOpenAIParse_429WithRetryAfter(t *testing.T) {
	body := []byte(`{"error":{"message":"slow down"}}`)
	parsed := openAIParseResponse(body, 429, "30", "moonshot")
	if parsed.Status != "rate_limited" {
		t.Errorf("status = %q", parsed.Status)
	}
	if parsed.Metadata["retry_after"] != "30" {
		t.Errorf("retry_after = %v, want 30", parsed.Metadata["retry_after"])
	}
}

func TestOpenAIParse_503(t *testing.T) {
	body := []byte(`{"error":"overloaded"}`)
	parsed := openAIParseResponse(body, 503, "", "moonshot")
	if parsed.Status != "rate_limited" {
		t.Errorf("status = %q", parsed.Status)
	}
}

func TestOpenAIParse_401(t *testing.T) {
	body := []byte(`{"error":{"message":"invalid key"}}`)
	parsed := openAIParseResponse(body, 401, "", "moonshot")
	if parsed.Status != "error" {
		t.Errorf("status = %q", parsed.Status)
	}
}

func TestOpenAIParse_MalformedJSON(t *testing.T) {
	parsed := openAIParseResponse([]byte(`not json`), 200, "", "moonshot")
	if parsed.Status != "error" {
		t.Errorf("status = %q", parsed.Status)
	}
	if parsed.ParseError == nil {
		t.Error("ParseError = nil, want set")
	}
}

func TestOpenAIParse_MissingChoices(t *testing.T) {
	parsed := openAIParseResponse([]byte(`{"model":"x"}`), 200, "", "moonshot")
	if parsed.Status != "error" {
		t.Errorf("status = %q", parsed.Status)
	}
	if !strings.Contains(parsed.Response, "missing choices") {
		t.Errorf("response = %q", parsed.Response)
	}
}

func newTestBackend(t *testing.T, handler http.HandlerFunc) (*OpenAIHTTPBackend, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	cfg := testConfig()
	cfg.BaseURL = srv.URL
	cfg.APIKeyEnv = "MOONSHOT_API_KEY_TEST"
	t.Setenv("MOONSHOT_API_KEY_TEST", "sk-test")
	b := NewOpenAIHTTPBackend(cfg, nil)
	return b, srv
}

func TestOpenAIHTTPBackend_Run_Success(t *testing.T) {
	b, srv := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("auth header = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"model":"kimi-k2-0711-preview",
			"choices":[{"message":{"content":"pong"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":5,"completion_tokens":1}
		}`)
	})
	defer srv.Close()

	res, err := b.Run(context.Background(), Request{Prompt: "ping", Timeout: 10})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != "ok" {
		t.Errorf("status = %q", res.Status)
	}
	if res.Response != "pong" {
		t.Errorf("response = %q", res.Response)
	}
	if res.Model != "kimi-k2-0711-preview" {
		t.Errorf("model = %q, want kimi-k2-0711-preview (from response)", res.Model)
	}
	if res.Metadata["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v", res.Metadata["finish_reason"])
	}
}

func TestOpenAIHTTPBackend_Run_RateLimited(t *testing.T) {
	b, srv := newTestBackend(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "15")
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":{"message":"too many"}}`)
	})
	defer srv.Close()
	res, _ := b.Run(context.Background(), Request{Prompt: "x", Timeout: 10})
	if res.Status != "rate_limited" {
		t.Errorf("status = %q", res.Status)
	}
	if res.Metadata["retry_after"] != "15" {
		t.Errorf("retry_after = %v", res.Metadata["retry_after"])
	}
}

func TestOpenAIHTTPBackend_Run_MissingModel(t *testing.T) {
	cfg := testConfig()
	cfg.DefaultModel = ""
	cfg.APIKeyEnv = "MOONSHOT_API_KEY_TEST"
	t.Setenv("MOONSHOT_API_KEY_TEST", "sk-test")
	b := NewOpenAIHTTPBackend(cfg, nil)
	res, _ := b.Run(context.Background(), Request{Prompt: "x", Timeout: 10})
	if res.Status != "error" {
		t.Errorf("status = %q", res.Status)
	}
	if !strings.Contains(res.Stderr, "no model resolved") {
		t.Errorf("stderr = %q", res.Stderr)
	}
}

func TestOpenAIHTTPBackend_Run_CtxDeadlineDuringRequest(t *testing.T) {
	b, srv := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
			w.WriteHeader(200)
		case <-r.Context().Done():
		}
	})
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	res, _ := b.Run(ctx, Request{Prompt: "x", Timeout: 10})
	if res.Status != "timeout" {
		t.Errorf("status = %q, want timeout", res.Status)
	}
}

func TestOpenAIHTTPBackend_Run_ResponseSizeCap(t *testing.T) {
	b, srv := newTestBackend(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		bigJunk := bytes.Repeat([]byte("x"), 9*1024*1024)
		_, _ = w.Write(bigJunk)
	})
	defer srv.Close()
	res, _ := b.Run(context.Background(), Request{Prompt: "x", Timeout: 10})
	if res.Status != "error" {
		t.Errorf("status = %q, want error (truncated body unparseable)", res.Status)
	}
}

func TestOpenAIHTTPBackend_Run_ConcurrencyGate_Deadline(t *testing.T) {
	// Use a channel gated by the test — closed in cleanup so the handler
	// always returns promptly even if the client's context-cancel doesn't
	// reach the server fast enough (net/http's request-context propagation
	// on client disconnect is not immediate).
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer func() { close(release); srv.Close() }()

	cfg := testConfig()
	cfg.BaseURL = srv.URL
	cfg.APIKeyEnv = "MOONSHOT_API_KEY_TEST"
	cfg.MaxConcurrent = 1
	t.Setenv("MOONSHOT_API_KEY_TEST", "sk-test")
	b := NewOpenAIHTTPBackend(cfg, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)
	results := make([]*Result, 2)
	for i := range 2 {
		go func(idx int) {
			defer wg.Done()
			results[idx], _ = b.Run(ctx, Request{Prompt: "x", Timeout: 10})
		}(i)
	}
	wg.Wait()

	for i, r := range results {
		if r == nil {
			t.Fatalf("results[%d] = nil", i)
		}
		if r.Status == "ok" {
			t.Errorf("results[%d] = ok; gate let two requests through at once", i)
		}
	}
}

func TestOpenAIHTTPBackend_Run_InlinesFiles(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(fp, []byte("hello from file"), 0o644); err != nil {
		t.Fatal(err)
	}

	var seenBody bytes.Buffer
	var mu sync.Mutex
	b, srv := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		_, _ = io.Copy(&seenBody, r.Body)
		mu.Unlock()
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	})
	defer srv.Close()

	_, err := b.Run(context.Background(), Request{
		Prompt:  "summarize",
		Files:   []string{fp},
		Timeout: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	body := seenBody.String()
	mu.Unlock()
	if !strings.Contains(body, "<file path=") || !strings.Contains(body, "hello from file") {
		t.Errorf("expected file-inlining in body; got: %s", body)
	}
}
