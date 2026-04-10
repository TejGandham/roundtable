package roundtable

import (
	"encoding/json"
	"strings"
)

type Result struct {
	Response        string  `json:"response"`
	Model           string  `json:"model"`
	Status          string  `json:"status"`
	ExitCode        *int    `json:"exit_code"`
	ExitSignal      *string `json:"exit_signal"`
	Stderr          string  `json:"stderr"`
	ElapsedMs       int64   `json:"elapsed_ms"`
	ParseError      *string `json:"parse_error"`
	Truncated       bool    `json:"truncated"`
	StderrTruncated bool    `json:"stderr_truncated"`
	SessionID       *string `json:"session_id"`
}

func NotFoundResult(backendName, model string) *Result {
	if model == "" {
		model = "cli-default"
	}
	stderr := backendName + " CLI not found in PATH"
	return &Result{Model: model, Status: "not_found", Stderr: stderr}
}

func ProbeFailedResult(backendName, model, reason string, exitCode *int) *Result {
	if model == "" {
		model = "cli-default"
	}
	stderr := backendName + " CLI probe failed: " + reason + ". Run " + strings.ToLower(backendName) + " --version to diagnose."
	return &Result{Model: model, Status: "probe_failed", ExitCode: exitCode, Stderr: stderr}
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
