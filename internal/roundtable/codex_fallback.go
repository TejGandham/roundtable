package roundtable

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// CodexFallbackBackend implements Backend for the Codex CLI in subprocess-per-
// request mode (exec --json). Used when the Codex app-server is unavailable.
type CodexFallbackBackend struct {
	mu        sync.Mutex
	runner    *SubprocessRunner
	execPath  string
	model     string
	reasoning string // optional reasoning_effort override
}

func NewCodexFallbackBackend(model, reasoning string) *CodexFallbackBackend {
	return &CodexFallbackBackend{
		runner:    &SubprocessRunner{},
		model:     model,
		reasoning: reasoning,
	}
}

func (c *CodexFallbackBackend) Name() string { return "codex" }

func (c *CodexFallbackBackend) Start(_ context.Context) error { return nil }

func (c *CodexFallbackBackend) Stop() error { return nil }

// Healthy resolves the executable and runs --version as a probe.
func (c *CodexFallbackBackend) Healthy(ctx context.Context) error {
	path := ResolveExecutable("codex")
	if path == "" {
		return fmt.Errorf("codex executable not found")
	}

	c.mu.Lock()
	c.execPath = path
	c.mu.Unlock()

	out := c.runner.Probe(ctx, path, []string{"--version"})
	if out.ExitCode == nil || *out.ExitCode != 0 {
		return fmt.Errorf("codex probe failed: exit_code=%v", out.ExitCode)
	}
	return nil
}

// Run executes a Codex CLI request in exec --json mode and returns a
// normalized Result.
func (c *CodexFallbackBackend) Run(ctx context.Context, req Request) (*Result, error) {
	c.mu.Lock()
	ep := c.execPath
	c.mu.Unlock()
	if ep == "" {
		return NotFoundResult("codex", req.Model), nil
	}

	args := codexFallbackBuildArgs(req, c.model, c.reasoning)
	raw := c.runner.Run(ctx, ep, args)
	parsed := codexFallbackParseOutput(string(raw.Stdout))

	model := req.Model
	if model == "" {
		model = c.model
	}

	return BuildResult(raw, parsed, model), nil
}

// codexFallbackBuildArgs produces CLI arguments matching codex.ex build_args/2.
//
// Format: [exec, --json, --dangerously-bypass-approvals-and-sandbox,
//
//	-c model=<model>?, -c reasoning_effort=<reasoning>?,
//	<resume_action>, <prompt>]
//
// Resume modes:
//   - nil/empty: [prompt]
//   - "last":    [resume, --last, prompt]
//   - session:   [resume, session_id, prompt]
func codexFallbackBuildArgs(req Request, defaultModel, reasoning string) []string {
	args := []string{"exec", "--json", "--dangerously-bypass-approvals-and-sandbox"}

	model := req.Model
	if model == "" {
		model = defaultModel
	}
	if model != "" {
		args = append(args, "-c", "model="+model)
	}

	if reasoning != "" {
		args = append(args, "-c", "reasoning_effort="+reasoning)
	}

	switch req.Resume {
	case "":
		args = append(args, req.Prompt)
	case "last":
		args = append(args, "resume", "--last", req.Prompt)
	default:
		args = append(args, "resume", req.Resume, req.Prompt)
	}

	return args
}

// codexFallbackParseOutput parses JSONL output from codex exec --json.
//
// Processes events line by line:
//   - item.completed (type=agent_message): accumulate text messages
//   - thread.started: capture thread_id as session ID
//   - turn.completed: capture usage stats
//   - error: accumulate error messages
//
// Messages are joined in chronological order with \n\n.
// Errors are joined in chronological order with \n.
func codexFallbackParseOutput(stdout string) ParsedOutput {
	lines := strings.Split(stdout, "\n")

	var messages []string
	var errors []string
	var usage any
	var threadID *string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}

		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		eventType, _ := event["type"].(string)
		switch eventType {
		case "item.completed":
			item, ok := event["item"].(map[string]any)
			if !ok {
				continue
			}
			itemType, _ := item["type"].(string)
			text, _ := item["text"].(string)
			if itemType == "agent_message" && strings.TrimSpace(text) != "" {
				messages = append(messages, strings.TrimSpace(text))
			}

		case "thread.started":
			if tid, ok := event["thread_id"].(string); ok {
				threadID = &tid
			}

		case "turn.completed":
			if u, ok := event["usage"]; ok {
				usage = u
			}

		case "error":
			if msg, ok := event["message"].(string); ok {
				errors = append(errors, msg)
			}
		}
	}

	metadata := map[string]any{}
	if usage != nil {
		metadata["usage"] = usage
	}

	// Messages found: success path
	if len(messages) > 0 {
		// Go append preserves chronological order; join directly (no reversal needed)
		return ParsedOutput{
			Response:  strings.Join(messages, "\n\n"),
			Status:    "ok",
			Metadata:  metadata,
			SessionID: threadID,
		}
	}

	// Errors found: error path
	if len(errors) > 0 {
		return ParsedOutput{
			Response:  strings.Join(errors, "\n"),
			Status:    "error",
			Metadata:  metadata,
			SessionID: threadID,
		}
	}

	// Raw stdout fallback
	trimmed := strings.TrimSpace(stdout)
	if trimmed != "" {
		pe := "No JSONL events found; using raw output"
		return ParsedOutput{
			Response:   trimmed,
			Status:     "error",
			ParseError: &pe,
		}
	}

	// Empty output
	pe := "No output from codex"
	return ParsedOutput{
		Response:   "",
		Status:     "error",
		ParseError: &pe,
	}
}
