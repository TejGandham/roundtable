package httpmcp

import (
	"encoding/json"
	"sync"
	"sync/atomic"
)

// Metrics holds server-wide counters. Field names on the JSON output
// follow Prometheus conventions (roundtable_backend_*) so a future
// migration to client_golang needs only a transport swap, not a
// rename.
type Metrics struct {
	TotalRequests  atomic.Int64
	DispatchErrors atomic.Int64

	mu sync.Mutex
	// backendRequests counts per (backend, status). Key format: "backend/status".
	backendRequests map[string]*atomic.Int64
	// backendDurationSum accumulates elapsed_ms per backend.
	backendDurationSum map[string]*atomic.Int64
	// backendDurationCount counts samples per backend (for computing mean).
	backendDurationCount map[string]*atomic.Int64
}

// ObserveBackend records a single backend call's outcome.
// `status` is the Result.Status string ("ok", "error", "rate_limited",
// "timeout", etc.). `elapsedMs` is the wall-clock duration.
func (m *Metrics) ObserveBackend(backend, status string, elapsedMs int64) {
	key := backend + "/" + status
	m.mu.Lock()
	if m.backendRequests == nil {
		m.backendRequests = map[string]*atomic.Int64{}
		m.backendDurationSum = map[string]*atomic.Int64{}
		m.backendDurationCount = map[string]*atomic.Int64{}
	}
	c, ok := m.backendRequests[key]
	if !ok {
		c = &atomic.Int64{}
		m.backendRequests[key] = c
	}
	ds, ok := m.backendDurationSum[backend]
	if !ok {
		ds = &atomic.Int64{}
		m.backendDurationSum[backend] = ds
	}
	dc, ok := m.backendDurationCount[backend]
	if !ok {
		dc = &atomic.Int64{}
		m.backendDurationCount[backend] = dc
	}
	m.mu.Unlock()
	c.Add(1)
	ds.Add(elapsedMs)
	dc.Add(1)
}

type metricsSnapshot struct {
	TotalRequests  int64 `json:"total_requests"`
	DispatchErrors int64 `json:"dispatch_errors"`

	BackendRequests      map[string]int64 `json:"roundtable_backend_requests_total"`
	BackendDurationSum   map[string]int64 `json:"roundtable_backend_request_duration_ms_sum"`
	BackendDurationCount map[string]int64 `json:"roundtable_backend_request_duration_ms_count"`
}

func (m *Metrics) Snapshot() metricsSnapshot {
	snap := metricsSnapshot{
		TotalRequests:        m.TotalRequests.Load(),
		DispatchErrors:       m.DispatchErrors.Load(),
		BackendRequests:      map[string]int64{},
		BackendDurationSum:   map[string]int64{},
		BackendDurationCount: map[string]int64{},
	}
	m.mu.Lock()
	for k, v := range m.backendRequests {
		snap.BackendRequests[k] = v.Load()
	}
	for k, v := range m.backendDurationSum {
		snap.BackendDurationSum[k] = v.Load()
	}
	for k, v := range m.backendDurationCount {
		snap.BackendDurationCount[k] = v.Load()
	}
	m.mu.Unlock()
	return snap
}

func (m *Metrics) JSON() []byte {
	data, _ := json.Marshal(m.Snapshot())
	return data
}
