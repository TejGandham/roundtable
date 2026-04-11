package roundtable

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseAgentsEmpty(t *testing.T) {
	specs, err := ParseAgents("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if specs != nil {
		t.Fatalf("expected nil, got %v", specs)
	}
}

func TestParseAgentsValid(t *testing.T) {
	input := `[{"cli":"gemini","model":"pro"},{"cli":"codex","name":"my-codex","role":"planner"}]`
	specs, err := ParseAgents(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(specs))
	}
	if specs[0].Name != "gemini" || specs[0].CLI != "gemini" || specs[0].Model != "pro" {
		t.Errorf("spec[0] = %+v", specs[0])
	}
	if specs[1].Name != "my-codex" || specs[1].CLI != "codex" || specs[1].Role != "planner" {
		t.Errorf("spec[1] = %+v", specs[1])
	}
}

func TestParseAgentsInvalidJSON(t *testing.T) {
	_, err := ParseAgents("not json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseAgentsNotArray(t *testing.T) {
	_, err := ParseAgents(`{"cli":"gemini"}`)
	if err == nil || err.Error() != "agents must be a JSON array" {
		t.Fatalf("expected 'agents must be a JSON array', got %v", err)
	}
}

func TestParseAgentsEmptyArray(t *testing.T) {
	_, err := ParseAgents("[]")
	if err == nil || err.Error() != "agents list cannot be empty" {
		t.Fatalf("expected 'agents list cannot be empty', got %v", err)
	}
}

func TestParseAgentsMissingCLI(t *testing.T) {
	_, err := ParseAgents(`[{"name":"foo"}]`)
	if err == nil {
		t.Fatal("expected error for missing cli")
	}
}

func TestParseAgentsUnknownCLI(t *testing.T) {
	_, err := ParseAgents(`[{"cli":"gpt"}]`)
	if err == nil {
		t.Fatal("expected error for unknown CLI")
	}
}

func TestParseAgentsDuplicateNames(t *testing.T) {
	_, err := ParseAgents(`[{"cli":"gemini"},{"cli":"gemini"}]`)
	if err == nil {
		t.Fatal("expected error for duplicate names")
	}
}

func TestParseAgentsReservedName(t *testing.T) {
	_, err := ParseAgents(`[{"cli":"gemini","name":"meta"}]`)
	if err == nil {
		t.Fatal("expected error for reserved name")
	}
}

func TestResolveRoleAgentOverride(t *testing.T) {
	agent := AgentSpec{CLI: "gemini", Role: "codereviewer"}
	req := ToolRequest{Role: "default", GeminiRole: "planner"}
	got := resolveRole(agent, req)
	if got != "codereviewer" {
		t.Errorf("resolveRole = %q, want codereviewer", got)
	}
}

func TestResolveRolePerCLI(t *testing.T) {
	agent := AgentSpec{CLI: "gemini"}
	req := ToolRequest{Role: "default", GeminiRole: "planner"}
	got := resolveRole(agent, req)
	if got != "planner" {
		t.Errorf("resolveRole = %q, want planner", got)
	}
}

func TestResolveRoleToolDefault(t *testing.T) {
	agent := AgentSpec{CLI: "gemini"}
	req := ToolRequest{Role: "codereviewer"}
	got := resolveRole(agent, req)
	if got != "codereviewer" {
		t.Errorf("resolveRole = %q, want codereviewer", got)
	}
}

func TestResolveRoleFallbackDefault(t *testing.T) {
	agent := AgentSpec{CLI: "gemini"}
	req := ToolRequest{}
	got := resolveRole(agent, req)
	if got != "default" {
		t.Errorf("resolveRole = %q, want default", got)
	}
}

func TestResolveModelAgentOverride(t *testing.T) {
	agent := AgentSpec{CLI: "gemini", Model: "pro-exp"}
	req := ToolRequest{GeminiModel: "pro"}
	got := resolveModel(agent, req)
	if got != "pro-exp" {
		t.Errorf("resolveModel = %q, want pro-exp", got)
	}
}

func TestResolveModelPerCLI(t *testing.T) {
	agent := AgentSpec{CLI: "codex"}
	req := ToolRequest{CodexModel: "gpt-5.4"}
	got := resolveModel(agent, req)
	if got != "gpt-5.4" {
		t.Errorf("resolveModel = %q, want gpt-5.4", got)
	}
}

func TestResolveResumeAgentOverride(t *testing.T) {
	agent := AgentSpec{CLI: "claude", Resume: "sess_abc"}
	req := ToolRequest{ClaudeResume: "sess_old"}
	got := resolveResume(agent, req)
	if got != "sess_abc" {
		t.Errorf("resolveResume = %q, want sess_abc", got)
	}
}

func TestRunDefaultAgents(t *testing.T) {
	gemini := &mockBackend{
		name:      "gemini",
		runResult: &Result{Model: "gemini-pro", Status: "ok", Response: "hello from gemini"},
	}
	codex := &mockBackend{
		name:      "codex",
		runResult: &Result{Model: "gpt-4", Status: "ok", Response: "hello from codex"},
	}
	claude := &mockBackend{
		name:      "claude",
		runResult: &Result{Model: "sonnet", Status: "ok", Response: "hello from claude"},
	}

	backends := map[string]Backend{
		"gemini": gemini,
		"codex":  codex,
		"claude": claude,
	}

	req := ToolRequest{
		Prompt:  "test prompt",
		Role:    "default",
		Timeout: 10,
	}

	data, err := Run(context.Background(), req, backends)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	var result map[string]json.RawMessage
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Should have gemini, codex, claude, and meta
	for _, name := range []string{"gemini", "codex", "claude", "meta"} {
		if _, ok := result[name]; !ok {
			t.Errorf("missing key %q in result", name)
		}
	}

	// Verify each backend was called
	if gemini.runCalls.Load() != 1 {
		t.Errorf("gemini run calls = %d, want 1", gemini.runCalls.Load())
	}
	if codex.runCalls.Load() != 1 {
		t.Errorf("codex run calls = %d, want 1", codex.runCalls.Load())
	}
	if claude.runCalls.Load() != 1 {
		t.Errorf("claude run calls = %d, want 1", claude.runCalls.Load())
	}
}

func TestRunCustomAgents(t *testing.T) {
	gemini := &mockBackend{
		name:      "gemini",
		runResult: &Result{Model: "gemini-pro", Status: "ok", Response: "hello"},
	}

	backends := map[string]Backend{
		"gemini": gemini,
		"codex":  &mockBackend{name: "codex", runResult: &Result{Status: "ok"}},
		"claude": &mockBackend{name: "claude", runResult: &Result{Status: "ok"}},
	}

	req := ToolRequest{
		Prompt:  "test",
		Role:    "default",
		Timeout: 10,
		Agents: []AgentSpec{
			{Name: "gemini", CLI: "gemini"},
		},
	}

	data, err := Run(context.Background(), req, backends)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	var result map[string]json.RawMessage
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Should only have gemini + meta (not codex, claude)
	if _, ok := result["gemini"]; !ok {
		t.Error("missing gemini in result")
	}
	if _, ok := result["codex"]; ok {
		t.Error("unexpected codex in result — custom agents should only include gemini")
	}
	if _, ok := result["claude"]; ok {
		t.Error("unexpected claude in result — custom agents should only include gemini")
	}
}

func TestRunPromptSuffix(t *testing.T) {
	var capturedPrompt string
	gemini := &mockBackend{
		name:      "gemini",
		runResult: &Result{Status: "ok"},
	}

	// Override Run to capture the prompt
	captureBackend := &promptCaptureBackend{
		Backend:  gemini,
		captured: &capturedPrompt,
	}

	backends := map[string]Backend{
		"gemini": captureBackend,
		"codex":  &mockBackend{name: "codex", runResult: &Result{Status: "ok"}},
		"claude": &mockBackend{name: "claude", runResult: &Result{Status: "ok"}},
	}

	req := ToolRequest{
		Prompt:       "Analyze this",
		PromptSuffix: "\n\nProvide conclusions.",
		Role:         "default",
		Timeout:      10,
		Agents:       []AgentSpec{{Name: "gemini", CLI: "gemini"}},
	}

	_, err := Run(context.Background(), req, backends)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if capturedPrompt == "" {
		t.Fatal("prompt was not captured")
	}
	if !strings.Contains(capturedPrompt, "Analyze this\n\nProvide conclusions.") {
		t.Errorf("prompt does not contain suffix: %s", capturedPrompt)
	}
}

// promptCaptureBackend wraps a Backend and captures the prompt from Run.
type promptCaptureBackend struct {
	Backend
	captured *string
}

func (p *promptCaptureBackend) Run(ctx context.Context, req Request) (*Result, error) {
	*p.captured = req.Prompt
	return p.Backend.Run(ctx, req)
}

func (p *promptCaptureBackend) Healthy(ctx context.Context) error {
	return p.Backend.Healthy(ctx)
}

func (p *promptCaptureBackend) Name() string {
	return p.Backend.Name()
}

func TestRunMissingBackend(t *testing.T) {
	// Only gemini backend registered, but default agents include codex and claude
	backends := map[string]Backend{
		"gemini": &mockBackend{
			name:      "gemini",
			runResult: &Result{Model: "pro", Status: "ok", Response: "hello"},
		},
	}

	req := ToolRequest{
		Prompt:  "test",
		Role:    "default",
		Timeout: 10,
	}

	data, err := Run(context.Background(), req, backends)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	var result map[string]json.RawMessage
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// codex and claude should have not_found status
	for _, name := range []string{"codex", "claude"} {
		var r Result
		if err := json.Unmarshal(result[name], &r); err != nil {
			t.Fatalf("unmarshal %s: %v", name, err)
		}
		if r.Status != "not_found" {
			t.Errorf("%s status = %q, want not_found", name, r.Status)
		}
	}
}

func TestRunDefaultAgentsEnv(t *testing.T) {
	t.Setenv("ROUNDTABLE_DEFAULT_AGENTS", `[{"cli":"gemini"}]`)

	gemini := &mockBackend{
		name:      "gemini",
		runResult: &Result{Model: "pro", Status: "ok"},
	}
	codex := &mockBackend{
		name:      "codex",
		runResult: &Result{Status: "ok"},
	}

	backends := map[string]Backend{
		"gemini": gemini,
		"codex":  codex,
		"claude": &mockBackend{name: "claude", runResult: &Result{Status: "ok"}},
	}

	req := ToolRequest{
		Prompt:  "test",
		Role:    "default",
		Timeout: 10,
	}

	data, err := Run(context.Background(), req, backends)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	var result map[string]json.RawMessage
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := result["gemini"]; !ok {
		t.Error("missing gemini")
	}
	// codex should NOT be present since env only specifies gemini
	if _, ok := result["codex"]; ok {
		t.Error("unexpected codex — env agents should only include gemini")
	}

	if gemini.runCalls.Load() != 1 {
		t.Errorf("gemini run calls = %d, want 1", gemini.runCalls.Load())
	}
	if codex.runCalls.Load() != 0 {
		t.Errorf("codex run calls = %d, want 0", codex.runCalls.Load())
	}
}

func TestRunXrayRoles(t *testing.T) {
	var geminiPrompt, codexPrompt, claudePrompt string

	gemini := &promptCaptureBackend{
		Backend:  &mockBackend{name: "gemini", runResult: &Result{Status: "ok"}},
		captured: &geminiPrompt,
	}
	codex := &promptCaptureBackend{
		Backend:  &mockBackend{name: "codex", runResult: &Result{Status: "ok"}},
		captured: &codexPrompt,
	}
	claude := &promptCaptureBackend{
		Backend:  &mockBackend{name: "claude", runResult: &Result{Status: "ok"}},
		captured: &claudePrompt,
	}

	backends := map[string]Backend{
		"gemini": gemini,
		"codex":  codex,
		"claude": claude,
	}

	// xray tool spec: gemini=planner, codex=codereviewer, claude=default
	req := ToolRequest{
		Prompt:     "Inspect this",
		GeminiRole: "planner",
		CodexRole:  "codereviewer",
		ClaudeRole: "default",
		Timeout:    10,
	}

	_, err := Run(context.Background(), req, backends)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// Each backend should have received a different assembled prompt
	// (because different roles have different role prompts)
	// We just verify that prompts were captured and contain the request
	for name, prompt := range map[string]string{
		"gemini": geminiPrompt,
		"codex":  codexPrompt,
		"claude": claudePrompt,
	} {
		if prompt == "" {
			t.Errorf("%s prompt was empty", name)
			continue
		}
		if !strings.Contains(prompt, "Inspect this") {
			t.Errorf("%s prompt does not contain user request: %s", name, prompt)
		}
	}
}

// Verify that resolveAgents falls through to defaults when env is set but invalid.
func TestResolveAgentsInvalidEnvFallsThrough(t *testing.T) {
	t.Setenv("ROUNDTABLE_DEFAULT_AGENTS", "not valid json")
	agents := resolveAgents(ToolRequest{})
	if len(agents) != 3 {
		t.Fatalf("expected 3 default agents, got %d", len(agents))
	}
}

// Verify that resolveAgents uses explicit agents over env.
func TestResolveAgentsExplicitOverEnv(t *testing.T) {
	t.Setenv("ROUNDTABLE_DEFAULT_AGENTS", `[{"cli":"codex"}]`)
	req := ToolRequest{
		Agents: []AgentSpec{{Name: "gemini", CLI: "gemini"}},
	}
	agents := resolveAgents(req)
	if len(agents) != 1 || agents[0].CLI != "gemini" {
		t.Fatalf("expected explicit gemini agent, got %v", agents)
	}
}
