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
