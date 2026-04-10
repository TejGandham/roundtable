//go:build !windows

package roundtable

import (
	"os/exec"
	"syscall"
)

// configureProcGroup sets Setpgid so the child gets its own process group.
// On timeout, exec.CommandContext + cmd.Cancel kills the group atomically.
func configureProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Kill the entire process group (negative PID)
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err != nil {
			// Fallback: kill just the process
			return cmd.Process.Kill()
		}
		return nil
	}
}

// killProcessGroup sends SIGKILL to the process group. Called as deferred
// cleanup after cmd.Wait() to prevent orphaned grandchildren.
func killProcessGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

// exitSignalName extracts the signal name from an ExitError on Unix.
func exitSignalName(exitErr *exec.ExitError) string {
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok {
		if ws.Signaled() {
			return ws.Signal().String()
		}
	}
	return ""
}
