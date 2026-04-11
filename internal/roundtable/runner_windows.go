//go:build windows

package roundtable

import "os/exec"

// configureProcGroup is a no-op on Windows. Process tree cleanup is handled
// by cmd.Cancel defaulting to Process.Kill(), and WaitDelay allows children
// to exit. For deeper tree kill, we'd need CREATE_NEW_PROCESS_GROUP +
// GenerateConsoleCtrlEvent, but that's a future enhancement.
func configureProcGroup(cmd *exec.Cmd) {
	// No Setpgid equivalent on Windows.
	// cmd.Cancel defaults to Process.Kill() which is sufficient.
}

// killProcessGroup is a no-op on Windows. See configureProcGroup comment.
func killProcessGroup(pid int) {}

// exitSignalName always returns empty on Windows (no Unix signals).
func exitSignalName(exitErr *exec.ExitError) string {
	return ""
}
