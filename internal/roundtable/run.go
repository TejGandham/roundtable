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
	Name   string // display name, defaults to CLI name
	CLI    string // "gemini", "codex", or "claude"
	Model  string // per-agent model override
	Role   string // per-agent role override
	Resume string // per-agent resume/session ID
}

// ToolRequest is the structured input from the MCP tool handler.
// Maps to the Elixir args map that Roundtable.run/1 receives.
type ToolRequest struct {
	Prompt       string
	PromptSuffix string // tool-specific suffix (deepdive, architect, challenge)
	Files        []string
	Timeout      int

	// Default role for all backends (from ToolSpec.Role)
	Role string

	// Per-CLI role overrides (from ToolSpec, e.g. xray)
	GeminiRole string
	CodexRole  string
	ClaudeRole string

	// Per-CLI model overrides (from ToolInput)
	GeminiModel string
	CodexModel  string
	ClaudeModel string

	// Per-CLI resume session IDs (from ToolInput)
	GeminiResume string
	CodexResume  string
	ClaudeResume string

	// Custom agents (parsed from ToolInput.Agents JSON)
	Agents []AgentSpec

	// Role directories
	RolesDir        string
	ProjectRolesDir string
}

var validCLIs = map[string]bool{"gemini": true, "codex": true, "claude": true}
var reservedNames = map[string]bool{"meta": true}

// ParseAgents parses and validates a JSON string of agent specs.
// Returns nil, nil for empty/nil input (use defaults).
// Matches common.ex parse_agents/1 + validate_agents/1.
func ParseAgents(agentsJSON string) ([]AgentSpec, error) {
	agentsJSON = strings.TrimSpace(agentsJSON)
	if agentsJSON == "" {
		return nil, nil
	}

	var raw []map[string]any
	if err := json.Unmarshal([]byte(agentsJSON), &raw); err != nil {
		// Check if it parsed as non-array
		var single any
		if json.Unmarshal([]byte(agentsJSON), &single) == nil {
			return nil, fmt.Errorf("agents must be a JSON array")
		}
		return nil, fmt.Errorf("agents is not valid JSON")
	}

	if len(raw) == 0 {
		return nil, fmt.Errorf("agents list cannot be empty")
	}

	specs := make([]AgentSpec, 0, len(raw))
	names := make(map[string]bool, len(raw))

	for _, entry := range raw {
		cli, _ := entry["cli"].(string)
		if cli == "" {
			return nil, fmt.Errorf("each agent must specify a \"cli\" field")
		}
		if !validCLIs[cli] {
			return nil, fmt.Errorf("unknown CLI type: %s. Valid types: gemini, codex, claude", cli)
		}

		name, _ := entry["name"].(string)
		if name == "" {
			name = cli
		}

		if reservedNames[name] {
			return nil, fmt.Errorf("agent name %q is reserved", name)
		}
		if names[name] {
			return nil, fmt.Errorf("duplicate agent names: %s", name)
		}
		names[name] = true

		model, _ := entry["model"].(string)
		role, _ := entry["role"].(string)
		resume, _ := entry["resume"].(string)

		specs = append(specs, AgentSpec{
			Name:   name,
			CLI:    cli,
			Model:  model,
			Role:   role,
			Resume: resume,
		})
	}

	return specs, nil
}

func defaultAgents() []AgentSpec {
	return []AgentSpec{
		{Name: "gemini", CLI: "gemini"},
		{Name: "codex", CLI: "codex"},
		{Name: "claude", CLI: "claude"},
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
		// Invalid env value: fall through to defaults (matches Elixir behavior)
	}

	return defaultAgents()
}

// resolveRole determines the effective role for a given agent.
// Priority: agent spec role > per-CLI role from tool > tool default role > "default"
func resolveRole(agent AgentSpec, req ToolRequest) string {
	if agent.Role != "" {
		return agent.Role
	}

	switch agent.CLI {
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

	switch agent.CLI {
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

	switch agent.CLI {
	case "gemini":
		return req.GeminiResume
	case "codex":
		return req.CodexResume
	case "claude":
		return req.ClaudeResume
	}

	return ""
}

// Run executes a full roundtable dispatch cycle. This is the native Go
// replacement for the roundtable-cli escript.
//
// It resolves agents, loads roles, assembles per-agent prompts, probes
// backends for health, dispatches in parallel, and returns the JSON-encoded
// DispatchResult.
//
// The backends map is keyed by CLI name ("gemini", "codex", "claude").
// If a requested CLI has no matching backend, it gets a not_found result.
func Run(ctx context.Context, req ToolRequest, backends map[string]Backend) ([]byte, error) {
	agents := resolveAgents(req)

	// Build the effective prompt (with suffix applied)
	basePrompt := req.Prompt
	if req.PromptSuffix != "" {
		basePrompt += req.PromptSuffix
	}

	// Build per-agent configs: resolve role, load prompt, create Request
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

		// Load role prompt
		rolePrompt, err := LoadRolePrompt(role, req.RolesDir, req.ProjectRolesDir)
		if err != nil {
			return nil, fmt.Errorf("role %q for agent %q: %w", role, agent.Name, err)
		}

		// Assemble full prompt
		assembledPrompt := AssemblePrompt(rolePrompt, basePrompt, req.Files)

		// Find backend for this agent's CLI
		backend, ok := backends[agent.CLI]
		if !ok {
			// No backend registered for this CLI — will get not_found result
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

	// Phase 1: Parallel health probes
	type probeResult struct {
		index   int
		healthy bool
		err     error
	}

	probeCh := make(chan probeResult, len(configs))
	for i, cfg := range configs {
		go func(idx int, c agentConfig) {
			if c.backend == nil {
				probeCh <- probeResult{index: idx, healthy: false, err: fmt.Errorf("no backend for CLI %q", c.spec.CLI)}
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

	// Phase 2: Parallel Run for healthy backends
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
			// Record probe failure or not_found
			if cfg.backend == nil {
				results[cfg.spec.Name] = NotFoundResult(cfg.spec.Name, cfg.request.Model)
			} else {
				reason := "unknown"
				if probeResults[i].err != nil {
					reason = probeResults[i].err.Error()
				}
				results[cfg.spec.Name] = ProbeFailedResult(cfg.spec.Name, cfg.request.Model, reason, nil)
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

	// Build dispatch result
	dr := &DispatchResult{
		Results: results,
		Meta:    BuildMeta(results, req.Files, roles),
	}

	return json.Marshal(dr)
}
