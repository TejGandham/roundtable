package roundtable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFullPromptPipeline simulates a complete request: load role, assemble
// prompt with file references, then build a normalized result.
func TestFullPromptPipeline(t *testing.T) {
	// Set up a temp file to reference
	dir := t.TempDir()
	codePath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(codePath, []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Step 1: Load role from embedded defaults
	rolePrompt, err := LoadRolePrompt("default", "", "")
	if err != nil {
		t.Fatalf("LoadRolePrompt: %v", err)
	}

	// Step 2: Assemble prompt
	prompt := AssemblePrompt(rolePrompt, "Review this Go code for correctness.", []string{codePath})

	// Verify structure
	if !strings.Contains(prompt, "senior software engineer") {
		t.Error("prompt missing role content")
	}
	if !strings.Contains(prompt, "=== REQUEST ===") {
		t.Error("prompt missing request section")
	}
	if !strings.Contains(prompt, "=== FILES ===") {
		t.Error("prompt missing files section")
	}
	if !strings.Contains(prompt, "main.go") {
		t.Error("prompt missing file reference")
	}

	// Step 3: Simulate subprocess output and build result
	raw := RawRunOutput{
		Stdout:    []byte(`{"response":"Code looks good."}`),
		ExitCode:  intPtr(0),
		ElapsedMs: 4500,
	}
	parsed := ParsedOutput{
		Response: "Code looks good. No issues found.",
		Status:   "ok",
		Metadata: map[string]any{"model_used": "gemini-2.5-pro"},
	}

	result := BuildResult(raw, parsed, "")

	if result.Status != "ok" {
		t.Errorf("result.status = %q, want ok", result.Status)
	}
	if result.Model != "gemini-2.5-pro" {
		t.Errorf("result.model = %q, want gemini-2.5-pro", result.Model)
	}
	if result.Response != "Code looks good. No issues found." {
		t.Errorf("result.response = %q", result.Response)
	}
}

// TestProjectRoleOverridePipeline verifies that a project-level role
// override flows through to the assembled prompt.
func TestProjectRoleOverridePipeline(t *testing.T) {
	projectDir := t.TempDir()
	customRole := "You are a security auditor. Focus exclusively on vulnerabilities."
	if err := os.WriteFile(filepath.Join(projectDir, "default.txt"), []byte(customRole), 0644); err != nil {
		t.Fatal(err)
	}

	rolePrompt, err := LoadRolePrompt("default", "", projectDir)
	if err != nil {
		t.Fatalf("LoadRolePrompt: %v", err)
	}

	prompt := AssemblePrompt(rolePrompt, "Check auth handling.", nil)

	if !strings.Contains(prompt, "security auditor") {
		t.Error("expected project role override in prompt")
	}
	if strings.Contains(prompt, "senior software engineer") {
		t.Error("default embedded role should be overridden")
	}
}

// TestTimeoutResultPipeline verifies the timeout path end-to-end.
func TestTimeoutResultPipeline(t *testing.T) {
	// Load and assemble (lightweight — just verifying the seam)
	_, err := LoadRolePrompt("planner", "", "")
	if err != nil {
		t.Fatalf("LoadRolePrompt: %v", err)
	}

	// Simulate a timed-out subprocess
	raw := RawRunOutput{
		TimedOut:  true,
		ElapsedMs: 900000,
		Stderr:    "context deadline exceeded",
	}
	parsed := ParsedOutput{
		Response: "",
		Status:   "ok",
	}

	result := BuildResult(raw, parsed, "gemini-2.5-pro")

	if result.Status != "timeout" {
		t.Errorf("status = %q, want timeout", result.Status)
	}
	if !strings.Contains(result.Response, "timed out") {
		t.Error("expected timeout message in response")
	}
	if result.Model != "gemini-2.5-pro" {
		t.Errorf("model = %q, want gemini-2.5-pro", result.Model)
	}
}
