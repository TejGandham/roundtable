package roundtable

import "fmt"

// RawRunOutput holds the raw output from a subprocess execution,
// before any CLI-specific parsing. Populated by the subprocess runner
// (to be implemented in Phase 2C).
type RawRunOutput struct {
	Stdout          []byte  // raw stdout bytes
	Stderr          string  // stderr (may be truncated)
	ExitCode        *int    // nil if process was signaled
	ExitSignal      *string // e.g. "SIGKILL", nil if exited normally
	TimedOut        bool    // true if the process was killed due to timeout
	ElapsedMs       int64   // wall-clock milliseconds
	Truncated       bool    // true if stdout was capped
	StderrTruncated bool    // true if stderr was capped
}

// ParsedOutput holds the result of CLI-specific output parsing.
// Each backend parser (Gemini, Claude, Codex) produces this struct
// from the raw stdout/stderr. Parsers will be implemented in Phase 2C.
type ParsedOutput struct {
	Response   string         // extracted response text
	Status     string         // "ok", "error", "rate_limited", etc.
	ParseError *string        // non-nil if parsing failed
	SessionID  *string        // session ID extracted from output (Claude)
	Metadata   map[string]any // includes "model_used", "tokens", "usage", etc.
}

// BuildResult normalizes raw subprocess output and parsed CLI output into
// a Result that matches the Elixir output.ex JSON contract.
//
// This implements the third clause of build_result/6 — the normal run path.
// The not_found and probe_failed cases are handled by NotFoundResult and
// ProbeFailedResult in result.go.
//
// Status priority:
//  1. TimedOut   → "timeout"  (overrides everything)
//  2. ExitSignal → "terminated"
//  3. Non-zero exit + parsed status "ok" → "error"
//  4. Otherwise  → parsed status as-is
func BuildResult(raw RawRunOutput, parsed ParsedOutput, fallbackModel string) *Result {
	// Compute status
	status := parsed.Status
	switch {
	case raw.TimedOut:
		status = "timeout"
	case raw.ExitSignal != nil:
		status = "terminated"
	case raw.ExitCode != nil && *raw.ExitCode != 0 && parsed.Status == "ok":
		status = "error"
	}

	// Compute response — timeout message overrides parsed response
	response := parsed.Response
	if raw.TimedOut {
		timeoutSeconds := max((raw.ElapsedMs+999)/1000, 1)
		response = fmt.Sprintf(
			"Request timed out after %ds. Retry with a longer timeout or resume the session.",
			timeoutSeconds,
		)
	}

	// Compute model — prefer parsed Metadata, fall back to request-level, then default
	var model string
	if m, ok := parsed.Metadata["model_used"].(string); ok && m != "" {
		model = m
	}
	if model == "" {
		model = fallbackModel
	}
	if model == "" {
		model = "cli-default"
	}

	// Clear parse_error on timeout — the response is synthetic, not parsed
	var parseError *string
	if !raw.TimedOut {
		parseError = parsed.ParseError
	}

	return &Result{
		Response:        response,
		Model:           model,
		Status:          status,
		ExitCode:        raw.ExitCode,
		ExitSignal:      raw.ExitSignal,
		Stderr:          raw.Stderr,
		ElapsedMs:       raw.ElapsedMs,
		ParseError:      parseError,
		Truncated:       raw.Truncated,
		StderrTruncated: raw.StderrTruncated,
		SessionID:       parsed.SessionID,
	}
}

// BuildMeta computes aggregate metadata across all results in a roundtable
// session. Ports build_meta/2 from output.ex.
//
// TotalElapsedMs is the max of all results' ElapsedMs (wall-clock of the
// slowest participant). FilesReferenced and DynamicFields (role names) are
// passed through as-is.
func BuildMeta(results map[string]*Result, files []string, roles map[string]string) Meta {
	var maxElapsed int64
	for _, r := range results {
		if r.ElapsedMs > maxElapsed {
			maxElapsed = r.ElapsedMs
		}
	}
	if files == nil {
		files = []string{}
	}
	return Meta{
		TotalElapsedMs:  maxElapsed,
		FilesReferenced: files,
		DynamicFields:   roles,
	}
}
