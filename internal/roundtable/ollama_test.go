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

// -----------------------------------------------------------------------
// Task 4: inlineFileContents tests
// -----------------------------------------------------------------------

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

// -----------------------------------------------------------------------
// Task 5: Run() HTTP flow tests
// -----------------------------------------------------------------------

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
	if res.ElapsedMs < 0 {
		t.Errorf("elapsed_ms = %d, want >= 0", res.ElapsedMs)
	}
	// End-to-end metadata propagation: parser populates parsed.Metadata,
	// BuildResult propagates it onto Result.Metadata (Task 0). done_reason
	// is the load-bearing one — it's how callers detect 16K-cap truncation.
	if res.Metadata == nil {
		t.Fatal("res.Metadata = nil; Task 0 propagation did not land")
	}
	if got := res.Metadata["done_reason"]; got != "stop" {
		t.Errorf("metadata done_reason = %v, want stop", got)
	}
}

func TestOllamaRun_TruncationSurfaced(t *testing.T) {
	// done_reason=length must survive the parser → BuildResult → Result
	// pipeline so callers can detect 16K-cap truncation (§5.3).
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

func TestOllamaRun_NoModelResolvable(t *testing.T) {
	t.Setenv("OLLAMA_API_KEY", "sk-test-value")
	b := NewOllamaBackend("", nil) // no default
	res, err := b.Run(context.Background(), Request{Prompt: "hi"})
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
	// Server hangs; client context expires first. Expect status=timeout
	// with BuildResult's "Request timed out after" response formatting.
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
	if !strings.Contains(res.Response, "timed out") {
		t.Errorf("response = %q, want 'timed out' message from BuildResult", res.Response)
	}
}

func TestOllamaRun_NetworkError(t *testing.T) {
	t.Setenv("OLLAMA_API_KEY", "sk-test-value")
	t.Setenv("OLLAMA_BASE_URL", "http://127.0.0.1:1") // refused
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
	// Verify path composition: OLLAMA_BASE_URL set to httptest, POST lands
	// at /api/chat.
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
		t.Errorf("path = %q, want /api/chat", gotURL)
	}
}

func TestOllamaRun_DefaultBaseURL_FallsThroughWhenUnset(t *testing.T) {
	// When OLLAMA_BASE_URL is unset, Run falls back to https://ollama.com.
	// Use a canceled ctx to prevent any real network I/O. If the env-missing
	// short-circuit were wrongly reintroduced, status would not be the
	// ctx-canceled "error" we assert below.
	t.Setenv("OLLAMA_API_KEY", "sk-test-value")
	t.Setenv("OLLAMA_BASE_URL", "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := NewOllamaBackend("kimi-k2.6:cloud", nil)
	res, _ := b.Run(ctx, Request{Prompt: "hi"})
	if res.Status != "error" {
		t.Errorf("status = %q, want error (canceled ctx)", res.Status)
	}
	if res.Stderr == "" {
		t.Error("stderr empty; expected canceled-ctx error message")
	}
}

func TestOllamaRun_InlinesFileContents(t *testing.T) {
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

	// Run() disables HTML escaping on the JSON encoder, so < and > travel
	// the wire verbatim. Quotes inside the content string are still
	// JSON-escaped as \" per JSON spec — search for that form.
	body := string(gotBody)
	for _, want := range []string{
		`<file path=\"` + p1 + `\">`,
		"alpha body",
		`<file path=\"` + p2 + `\">`,
		"beta body",
		"review these",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("request body missing %q; body was:\n%s", want, body)
		}
	}
}

func TestOllamaRun_NoFilesNoInlining(t *testing.T) {
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

// -----------------------------------------------------------------------
// Task 5a: Concurrency gate tests
// -----------------------------------------------------------------------

func TestOllamaRun_SerializesOverCap(t *testing.T) {
	// With OLLAMA_MAX_CONCURRENT_REQUESTS=1 and two concurrent Runs against
	// a server that holds each request for 50ms, the second call must start
	// ≥40ms after the first (allowing scheduler slack). Without the gate,
	// both calls hit the server within milliseconds.
	var (
		mu   sync.Mutex
		seen []time.Time
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

	go func() { _, _ = b.Run(context.Background(), Request{Prompt: "holder", Model: "kimi-k2.6:cloud"}) }()
	time.Sleep(20 * time.Millisecond) // let the holder Acquire

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
		{"", 3},           // default
		{"1", 1},          // free tier
		{"10", 10},        // max tier
		{"notanumber", 3}, // invalid → default
		{"0", 3},          // zero invalid → default
		{"-2", 3},         // negative invalid → default
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

// -----------------------------------------------------------------------
// Task 8: observe wiring E2E
// -----------------------------------------------------------------------

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
