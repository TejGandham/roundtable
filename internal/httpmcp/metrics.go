package httpmcp

import (
	"encoding/json"
	"sync/atomic"
)

type Metrics struct {
	TotalRequests      atomic.Int64
	BackendTimeouts    atomic.Int64
	BackendNonZeroExit atomic.Int64
	BackendParseErrors atomic.Int64
}

type metricsSnapshot struct {
	TotalRequests      int64 `json:"total_requests"`
	BackendTimeouts    int64 `json:"backend_timeouts"`
	BackendNonZeroExit int64 `json:"backend_non_zero_exit"`
	BackendParseErrors int64 `json:"backend_parse_errors"`
}

func (m *Metrics) Snapshot() metricsSnapshot {
	return metricsSnapshot{
		TotalRequests:      m.TotalRequests.Load(),
		BackendTimeouts:    m.BackendTimeouts.Load(),
		BackendNonZeroExit: m.BackendNonZeroExit.Load(),
		BackendParseErrors: m.BackendParseErrors.Load(),
	}
}

func (m *Metrics) JSON() []byte {
	data, _ := json.Marshal(m.Snapshot())
	return data
}
