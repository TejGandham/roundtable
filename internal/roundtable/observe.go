package roundtable

// ObserveFunc is the optional metrics hook invoked once per backend.Run()
// with (provider, model, status, elapsedMs). Wired at the composition
// root (cmd/roundtable-http-mcp/main.go) to route into
// httpmcp.Metrics.ObserveProvider. Defined in the roundtable package so
// backends don't import httpmcp (which would cycle, since httpmcp imports
// roundtable). A func value carries no package dependency, so no cycle.
//
// Callers MUST pass a non-nil function. Constructors that accept an
// ObserveFunc are responsible for normalizing nil to a no-op closure.
type ObserveFunc func(provider, model, status string, elapsedMs int64)
