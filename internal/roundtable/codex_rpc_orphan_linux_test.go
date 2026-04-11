//go:build linux

package roundtable_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestCodexRPC_NoOrphanOnKill9 verifies that SIGKILL to the parent
// process does not leak a codex app-server child. Linux-only because
// it relies on PR_SET_PDEATHSIG semantics.
func TestCodexRPC_NoOrphanOnKill9(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("pdeathsig is linux-only")
	}
	if testing.Short() {
		t.Skip("integration test")
	}

	dir := t.TempDir()

	// 1. Write a "codex" shim shell script that satisfies the initialize
	//    handshake and then sleeps.
	shimPath := filepath.Join(dir, "codex-shim.sh")
	shimContent := "#!/bin/sh\n" +
		"# Read the initialize request (one line of JSON on stdin).\n" +
		"read line\n" +
		`printf '{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"shim","version":"0"}}}\n'` + "\n" +
		"# exec so sleep replaces sh and inherits pdeathsig as the direct child.\n" +
		"exec sleep 30\n"
	if err := os.WriteFile(shimPath, []byte(shimContent), 0o755); err != nil {
		t.Fatal(err)
	}

	// 2. Build the orphan helper.
	helperSrc := filepath.Join("testdata", "orphan_helper", "main.go")
	if _, err := os.Stat(helperSrc); err != nil {
		t.Fatalf("missing helper source %s: %v", helperSrc, err)
	}
	helperBin := filepath.Join(dir, "orphan-helper")
	goBin, err := exec.LookPath("go")
	if err != nil {
		// mise-managed go may not be on PATH in test env; let the caller set GO_BIN.
		if env := os.Getenv("GO_BIN"); env != "" {
			goBin = env
		} else {
			t.Skipf("go binary not found on PATH and GO_BIN not set: %v", err)
		}
	}
	build := exec.Command(goBin, "build", "-o", helperBin, "./testdata/orphan_helper")
	build.Stdout = os.Stderr
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("go build helper: %v", err)
	}

	// 3. Spawn the helper. It will launch the shim and write its own PID to pidFile.
	pidFile := filepath.Join(dir, "helper.pid")
	helper := exec.Command(helperBin, shimPath, pidFile)
	helper.Stdout = os.Stderr
	helper.Stderr = os.Stderr
	if err := helper.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	t.Cleanup(func() {
		if helper.Process != nil {
			_ = helper.Process.Kill()
			_, _ = helper.Process.Wait()
		}
	})

	// 4. Wait for pidFile to appear (indicates Start() succeeded).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(pidFile); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := os.Stat(pidFile); err != nil {
		_ = helper.Process.Kill()
		t.Fatalf("helper did not reach ready state: %v", err)
	}

	// Record the helper's descendants before we kill it. We want to assert
	// EVERY one of these is dead after SIGKILL+pdeathsig, not just processes
	// whose argv contains shimPath (sleep does not).
	helperPidStr := strings.TrimSpace(string(mustReadFile(t, pidFile)))
	descOut, _ := exec.Command("pgrep", "-P", helperPidStr).CombinedOutput()
	descendants := strings.Fields(strings.TrimSpace(string(descOut)))

	// 5. SIGKILL the helper. Linux pdeathsig should atomically kill the shim.
	if err := helper.Process.Kill(); err != nil {
		t.Fatalf("kill helper: %v", err)
	}
	_, _ = helper.Process.Wait()

	// 6. Give the kernel a moment to deliver pdeathsig.
	time.Sleep(300 * time.Millisecond)

	// 7. Every descendant we recorded must be gone.
	var survivors []string
	for _, pidStr := range descendants {
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		// kill -0 returns nil if the process exists, error if not.
		if err := syscall.Kill(pid, syscall.Signal(0)); err == nil {
			survivors = append(survivors, pidStr)
		}
	}
	if len(survivors) > 0 {
		// Best-effort cleanup.
		for _, pidStr := range survivors {
			if pid, err := strconv.Atoi(pidStr); err == nil {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
		t.Fatalf("codex shim descendants survived parent SIGKILL: %v", survivors)
	}

	// Belt-and-suspenders: also check pgrep -f on the shim path.
	out, err := exec.Command("pgrep", "-f", shimPath).CombinedOutput()
	if shimSurvivors := strings.TrimSpace(string(out)); err == nil && shimSurvivors != "" {
		_ = exec.Command("pkill", "-9", "-f", shimPath).Run()
		t.Fatalf("codex shim survived parent SIGKILL, pids: %s", shimSurvivors)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readFile %s: %v", path, err)
	}
	return data
}
