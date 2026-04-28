package roundtable

import (
	"encoding/json"
	"fmt"

	"github.com/TejGandham/roundtable/internal/roundtable/dispatchschema"
)

type Result struct {
	Response        string         `json:"response"`
	Model           string         `json:"model"`
	Status          string         `json:"status"`
	ExitCode        *int           `json:"exit_code"`
	ExitSignal      *string        `json:"exit_signal"`
	Stderr          string         `json:"stderr"`
	ElapsedMs       int64          `json:"elapsed_ms"`
	ParseError      *string        `json:"parse_error"`
	Truncated       bool           `json:"truncated"`
	StderrTruncated bool           `json:"stderr_truncated"`
	SessionID       *string        `json:"session_id"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	// Structured carries the parsed JSON payload produced by
	// dispatchschema.Validate when a schema was supplied for this dispatch.
	// VALUE-type json.RawMessage (not pointer): json.RawMessage is already
	// a slice and omitempty elides it when nil, avoiding a double indirection.
	// Discipline guardrail: never assigned a literal-null payload by the
	// validator — a null-parse result is surfaced as StructuredError instead.
	Structured json.RawMessage `json:"structured,omitempty"`
	// StructuredError carries per-panelist validation failure metadata when
	// dispatchschema.Validate rejects the panelist's response. Pointer so
	// omitempty elides it on the success / nil-schema path. Same egress
	// policy as Response — Excerpt may contain sensitive panelist content.
	StructuredError *dispatchschema.ValidationError `json:"structured_error,omitempty"`
}

func NotFoundResult(providerID, model string) *Result {
	if model == "" {
		model = "cli-default"
	}
	return &Result{
		Model:  model,
		Status: "not_found",
		Stderr: fmt.Sprintf("provider %q not registered", providerID),
	}
}

func ProbeFailedResult(providerID, model, reason string, exitCode *int) *Result {
	if model == "" {
		model = "cli-default"
	}
	return &Result{
		Model:    model,
		Status:   "probe_failed",
		ExitCode: exitCode,
		Stderr:   fmt.Sprintf("provider %q probe failed: %s", providerID, reason),
	}
}

// ConfigErrorResult is the HTTP-native analogue of NotFoundResult/ProbeFailedResult.
// Use it when a backend cannot run due to missing/invalid configuration
// (e.g., missing API key, unresolvable model) rather than a missing binary
// or a failed probe. Status is "error" so callers treat it as a normal
// per-agent failure, not a dispatch-wide fault.
func ConfigErrorResult(backendName, model, reason string) *Result {
	if model == "" {
		model = "cli-default"
	}
	return &Result{
		Model:    model,
		Status:   "error",
		Response: backendName + " backend misconfigured: " + reason,
		Stderr:   reason,
	}
}

type Meta struct {
	TotalElapsedMs  int64             `json:"total_elapsed_ms"`
	FilesReferenced []string          `json:"files_referenced"`
	DynamicFields   map[string]string `json:"-"`
}

func (m Meta) MarshalJSON() ([]byte, error) {
	base := map[string]any{
		"total_elapsed_ms": m.TotalElapsedMs,
		"files_referenced": m.FilesReferenced,
	}
	for k, v := range m.DynamicFields {
		base[k] = v
	}
	return json.Marshal(base)
}

func (m *Meta) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if v, ok := raw["total_elapsed_ms"]; ok {
		_ = json.Unmarshal(v, &m.TotalElapsedMs)
	}
	if v, ok := raw["files_referenced"]; ok {
		_ = json.Unmarshal(v, &m.FilesReferenced)
	}
	m.DynamicFields = make(map[string]string)
	for k, v := range raw {
		if k == "total_elapsed_ms" || k == "files_referenced" {
			continue
		}
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			m.DynamicFields[k] = s
		}
	}
	return nil
}

type DispatchResult struct {
	Results map[string]*Result `json:"-"`
	Meta    Meta               `json:"meta"`
}

func (d DispatchResult) MarshalJSON() ([]byte, error) {
	m := make(map[string]any, len(d.Results)+1)
	for name, result := range d.Results {
		m[name] = result
	}
	m["meta"] = d.Meta
	return json.Marshal(m)
}
