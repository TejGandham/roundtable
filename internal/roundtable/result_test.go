package roundtable

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResultJSON(t *testing.T) {
	exitCode := 0
	sessionID := "sess_abc123"
	r := &Result{
		Response: "analysis complete", Model: "o3-pro", Status: "ok",
		ExitCode: &exitCode, ElapsedMs: 4200, SessionID: &sessionID,
	}
	data, err := json.Marshal(r)
	if err != nil { t.Fatalf("marshal: %v", err) }
	var got Result
	if err := json.Unmarshal(data, &got); err != nil { t.Fatalf("unmarshal: %v", err) }
	if got.Response != r.Response { t.Errorf("response = %q, want %q", got.Response, r.Response) }
	if got.Status != "ok" { t.Errorf("status = %q, want ok", got.Status) }
	if got.ExitCode == nil || *got.ExitCode != 0 { t.Errorf("exit_code = %v, want 0", got.ExitCode) }
	if got.SessionID == nil || *got.SessionID != "sess_abc123" { t.Errorf("session_id = %v, want sess_abc123", got.SessionID) }
}

func TestResultJSONNilPointers(t *testing.T) {
	r := &Result{Model: "cli-default", Status: "not_found", Stderr: "codex CLI not found in PATH"}
	data, err := json.Marshal(r)
	if err != nil { t.Fatalf("marshal: %v", err) }
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil { t.Fatalf("unmarshal raw: %v", err) }
	for _, field := range []string{"exit_code", "exit_signal", "parse_error", "session_id"} {
		v, ok := raw[field]
		if !ok { t.Errorf("field %q missing from JSON", field) } else if v != nil { t.Errorf("field %q = %v, want null", field, v) }
	}
}

func TestMetaJSON(t *testing.T) {
	m := Meta{
		TotalElapsedMs: 5000, FilesReferenced: []string{"main.go", "lib/app.ex"},
		DynamicFields: map[string]string{"gemini_role": "planner", "codex_role": "default"},
	}
	data, err := json.Marshal(m)
	if err != nil { t.Fatalf("marshal: %v", err) }
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil { t.Fatalf("unmarshal raw: %v", err) }
	if raw["total_elapsed_ms"].(float64) != 5000 { t.Errorf("total_elapsed_ms = %v, want 5000", raw["total_elapsed_ms"]) }
	if raw["gemini_role"] != "planner" { t.Errorf("gemini_role = %v, want planner", raw["gemini_role"]) }
	if raw["codex_role"] != "default" { t.Errorf("codex_role = %v, want default", raw["codex_role"]) }
	var got Meta
	if err := got.UnmarshalJSON(data); err != nil { t.Fatalf("UnmarshalJSON: %v", err) }
	if got.TotalElapsedMs != 5000 { t.Errorf("round-trip total_elapsed_ms = %d, want 5000", got.TotalElapsedMs) }
	if got.DynamicFields["gemini_role"] != "planner" { t.Errorf("round-trip gemini_role = %q, want planner", got.DynamicFields["gemini_role"]) }
}

func TestDispatchResultJSON(t *testing.T) {
	exitCode := 0
	d := DispatchResult{
		Results: map[string]*Result{"codex": {Response: "done", Model: "o3-pro", Status: "ok", ExitCode: &exitCode, ElapsedMs: 3000}},
		Meta: Meta{TotalElapsedMs: 3000, FilesReferenced: []string{"main.go"}, DynamicFields: map[string]string{"codex_role": "default"}},
	}
	data, err := json.Marshal(d)
	if err != nil { t.Fatalf("marshal: %v", err) }
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil { t.Fatalf("unmarshal: %v", err) }
	if _, ok := raw["codex"]; !ok { t.Error("missing codex key") }
	if _, ok := raw["meta"]; !ok { t.Error("missing meta key") }
}

func TestNotFoundResult(t *testing.T) {
	r := NotFoundResult("codex", "")
	if r.Status != "not_found" { t.Errorf("status = %q, want not_found", r.Status) }
	if r.Model != "cli-default" { t.Errorf("model = %q, want cli-default", r.Model) }
}

func TestProbeFailedResult(t *testing.T) {
	exitCode := 1
	r := ProbeFailedResult("gemini", "gemini-2.5-pro", "exited with code 1", &exitCode)
	if r.Status != "probe_failed" { t.Errorf("status = %q, want probe_failed", r.Status) }
	if r.ExitCode == nil || *r.ExitCode != 1 { t.Errorf("exit_code = %v, want 1", r.ExitCode) }
}

func TestConfigErrorResult(t *testing.T) {
	r := ConfigErrorResult("ollama", "kimi-k2.6:cloud", "OLLAMA_API_KEY not set")
	if r.Status != "error" {
		t.Errorf("status = %q, want error", r.Status)
	}
	if r.Model != "kimi-k2.6:cloud" {
		t.Errorf("model = %q, want kimi-k2.6:cloud", r.Model)
	}
	if !strings.Contains(r.Stderr, "OLLAMA_API_KEY not set") {
		t.Errorf("stderr = %q, want substring 'OLLAMA_API_KEY not set'", r.Stderr)
	}
	if !strings.Contains(r.Response, "ollama") {
		t.Errorf("response = %q, want substring 'ollama'", r.Response)
	}
}

func TestConfigErrorResult_DefaultModel(t *testing.T) {
	r := ConfigErrorResult("ollama", "", "no model configured")
	if r.Model != "cli-default" {
		t.Errorf("model = %q, want cli-default", r.Model)
	}
}
