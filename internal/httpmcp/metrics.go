package httpmcp

import (
	"encoding/json"
	"sync/atomic"
)

type Metrics struct {
	TotalRequests  atomic.Int64
	DispatchErrors atomic.Int64
}

type metricsSnapshot struct {
	TotalRequests  int64 `json:"total_requests"`
	DispatchErrors int64 `json:"dispatch_errors"`
}

func (m *Metrics) Snapshot() metricsSnapshot {
	return metricsSnapshot{
		TotalRequests:  m.TotalRequests.Load(),
		DispatchErrors: m.DispatchErrors.Load(),
	}
}

func (m *Metrics) JSON() []byte {
	data, _ := json.Marshal(m.Snapshot())
	return data
}
