package roundtable

// ObserveFunc is the optional metrics hook invoked once per backend.Run()
// with (provider, model, status, elapsedMs). Defined in the roundtable
// package as a func value so backends never need to import a metrics
// package.
//
// Callers MUST pass a non-nil function. Constructors that accept an
// ObserveFunc are responsible for normalizing nil to a no-op closure.
type ObserveFunc func(provider, model, status string, elapsedMs int64)
