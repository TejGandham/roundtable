package roundtable

import "context"

// Backend is the interface for a model backend (Codex, Gemini, Claude, etc.).
//
// Lifecycle:
//  1. Start() — acquire resources (launch subprocess, open connection)
//  2. Healthy() — lightweight probe; called before Run in the Dispatcher
//  3. Run() — execute a prompt and return a Result
//  4. Stop() — release resources
//
// Backends must be safe for concurrent Healthy() and Run() calls from the
// Dispatcher's errgroup goroutines.
type Backend interface {
	// Name returns the backend identifier (e.g. "codex", "gemini", "claude").
	// Used as the key in DispatchResult.Results.
	Name() string

	// Start initializes the backend. For subprocess backends this launches the
	// process. For long-lived backends (like Codex app-server) this establishes
	// the connection. Called once before any Healthy/Run calls.
	Start(ctx context.Context) error

	// Stop releases all resources. Called once when the backend is no longer
	// needed. Must be safe to call even if Start was never called or failed.
	Stop() error

	// Healthy performs a lightweight health check. Returns nil if the backend
	// is ready to accept Run calls. The Dispatcher calls this during the probe
	// phase with a 5-second timeout.
	Healthy(ctx context.Context) error

	// Run executes the given request and returns a Result. The context carries
	// the deadline (tool_timeout + 30s grace). Implementations must respect
	// context cancellation and return a Result with an appropriate status on
	// timeout or cancellation.
	Run(ctx context.Context, req Request) (*Result, error)
}
