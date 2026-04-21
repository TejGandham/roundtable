package httpmcp

import (
	"encoding/json"
	"sync"
	"sync/atomic"
)

// maxModelsPerProvider caps the number of distinct model labels observed
// per provider before new models collapse to "_other". This enforces FR-28
// (bounded cardinality) and hardens against memory-DoS from a client that
// sends unbounded unique model strings in AgentSpec.Model.
const maxModelsPerProvider = 32

// maxModelLabelLen caps the length of a single model label string. Real
// model ids are well under this; a longer value is treated as "_other" to
// avoid clogging log/metrics output with bloated labels.
const maxModelLabelLen = 128

// otherModelLabel is the spillover bucket used when a client exceeds the
// per-provider model cap or sends a label over maxModelLabelLen.
const otherModelLabel = "_other"

// Metrics holds server-wide counters. JSON output keys follow Prometheus
// conventions (roundtable_provider_*) so a future migration to
// client_golang needs only a transport swap, not a rename.
type Metrics struct {
	TotalRequests  atomic.Int64
	DispatchErrors atomic.Int64

	mu sync.Mutex
	// providerRequests counts per (provider, model, status). Key: "provider/model/status".
	providerRequests map[string]*atomic.Int64
	// providerDurationSum accumulates elapsed_ms per (provider, model). Key: "provider/model".
	providerDurationSum map[string]*atomic.Int64
	// providerDurationCount counts samples per (provider, model).
	providerDurationCount map[string]*atomic.Int64

	// providerModels tracks distinct model labels seen per provider for
	// cardinality bounding. After a provider accumulates maxModelsPerProvider
	// distinct models, new models are coerced to otherModelLabel.
	providerModels map[string]map[string]struct{}

	// providers is the snapshot of registered HTTP providers, set once at
	// startup by SetProviders and surfaced on /metricsz.
	providers []ProviderInfoDTO
}

// ProviderInfoDTO mirrors roundtable.ProviderInfo for JSON exposure on
// /metricsz. Duplicated here so this package stays free of a dependency
// on internal/roundtable for its metrics types.
type ProviderInfoDTO struct {
	ID           string `json:"id"`
	BaseURL      string `json:"base_url"`
	DefaultModel string `json:"default_model,omitempty"`
}

// SetProviders records the registered provider set for /metricsz. Called
// once at startup by the composition root after all providers have been
// built. Safe to call multiple times (each replaces the snapshot).
func (m *Metrics) SetProviders(p []ProviderInfoDTO) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.providers = append([]ProviderInfoDTO(nil), p...)
}

// ObserveProvider records a single backend call's outcome.
// `provider` is the registered provider id. `model` is the resolved model id
// used for the call. `status` is the Result.Status string. `elapsedMs` is
// wall-clock duration.
//
// The `model` label is bounded to prevent memory-DoS: strings over
// maxModelLabelLen and models beyond maxModelsPerProvider per provider are
// coerced to otherModelLabel.
func (m *Metrics) ObserveProvider(provider, model, status string, elapsedMs int64) {
	m.mu.Lock()
	if m.providerRequests == nil {
		m.providerRequests = map[string]*atomic.Int64{}
		m.providerDurationSum = map[string]*atomic.Int64{}
		m.providerDurationCount = map[string]*atomic.Int64{}
		m.providerModels = map[string]map[string]struct{}{}
	}

	model = boundedModelLabelLocked(m.providerModels, provider, model)
	reqKey := provider + "/" + model + "/" + status
	durKey := provider + "/" + model

	c, ok := m.providerRequests[reqKey]
	if !ok {
		c = &atomic.Int64{}
		m.providerRequests[reqKey] = c
	}
	ds, ok := m.providerDurationSum[durKey]
	if !ok {
		ds = &atomic.Int64{}
		m.providerDurationSum[durKey] = ds
	}
	dc, ok := m.providerDurationCount[durKey]
	if !ok {
		dc = &atomic.Int64{}
		m.providerDurationCount[durKey] = dc
	}
	m.mu.Unlock()
	c.Add(1)
	ds.Add(elapsedMs)
	dc.Add(1)
}

// boundedModelLabelLocked enforces cardinality. Caller MUST hold m.mu.
// Reserved labels (otherModelLabel, "") pass through unchanged; oversized
// strings become otherModelLabel; new labels past the per-provider cap
// become otherModelLabel.
func boundedModelLabelLocked(seen map[string]map[string]struct{}, provider, model string) string {
	if model == "" || model == otherModelLabel {
		return model
	}
	if len(model) > maxModelLabelLen {
		return otherModelLabel
	}
	providerSet, ok := seen[provider]
	if !ok {
		providerSet = make(map[string]struct{})
		seen[provider] = providerSet
	}
	if _, known := providerSet[model]; known {
		return model
	}
	if len(providerSet) >= maxModelsPerProvider {
		return otherModelLabel
	}
	providerSet[model] = struct{}{}
	return model
}

type metricsSnapshot struct {
	TotalRequests  int64 `json:"total_requests"`
	DispatchErrors int64 `json:"dispatch_errors"`

	ProviderRequests      map[string]int64  `json:"roundtable_provider_requests_total"`
	ProviderDurationSum   map[string]int64  `json:"roundtable_provider_request_duration_ms_sum"`
	ProviderDurationCount map[string]int64  `json:"roundtable_provider_request_duration_ms_count"`
	ProvidersRegistered   []ProviderInfoDTO `json:"roundtable_providers_registered"`
}

func (m *Metrics) Snapshot() metricsSnapshot {
	snap := metricsSnapshot{
		TotalRequests:         m.TotalRequests.Load(),
		DispatchErrors:        m.DispatchErrors.Load(),
		ProviderRequests:      map[string]int64{},
		ProviderDurationSum:   map[string]int64{},
		ProviderDurationCount: map[string]int64{},
	}
	m.mu.Lock()
	for k, v := range m.providerRequests {
		snap.ProviderRequests[k] = v.Load()
	}
	for k, v := range m.providerDurationSum {
		snap.ProviderDurationSum[k] = v.Load()
	}
	for k, v := range m.providerDurationCount {
		snap.ProviderDurationCount[k] = v.Load()
	}
	snap.ProvidersRegistered = append([]ProviderInfoDTO(nil), m.providers...)
	m.mu.Unlock()
	return snap
}

func (m *Metrics) JSON() []byte {
	data, _ := json.Marshal(m.Snapshot())
	return data
}
