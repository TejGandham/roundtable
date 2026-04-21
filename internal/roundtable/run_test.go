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
	input := `[{"provider":"gemini","model":"pro"},{"provider":"codex","name":"my-codex","role":"planner"}]`
	specs, err := ParseAgents(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(specs))
	}
	if specs[0].Name != "gemini" || specs[0].Provider != "gemini" || specs[0].Model != "pro" {
		t.Errorf("spec[0] = %+v", specs[0])
	}
	if specs[1].Name != "my-codex" || specs[1].Provider != "codex" || specs[1].Role != "planner" {
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
	_, err := ParseAgents(`{"provider":"gemini"}`)
	if err == nil || !strings.Contains(err.Error(), "JSON array") {
		t.Fatalf("expected 'agents must be a JSON array', got %v", err)
	}
}

func TestParseAgentsEmptyArray(t *testing.T) {
	_, err := ParseAgents("[]")
	if err == nil || err.Error() != "agents list cannot be empty" {
		t.Fatalf("expected 'agents list cannot be empty', got %v", err)
	}
}

func TestParseAgentsMissingProvider(t *testing.T) {
	_, err := ParseAgents(`[{"name":"foo"}]`)
	if err == nil {
		t.Fatal("expected error for missing provider")
	}
}

// Unknown provider ids go through the dispatcher's not_found path (FR-10);
// ParseAgents must not reject them.
func TestParseAgents_AcceptsUnknownProvider(t *testing.T) {
	specs, err := ParseAgents(`[{"provider":"my-custom-one","model":"x"}]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(specs) != 1 || specs[0].Provider != "my-custom-one" {
		t.Errorf("specs = %+v", specs)
	}
}

func TestParseAgents_RejectsCLIField(t *testing.T) {
	_, err := ParseAgents(`[{"cli":"gemini"}]`)
	if err == nil || !strings.Contains(err.Error(), "cli") {
		t.Errorf("expected error mentioning cli, got: %v", err)
	}
}

func TestParseAgents_RejectsUnknownField(t *testing.T) {
	_, err := ParseAgents(`[{"provider":"x","bogus":"1"}]`)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("expected unknown-field error, got: %v", err)
	}
}

func TestParseAgentsDuplicateNames(t *testing.T) {
	_, err := ParseAgents(`[{"provider":"gemini"},{"provider":"gemini"}]`)
	if err == nil {
		t.Fatal("expected error for duplicate names")
	}
}

func TestParseAgentsReservedName(t *testing.T) {
	_, err := ParseAgents(`[{"provider":"gemini","name":"meta"}]`)
	if err == nil {
		t.Fatal("expected error for reserved name")
	}
}

func TestResolveRoleAgentOverride(t *testing.T) {
	agent := AgentSpec{Provider: "gemini", Role: "codereviewer"}
	req := ToolRequest{Role: "default", GeminiRole: "planner"}
	got := resolveRole(agent, req)
	if got != "codereviewer" {
		t.Errorf("resolveRole = %q, want codereviewer", got)
	}
}

func TestResolveRolePerCLI(t *testing.T) {
	agent := AgentSpec{Provider: "gemini"}
	req := ToolRequest{Role: "default", GeminiRole: "planner"}
	got := resolveRole(agent, req)
	if got != "planner" {
		t.Errorf("resolveRole = %q, want planner", got)
	}
}

func TestResolveRoleToolDefault(t *testing.T) {
	agent := AgentSpec{Provider: "gemini"}
	req := ToolRequest{Role: "codereviewer"}
	got := resolveRole(agent, req)
	if got != "codereviewer" {
		t.Errorf("resolveRole = %q, want codereviewer", got)
	}
}

func TestResolveRoleFallbackDefault(t *testing.T) {
	agent := AgentSpec{Provider: "gemini"}
	req := ToolRequest{}
	got := resolveRole(agent, req)
	if got != "default" {
		t.Errorf("resolveRole = %q, want default", got)
	}
}

func TestResolveModelAgentOverride(t *testing.T) {
	agent := AgentSpec{Provider: "gemini", Model: "pro-exp"}
	req := ToolRequest{GeminiModel: "pro"}
	got := resolveModel(agent, req)
	if got != "pro-exp" {
		t.Errorf("resolveModel = %q, want pro-exp", got)
	}
}

func TestResolveModelPerCLI(t *testing.T) {
	agent := AgentSpec{Provider: "codex"}
	req := ToolRequest{CodexModel: "gpt-5.4"}
	got := resolveModel(agent, req)
	if got != "gpt-5.4" {
		t.Errorf("resolveModel = %q, want gpt-5.4", got)
	}
}

func TestResolveResumeAgentOverride(t *testing.T) {
	agent := AgentSpec{Provider: "claude", Resume: "sess_abc"}
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

	for _, name := range []string{"gemini", "codex", "claude", "meta"} {
		if _, ok := result[name]; !ok {
			t.Errorf("missing key %q in result", name)
		}
	}

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
			{Name: "gemini", Provider: "gemini"},
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
		Agents:       []AgentSpec{{Name: "gemini", Provider: "gemini"}},
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
	t.Setenv("ROUNDTABLE_DEFAULT_AGENTS", `[{"provider":"gemini"}]`)

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

func TestResolveAgentsInvalidEnvFallsThrough(t *testing.T) {
	t.Setenv("ROUNDTABLE_DEFAULT_AGENTS", "not valid json")
	agents := resolveAgents(ToolRequest{})
	if len(agents) != 3 {
		t.Fatalf("expected 3 default agents, got %d", len(agents))
	}
}

func TestResolveAgentsExplicitOverEnv(t *testing.T) {
	t.Setenv("ROUNDTABLE_DEFAULT_AGENTS", `[{"provider":"codex"}]`)
	req := ToolRequest{
		Agents: []AgentSpec{{Name: "gemini", Provider: "gemini"}},
	}
	agents := resolveAgents(req)
	if len(agents) != 1 || agents[0].Provider != "gemini" {
		t.Fatalf("expected explicit gemini agent, got %v", agents)
	}
}

// Invariant: HTTP-native providers (any id registered via
// ROUNDTABLE_PROVIDERS) must be opt-in, never default. The default set
// contains only the three built-in subprocess backends. See the docstring
// on defaultAgents() in run.go for rationale. (FR-22)
func TestDefaultAgents_ExcludesAllHTTPProviders(t *testing.T) {
	builtins := map[string]bool{"gemini": true, "codex": true, "claude": true}
	got := defaultAgents()
	if len(got) != len(builtins) {
		t.Errorf("defaultAgents() len = %d, want %d", len(got), len(builtins))
	}
	for _, a := range got {
		if !builtins[a.Provider] {
			t.Errorf("defaultAgents() includes non-subprocess provider %q — invariant broken", a.Provider)
		}
	}
}

// NotFoundResult receives cfg.spec.Provider (the backend identifier), not
// cfg.spec.Name (the agent display name). For an agent
// {"provider":"ollama","name":"kimi"} the Stderr must name "ollama", not
// "kimi".
func TestRunMissingBackend_StderrMentionsProviderNotName(t *testing.T) {
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
		Agents: []AgentSpec{
			{Provider: "ollama", Name: "kimi", Model: "kimi-k2.6:cloud"},
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

	var r Result
	if err := json.Unmarshal(result["kimi"], &r); err != nil {
		t.Fatalf("unmarshal kimi: %v", err)
	}
	if r.Status != "not_found" {
		t.Errorf("status = %q, want not_found", r.Status)
	}
	if !strings.Contains(r.Stderr, "ollama") {
		t.Errorf("stderr = %q, want mention of 'ollama' (provider)", r.Stderr)
	}
	if strings.Contains(r.Stderr, "kimi") {
		t.Errorf("stderr = %q, should NOT mention 'kimi' (agent name, not provider)", r.Stderr)
	}
}
