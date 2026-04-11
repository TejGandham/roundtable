package roundtable

import (
	"context"
	"sync/atomic"
	"time"
)

// mockBackend is a test double implementing the Backend interface.
// Shared by run_test.go.
type mockBackend struct {
	name        string
	healthErr   error
	healthDelay time.Duration
	runResult   *Result
	runErr      error
	runDelay    time.Duration

	healthCalls atomic.Int32
	runCalls    atomic.Int32
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
			return &Result{
				Model:     req.Model,
				Status:    "timeout",
				ElapsedMs: m.runDelay.Milliseconds(),
			}, ctx.Err()
		}
	}
	return m.runResult, m.runErr
}
