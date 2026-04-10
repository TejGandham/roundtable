package httpmcp

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBuildBackendArgs(t *testing.T) {
	timeout := 30
	config := Config{
		RolesDir:        "/tmp/roles",
		ProjectRolesDir: "/tmp/project-roles",
	}

	args := buildBackendArgs(ToolSpec{
		Name:         "deepdive",
		Role:         "planner",
		PromptSuffix: "\n\nProvide conclusions, assumptions, alternatives, and confidence level.",
	}, ToolInput{
		Prompt:       "Analyze this",
		Files:        "lib/a.ex,lib/b.ex",
		Timeout:      &timeout,
		GeminiModel:  "gemini-2.5-pro",
		CodexModel:   "gpt-5.4",
		ClaudeModel:  "sonnet",
		GeminiResume: "latest",
		CodexResume:  "last",
		ClaudeResume: "sess_123",
		Agents:       `[{"cli":"codex"}]`,
	}, config)

	want := []string{
		"--prompt", "Analyze this\n\nProvide conclusions, assumptions, alternatives, and confidence level.",
		"--role", "planner",
		"--files", "lib/a.ex,lib/b.ex",
		"--timeout", "30",
		"--gemini-model", "gemini-2.5-pro",
		"--codex-model", "gpt-5.4",
		"--claude-model", "sonnet",
		"--gemini-resume", "latest",
		"--codex-resume", "last",
		"--claude-resume", "sess_123",
		"--agents", `[{"cli":"codex"}]`,
		"--roles-dir", "/tmp/roles",
		"--project-roles-dir", "/tmp/project-roles",
	}

	if !reflect.DeepEqual(args, want) {
		t.Fatalf("buildBackendArgs mismatch\n got: %#v\nwant: %#v", args, want)
	}
}

func TestBuildBackendArgsXrayRoles(t *testing.T) {
	args := buildBackendArgs(ToolSpec{
		Name:       "xray",
		GeminiRole: "planner",
		CodexRole:  "codereviewer",
		ClaudeRole: "default",
	}, ToolInput{Prompt: "Inspect this"}, Config{})

	if slicesContain(args, "--role") {
		t.Fatalf("xray args unexpectedly contained --role: %#v", args)
	}

	want := []string{
		"--prompt", "Inspect this",
		"--gemini-role", "planner",
		"--codex-role", "codereviewer",
		"--claude-role", "default",
	}

	if !reflect.DeepEqual(args, want) {
		t.Fatalf("buildBackendArgs mismatch\n got: %#v\nwant: %#v", args, want)
	}
}

func TestBackendCallSuccess(t *testing.T) {
	script := writeExecutable(t, `#!/bin/sh
printf '{ "gemini": { "status": "ok" }, "meta": { "total_elapsed_ms": 1 } }\n'
`)

	backend := NewBackend(Config{
		BackendPath:  script,
		RequestGrace: 100 * time.Millisecond,
	}, ExecRunner{})

	text, isError := backend.Call(context.Background(), ToolSpec{Name: "hivemind", Role: "default"}, ToolInput{
		Prompt: "hello",
	})

	if isError {
		t.Fatalf("expected success, got error: %s", text)
	}
	if !strings.Contains(text, `"gemini"`) {
		t.Fatalf("unexpected success payload: %s", text)
	}
}

func TestBackendCallReturnsToolErrorFromJSON(t *testing.T) {
	script := writeExecutable(t, `#!/bin/sh
printf '{ "error": "backend exploded" }\n'
exit 1
`)

	backend := NewBackend(Config{
		BackendPath:  script,
		RequestGrace: 100 * time.Millisecond,
	}, ExecRunner{})

	text, isError := backend.Call(context.Background(), ToolSpec{Name: "hivemind", Role: "default"}, ToolInput{
		Prompt: "hello",
	})

	if !isError {
		t.Fatalf("expected tool error, got success: %s", text)
	}
	if text != "backend exploded" {
		t.Fatalf("unexpected error text: %s", text)
	}
}

func TestBackendCallTimeout(t *testing.T) {
	script := writeExecutable(t, `#!/bin/sh
sleep 5
printf '{ "gemini": { "status": "ok" } }\n'
`)

	timeout := 1
	backend := NewBackend(Config{
		BackendPath:  script,
		RequestGrace: 100 * time.Millisecond,
	}, ExecRunner{})

	text, isError := backend.Call(context.Background(), ToolSpec{Name: "hivemind", Role: "default"}, ToolInput{
		Prompt:  "hello",
		Timeout: &timeout,
	})

	if !isError {
		t.Fatalf("expected timeout error, got success: %s", text)
	}
	if !strings.Contains(text, "timed out") {
		t.Fatalf("unexpected timeout text: %s", text)
	}
}

func TestProbeBackend(t *testing.T) {
	script := writeExecutable(t, `#!/bin/sh
printf '{ "error": "Missing required --prompt argument", "usage": "roundtable --prompt ..." }\n'
exit 1
`)

	backend := NewBackend(Config{
		BackendPath:  script,
		ProbeTimeout: 1 * time.Second,
	}, ExecRunner{})

	if err := backend.Probe(context.Background()); err != nil {
		t.Fatalf("probe failed: %v", err)
	}
}

func writeExecutable(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "fake-backend.sh")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}

func slicesContain(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}
