package roundtable

import (
	"testing"
)

func TestClaudeBuildArgs_Basic(t *testing.T) {
	args := claudeBuildArgs(Request{Prompt: "hello world"})
	expected := []string{"-p", "--output-format", "json", "--dangerously-skip-permissions", "hello world"}
	assertArgsEqual(t, args, expected)
}

func TestClaudeBuildArgs_WithModel(t *testing.T) {
	args := claudeBuildArgs(Request{Prompt: "test", Model: "claude-sonnet-4-20250514"})
	expected := []string{"-p", "--output-format", "json", "--dangerously-skip-permissions", "--model", "claude-sonnet-4-20250514", "test"}
	assertArgsEqual(t, args, expected)
}

func TestClaudeBuildArgs_WithResume(t *testing.T) {
	args := claudeBuildArgs(Request{Prompt: "continue", Resume: "sess_abc"})
	expected := []string{"-p", "--output-format", "json", "--dangerously-skip-permissions", "-r", "sess_abc", "continue"}
	assertArgsEqual(t, args, expected)
}

func TestClaudeBuildArgs_WithModelAndResume(t *testing.T) {
	args := claudeBuildArgs(Request{Prompt: "go", Model: "opus", Resume: "last"})
	expected := []string{"-p", "--output-format", "json", "--dangerously-skip-permissions", "--model", "opus", "-r", "last", "go"}
	assertArgsEqual(t, args, expected)
}

func TestClaudeParse_Success(t *testing.T) {
	stdout := `{"result":"Code looks good.","session_id":"s1","modelUsage":{"claude-sonnet-4-20250514":{}}}`
	parsed := claudeParseOutput(stdout)

	if parsed.Status != "ok" {
		t.Errorf("status = %q, want ok", parsed.Status)
	}
	if parsed.Response != "Code looks good." {
		t.Errorf("response = %q", parsed.Response)
	}
	if parsed.SessionID == nil || *parsed.SessionID != "s1" {
		t.Errorf("session_id = %v, want s1", parsed.SessionID)
	}
	if parsed.Metadata["model_used"] != "claude-sonnet-4-20250514" {
		t.Errorf("model_used = %v", parsed.Metadata["model_used"])
	}
}

func TestClaudeParse_IsError(t *testing.T) {
	stdout := `{"is_error":true,"result":"Auth failed","session_id":"s2"}`
	parsed := claudeParseOutput(stdout)

	if parsed.Status != "error" {
		t.Errorf("status = %q, want error", parsed.Status)
	}
	if parsed.Response != "Auth failed" {
		t.Errorf("response = %q", parsed.Response)
	}
	if parsed.SessionID == nil || *parsed.SessionID != "s2" {
		t.Errorf("session_id = %v, want s2", parsed.SessionID)
	}
}

func TestClaudeParse_JSONFailed(t *testing.T) {
	parsed := claudeParseOutput("not json at all")

	if parsed.Status != "error" {
		t.Errorf("status = %q, want error", parsed.Status)
	}
	if parsed.ParseError == nil || *parsed.ParseError != "JSON parse failed" {
		t.Errorf("parse_error = %v, want 'JSON parse failed'", parsed.ParseError)
	}
	if parsed.Response != "not json at all" {
		t.Errorf("response = %q, want raw stdout", parsed.Response)
	}
}

func TestClaudeParse_EmptyStdout(t *testing.T) {
	parsed := claudeParseOutput("")

	if parsed.Status != "error" {
		t.Errorf("status = %q, want error", parsed.Status)
	}
}

func TestClaudeParse_ANSIStrip(t *testing.T) {
	// Simulate ANSI-escaped model name in modelUsage key
	stdout := `{"result":"ok","modelUsage":{"\u001b[1mclaude-sonnet-4-20250514\u001b[0m":{}}}`
	parsed := claudeParseOutput(stdout)

	if parsed.Metadata == nil {
		t.Fatal("metadata should not be nil")
	}
	model := parsed.Metadata["model_used"].(string)
	if model != "claude-sonnet-4-20250514" {
		t.Errorf("model_used = %q, want claude-sonnet-4-20250514 (ANSI stripped)", model)
	}
}

func TestClaudeParse_BracketArtifact(t *testing.T) {
	// Residual bracket artifact: [1m] at the end
	stdout := `{"result":"ok","modelUsage":{"claude-sonnet-4-20250514[1m]":{}}}`
	parsed := claudeParseOutput(stdout)

	model := parsed.Metadata["model_used"].(string)
	if model != "claude-sonnet-4-20250514" {
		t.Errorf("model_used = %q, want claude-sonnet-4-20250514 (bracket stripped)", model)
	}
}

func TestClaudeParse_NoModelUsage(t *testing.T) {
	stdout := `{"result":"done"}`
	parsed := claudeParseOutput(stdout)

	if parsed.Metadata != nil {
		t.Errorf("metadata = %v, want nil (no modelUsage)", parsed.Metadata)
	}
}

func TestClaudeParse_EmptyModelUsage(t *testing.T) {
	stdout := `{"result":"done","modelUsage":{}}`
	parsed := claudeParseOutput(stdout)

	if parsed.Metadata != nil {
		t.Errorf("metadata = %v, want nil (empty modelUsage)", parsed.Metadata)
	}
}

func TestClaudeParse_NilResult(t *testing.T) {
	stdout := `{"session_id":"s3"}`
	parsed := claudeParseOutput(stdout)

	if parsed.Status != "ok" {
		t.Errorf("status = %q, want ok", parsed.Status)
	}
	if parsed.Response != "" {
		t.Errorf("response = %q, want empty (nil result)", parsed.Response)
	}
}

func TestStripANSI(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"\x1b[1mhello\x1b[0m", "hello"},
		{"\x1b[31;1mbold red\x1b[0m", "bold red"},
		{"no ansi", "no ansi"},
		{"\x1b[Kclear line", "clear line"},
		{"\x1b[Hmove home", "move home"},
		{"model[1m]", "model"},
		{"", ""},
	}

	for _, tt := range tests {
		got := stripANSI(tt.input)
		if got != tt.want {
			t.Errorf("stripANSI(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
