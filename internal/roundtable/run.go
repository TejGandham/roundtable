package roundtable

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	// ProbeTimeout is the maximum time for a single backend health check.
	ProbeTimeout = 5 * time.Second

	// RunGrace is added to the tool timeout for the Run phase deadline.
	RunGrace = 30 * time.Second
)

// AgentSpec describes a single agent to dispatch to.
type AgentSpec struct {
	Name     string
	Provider string
	Model    string
	Role     string
	Resume   string
}

// ToolRequest is the structured input from the MCP tool handler.
// Maps to the Elixir args map that Roundtable.run/1 receives.
type ToolRequest struct {
	Prompt       string
	PromptSuffix string
	Files        []string
	Timeout      int

	Role string

	GeminiRole string
	CodexRole  string
	ClaudeRole string

	GeminiModel string
	CodexModel  string
	ClaudeModel string

	GeminiResume string
	CodexResume  string
	ClaudeResume string

	Agents []AgentSpec

	RolesDir        string
	ProjectRolesDir string
}

var reservedNames = map[string]bool{"meta": true}

// agentSpecJSON is the wire shape for one entry in the agents JSON array.
// DisallowUnknownFields on the decoder rejects legacy keys (like "cli") and
// typos at decode time.
type agentSpecJSON struct {
	Name     string `json:"name,omitempty"`
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
	Role     string `json:"role,omitempty"`
	Resume   string `json:"resume,omitempty"`
}

// ParseAgents parses and validates a JSON string of agent specs.
// Returns nil, nil for empty/nil input (use defaults).
func ParseAgents(agentsJSON string) ([]AgentSpec, error) {
	agentsJSON = strings.TrimSpace(agentsJSON)
	if agentsJSON == "" {
		return nil, nil
	}

	dec := json.NewDecoder(strings.NewReader(agentsJSON))
	dec.DisallowUnknownFields()

	var raw []agentSpecJSON
	if err := dec.Decode(&raw); err != nil {
		msg := err.Error()
		if strings.Contains(msg, `"cli"`) {
			return nil, fmt.Errorf(`agents: unknown field "cli"; use "provider"`)
		}
		if strings.Contains(msg, "unknown field") {
			return nil, fmt.Errorf("agents: %s", msg)
		}
		var single any
		if json.Unmarshal([]byte(agentsJSON), &single) == nil {
			if _, isArr := single.([]any); !isArr {
				return nil, fmt.Errorf("agents must be a JSON array")
			}
		}
		return nil, fmt.Errorf("agents is not valid JSON: %w", err)
	}

	if len(raw) == 0 {
		return nil, fmt.Errorf("agents list cannot be empty")
	}

	specs := make([]AgentSpec, 0, len(raw))
	names := make(map[string]bool, len(raw))

	for _, entry := range raw {
		if entry.Provider == "" {
			return nil, fmt.Errorf(`each agent must specify a "provider" field`)
		}

		name := entry.Name
		if name == "" {
			name = entry.Provider
		}
		if reservedNames[name] {
			return nil, fmt.Errorf("agent name %q is reserved", name)
		}
		if names[name] {
			return nil, fmt.Errorf("duplicate agent names: %s", name)
		}
		names[name] = true

		specs = append(specs, AgentSpec{
			Name:     name,
			Provider: entry.Provider,
			Model:    entry.Model,
			Role:     entry.Role,
			Resume:   entry.Resume,
		})
	}

	return specs, nil
}

// defaultAgents is the fan-out set for dispatches without explicit agents
// or ROUNDTABLE_DEFAULT_AGENTS override.
//
// Invariant: ONLY built-in subprocess backends (gemini/codex/claude) appear
// here. No HTTP-native provider (anything registered via ROUNDTABLE_PROVIDERS)
// is ever a default — callers must opt in explicitly via the agents JSON
// or ROUNDTABLE_DEFAULT_AGENTS. Codified as
// TestDefaultAgents_ExcludesAllHTTPProviders.
func defaultAgents() []AgentSpec {
	return []AgentSpec{
		{Name: "gemini", Provider: "gemini"},
		{Name: "codex", Provider: "codex"},
		{Name: "claude", Provider: "claude"},
	}
}

// resolveAgents determines which agents to dispatch to.
// Priority: explicit agents > ROUNDTABLE_DEFAULT_AGENTS env > default three.
func resolveAgents(req ToolRequest) []AgentSpec {
	if len(req.Agents) > 0 {
		return req.Agents
	}

	envValue := os.Getenv("ROUNDTABLE_DEFAULT_AGENTS")
	if envValue != "" {
		specs, err := ParseAgents(envValue)
		if err == nil && len(specs) > 0 {
			return specs
		}
	}

	return defaultAgents()
}

// resolveRole determines the effective role for a given agent.
// Priority: agent spec role > per-CLI role from tool > tool default role > "default"
func resolveRole(agent AgentSpec, req ToolRequest) string {
	if agent.Role != "" {
		return agent.Role
	}

	switch agent.Provider {
	case "gemini":
		if req.GeminiRole != "" {
			return req.GeminiRole
		}
	case "codex":
		if req.CodexRole != "" {
			return req.CodexRole
		}
	case "claude":
		if req.ClaudeRole != "" {
			return req.ClaudeRole
		}
	}

	if req.Role != "" {
		return req.Role
	}

	return "default"
}

// resolveModel determines the effective model for a given agent.
// Priority: agent spec model > per-CLI model from tool input
func resolveModel(agent AgentSpec, req ToolRequest) string {
	if agent.Model != "" {
		return agent.Model
	}

	switch agent.Provider {
	case "gemini":
		return req.GeminiModel
	case "codex":
		return req.CodexModel
	case "claude":
		return req.ClaudeModel
	}

	return ""
}

// resolveResume determines the effective resume/session ID for a given agent.
// Priority: agent spec resume > per-CLI resume from tool input
func resolveResume(agent AgentSpec, req ToolRequest) string {
	if agent.Resume != "" {
		return agent.Resume
	}

	switch agent.Provider {
	case "gemini":
		return req.GeminiResume
	case "codex":
		return req.CodexResume
	case "claude":
		return req.ClaudeResume
	}

	return ""
}

// Run executes a full roundtable dispatch cycle.
//
// It resolves agents, loads roles, assembles per-agent prompts, probes
// backends for health, dispatches in parallel, and returns the JSON-encoded
// DispatchResult.
//
// The backends map is keyed by provider id (subprocess: "gemini", "codex",
// "claude"; HTTP providers: operator-chosen ids from ROUNDTABLE_PROVIDERS).
// If a requested provider has no matching backend, the agent gets a
// not_found result.
func Run(ctx context.Context, req ToolRequest, backends map[string]Backend) ([]byte, error) {
	agents := resolveAgents(req)

	basePrompt := req.Prompt
	if req.PromptSuffix != "" {
		basePrompt += req.PromptSuffix
	}

	type agentConfig struct {
		spec    AgentSpec
		request Request
		backend Backend
		role    string
	}

	var configs []agentConfig
	roles := make(map[string]string)

	if len(agents) == 0 {
		return nil, fmt.Errorf("no agents to dispatch")
	}

	for _, agent := range agents {
		role := resolveRole(agent, req)
		model := resolveModel(agent, req)
		resume := resolveResume(agent, req)

		rolePrompt, err := LoadRolePrompt(role, req.RolesDir, req.ProjectRolesDir)
		if err != nil {
			return nil, fmt.Errorf("role %q for agent %q: %w", role, agent.Name, err)
		}

		assembledPrompt := AssemblePrompt(rolePrompt, basePrompt, req.Files)

		backend, ok := backends[agent.Provider]
		if !ok {
			backend = nil
		}

		timeout := req.Timeout
		if timeout <= 0 {
			timeout = DefaultTimeout
		}

		configs = append(configs, agentConfig{
			spec: agent,
			request: Request{
				Prompt:          assembledPrompt,
				Files:           req.Files,
				Timeout:         timeout,
				Role:            role,
				Model:           model,
				Resume:          resume,
				RolesDir:        req.RolesDir,
				ProjectRolesDir: req.ProjectRolesDir,
			},
			backend: backend,
			role:    role,
		})

		roles[agent.Name+"_role"] = role
	}

	type probeResult struct {
		index   int
		healthy bool
		err     error
	}

	probeCh := make(chan probeResult, len(configs))
	for i, cfg := range configs {
		go func(idx int, c agentConfig) {
			if c.backend == nil {
				probeCh <- probeResult{index: idx, healthy: false, err: fmt.Errorf("no backend for provider %q", c.spec.Provider)}
				return
			}
			probeCtx, cancel := context.WithTimeout(ctx, ProbeTimeout)
			defer cancel()
			err := c.backend.Healthy(probeCtx)
			probeCh <- probeResult{index: idx, healthy: err == nil, err: err}
		}(i, cfg)
	}

	probeResults := make([]probeResult, len(configs))
	for range configs {
		pr := <-probeCh
		probeResults[pr.index] = pr
	}

	results := make(map[string]*Result, len(configs))

	runDeadline := time.Duration(configs[0].request.Timeout)*time.Second + RunGrace
	runCtx, runCancel := context.WithTimeout(ctx, runDeadline)
	defer runCancel()

	type runResult struct {
		name   string
		result *Result
	}

	runCh := make(chan runResult, len(configs))
	runCount := 0

	for i, cfg := range configs {
		if !probeResults[i].healthy {
			if cfg.backend == nil {
				results[cfg.spec.Name] = NotFoundResult(cfg.spec.Provider, cfg.request.Model)
			} else {
				reason := "unknown"
				if probeResults[i].err != nil {
					reason = probeResults[i].err.Error()
				}
				results[cfg.spec.Name] = ProbeFailedResult(cfg.spec.Provider, cfg.request.Model, reason, nil)
			}
			continue
		}

		runCount++
		go func(c agentConfig) {
			defer func() {
				if r := recover(); r != nil {
					runCh <- runResult{
						name: c.spec.Name,
						result: &Result{
							Model:  c.request.Model,
							Status: "error",
							Stderr: fmt.Sprintf("backend panic: %v", r),
						},
					}
				}
			}()
			result, err := c.backend.Run(runCtx, c.request)
			if err != nil && result == nil {
				result = &Result{
					Model:  c.request.Model,
					Status: "error",
					Stderr: err.Error(),
				}
			}
			runCh <- runResult{name: c.spec.Name, result: result}
		}(cfg)
	}

	for range runCount {
		rr := <-runCh
		results[rr.name] = rr.result
	}

	dr := &DispatchResult{
		Results: results,
		Meta:    BuildMeta(results, req.Files, roles),
	}

	return json.Marshal(dr)
}
