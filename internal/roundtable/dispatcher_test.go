package roundtable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type mockBackend struct {
	name        string
	healthErr   error
	healthDelay time.Duration
	runResult   *Result
	runErr      error
	runDelay    time.Duration
	healthCalls atomic.Int64
	runCalls    atomic.Int64
}

func (m *mockBackend) Name() string { return m.name }

func (m *mockBackend) Start(_ context.Context) error { return nil }

func (m *mockBackend) Stop() error { return nil }

func (m *mockBackend) Healthy(ctx context.Context) error {
	m.healthCalls.Add(1)
	if m.healthDelay > 0 {
		select {
		case <-time.After(m.healthDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return m.healthErr
}

func (m *mockBackend) Run(ctx context.Context, req Request) (*Result, error) {
	m.runCalls.Add(1)
	if m.runDelay > 0 {
		select {
		case <-time.After(m.runDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return m.runResult, m.runErr
}

func TestDispatchAllHealthy(t *testing.T) {
	b1 := &mockBackend{
		name:      "b1",
		runResult: &Result{Model: "m1", Status: "ok", Response: "hello from b1"},
	}
	b2 := &mockBackend{
		name:      "b2",
		runResult: &Result{Model: "m2", Status: "ok", Response: "hello from b2"},
	}

	d := NewDispatcher(b1, b2)
	req := Request{Prompt: "test", Model: "gpt4", Timeout: 10}
	dr := d.Dispatch(context.Background(), req, map[string]string{"role": "reviewer"})

	if len(dr.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(dr.Results))
	}
	if dr.Results["b1"].Status != "ok" {
		t.Errorf("b1 status = %q, want ok", dr.Results["b1"].Status)
	}
	if dr.Results["b2"].Status != "ok" {
		t.Errorf("b2 status = %q, want ok", dr.Results["b2"].Status)
	}
	if dr.Meta.TotalElapsedMs < 0 {
		t.Error("expected non-negative TotalElapsedMs")
	}
	if dr.Meta.DynamicFields["role"] != "reviewer" {
		t.Errorf("role = %q, want reviewer", dr.Meta.DynamicFields["role"])
	}
}

func TestDispatchProbeFailure(t *testing.T) {
	healthy := &mockBackend{
		name:      "healthy",
		runResult: &Result{Model: "m", Status: "ok"},
	}
	unhealthy := &mockBackend{
		name:      "unhealthy",
		healthErr: errors.New("connection refused"),
	}

	d := NewDispatcher(healthy, unhealthy)
	req := Request{Prompt: "test", Timeout: 10}
	dr := d.Dispatch(context.Background(), req, nil)

	if dr.Results["healthy"].Status != "ok" {
		t.Errorf("healthy status = %q, want ok", dr.Results["healthy"].Status)
	}
	if dr.Results["unhealthy"].Status != "probe_failed" {
		t.Errorf("unhealthy status = %q, want probe_failed", dr.Results["unhealthy"].Status)
	}
	if unhealthy.runCalls.Load() != 0 {
		t.Error("Run should never be called on unhealthy backend")
	}
}

func TestDispatchProbeTimeout(t *testing.T) {
	slow := &mockBackend{
		name:        "slow",
		healthDelay: 10 * time.Second, // exceeds ProbeTimeout of 5s
	}

	d := NewDispatcher(slow)
	req := Request{Prompt: "test", Timeout: 10}
	start := time.Now()
	dr := d.Dispatch(context.Background(), req, nil)
	elapsed := time.Since(start)

	if elapsed >= 10*time.Second {
		t.Errorf("probe did not time out early, elapsed = %v", elapsed)
	}
	if dr.Results["slow"].Status != "probe_failed" {
		t.Errorf("slow status = %q, want probe_failed", dr.Results["slow"].Status)
	}
	if slow.runCalls.Load() != 0 {
		t.Error("Run should never be called after probe timeout")
	}
}

func TestDispatchRunError(t *testing.T) {
	b := &mockBackend{
		name:   "errbackend",
		runErr: errors.New("run failed"),
	}

	d := NewDispatcher(b)
	req := Request{Prompt: "test", Model: "gpt4", Timeout: 10}
	dr := d.Dispatch(context.Background(), req, nil)

	r := dr.Results["errbackend"]
	if r == nil {
		t.Fatal("expected result for errbackend")
	}
	if r.Status != "error" {
		t.Errorf("status = %q, want error", r.Status)
	}
	if r.Stderr != "run failed" {
		t.Errorf("stderr = %q, want 'run failed'", r.Stderr)
	}
}

func TestDispatchRunsInParallel(t *testing.T) {
	b1 := &mockBackend{
		name:      "p1",
		runDelay:  100 * time.Millisecond,
		runResult: &Result{Status: "ok"},
	}
	b2 := &mockBackend{
		name:      "p2",
		runDelay:  100 * time.Millisecond,
		runResult: &Result{Status: "ok"},
	}

	d := NewDispatcher(b1, b2)
	req := Request{Prompt: "test", Timeout: 10}
	start := time.Now()
	d.Dispatch(context.Background(), req, nil)
	elapsed := time.Since(start)

	if elapsed >= 250*time.Millisecond {
		t.Errorf("backends did not run in parallel: elapsed = %v, want < 250ms", elapsed)
	}
}

func TestDispatchNoBackends(t *testing.T) {
	d := NewDispatcher()
	req := Request{Prompt: "test", Timeout: 10}
	dr := d.Dispatch(context.Background(), req, nil)

	if len(dr.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(dr.Results))
	}
}

func TestDispatchFilesReferenced(t *testing.T) {
	b := &mockBackend{
		name:      "b",
		runResult: &Result{Status: "ok"},
	}

	d := NewDispatcher(b)
	req := Request{
		Prompt:  "test",
		Files:   []string{"foo.go", "bar.go"},
		Timeout: 10,
	}
	dr := d.Dispatch(context.Background(), req, nil)

	if len(dr.Meta.FilesReferenced) != 2 {
		t.Fatalf("expected 2 files, got %d", len(dr.Meta.FilesReferenced))
	}
	if dr.Meta.FilesReferenced[0] != "foo.go" || dr.Meta.FilesReferenced[1] != "bar.go" {
		t.Errorf("files = %v, want [foo.go bar.go]", dr.Meta.FilesReferenced)
	}
}
