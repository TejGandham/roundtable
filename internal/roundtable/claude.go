package roundtable

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sync"
)

// ansiRegex strips ANSI escape codes and residual bracket artifacts from
// Claude's modelUsage keys. Matches the Elixir regex:
//
//	\x1B\[[0-9;]*[mGKHF]|\[[0-9;]*m\]
var ansiRegex = regexp.MustCompile(`\x1B\[[0-9;]*[mGKHF]|\[[0-9;]*m\]`)

// ClaudeBackend implements Backend for the Claude CLI (subprocess-per-request).
type ClaudeBackend struct {
	mu       sync.Mutex
	runner   *SubprocessRunner
	execPath string
	model    string
}

func NewClaudeBackend(model string) *ClaudeBackend {
	return &ClaudeBackend{
		runner: &SubprocessRunner{},
		model:  model,
	}
}

func (c *ClaudeBackend) Name() string { return "claude" }

func (c *ClaudeBackend) Start(_ context.Context) error { return nil }

func (c *ClaudeBackend) Stop() error { return nil }

// Healthy resolves the executable and runs --version as a probe.
func (c *ClaudeBackend) Healthy(ctx context.Context) error {
	path := ResolveExecutable("claude")
	if path == "" {
		return fmt.Errorf("claude executable not found")
	}

	c.mu.Lock()
	c.execPath = path
	c.mu.Unlock()

	out := c.runner.Probe(ctx, path, []string{"--version"})
	if out.ExitCode == nil || *out.ExitCode != 0 {
		return fmt.Errorf("claude probe failed: exit_code=%v", out.ExitCode)
	}
	return nil
}

// Run executes a Claude CLI request and returns a normalized Result.
func (c *ClaudeBackend) Run(ctx context.Context, req Request) (*Result, error) {
	c.mu.Lock()
	ep := c.execPath
	c.mu.Unlock()
	if ep == "" {
		return NotFoundResult("claude", req.Model), nil
	}

	args := claudeBuildArgs(req)
	raw := c.runner.Run(ctx, ep, args)
	parsed := claudeParseOutput(string(raw.Stdout))

	model := req.Model
	if model == "" {
		model = c.model
	}

	return BuildResult(raw, parsed, model), nil
}

// claudeBuildArgs produces CLI arguments matching claude.ex build_args/2.
//
// Format: [-p, --output-format, json, --dangerously-skip-permissions,
//
//	--model <model>?, -r <resume>?, <prompt>]
func claudeBuildArgs(req Request) []string {
	args := []string{"-p", "--output-format", "json", "--dangerously-skip-permissions"}

	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.Resume != "" {
		args = append(args, "-r", req.Resume)
	}

	args = append(args, req.Prompt)
	return args
}

// claudeParseOutput parses Claude CLI JSON output into ParsedOutput.
// Claude outputs JSON to stdout only (no stderr fallback like Gemini).
func claudeParseOutput(stdout string) ParsedOutput {
	var data map[string]any
	if err := json.Unmarshal([]byte(stdout), &data); err != nil {
		pe := "JSON parse failed"
		response := stdout
		if response == "" {
			response = ""
		}
		return ParsedOutput{
			Response:   response,
			Status:     "error",
			ParseError: &pe,
		}
	}

	// Check is_error flag
	if isErr, ok := data["is_error"].(bool); ok && isErr {
		response := ""
		if r, ok := data["result"].(string); ok {
			response = r
		}
		var sessionID *string
		if sid, ok := data["session_id"].(string); ok {
			sessionID = &sid
		}
		return ParsedOutput{
			Response:  response,
			Status:    "error",
			SessionID: sessionID,
		}
	}

	// Success path
	response := ""
	if r, ok := data["result"].(string); ok {
		response = r
	}

	var sessionID *string
	if sid, ok := data["session_id"].(string); ok {
		sessionID = &sid
	}

	return ParsedOutput{
		Response:  response,
		Status:    "ok",
		Metadata:  claudeExtractMetadata(data),
		SessionID: sessionID,
	}
}

// claudeExtractMetadata extracts model name from modelUsage keys.
// Model names may contain ANSI escape codes which must be stripped.
func claudeExtractMetadata(data map[string]any) map[string]any {
	modelUsage, ok := data["modelUsage"].(map[string]any)
	if !ok || len(modelUsage) == 0 {
		return nil
	}

	// Get first key (model name)
	var rawName string
	for k := range modelUsage {
		rawName = k
		break
	}

	cleanName := stripANSI(rawName)
	if cleanName == "" {
		cleanName = rawName
	}

	return map[string]any{"model_used": cleanName}
}

// stripANSI removes ANSI escape codes from a string.
func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}
