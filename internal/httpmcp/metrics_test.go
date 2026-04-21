package httpmcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMetrics_ObserveProvider(t *testing.T) {
	m := &Metrics{}
	m.ObserveProvider("moonshot", "kimi-k2-0711-preview", "ok", 120)
	m.ObserveProvider("moonshot", "kimi-k2-0711-preview", "ok", 240)
	m.ObserveProvider("moonshot", "kimi-k2-0711-preview", "rate_limited", 50)
	m.ObserveProvider("ollama", "kimi-k2.6:cloud", "ok", 300)

	raw := m.JSON()
	var snap map[string]any
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	reqs, ok := snap["roundtable_provider_requests_total"].(map[string]any)
	if !ok {
		t.Fatalf("missing roundtable_provider_requests_total; got: %v", snap)
	}
	wantKeys := []string{
		"moonshot/kimi-k2-0711-preview/ok",
		"moonshot/kimi-k2-0711-preview/rate_limited",
		"ollama/kimi-k2.6:cloud/ok",
	}
	for _, k := range wantKeys {
		if _, ok := reqs[k]; !ok {
			t.Errorf("missing request counter key %q; got: %v", k, reqs)
		}
	}
	if count, _ := reqs["moonshot/kimi-k2-0711-preview/ok"].(float64); count != 2 {
		t.Errorf("moonshot ok count = %v, want 2", count)
	}

	durSum, ok := snap["roundtable_provider_request_duration_ms_sum"].(map[string]any)
	if !ok {
		t.Fatalf("missing roundtable_provider_request_duration_ms_sum")
	}
	if sum, _ := durSum["moonshot/kimi-k2-0711-preview"].(float64); sum != 410 {
		t.Errorf("moonshot duration sum = %v, want 410", sum)
	}
}

func TestMetrics_ProvidersRegisteredInSnapshot(t *testing.T) {
	m := &Metrics{}
	m.SetProviders([]ProviderInfoDTO{
		{ID: "moonshot", BaseURL: "https://api.moonshot.cn/v1", DefaultModel: "kimi-k2-0711-preview"},
		{ID: "ollama", BaseURL: "https://ollama.com/v1"},
	})
	raw := m.JSON()
	if !strings.Contains(string(raw), `"roundtable_providers_registered"`) {
		t.Errorf("missing providers_registered in output: %s", raw)
	}
	if !strings.Contains(string(raw), `"moonshot"`) || !strings.Contains(string(raw), `"ollama"`) {
		t.Errorf("missing provider ids: %s", raw)
	}
}
