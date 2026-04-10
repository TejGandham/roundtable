package httpmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const defaultToolTimeoutSeconds = 900

type ToolInput struct {
	Prompt       string `json:"prompt"`
	Files        string `json:"files,omitempty"`
	Timeout      *int   `json:"timeout,omitempty"`
	GeminiModel  string `json:"gemini_model,omitempty"`
	CodexModel   string `json:"codex_model,omitempty"`
	ClaudeModel  string `json:"claude_model,omitempty"`
	GeminiResume string `json:"gemini_resume,omitempty"`
	CodexResume  string `json:"codex_resume,omitempty"`
	ClaudeResume string `json:"claude_resume,omitempty"`
	Agents       string `json:"agents,omitempty"`
}

type ToolSpec struct {
	Name         string
	Description  string
	PromptSuffix string
	Role         string
	GeminiRole   string
	CodexRole    string
	ClaudeRole   string
}

type RunResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Err      error
}

type Runner interface {
	Run(ctx context.Context, path string, args []string) RunResult
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, path string, args []string) RunResult {
	cmd := exec.CommandContext(ctx, path, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = 2 * time.Second

	if runtime.GOOS != "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return RunResult{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		ExitCode: exitCode,
		Err:      err,
	}
}

type Backend struct {
	config  Config
	runner  Runner
	metrics *Metrics
}

func NewBackend(config Config, runner Runner, metrics *Metrics) *Backend {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Backend{config: config, runner: runner, metrics: metrics}
}

func (b *Backend) Probe(ctx context.Context) error {
	path, err := b.config.ResolvedBackendPath()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, b.config.ProbeTimeout)
	defer cancel()

	result := b.runner.Run(ctx, path, nil)
	if ctx.Err() != nil {
		return fmt.Errorf("backend probe timed out after %s", b.config.ProbeTimeout)
	}
	if len(bytes.TrimSpace(result.Stdout)) == 0 {
		return fmt.Errorf("backend probe returned empty stdout: %s", compactOutput(result.Stderr))
	}

	payload, err := decodeJSONObject(result.Stdout)
	if err != nil {
		return fmt.Errorf("backend probe returned invalid JSON: %w", err)
	}
	if _, ok := payload["error"].(string); !ok {
		return fmt.Errorf("backend probe did not return an error object")
	}
	return nil
}

func (b *Backend) Call(ctx context.Context, spec ToolSpec, input ToolInput) (string, bool) {
	path, err := b.config.ResolvedBackendPath()
	if err != nil {
		return fmt.Sprintf("roundtable backend unavailable: %v", err), true
	}

	deadline := time.Duration(toolTimeoutSeconds(input.Timeout))*time.Second + b.config.RequestGrace
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	args := buildBackendArgs(spec, input, b.config)
	result := b.runner.Run(ctx, path, args)

	if ctx.Err() != nil {
		b.metrics.BackendTimeouts.Add(1)
		return fmt.Sprintf("roundtable backend timed out after %s", deadline.Truncate(time.Second)), true
	}

	stdout := strings.TrimSpace(string(result.Stdout))
	if result.Err == nil {
		if stdout == "" {
			b.metrics.BackendParseErrors.Add(1)
			return "roundtable backend returned empty stdout", true
		}
		if _, err := decodeJSONObject(result.Stdout); err != nil {
			b.metrics.BackendParseErrors.Add(1)
			return fmt.Sprintf("roundtable backend returned invalid JSON: %v", err), true
		}
		return stdout, false
	}

	b.metrics.BackendNonZeroExit.Add(1)

	if stdout != "" {
		if payload, err := decodeJSONObject(result.Stdout); err == nil {
			if message, ok := payload["error"].(string); ok && message != "" {
				return message, true
			}
		}
	}

	stderr := compactOutput(result.Stderr)
	if stderr == "" {
		stderr = compactOutput(result.Stdout)
	}
	if stderr == "" {
		stderr = result.Err.Error()
	}

	return fmt.Sprintf("roundtable backend exited with code %d: %s", result.ExitCode, stderr), true
}

func buildBackendArgs(spec ToolSpec, input ToolInput, config Config) []string {
	prompt := input.Prompt
	if spec.PromptSuffix != "" {
		prompt += spec.PromptSuffix
	}

	args := []string{"--prompt", prompt}

	if spec.Role != "" {
		args = append(args, "--role", spec.Role)
	}
	if spec.GeminiRole != "" {
		args = append(args, "--gemini-role", spec.GeminiRole)
	}
	if spec.CodexRole != "" {
		args = append(args, "--codex-role", spec.CodexRole)
	}
	if spec.ClaudeRole != "" {
		args = append(args, "--claude-role", spec.ClaudeRole)
	}
	if input.Files != "" {
		args = append(args, "--files", input.Files)
	}
	if input.Timeout != nil {
		args = append(args, "--timeout", strconv.Itoa(*input.Timeout))
	}
	if input.GeminiModel != "" {
		args = append(args, "--gemini-model", input.GeminiModel)
	}
	if input.CodexModel != "" {
		args = append(args, "--codex-model", input.CodexModel)
	}
	if input.ClaudeModel != "" {
		args = append(args, "--claude-model", input.ClaudeModel)
	}
	if input.GeminiResume != "" {
		args = append(args, "--gemini-resume", input.GeminiResume)
	}
	if input.CodexResume != "" {
		args = append(args, "--codex-resume", input.CodexResume)
	}
	if input.ClaudeResume != "" {
		args = append(args, "--claude-resume", input.ClaudeResume)
	}
	if input.Agents != "" {
		args = append(args, "--agents", input.Agents)
	}
	if config.RolesDir != "" {
		args = append(args, "--roles-dir", config.RolesDir)
	}
	if config.ProjectRolesDir != "" {
		args = append(args, "--project-roles-dir", config.ProjectRolesDir)
	}

	return args
}

func toolTimeoutSeconds(timeout *int) int {
	if timeout == nil || *timeout <= 0 {
		return defaultToolTimeoutSeconds
	}
	return *timeout
}

func decodeJSONObject(data []byte) (map[string]any, error) {
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func compactOutput(data []byte) string {
	text := strings.TrimSpace(string(data))
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 300 {
		return text[:300] + "..."
	}
	return text
}
