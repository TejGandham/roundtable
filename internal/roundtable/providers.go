package roundtable

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ProviderConfig is one registered OpenAI-compatible HTTP provider.
type ProviderConfig struct {
	ID                    string
	BaseURL               string
	APIKeyEnv             string
	DefaultModel          string
	MaxConcurrent         int
	ResponseHeaderTimeout time.Duration
	GateSlowLogThreshold  time.Duration
}

// ProviderInfo is the read-only enumeration surface for /metricsz and startup
// logging. Kept separate from ProviderConfig so the credential-env-var name
// is not accidentally exposed.
type ProviderInfo struct {
	ID           string `json:"id"`
	BaseURL      string `json:"base_url"`
	DefaultModel string `json:"default_model,omitempty"`
}

// Built-in subprocess backend ids. LoadProviderRegistry rejects HTTP providers
// that try to claim these identifiers — a subprocess backend and an HTTP
// provider sharing a key in the backend map is a silent routing bug waiting
// to happen.
var builtInSubprocessIDs = map[string]bool{
	"gemini": true,
	"codex":  true,
	"claude": true,
}

// providerJSON is the wire shape for one entry in ROUNDTABLE_PROVIDERS.
// Kept private; converted to ProviderConfig with defaults applied.
type providerJSON struct {
	ID                    string `json:"id"`
	BaseURL               string `json:"base_url"`
	APIKeyEnv             string `json:"api_key_env"`
	DefaultModel          string `json:"default_model"`
	MaxConcurrent         int    `json:"max_concurrent"`
	ResponseHeaderTimeout string `json:"response_header_timeout"`
	GateSlowLogThreshold  string `json:"gate_slow_log_threshold"`
}

// LoadProviderRegistry parses the ROUNDTABLE_PROVIDERS env var (via the
// injected getenv function) and returns one ProviderConfig per entry.
// Returns (nil, nil) when the var is unset or empty. Returns (nil, error)
// on any parse or validation failure — caller logs and proceeds without
// HTTP providers.
func LoadProviderRegistry(getenv func(string) string) ([]ProviderConfig, error) {
	raw := strings.TrimSpace(getenv("ROUNDTABLE_PROVIDERS"))
	if raw == "" {
		return nil, nil
	}

	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()

	var entries []providerJSON
	if err := dec.Decode(&entries); err != nil {
		return nil, fmt.Errorf("ROUNDTABLE_PROVIDERS: %w", err)
	}

	cfgs := make([]ProviderConfig, 0, len(entries))
	seen := make(map[string]bool, len(entries))

	for i, e := range entries {
		if e.ID == "" {
			return nil, fmt.Errorf("ROUNDTABLE_PROVIDERS[%d]: id is required", i)
		}
		if builtInSubprocessIDs[e.ID] {
			return nil, fmt.Errorf("ROUNDTABLE_PROVIDERS[%d]: id %q collides with built-in subprocess backend", i, e.ID)
		}
		if seen[e.ID] {
			return nil, fmt.Errorf("ROUNDTABLE_PROVIDERS[%d]: duplicate id %q", i, e.ID)
		}
		seen[e.ID] = true
		if e.BaseURL == "" {
			return nil, fmt.Errorf("ROUNDTABLE_PROVIDERS[%d] (%s): base_url is required", i, e.ID)
		}
		if e.APIKeyEnv == "" {
			return nil, fmt.Errorf("ROUNDTABLE_PROVIDERS[%d] (%s): api_key_env is required", i, e.ID)
		}

		maxConc := e.MaxConcurrent
		if maxConc <= 0 {
			maxConc = 3
		}

		rhTimeout := 60 * time.Second
		if e.ResponseHeaderTimeout != "" {
			d, err := time.ParseDuration(e.ResponseHeaderTimeout)
			if err != nil {
				return nil, fmt.Errorf("ROUNDTABLE_PROVIDERS[%d] (%s): response_header_timeout: %w", i, e.ID, err)
			}
			if d <= 0 {
				return nil, fmt.Errorf("ROUNDTABLE_PROVIDERS[%d] (%s): response_header_timeout must be positive", i, e.ID)
			}
			rhTimeout = d
		}

		gateThresh := 100 * time.Millisecond
		if e.GateSlowLogThreshold != "" {
			d, err := time.ParseDuration(e.GateSlowLogThreshold)
			if err != nil {
				return nil, fmt.Errorf("ROUNDTABLE_PROVIDERS[%d] (%s): gate_slow_log_threshold: %w", i, e.ID, err)
			}
			if d <= 0 {
				return nil, fmt.Errorf("ROUNDTABLE_PROVIDERS[%d] (%s): gate_slow_log_threshold must be positive", i, e.ID)
			}
			gateThresh = d
		}

		cfgs = append(cfgs, ProviderConfig{
			ID:                    e.ID,
			BaseURL:               e.BaseURL,
			APIKeyEnv:             e.APIKeyEnv,
			DefaultModel:          e.DefaultModel,
			MaxConcurrent:         maxConc,
			ResponseHeaderTimeout: rhTimeout,
			GateSlowLogThreshold:  gateThresh,
		})
	}
	return cfgs, nil
}
