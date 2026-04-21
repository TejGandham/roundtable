package httpmcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestMetrics_ObserveProvider(t *testing.T) {
	m := &Metrics{}
	m.ObserveProvider("moonshot", "kimi-k2-0711-preview", "ok", 120)
	m.ObserveProvider("moonshot", "kimi-k2-0711-preview", "ok", 240)
	m.ObserveProvider("moonshot", "kimi-k2-0711-preview", "rate_limited", 50)
	m.ObserveProvider("fireworks", "accounts/fireworks/models/kimi-k2p6", "ok", 300)

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
		"moonshot|kimi-k2-0711-preview|ok",
		"moonshot|kimi-k2-0711-preview|rate_limited",
		"fireworks|accounts/fireworks/models/kimi-k2p6|ok",
	}
	for _, k := range wantKeys {
		if _, ok := reqs[k]; !ok {
			t.Errorf("missing request counter key %q; got: %v", k, reqs)
		}
	}
	if count, _ := reqs["moonshot|kimi-k2-0711-preview|ok"].(float64); count != 2 {
		t.Errorf("moonshot ok count = %v, want 2", count)
	}

	durSum, ok := snap["roundtable_provider_request_duration_ms_sum"].(map[string]any)
	if !ok {
		t.Fatalf("missing roundtable_provider_request_duration_ms_sum")
	}
	if sum, _ := durSum["moonshot|kimi-k2-0711-preview"].(float64); sum != 410 {
		t.Errorf("moonshot duration sum = %v, want 410", sum)
	}
}

// Regression guard: Fireworks and other providers serve models under paths
// like "accounts/fireworks/models/kimi-k2p6". The metric key delimiter
// must not collide with slashes in model identifiers.
func TestMetrics_ObserveProvider_SlashesInModelIDSurvive(t *testing.T) {
	m := &Metrics{}
	m.ObserveProvider("fireworks", "accounts/fireworks/models/kimi-k2p6", "ok", 100)
	snap := m.Snapshot()
	wantKey := "fireworks|accounts/fireworks/models/kimi-k2p6|ok"
	if _, ok := snap.ProviderRequests[wantKey]; !ok {
		t.Errorf("missing key %q; got: %v", wantKey, snap.ProviderRequests)
	}
	for k := range snap.ProviderRequests {
		parts := strings.Split(k, "|")
		if len(parts) != 3 {
			t.Errorf("key %q splits into %d parts on pipe, want 3", k, len(parts))
		}
	}
}

// Cardinality-DoS hardening: model strings are user-controllable via MCP
// input (AgentSpec.Model → resolveModel → Run → observe). Without a bound,
// a client sending N unique models allocates N *atomic.Int64 pointers for
// the process lifetime. After maxModelsPerProvider distinct models, new
// ones collapse to "_other".
func TestMetrics_ObserveProvider_BoundsModelCardinality(t *testing.T) {
	m := &Metrics{}
	for i := 0; i < maxModelsPerProvider+50; i++ {
		m.ObserveProvider("moonshot", fmt.Sprintf("model-%d", i), "ok", 10)
	}
	snap := m.Snapshot()

	var distinctModels int
	seenOther := false
	for k := range snap.ProviderRequests {
		if strings.HasPrefix(k, "moonshot|"+otherModelLabel+"|") {
			seenOther = true
			continue
		}
		if strings.HasPrefix(k, "moonshot|") {
			distinctModels++
		}
	}
	if distinctModels > maxModelsPerProvider {
		t.Errorf("distinct model labels = %d, want <= %d", distinctModels, maxModelsPerProvider)
	}
	if !seenOther {
		t.Errorf("expected overflow to be bucketed into %q; keys: %v", otherModelLabel, snap.ProviderRequests)
	}
	var otherCount float64
	if v, ok := snap.ProviderRequests["moonshot|"+otherModelLabel+"|ok"]; ok {
		otherCount = float64(v)
	}
	if otherCount < 50 {
		t.Errorf("_other count = %v, want >= 50", otherCount)
	}
}

func TestMetrics_ObserveProvider_RejectsOversizeLabel(t *testing.T) {
	m := &Metrics{}
	long := strings.Repeat("x", maxModelLabelLen+1)
	m.ObserveProvider("moonshot", long, "ok", 10)
	snap := m.Snapshot()
	if _, ok := snap.ProviderRequests["moonshot|"+long+"|ok"]; ok {
		t.Errorf("oversize label should not appear in keys: %v", snap.ProviderRequests)
	}
	if _, ok := snap.ProviderRequests["moonshot|"+otherModelLabel+"|ok"]; !ok {
		t.Errorf("oversize label should be bucketed into %q; keys: %v", otherModelLabel, snap.ProviderRequests)
	}
}

func TestMetrics_ObserveProvider_KnownLabelsStillCount(t *testing.T) {
	m := &Metrics{}
	m.ObserveProvider("moonshot", "kimi-k2-0711-preview", "ok", 10)
	m.ObserveProvider("moonshot", "kimi-k2-0711-preview", "ok", 10)
	snap := m.Snapshot()
	if v := snap.ProviderRequests["moonshot|kimi-k2-0711-preview|ok"]; v != 2 {
		t.Errorf("legit repeat count = %d, want 2", v)
	}
}

func TestMetrics_ProvidersRegisteredInSnapshot(t *testing.T) {
	m := &Metrics{}
	m.SetProviders([]ProviderInfoDTO{
		{ID: "moonshot", BaseURL: "https://api.moonshot.cn/v1", DefaultModel: "kimi-k2-0711-preview"},
		{ID: "fireworks", BaseURL: "https://api.fireworks.ai/inference/v1"},
	})
	raw := m.JSON()
	if !strings.Contains(string(raw), `"roundtable_providers_registered"`) {
		t.Errorf("missing providers_registered in output: %s", raw)
	}
	if !strings.Contains(string(raw), `"moonshot"`) || !strings.Contains(string(raw), `"fireworks"`) {
		t.Errorf("missing provider ids: %s", raw)
	}
}
