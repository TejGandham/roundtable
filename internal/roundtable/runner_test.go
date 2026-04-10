package roundtable

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test uses Unix shell scripts")
	}
}

// writeScript creates an executable shell script in dir, returns its path.
func writeScript(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+content), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSubprocessRunner_NormalExit(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	script := writeScript(t, dir, "ok.sh", `echo '{"response":"hello"}'`)

	runner := &SubprocessRunner{}
	out := runner.Run(context.Background(), script, nil)

	if out.ExitCode == nil || *out.ExitCode != 0 {
		t.Errorf("exit_code = %v, want 0", out.ExitCode)
	}
	if !strings.Contains(string(out.Stdout), `"response":"hello"`) {
		t.Errorf("stdout = %q, want JSON response", string(out.Stdout))
	}
	if out.TimedOut {
		t.Error("should not be timed out")
	}
	if out.ElapsedMs < 0 {
		t.Error("elapsed_ms should be non-negative")
	}
}

func TestSubprocessRunner_NonZeroExit(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	script := writeScript(t, dir, "fail.sh", `echo "oops" >&2; exit 42`)

	runner := &SubprocessRunner{}
	out := runner.Run(context.Background(), script, nil)

	if out.ExitCode == nil || *out.ExitCode != 42 {
		t.Errorf("exit_code = %v, want 42", out.ExitCode)
	}
	if !strings.Contains(out.Stderr, "oops") {
		t.Errorf("stderr = %q, want 'oops'", out.Stderr)
	}
}

func TestSubprocessRunner_Timeout(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	script := writeScript(t, dir, "hang.sh", `sleep 60`)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	runner := &SubprocessRunner{}
	out := runner.Run(ctx, script, nil)

	if !out.TimedOut {
		t.Error("should be timed out")
	}
	// On timeout, exit code should be nil (matches Elixir)
	// (or may be non-nil depending on how the OS reports it — check both)
}

func TestSubprocessRunner_StdoutTruncation(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	// Generate 200 bytes of output; set limit to 100
	script := writeScript(t, dir, "big.sh", `dd if=/dev/zero bs=200 count=1 2>/dev/null | tr '\0' 'A'`)

	runner := &SubprocessRunner{StdoutLimit: 100}
	out := runner.Run(context.Background(), script, nil)

	if !out.Truncated {
		t.Error("stdout should be truncated")
	}
	if len(out.Stdout) > 100 {
		t.Errorf("stdout length = %d, want <= 100", len(out.Stdout))
	}
}

func TestSubprocessRunner_StderrTruncation(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	script := writeScript(t, dir, "bigerr.sh", `dd if=/dev/zero bs=200 count=1 2>/dev/null | tr '\0' 'E' >&2`)

	runner := &SubprocessRunner{StderrLimit: 100}
	out := runner.Run(context.Background(), script, nil)

	if !out.StderrTruncated {
		t.Error("stderr should be truncated")
	}
	if len(out.Stderr) > 100 {
		t.Errorf("stderr length = %d, want <= 100", len(out.Stderr))
	}
}

func TestSubprocessRunner_Probe(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	script := writeScript(t, dir, "version.sh", `echo "v1.2.3"`)

	runner := &SubprocessRunner{}
	out := runner.Probe(context.Background(), script, []string{"--version"})

	if out.ExitCode == nil || *out.ExitCode != 0 {
		t.Errorf("probe exit_code = %v, want 0", out.ExitCode)
	}
	if !strings.Contains(string(out.Stdout), "v1.2.3") {
		t.Errorf("probe stdout = %q, want v1.2.3", string(out.Stdout))
	}
}

func TestSubprocessRunner_ArgsPassedThrough(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	script := writeScript(t, dir, "echo_args.sh", `echo "$@"`)

	runner := &SubprocessRunner{}
	out := runner.Run(context.Background(), script, []string{"hello", "world"})

	if !strings.Contains(string(out.Stdout), "hello world") {
		t.Errorf("stdout = %q, want 'hello world'", string(out.Stdout))
	}
}

func TestSubprocessRunner_RoundtableActiveEnv(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	script := writeScript(t, dir, "env.sh", `echo "$ROUNDTABLE_ACTIVE"`)

	runner := &SubprocessRunner{}
	out := runner.Run(context.Background(), script, nil)

	if strings.TrimSpace(string(out.Stdout)) != "1" {
		t.Errorf("ROUNDTABLE_ACTIVE = %q, want '1'", strings.TrimSpace(string(out.Stdout)))
	}
}

func TestResolveExecutable_EnvVar(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	fakePath := filepath.Join(dir, "mybin")
	if err := os.WriteFile(fakePath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ROUNDTABLE_MYBIN_PATH", fakePath)
	got := ResolveExecutable("mybin")
	if got != fakePath {
		t.Errorf("ResolveExecutable = %q, want %q", got, fakePath)
	}
}

func TestResolveExecutable_EnvVarMissing(t *testing.T) {
	t.Setenv("ROUNDTABLE_NOBIN_PATH", "/nonexistent/path/nobin")
	got := ResolveExecutable("nobin")
	if got != "" {
		t.Errorf("ResolveExecutable = %q, want empty (file doesn't exist)", got)
	}
}

func TestResolveExecutable_ExtraPath(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	fakePath := filepath.Join(dir, "gemini")
	if err := os.WriteFile(fakePath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// Clear the direct env var to force EXTRA_PATH lookup
	t.Setenv("ROUNDTABLE_GEMINI_PATH", "")
	t.Setenv("ROUNDTABLE_EXTRA_PATH", dir)

	got := ResolveExecutable("gemini")
	if got != fakePath {
		t.Errorf("ResolveExecutable = %q, want %q", got, fakePath)
	}
}

func TestResolveExecutable_SystemPath(t *testing.T) {
	// "sh" should always be on PATH on Unix systems
	if runtime.GOOS == "windows" {
		t.Skip("test uses Unix binary")
	}
	t.Setenv("ROUNDTABLE_SH_PATH", "")
	t.Setenv("ROUNDTABLE_EXTRA_PATH", "")

	got := ResolveExecutable("sh")
	if got == "" {
		t.Error("ResolveExecutable('sh') should find /bin/sh or similar")
	}
}

func TestResolveExecutable_NotFound(t *testing.T) {
	t.Setenv("ROUNDTABLE_NONEXISTENT_CLI_PATH", "")
	t.Setenv("ROUNDTABLE_EXTRA_PATH", "")
	got := ResolveExecutable("nonexistent_cli_" + fmt.Sprintf("%d", time.Now().UnixNano()))
	if got != "" {
		t.Errorf("ResolveExecutable = %q, want empty", got)
	}
}

func TestLimitedWriter_UnderLimit(t *testing.T) {
	w := NewLimitedWriter(100)
	n, err := w.Write([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Errorf("n = %d, want 5", n)
	}
	if w.String() != "hello" {
		t.Errorf("buf = %q, want 'hello'", w.String())
	}
	if w.Truncated() {
		t.Error("should not be truncated")
	}
}

func TestLimitedWriter_OverLimit(t *testing.T) {
	w := NewLimitedWriter(5)
	n, err := w.Write([]byte("hello world"))
	if err != nil {
		t.Fatal(err)
	}
	// Should report full consumption
	if n != 11 {
		t.Errorf("n = %d, want 11", n)
	}
	if len(w.Bytes()) > 5 {
		t.Errorf("buf len = %d, want <= 5", len(w.Bytes()))
	}
	if !w.Truncated() {
		t.Error("should be truncated")
	}
}

func TestLimitedWriter_ExactLimit(t *testing.T) {
	w := NewLimitedWriter(5)
	w.Write([]byte("hello"))
	if w.Truncated() {
		t.Error("should not be truncated at exact limit")
	}
	// Next write should trigger truncation
	w.Write([]byte("x"))
	if !w.Truncated() {
		t.Error("should be truncated after exceeding limit")
	}
}

// Verify unused import suppression
var _ = exec.LookPath
