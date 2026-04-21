package httpmcp

import (
	"encoding/json"
	"testing"
)

func TestMetrics_BackendCounter(t *testing.T) {
	m := &Metrics{}
	m.ObserveBackend("ollama", "ok", 120)
	m.ObserveBackend("ollama", "ok", 240)
	m.ObserveBackend("ollama", "rate_limited", 50)
	m.ObserveBackend("gemini", "ok", 300)

	data := m.JSON()
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// roundtable_backend_requests_total{backend="ollama",status="ok"} == 2
	bt, ok := got["roundtable_backend_requests_total"].(map[string]any)
	if !ok {
		t.Fatalf("roundtable_backend_requests_total missing or wrong type: %v", got)
	}
	if v, _ := bt["ollama/ok"].(float64); v != 2 {
		t.Errorf("ollama/ok = %v, want 2", bt["ollama/ok"])
	}
	if v, _ := bt["ollama/rate_limited"].(float64); v != 1 {
		t.Errorf("ollama/rate_limited = %v, want 1", bt["ollama/rate_limited"])
	}
	if v, _ := bt["gemini/ok"].(float64); v != 1 {
		t.Errorf("gemini/ok = %v, want 1", bt["gemini/ok"])
	}

	// roundtable_backend_request_duration_ms_sum{backend="ollama"} == 410
	ds, ok := got["roundtable_backend_request_duration_ms_sum"].(map[string]any)
	if !ok {
		t.Fatalf("duration_sum missing: %v", got)
	}
	if v, _ := ds["ollama"].(float64); v != 410 {
		t.Errorf("ollama sum = %v, want 410", ds["ollama"])
	}
}
