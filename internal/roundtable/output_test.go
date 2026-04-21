package roundtable

import (
	"encoding/json"
	"strings"
	"testing"
)

func intPtr(i int) *int       { return &i }
func strPtr(s string) *string { return &s }

func TestBuildResult_NormalSuccess(t *testing.T) {
	raw := RawRunOutput{
		Stdout:    []byte(`{"response":"done"}`),
		ExitCode:  intPtr(0),
		ElapsedMs: 3000,
	}
	parsed := ParsedOutput{
		Response: "Analysis complete.",
		Status:   "ok",
		Metadata: map[string]any{"model_used": "gemini-2.5-pro"},
	}

	r := BuildResult(raw, parsed, "fallback-model")

	if r.Status != "ok" {
		t.Errorf("status = %q, want ok", r.Status)
	}
	if r.Response != "Analysis complete." {
		t.Errorf("response = %q, want 'Analysis complete.'", r.Response)
	}
	if r.Model != "gemini-2.5-pro" {
		t.Errorf("model = %q, want gemini-2.5-pro", r.Model)
	}
	if r.ElapsedMs != 3000 {
		t.Errorf("elapsed_ms = %d, want 3000", r.ElapsedMs)
	}
	if r.ExitCode == nil || *r.ExitCode != 0 {
		t.Errorf("exit_code = %v, want 0", r.ExitCode)
	}
}

func TestBuildResult_Timeout(t *testing.T) {
	raw := RawRunOutput{
		ExitCode:  intPtr(-1),
		TimedOut:  true,
		ElapsedMs: 900000,
	}
	parsed := ParsedOutput{
		Response:   "",
		Status:     "ok",
		ParseError: strPtr("empty response"),
	}

	r := BuildResult(raw, parsed, "o3-pro")

	if r.Status != "timeout" {
		t.Errorf("status = %q, want timeout", r.Status)
	}
	if !strings.Contains(r.Response, "timed out after 900s") {
		t.Errorf("expected timeout message with 900s, got: %q", r.Response)
	}
	if r.ParseError != nil {
		t.Errorf("parse_error should be nil on timeout, got: %v", *r.ParseError)
	}
	if r.Model != "o3-pro" {
		t.Errorf("model = %q, want o3-pro (fallback)", r.Model)
	}
}

func TestBuildResult_TimeoutRoundsUp(t *testing.T) {
	raw := RawRunOutput{
		TimedOut:  true,
		ElapsedMs: 1, // 1ms rounds up to 1s
	}
	parsed := ParsedOutput{Status: "ok"}

	r := BuildResult(raw, parsed, "")

	if !strings.Contains(r.Response, "after 1s") {
		t.Errorf("expected 1s rounding, got: %q", r.Response)
	}
}

func TestBuildResult_Terminated(t *testing.T) {
	raw := RawRunOutput{
		ExitSignal: strPtr("SIGKILL"),
		ElapsedMs:  5000,
		Stderr:     "killed by signal",
	}
	parsed := ParsedOutput{
		Response: "partial output",
		Status:   "ok",
	}

	r := BuildResult(raw, parsed, "")

	if r.Status != "terminated" {
		t.Errorf("status = %q, want terminated", r.Status)
	}
	if r.ExitSignal == nil || *r.ExitSignal != "SIGKILL" {
		t.Errorf("exit_signal = %v, want SIGKILL", r.ExitSignal)
	}
	if r.Response != "partial output" {
		t.Errorf("response = %q, want 'partial output'", r.Response)
	}
}

func TestBuildResult_NonZeroExitOverridesOkStatus(t *testing.T) {
	raw := RawRunOutput{
		ExitCode:  intPtr(1),
		ElapsedMs: 2000,
		Stderr:    "something went wrong",
	}
	parsed := ParsedOutput{
		Response: "partial",
		Status:   "ok",
	}

	r := BuildResult(raw, parsed, "")

	if r.Status != "error" {
		t.Errorf("status = %q, want error (non-zero exit overrides ok)", r.Status)
	}
	if r.Stderr != "something went wrong" {
		t.Errorf("stderr = %q, want 'something went wrong'", r.Stderr)
	}
}

func TestBuildResult_NonZeroExitPreservesErrorStatus(t *testing.T) {
	raw := RawRunOutput{
		ExitCode:  intPtr(1),
		ElapsedMs: 1000,
	}
	parsed := ParsedOutput{
		Response: "",
		Status:   "rate_limited",
	}

	r := BuildResult(raw, parsed, "")

	// Non-zero exit only overrides "ok" — other statuses are preserved
	if r.Status != "rate_limited" {
		t.Errorf("status = %q, want rate_limited (non-ok status preserved)", r.Status)
	}
}

func TestBuildResult_ModelFallbackChain(t *testing.T) {
	raw := RawRunOutput{ExitCode: intPtr(0)}

	// Case 1: parsed metadata model wins
	r := BuildResult(raw, ParsedOutput{Status: "ok", Metadata: map[string]any{"model_used": "parsed-model"}}, "fallback")
	if r.Model != "parsed-model" {
		t.Errorf("model = %q, want parsed-model", r.Model)
	}

	// Case 2: fallback model when metadata is empty
	r = BuildResult(raw, ParsedOutput{Status: "ok"}, "fallback")
	if r.Model != "fallback" {
		t.Errorf("model = %q, want fallback", r.Model)
	}

	// Case 3: cli-default when both are empty
	r = BuildResult(raw, ParsedOutput{Status: "ok"}, "")
	if r.Model != "cli-default" {
		t.Errorf("model = %q, want cli-default", r.Model)
	}
}

func TestBuildResult_TruncationFlags(t *testing.T) {
	raw := RawRunOutput{
		ExitCode:        intPtr(0),
		Truncated:       true,
		StderrTruncated: true,
	}
	parsed := ParsedOutput{Status: "ok"}

	r := BuildResult(raw, parsed, "")

	if !r.Truncated {
		t.Error("truncated should be true")
	}
	if !r.StderrTruncated {
		t.Error("stderr_truncated should be true")
	}
}

func TestBuildResult_ParseErrorPreserved(t *testing.T) {
	raw := RawRunOutput{ExitCode: intPtr(0)}
	parsed := ParsedOutput{
		Response:   "best effort",
		Status:     "ok",
		ParseError: strPtr("unexpected JSON structure"),
	}

	r := BuildResult(raw, parsed, "")

	if r.ParseError == nil {
		t.Fatal("parse_error should be non-nil")
	}
	if *r.ParseError != "unexpected JSON structure" {
		t.Errorf("parse_error = %q, want 'unexpected JSON structure'", *r.ParseError)
	}
}

func TestBuildResult_SessionIDPassthrough(t *testing.T) {
	raw := RawRunOutput{ExitCode: intPtr(0)}
	parsed := ParsedOutput{
		Status:    "ok",
		SessionID: strPtr("sess_abc123"),
	}

	r := BuildResult(raw, parsed, "")

	if r.SessionID == nil || *r.SessionID != "sess_abc123" {
		t.Errorf("session_id = %v, want sess_abc123", r.SessionID)
	}
}

func TestBuildResult_NilSessionID(t *testing.T) {
	raw := RawRunOutput{ExitCode: intPtr(0)}
	parsed := ParsedOutput{Status: "ok"}

	r := BuildResult(raw, parsed, "")

	if r.SessionID != nil {
		t.Errorf("session_id = %v, want nil", r.SessionID)
	}
}

func TestBuildResult_TimeoutOverridesExitSignal(t *testing.T) {
	// When both TimedOut and ExitSignal are set, timeout wins
	raw := RawRunOutput{
		TimedOut:   true,
		ExitSignal: strPtr("SIGKILL"),
		ElapsedMs:  30000,
	}
	parsed := ParsedOutput{Status: "ok"}

	r := BuildResult(raw, parsed, "")

	if r.Status != "timeout" {
		t.Errorf("status = %q, want timeout (should override terminated)", r.Status)
	}
	if !strings.Contains(r.Response, "timed out") {
		t.Error("expected timeout message in response")
	}
	// ExitSignal should still be passed through for diagnostics
	if r.ExitSignal == nil || *r.ExitSignal != "SIGKILL" {
		t.Errorf("exit_signal = %v, want SIGKILL (preserved for diagnostics)", r.ExitSignal)
	}
}

func TestBuildMeta_MaxElapsed(t *testing.T) {
	results := map[string]*Result{
		"fast": {ElapsedMs: 1000},
		"slow": {ElapsedMs: 5000},
		"mid":  {ElapsedMs: 3000},
	}
	meta := BuildMeta(results, []string{"a.go"}, map[string]string{"fast": "default"})
	if meta.TotalElapsedMs != 5000 {
		t.Errorf("total_elapsed_ms = %d, want 5000", meta.TotalElapsedMs)
	}
	if len(meta.FilesReferenced) != 1 || meta.FilesReferenced[0] != "a.go" {
		t.Errorf("files_referenced = %v, want [a.go]", meta.FilesReferenced)
	}
}

func TestBuildMeta_NilFilesBecomesEmptySlice(t *testing.T) {
	meta := BuildMeta(map[string]*Result{}, nil, nil)
	if meta.FilesReferenced == nil {
		t.Error("files_referenced should be empty slice, not nil")
	}
	if len(meta.FilesReferenced) != 0 {
		t.Errorf("files_referenced = %v, want []", meta.FilesReferenced)
	}
}

func TestBuildMeta_EmptyResults(t *testing.T) {
	meta := BuildMeta(map[string]*Result{}, []string{}, map[string]string{})
	if meta.TotalElapsedMs != 0 {
		t.Errorf("total_elapsed_ms = %d, want 0", meta.TotalElapsedMs)
	}
}

func TestBuildResult_PropagatesMetadata(t *testing.T) {
	raw := RawRunOutput{ExitCode: intPtr(0)}
	parsed := ParsedOutput{
		Status: "ok",
		Metadata: map[string]any{
			"model_used":  "kimi-k2.6:cloud",
			"done_reason": "length",
			"tokens":      map[string]any{"prompt_eval_count": 42, "eval_count": 8},
		},
	}
	r := BuildResult(raw, parsed, "fallback")

	if r.Metadata == nil {
		t.Fatal("Metadata = nil, want propagated map")
	}
	if got := r.Metadata["done_reason"]; got != "length" {
		t.Errorf("done_reason = %v, want length", got)
	}
	if r.Metadata["tokens"] == nil {
		t.Error("tokens missing from propagated metadata")
	}
	// model_used precedence still works (existing behavior, regression guard)
	if r.Model != "kimi-k2.6:cloud" {
		t.Errorf("model = %q, want kimi-k2.6:cloud (parsed metadata wins)", r.Model)
	}
}

func TestBuildResult_NilMetadata_OmittedFromJSON(t *testing.T) {
	raw := RawRunOutput{ExitCode: intPtr(0)}
	r := BuildResult(raw, ParsedOutput{Status: "ok"}, "model")
	if r.Metadata != nil {
		t.Errorf("Metadata = %v, want nil when parser provided none", r.Metadata)
	}
	// Belt-and-suspenders: confirm omitempty keeps it out of JSON.
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), `"metadata"`) {
		t.Errorf("JSON contains metadata key for nil map: %s", string(data))
	}
}
