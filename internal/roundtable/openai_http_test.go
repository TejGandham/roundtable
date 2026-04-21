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
	"sync/atomic"
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

func TestOpenAIParse_CachedTokens(t *testing.T) {
	body := []byte(`{
		"model":"accounts/fireworks/models/minimax-m2p7",
		"choices":[{"message":{"content":"hi"},"finish_reason":"stop"}],
		"usage":{
			"prompt_tokens":1024,
			"completion_tokens":4,
			"prompt_tokens_details":{"cached_tokens":900}
		}
	}`)
	parsed := openAIParseResponse(body, 200, "", "fireworks-minimax")
	if parsed.Status != "ok" {
		t.Fatalf("status = %q, want ok", parsed.Status)
	}
	tokens, ok := parsed.Metadata["tokens"].(map[string]any)
	if !ok {
		t.Fatalf("tokens not a map: %T", parsed.Metadata["tokens"])
	}
	got, ok := tokens["cached_tokens"].(float64)
	if !ok {
		t.Fatalf("cached_tokens missing or wrong type: %T", tokens["cached_tokens"])
	}
	if got != 900 {
		t.Errorf("cached_tokens = %v, want 900", got)
	}
}

func TestOpenAIParse_CachedTokensAbsent(t *testing.T) {
	body := []byte(`{
		"choices":[{"message":{"content":"hi"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":10,"completion_tokens":2}
	}`)
	parsed := openAIParseResponse(body, 200, "", "zai")
	tokens, _ := parsed.Metadata["tokens"].(map[string]any)
	if _, present := tokens["cached_tokens"]; present {
		t.Errorf("cached_tokens must be absent when prompt_tokens_details missing; got %v", tokens["cached_tokens"])
	}
}

func TestBuildMessages_SplitRoleAndUser(t *testing.T) {
	msgs := buildMessages(Request{
		RolePrompt:  "You are a code reviewer.",
		UserRequest: "Review this.",
	})
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2", len(msgs))
	}
	if msgs[0]["role"] != "system" || msgs[0]["content"] != "You are a code reviewer." {
		t.Errorf("system msg = %v", msgs[0])
	}
	if msgs[1]["role"] != "user" {
		t.Errorf("user msg role = %q", msgs[1]["role"])
	}
	if !strings.Contains(msgs[1]["content"], "=== REQUEST ===") {
		t.Errorf("user content missing request marker: %q", msgs[1]["content"])
	}
	if !strings.Contains(msgs[1]["content"], "Review this.") {
		t.Errorf("user content missing question: %q", msgs[1]["content"])
	}
}

func TestBuildMessages_LegacyPromptFallback(t *testing.T) {
	msgs := buildMessages(Request{Prompt: "single-message legacy"})
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
	if msgs[0]["role"] != "user" {
		t.Errorf("role = %q, want user", msgs[0]["role"])
	}
	if msgs[0]["content"] != "single-message legacy" {
		t.Errorf("content = %q", msgs[0]["content"])
	}
}

func TestBuildMessages_UserQuestionAtEnd(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "stable.go")
	if err := os.WriteFile(f, []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	msgs := buildMessages(Request{
		RolePrompt:  "ROLE",
		UserRequest: "DYNAMIC-QUESTION",
		Files:       []string{f},
	})
	user := msgs[1]["content"]
	qPos := strings.Index(user, "DYNAMIC-QUESTION")
	filePos := strings.Index(user, "<file path=")
	if qPos < 0 || filePos < 0 {
		t.Fatalf("missing markers in %q", user)
	}
	if filePos > qPos {
		t.Errorf("files must come BEFORE question for prefix cache stability; filePos=%d qPos=%d", filePos, qPos)
	}
	if strings.Contains(user, "=== FILES ===") {
		t.Errorf("=== FILES === trailer must be omitted on HTTP path (cache-hostile)")
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

// Strengthened semaphore proof: the Deadline-variant above only proves the
// gate does _something_ under stress (both results !=ok). This one proves
// actual serialization by tracking the max number of handler invocations
// in flight simultaneously via an atomic counter. The invariant —
// inFlight <= MaxConcurrent — must hold across a burst of N parallel Runs
// even when N >> MaxConcurrent.
func TestOpenAIHTTPBackend_Run_ConcurrencyGate_EnforcesSerialization(t *testing.T) {
	var inFlight atomic.Int32
	var peak atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		now := inFlight.Add(1)
		for {
			p := peak.Load()
			if now <= p || peak.CompareAndSwap(p, now) {
				break
			}
		}
		// Hold the slot long enough that any broken gate would admit a
		// second request while we're here. 30ms is plenty; real Run calls
		// finish in <1ms against this handler.
		time.Sleep(30 * time.Millisecond)
		inFlight.Add(-1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()

	const maxConc = 2
	const burst = 12
	cfg := testConfig()
	cfg.BaseURL = srv.URL
	cfg.APIKeyEnv = "MOONSHOT_API_KEY_TEST"
	cfg.MaxConcurrent = maxConc
	t.Setenv("MOONSHOT_API_KEY_TEST", "sk-test")
	b := NewOpenAIHTTPBackend(cfg, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	oks := atomic.Int32{}
	for i := 0; i < burst; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := b.Run(ctx, Request{Prompt: "x", Timeout: 10})
			if err != nil {
				t.Errorf("Run err: %v", err)
				return
			}
			if res.Status == "ok" {
				oks.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := peak.Load(); got > maxConc {
		t.Errorf("peak in-flight = %d, want <= MaxConcurrent=%d — gate broken", got, maxConc)
	}
	if oks.Load() != burst {
		t.Errorf("ok count = %d, want %d (all should eventually succeed via the gate)", oks.Load(), burst)
	}
}

// OpenAI-current returns message.content as an array of parts
// (multi-modal, tool use, refusal payloads). The old string-only parser
// would treat those responses as "ok" with an empty response — silent
// corruption. Parser must extract text parts.
func TestOpenAIParse_ContentAsArray(t *testing.T) {
	body := []byte(`{
		"model":"glm-4.6",
		"choices":[{"message":{"content":[
			{"type":"text","text":"Hello "},
			{"type":"text","text":"world"},
			{"type":"image_url","image_url":"data:..."}
		]},"finish_reason":"stop"}]
	}`)
	parsed := openAIParseResponse(body, 200, "", "zai")
	if parsed.Status != "ok" {
		t.Errorf("status = %q", parsed.Status)
	}
	if parsed.Response != "Hello world" {
		t.Errorf("response = %q, want 'Hello world'", parsed.Response)
	}
}

func TestOpenAIParse_ContentArrayEmptyTextParts(t *testing.T) {
	body := []byte(`{
		"model":"x",
		"choices":[{"message":{"content":[]},"finish_reason":"tool_calls"}]
	}`)
	parsed := openAIParseResponse(body, 200, "", "p")
	if parsed.Status != "ok" {
		t.Errorf("status = %q", parsed.Status)
	}
	if parsed.Response != "" {
		t.Errorf("response = %q, want empty (tool-only response)", parsed.Response)
	}
	if parsed.Metadata["finish_reason"] != "tool_calls" {
		t.Errorf("finish_reason must still be surfaced: %v", parsed.Metadata)
	}
}

// Empirically observed: Fireworks returned HTTP 299 on a long-running
// chat/completions request. Our original parser treated anything other
// than 200 as a hard error, emitting cryptic empty-bodied results. 2xx
// responses should fall through to the parse path; real parse failures
// still error via the JSON path.
func TestOpenAIParse_2xxNon200IsParsed(t *testing.T) {
	body := []byte(`{"model":"x","choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	for _, code := range []int{201, 202, 299} {
		parsed := openAIParseResponse(body, code, "", "p")
		if parsed.Status != "ok" {
			t.Errorf("HTTP %d: status = %q, want ok (2xx bodies must parse)", code, parsed.Status)
		}
		if parsed.Response != "ok" {
			t.Errorf("HTTP %d: response = %q, want ok", code, parsed.Response)
		}
	}
}

// 3xx codes are unexpected (we don't follow redirects) — still error-class.
func TestOpenAIParse_3xxIsError(t *testing.T) {
	parsed := openAIParseResponse([]byte(`{}`), 301, "", "p")
	if parsed.Status != "error" {
		t.Errorf("HTTP 301: status = %q, want error", parsed.Status)
	}
}

func TestOpenAIParse_ContentUnknownShape(t *testing.T) {
	body := []byte(`{"model":"x","choices":[{"message":{"content":42}}]}`)
	parsed := openAIParseResponse(body, 200, "", "p")
	if parsed.Status != "error" {
		t.Errorf("status = %q, want error (unrecognized content shape)", parsed.Status)
	}
}

// Documenting current behavior: OpenAI-compat servers occasionally emit
// {"content": null} for tool-call-only responses. json.Unmarshal treats
// JSON null as the zero value for *string, so we get status=ok with an
// empty response string — not a parse error. finish_reason is still
// surfaced so callers can distinguish empty-answer from tool-only-turn.
func TestOpenAIParse_ContentNull(t *testing.T) {
	body := []byte(`{"model":"x","choices":[{"message":{"content":null},"finish_reason":"tool_calls"}]}`)
	parsed := openAIParseResponse(body, 200, "", "p")
	if parsed.Status != "ok" {
		t.Errorf("status = %q, want ok (null content is legitimate)", parsed.Status)
	}
	if parsed.Response != "" {
		t.Errorf("response = %q, want empty string", parsed.Response)
	}
	if parsed.Metadata["finish_reason"] != "tool_calls" {
		t.Errorf("finish_reason must still be surfaced: %v", parsed.Metadata)
	}
}

// TOCTOU guard: Healthy() checked the env var, then something (rotation,
// ops error, test reset) cleared it. The dispatcher reaches Run() before
// a re-probe. We must fail fast with ConfigError, not burn a semaphore
// slot firing a guaranteed-401 request.
func TestOpenAIHTTPBackend_Run_EmptyKeyAtDispatchTime(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(500)
	}))
	defer srv.Close()

	cfg := testConfig()
	cfg.BaseURL = srv.URL
	cfg.APIKeyEnv = "MOONSHOT_API_KEY_UNSET_TEST"
	t.Setenv("MOONSHOT_API_KEY_UNSET_TEST", "")
	b := NewOpenAIHTTPBackend(cfg, nil)

	res, _ := b.Run(context.Background(), Request{Prompt: "x", Timeout: 10})
	if res.Status != "error" {
		t.Errorf("status = %q, want error", res.Status)
	}
	if !strings.Contains(res.Stderr, "MOONSHOT_API_KEY_UNSET_TEST") {
		t.Errorf("stderr = %q; should name the unset env var", res.Stderr)
	}
	if called {
		t.Error("handler was called; Run should have fast-failed before any network I/O")
	}
}

// Regression guard: if the dispatcher deadline fires during io.ReadAll of
// a slow body (not during the initial request), the status must still
// map to "timeout", matching the request-phase deadline path and FR-21.
func TestOpenAIHTTPBackend_Run_CtxDeadlineDuringBodyRead(t *testing.T) {
	b, srv := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		// Headers sent; now stall the body. ctx deadline fires here.
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	})
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	res, _ := b.Run(ctx, Request{Prompt: "x", Timeout: 10})
	if res.Status != "timeout" {
		t.Errorf("status = %q, want timeout (body-phase deadline)", res.Status)
	}
}

// Regression guard: MaxIdleConnsPerHost scales with MaxConcurrent so a
// max_concurrent=10 provider doesn't thrash its connection pool under
// burst traffic.
func TestNewHTTPTransport_IdlePoolScalesWithConcurrent(t *testing.T) {
	for _, tc := range []struct {
		maxConc  int
		wantIdle int
	}{
		{1, 4}, {3, 4}, {4, 4}, {10, 10}, {32, 32},
	} {
		tr := newHTTPTransport(60*time.Second, tc.maxConc)
		if tr.MaxIdleConnsPerHost != tc.wantIdle {
			t.Errorf("MaxConcurrent=%d: MaxIdleConnsPerHost = %d, want %d", tc.maxConc, tr.MaxIdleConnsPerHost, tc.wantIdle)
		}
	}
}

// A trailing slash on base_url would concatenate into "/v1//chat/completions"
// and hit 404 on strict upstreams. Normalize by trimming before append.
func TestOpenAIHTTPBackend_Run_TrailingSlashBaseURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()

	cfg := testConfig()
	cfg.BaseURL = srv.URL + "/"
	cfg.APIKeyEnv = "MOONSHOT_API_KEY_TRAILING"
	t.Setenv("MOONSHOT_API_KEY_TRAILING", "sk-test")
	b := NewOpenAIHTTPBackend(cfg, nil)

	res, err := b.Run(context.Background(), Request{Prompt: "x", Timeout: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "ok" {
		t.Errorf("status = %q, want ok (trailing slash must be normalized)", res.Status)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("request path = %q, want /chat/completions (no double slash)", gotPath)
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
