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

// Compile-time assertion that OllamaBackend satisfies Backend.
var _ Backend = (*OllamaBackend)(nil)
