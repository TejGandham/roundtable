package roundtable

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	MaxStdout      = 1 << 20   // 1 MB
	MaxStderr      = 512 << 10 // 512 KB
	MaxProbeOutput = 64 << 10  // 64 KB
)

// LimitedWriter wraps a bytes.Buffer and stops accepting writes after limit
// bytes. Once the limit is reached, Truncated() returns true. Thread-safe.
type LimitedWriter struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func NewLimitedWriter(limit int) *LimitedWriter {
	return &LimitedWriter{limit: limit}
}

func (w *LimitedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.buf.Len() >= w.limit {
		w.truncated = true
		return len(p), nil // consume but discard
	}

	remaining := w.limit - w.buf.Len()
	if len(p) <= remaining {
		w.buf.Write(p)
		return len(p), nil
	}

	w.buf.Write(p[:remaining])
	w.truncated = true
	return len(p), nil // report full consumption to avoid broken pipe
}

func (w *LimitedWriter) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Bytes()
}

func (w *LimitedWriter) String() string {
	return string(w.Bytes())
}

func (w *LimitedWriter) Truncated() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.truncated
}

// ResolveExecutable resolves the absolute path for a CLI executable.
//
// Resolution order:
//  1. ROUNDTABLE_<NAME>_PATH env var (must exist on disk)
//  2. Directories in ROUNDTABLE_EXTRA_PATH (searched before system PATH)
//  3. exec.LookPath (system PATH)
//
// Returns empty string if not found.
func ResolveExecutable(name string) string {
	// Level 1: explicit env var
	envKey := "ROUNDTABLE_" + strings.ToUpper(name) + "_PATH"
	if path := os.Getenv(envKey); path != "" {
		if _, err := os.Stat(path); err == nil {
			return path
		}
		return ""
	}

	// Level 2: ROUNDTABLE_EXTRA_PATH directories
	if extra := os.Getenv("ROUNDTABLE_EXTRA_PATH"); extra != "" {
		for _, dir := range strings.Split(extra, string(os.PathListSeparator)) {
			if dir == "" {
				continue
			}
			candidate := filepath.Join(dir, name)
			info, err := os.Stat(candidate)
			if err == nil && !info.IsDir() {
				return candidate
			}
		}
	}

	// Level 3: system PATH
	if path, err := exec.LookPath(name); err == nil {
		return path
	}

	return ""
}

// SubprocessRunner executes CLI subprocesses and returns RawRunOutput.
type SubprocessRunner struct {
	// StdoutLimit overrides MaxStdout (for testing). 0 means use default.
	StdoutLimit int
	// StderrLimit overrides MaxStderr (for testing). 0 means use default.
	StderrLimit int
}

// Run executes the CLI at path with args. The context controls timeout.
// Returns RawRunOutput with captured stdout/stderr, exit status, and timing.
func (r *SubprocessRunner) Run(ctx context.Context, path string, args []string) RawRunOutput {
	stdoutLimit := r.StdoutLimit
	if stdoutLimit <= 0 {
		stdoutLimit = MaxStdout
	}
	stderrLimit := r.StderrLimit
	if stderrLimit <= 0 {
		stderrLimit = MaxStderr
	}

	return r.run(ctx, path, args, stdoutLimit, stderrLimit)
}

// Probe runs a lightweight health check with reduced output limits.
// Equivalent to Elixir's probe_cli/3.
func (r *SubprocessRunner) Probe(ctx context.Context, path string, args []string) RawRunOutput {
	return r.run(ctx, path, args, MaxProbeOutput, MaxProbeOutput)
}

func (r *SubprocessRunner) run(ctx context.Context, path string, args []string, stdoutLimit, stderrLimit int) RawRunOutput {
	start := time.Now()

	cmd := exec.CommandContext(ctx, path, args...)
	stdoutW := NewLimitedWriter(stdoutLimit)
	stderrW := NewLimitedWriter(stderrLimit)
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW
	cmd.Stdin = nil // /dev/null — prevent consuming parent stdin

	// Set ROUNDTABLE_ACTIVE=1 and prepend ROUNDTABLE_EXTRA_PATH to PATH
	cmd.Env = subprocessEnv()

	// Platform-specific process group setup
	configureProcGroup(cmd)

	cmd.WaitDelay = 2 * time.Second

	if err := cmd.Start(); err != nil {
		return RawRunOutput{Stderr: err.Error()}
	}
	// Ensure process group is killed after Wait, preventing orphaned grandchildren.
	defer func() {
		if cmd.Process != nil {
			killProcessGroup(cmd.Process.Pid)
		}
	}()
	err := cmd.Wait()
	elapsed := time.Since(start).Milliseconds()

	out := RawRunOutput{
		Stdout:          stdoutW.Bytes(),
		Stderr:          stderrW.String(),
		ElapsedMs:       elapsed,
		Truncated:       stdoutW.Truncated(),
		StderrTruncated: stderrW.Truncated(),
	}

	if err == nil {
		exitCode := 0
		out.ExitCode = &exitCode
		return out
	}

	// Check if context was cancelled (timeout)
	if ctx.Err() != nil {
		out.TimedOut = true
		// ExitCode and ExitSignal stay nil on timeout (matches Elixir)
		return out
	}

	// Extract exit code from ExitError
	if exitErr, ok := err.(*exec.ExitError); ok {
		code := exitErr.ExitCode()
		out.ExitCode = &code
		// If killed by signal, exitCode is -1 on some platforms
		if code == -1 {
			signal := exitSignalName(exitErr)
			if signal != "" {
				out.ExitSignal = &signal
				out.ExitCode = nil
			}
		}
	}

	return out
}

// subprocessEnv builds the environment for child processes.
// Sets ROUNDTABLE_ACTIVE=1 and prepends ROUNDTABLE_EXTRA_PATH to PATH.
func subprocessEnv() []string {
	raw := os.Environ()
	env := make([]string, 0, len(raw)+1)
	for _, e := range raw {
		if !strings.HasPrefix(e, "ROUNDTABLE_ACTIVE=") {
			env = append(env, e)
		}
	}
	env = append(env, "ROUNDTABLE_ACTIVE=1")

	if extra := os.Getenv("ROUNDTABLE_EXTRA_PATH"); extra != "" {
		sysPath := os.Getenv("PATH")
		newPath := extra + string(os.PathListSeparator) + sysPath
		// Replace or append PATH
		found := false
		for i, e := range env {
			if strings.HasPrefix(e, "PATH=") {
				env[i] = "PATH=" + newPath
				found = true
				break
			}
		}
		if !found {
			env = append(env, "PATH="+newPath)
		}
	}

	return env
}
