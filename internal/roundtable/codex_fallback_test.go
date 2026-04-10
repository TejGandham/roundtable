package roundtable

import (
	"strings"
	"testing"
)

func TestCodexFallbackBuildArgs_Basic(t *testing.T) {
	args := codexFallbackBuildArgs(Request{Prompt: "hello"}, "", "")
	expected := []string{"exec", "--json", "--dangerously-bypass-approvals-and-sandbox", "hello"}
	assertArgsEqual(t, args, expected)
}

func TestCodexFallbackBuildArgs_WithModel(t *testing.T) {
	args := codexFallbackBuildArgs(Request{Prompt: "hello"}, "o3-mini", "")
	expected := []string{"exec", "--json", "--dangerously-bypass-approvals-and-sandbox", "-c", "model=o3-mini", "hello"}
	assertArgsEqual(t, args, expected)
}

func TestCodexFallbackBuildArgs_WithModelOverride(t *testing.T) {
	args := codexFallbackBuildArgs(Request{Prompt: "hello", Model: "o3-pro"}, "o3-mini", "")
	expected := []string{"exec", "--json", "--dangerously-bypass-approvals-and-sandbox", "-c", "model=o3-pro", "hello"}
	assertArgsEqual(t, args, expected)
}

func TestCodexFallbackBuildArgs_WithReasoning(t *testing.T) {
	args := codexFallbackBuildArgs(Request{Prompt: "hello"}, "o3-mini", "high")
	expected := []string{"exec", "--json", "--dangerously-bypass-approvals-and-sandbox", "-c", "model=o3-mini", "-c", "reasoning_effort=high", "hello"}
	assertArgsEqual(t, args, expected)
}

func TestCodexFallbackBuildArgs_ResumeLast(t *testing.T) {
	args := codexFallbackBuildArgs(Request{Prompt: "continue", Resume: "last"}, "", "")
	expected := []string{"exec", "--json", "--dangerously-bypass-approvals-and-sandbox", "resume", "--last", "continue"}
	assertArgsEqual(t, args, expected)
}

func TestCodexFallbackBuildArgs_ResumeSession(t *testing.T) {
	args := codexFallbackBuildArgs(Request{Prompt: "go", Resume: "sess_xyz"}, "", "")
	expected := []string{"exec", "--json", "--dangerously-bypass-approvals-and-sandbox", "resume", "sess_xyz", "go"}
	assertArgsEqual(t, args, expected)
}

func TestCodexFallbackBuildArgs_Full(t *testing.T) {
	args := codexFallbackBuildArgs(Request{Prompt: "go", Model: "o3-pro", Resume: "sess_1"}, "o3-mini", "medium")
	expected := []string{
		"exec", "--json", "--dangerously-bypass-approvals-and-sandbox",
		"-c", "model=o3-pro",
		"-c", "reasoning_effort=medium",
		"resume", "sess_1", "go",
	}
	assertArgsEqual(t, args, expected)
}

func TestCodexFallbackParse_SingleMessage(t *testing.T) {
	stdout := `{"type":"thread.started","thread_id":"t1"}
{"type":"item.completed","item":{"type":"agent_message","text":"Hello world"}}
{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":20}}`

	parsed := codexFallbackParseOutput(stdout)

	if parsed.Status != "ok" {
		t.Errorf("status = %q, want ok", parsed.Status)
	}
	if parsed.Response != "Hello world" {
		t.Errorf("response = %q, want 'Hello world'", parsed.Response)
	}
	if parsed.SessionID == nil || *parsed.SessionID != "t1" {
		t.Errorf("session_id = %v, want t1", parsed.SessionID)
	}
	if parsed.Metadata["usage"] == nil {
		t.Error("usage should be present in metadata")
	}
}

func TestCodexFallbackParse_MultipleMessages(t *testing.T) {
	stdout := `{"type":"item.completed","item":{"type":"agent_message","text":"First"}}
{"type":"item.completed","item":{"type":"agent_message","text":"Second"}}
{"type":"item.completed","item":{"type":"agent_message","text":"Third"}}`

	parsed := codexFallbackParseOutput(stdout)

	if parsed.Status != "ok" {
		t.Errorf("status = %q, want ok", parsed.Status)
	}
	// Messages in chronological order: First, Second, Third
	parts := strings.Split(parsed.Response, "\n\n")
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d: %v", len(parts), parts)
	}
	if parts[0] != "First" || parts[1] != "Second" || parts[2] != "Third" {
		t.Errorf("wrong order: %v, want [First, Second, Third]", parts)
	}
}

func TestCodexFallbackParse_ErrorEvent(t *testing.T) {
	stdout := `{"type":"error","message":"API key invalid"}
{"type":"error","message":"Auth failed"}`

	parsed := codexFallbackParseOutput(stdout)

	if parsed.Status != "error" {
		t.Errorf("status = %q, want error", parsed.Status)
	}
	// Errors in chronological order, joined with \n
	parts := strings.Split(parsed.Response, "\n")
	if len(parts) != 2 {
		t.Fatalf("expected 2 error parts, got %d", len(parts))
	}
	if parts[0] != "API key invalid" || parts[1] != "Auth failed" {
		t.Errorf("wrong error order: %v, want [API key invalid, Auth failed]", parts)
	}
}

func TestCodexFallbackParse_EmptyTextIgnored(t *testing.T) {
	stdout := `{"type":"item.completed","item":{"type":"agent_message","text":""}}
{"type":"item.completed","item":{"type":"agent_message","text":"  "}}
{"type":"item.completed","item":{"type":"agent_message","text":"actual"}}`

	parsed := codexFallbackParseOutput(stdout)

	if parsed.Response != "actual" {
		t.Errorf("response = %q, want 'actual' (empty messages filtered)", parsed.Response)
	}
}

func TestCodexFallbackParse_NonAgentMessageIgnored(t *testing.T) {
	stdout := `{"type":"item.completed","item":{"type":"tool_call","text":"should be ignored"}}
{"type":"item.completed","item":{"type":"agent_message","text":"included"}}`

	parsed := codexFallbackParseOutput(stdout)

	if parsed.Response != "included" {
		t.Errorf("response = %q, want 'included'", parsed.Response)
	}
}

func TestCodexFallbackParse_RawFallback(t *testing.T) {
	parsed := codexFallbackParseOutput("some non-json output")

	if parsed.Status != "error" {
		t.Errorf("status = %q, want error", parsed.Status)
	}
	if parsed.ParseError == nil || *parsed.ParseError != "No JSONL events found; using raw output" {
		t.Errorf("parse_error = %v", parsed.ParseError)
	}
	if parsed.Response != "some non-json output" {
		t.Errorf("response = %q", parsed.Response)
	}
}

func TestCodexFallbackParse_EmptyOutput(t *testing.T) {
	parsed := codexFallbackParseOutput("")

	if parsed.Status != "error" {
		t.Errorf("status = %q, want error", parsed.Status)
	}
	if parsed.ParseError == nil || *parsed.ParseError != "No output from codex" {
		t.Errorf("parse_error = %v", parsed.ParseError)
	}
}

func TestCodexFallbackParse_InvalidJSONLineSkipped(t *testing.T) {
	stdout := `{not valid json}
{"type":"item.completed","item":{"type":"agent_message","text":"valid"}}`

	parsed := codexFallbackParseOutput(stdout)

	if parsed.Status != "ok" {
		t.Errorf("status = %q, want ok", parsed.Status)
	}
	if parsed.Response != "valid" {
		t.Errorf("response = %q, want 'valid'", parsed.Response)
	}
}

func TestCodexFallbackParse_NonJSONLineSkipped(t *testing.T) {
	stdout := `Loading model...
{"type":"item.completed","item":{"type":"agent_message","text":"done"}}`

	parsed := codexFallbackParseOutput(stdout)

	if parsed.Status != "ok" {
		t.Errorf("status = %q, want ok", parsed.Status)
	}
	if parsed.Response != "done" {
		t.Errorf("response = %q, want 'done'", parsed.Response)
	}
}

func TestCodexFallbackParse_ThreadStartedCaptured(t *testing.T) {
	stdout := `{"type":"thread.started","thread_id":"thread_abc"}
{"type":"item.completed","item":{"type":"agent_message","text":"ok"}}
{"type":"turn.completed","usage":{"input_tokens":5}}`

	parsed := codexFallbackParseOutput(stdout)

	if parsed.SessionID == nil || *parsed.SessionID != "thread_abc" {
		t.Errorf("session_id = %v, want thread_abc", parsed.SessionID)
	}
}

func TestCodexFallbackParse_MessagesWinOverErrors(t *testing.T) {
	// When both messages and errors exist, messages take precedence
	stdout := `{"type":"error","message":"warning"}
{"type":"item.completed","item":{"type":"agent_message","text":"response"}}`

	parsed := codexFallbackParseOutput(stdout)

	if parsed.Status != "ok" {
		t.Errorf("status = %q, want ok (messages win)", parsed.Status)
	}
	if parsed.Response != "response" {
		t.Errorf("response = %q, want 'response'", parsed.Response)
	}
}

func TestCodexFallbackParse_UsageInMetadata(t *testing.T) {
	stdout := `{"type":"item.completed","item":{"type":"agent_message","text":"ok"}}
{"type":"turn.completed","usage":{"input_tokens":100,"output_tokens":50}}`

	parsed := codexFallbackParseOutput(stdout)

	usage := parsed.Metadata["usage"]
	if usage == nil {
		t.Fatal("usage should be present")
	}
	usageMap, ok := usage.(map[string]any)
	if !ok {
		t.Fatalf("usage type = %T, want map[string]any", usage)
	}
	if usageMap["input_tokens"] != float64(100) {
		t.Errorf("input_tokens = %v", usageMap["input_tokens"])
	}
}
