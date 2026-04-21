package roundtable

import (
	"strings"
	"testing"
	"time"
)

// fakeEnv returns a getenv function that looks up from the map.
func fakeEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadProviderRegistry_EmptyEnv(t *testing.T) {
	cfgs, err := LoadProviderRegistry(fakeEnv(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfgs != nil {
		t.Errorf("cfgs = %v, want nil", cfgs)
	}
}

func TestLoadProviderRegistry_WellFormedSingle(t *testing.T) {
	js := `[{
		"id": "moonshot",
		"base_url": "https://api.moonshot.cn/v1",
		"api_key_env": "MOONSHOT_API_KEY",
		"default_model": "kimi-k2-0711-preview",
		"max_concurrent": 5,
		"response_header_timeout": "90s"
	}]`
	cfgs, err := LoadProviderRegistry(fakeEnv(map[string]string{"ROUNDTABLE_PROVIDERS": js}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfgs) != 1 {
		t.Fatalf("len(cfgs) = %d, want 1", len(cfgs))
	}
	c := cfgs[0]
	if c.ID != "moonshot" || c.BaseURL != "https://api.moonshot.cn/v1" ||
		c.APIKeyEnv != "MOONSHOT_API_KEY" || c.DefaultModel != "kimi-k2-0711-preview" ||
		c.MaxConcurrent != 5 || c.ResponseHeaderTimeout != 90*time.Second {
		t.Errorf("unexpected parsed config: %+v", c)
	}
}

func TestLoadProviderRegistry_RejectsBuiltInCollision(t *testing.T) {
	js := `[{"id":"gemini","base_url":"https://x","api_key_env":"X"}]`
	_, err := LoadProviderRegistry(fakeEnv(map[string]string{"ROUNDTABLE_PROVIDERS": js}))
	if err == nil || !strings.Contains(err.Error(), "collides with built-in") {
		t.Errorf("expected built-in collision error, got: %v", err)
	}
}

func TestLoadProviderRegistry_RejectsDuplicateID(t *testing.T) {
	js := `[
		{"id":"moonshot","base_url":"https://a","api_key_env":"A"},
		{"id":"moonshot","base_url":"https://b","api_key_env":"B"}
	]`
	_, err := LoadProviderRegistry(fakeEnv(map[string]string{"ROUNDTABLE_PROVIDERS": js}))
	if err == nil || !strings.Contains(err.Error(), "duplicate id") {
		t.Errorf("expected duplicate id error, got: %v", err)
	}
}

func TestLoadProviderRegistry_RejectsMissingFields(t *testing.T) {
	cases := []struct {
		name string
		js   string
		want string
	}{
		{"empty id", `[{"base_url":"https://x","api_key_env":"X"}]`, "id is required"},
		{"empty base_url", `[{"id":"x","api_key_env":"X"}]`, "base_url is required"},
		{"empty api_key_env", `[{"id":"x","base_url":"https://x"}]`, "api_key_env is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadProviderRegistry(fakeEnv(map[string]string{"ROUNDTABLE_PROVIDERS": tc.js}))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want %q, got: %v", tc.want, err)
			}
		})
	}
}

func TestLoadProviderRegistry_RejectsUnknownField(t *testing.T) {
	js := `[{"id":"x","base_url":"https://x","api_key_env":"X","typo_field":"oops"}]`
	_, err := LoadProviderRegistry(fakeEnv(map[string]string{"ROUNDTABLE_PROVIDERS": js}))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("expected unknown-field error, got: %v", err)
	}
}

func TestLoadProviderRegistry_RejectsMalformedJSON(t *testing.T) {
	_, err := LoadProviderRegistry(fakeEnv(map[string]string{"ROUNDTABLE_PROVIDERS": "not json"}))
	if err == nil {
		t.Error("expected error on malformed JSON")
	}
}

func TestLoadProviderRegistry_RejectsBadDuration(t *testing.T) {
	js := `[{"id":"x","base_url":"https://x","api_key_env":"X","response_header_timeout":"forever"}]`
	_, err := LoadProviderRegistry(fakeEnv(map[string]string{"ROUNDTABLE_PROVIDERS": js}))
	if err == nil || !strings.Contains(err.Error(), "response_header_timeout") {
		t.Errorf("expected duration parse error, got: %v", err)
	}
}

func TestLoadProviderRegistry_AppliesDefaults(t *testing.T) {
	js := `[{"id":"x","base_url":"https://x","api_key_env":"X"}]`
	cfgs, err := LoadProviderRegistry(fakeEnv(map[string]string{"ROUNDTABLE_PROVIDERS": js}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := cfgs[0]
	if c.MaxConcurrent != 3 {
		t.Errorf("MaxConcurrent = %d, want default 3", c.MaxConcurrent)
	}
	if c.ResponseHeaderTimeout != 60*time.Second {
		t.Errorf("ResponseHeaderTimeout = %v, want default 60s", c.ResponseHeaderTimeout)
	}
	if c.GateSlowLogThreshold != 100*time.Millisecond {
		t.Errorf("GateSlowLogThreshold = %v, want default 100ms", c.GateSlowLogThreshold)
	}
}
