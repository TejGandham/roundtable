//go:build windows

package roundtable

import "os/exec"

// applyPdeathsig is a no-op on Windows. CodexBackend is not expected to
// be used on Windows in Phase A; the fallback subprocess path covers it.
func applyPdeathsig(cmd *exec.Cmd) {}
