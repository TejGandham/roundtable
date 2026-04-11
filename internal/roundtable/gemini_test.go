package roundtable

import (
	"strings"
	"testing"
)

func TestGeminiBuildArgs_Basic(t *testing.T) {
	args := geminiBuildArgs(Request{Prompt: "hello"})
	expected := []string{"-p", "hello", "-o", "json", "--yolo"}
	assertArgsEqual(t, args, expected)
}

func TestGeminiBuildArgs_WithModel(t *testing.T) {
	args := geminiBuildArgs(Request{Prompt: "hello", Model: "gemini-2.5-pro"})
	expected := []string{"-p", "hello", "-o", "json", "--yolo", "-m", "gemini-2.5-pro"}
	assertArgsEqual(t, args, expected)
}

func TestGeminiBuildArgs_WithResume(t *testing.T) {
	args := geminiBuildArgs(Request{Prompt: "hello", Resume: "sess_123"})
	// Resume comes first
	expected := []string{"--resume", "sess_123", "-p", "hello", "-o", "json", "--yolo"}
	assertArgsEqual(t, args, expected)
}

func TestGeminiBuildArgs_WithResumeAndModel(t *testing.T) {
	args := geminiBuildArgs(Request{Prompt: "hello", Resume: "sess_123", Model: "gemini-2.5-flash"})
	expected := []string{"--resume", "sess_123", "-p", "hello", "-o", "json", "--yolo", "-m", "gemini-2.5-flash"}
	assertArgsEqual(t, args, expected)
}

func TestGeminiParse_SuccessJSON(t *testing.T) {
	stdout := `{"response":"Analysis complete","stats":{"models":{"gemini-2.5-pro":{"tokens":1500}}},"session_id":"abc"}`
	parsed := geminiParseOutput(stdout, "")

	if parsed.Status != "ok" {
		t.Errorf("status = %q, want ok", parsed.Status)
	}
	if parsed.Response != "Analysis complete" {
		t.Errorf("response = %q", parsed.Response)
	}
	if parsed.Metadata["model_used"] != "gemini-2.5-pro" {
		t.Errorf("model_used = %v", parsed.Metadata["model_used"])
	}
	if parsed.Metadata["tokens"] == nil {
		t.Error("tokens should be present")
	}
	if parsed.SessionID == nil || *parsed.SessionID != "abc" {
		t.Errorf("session_id = %v, want abc", parsed.SessionID)
	}
}

func TestGeminiParse_StderrFallback(t *testing.T) {
	stderr := `{"response":"from stderr","session_id":"def"}`
	parsed := geminiParseOutput("not json", stderr)

	if parsed.Status != "ok" {
		t.Errorf("status = %q, want ok", parsed.Status)
	}
	if parsed.Response != "from stderr" {
		t.Errorf("response = %q", parsed.Response)
	}
}

func TestGeminiParse_ErrorResponse(t *testing.T) {
	stdout := `{"error":{"message":"Something broke","code":"INTERNAL"}}`
	parsed := geminiParseOutput(stdout, "")

	if parsed.Status != "error" {
		t.Errorf("status = %q, want error", parsed.Status)
	}
	if parsed.Response != "Something broke" {
		t.Errorf("response = %q", parsed.Response)
	}
}

func TestGeminiParse_RateLimited_429(t *testing.T) {
	stdout := `{"error":{"message":"Too many requests","code":429}}`
	parsed := geminiParseOutput(stdout, "")

	if parsed.Status != "rate_limited" {
		t.Errorf("status = %q, want rate_limited", parsed.Status)
	}
	if !strings.Contains(parsed.Response, "rate limited") {
		t.Errorf("response should contain rate limited message, got: %q", parsed.Response)
	}
}

func TestGeminiParse_RateLimited_ResourceExhausted(t *testing.T) {
	stdout := `{"error":{"message":"limit exceeded","status":"RESOURCE_EXHAUSTED"}}`
	parsed := geminiParseOutput(stdout, "")

	if parsed.Status != "rate_limited" {
		t.Errorf("status = %q, want rate_limited", parsed.Status)
	}
}

func TestGeminiParse_RateLimited_RawText(t *testing.T) {
	parsed := geminiParseOutput("Error 429: Too many requests", "")

	if parsed.Status != "rate_limited" {
		t.Errorf("status = %q, want rate_limited", parsed.Status)
	}
	if parsed.ParseError != nil {
		t.Error("parse_error should be nil for rate limited")
	}
}

func TestGeminiParse_RawTextError(t *testing.T) {
	parsed := geminiParseOutput("segfault", "")

	if parsed.Status != "error" {
		t.Errorf("status = %q, want error", parsed.Status)
	}
	if parsed.ParseError == nil || *parsed.ParseError != "JSON parse failed" {
		t.Errorf("parse_error = %v, want 'JSON parse failed'", parsed.ParseError)
	}
}

func TestGeminiParse_EmptyStdout_StderrRaw(t *testing.T) {
	parsed := geminiParseOutput("", "some stderr text")

	if parsed.Response != "some stderr text" {
		t.Errorf("response = %q, want stderr text", parsed.Response)
	}
}

func TestGeminiParse_EmptyResponse(t *testing.T) {
	stdout := `{"response":"","stats":{}}`
	parsed := geminiParseOutput(stdout, "")

	if parsed.Status != "ok" {
		t.Errorf("status = %q, want ok", parsed.Status)
	}
	if parsed.Response != "" {
		t.Errorf("response = %q, want empty", parsed.Response)
	}
}

func TestGeminiParse_NoModelsInStats(t *testing.T) {
	stdout := `{"response":"ok","stats":{"models":{}}}`
	parsed := geminiParseOutput(stdout, "")

	if parsed.Metadata != nil && parsed.Metadata["model_used"] != nil {
		t.Errorf("model_used should not be present when models map is empty")
	}
}

func TestGeminiParse_RateLimited_Quota(t *testing.T) {
	stdout := `{"error":{"message":"Quota exceeded for today"}}`
	parsed := geminiParseOutput(stdout, "")

	if parsed.Status != "rate_limited" {
		t.Errorf("status = %q, want rate_limited", parsed.Status)
	}
}

// assertArgsEqual is a test helper for comparing string slices.
func assertArgsEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("args length = %d, want %d\n  got:  %v\n  want: %v", len(got), len(want), got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q\n  got:  %v\n  want: %v", i, got[i], want[i], got, want)
			return
		}
	}
}
