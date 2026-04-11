//go:build linux

package roundtable

import (
	"os/exec"
	"syscall"
)

// applyPdeathsig sets Setpgid and PR_SET_PDEATHSIG so the kernel sends
// SIGKILL to the child immediately when the parent process dies,
// regardless of whether parent cleanup code runs. This closes the
// orphan window on SIGKILL/panic paths.
func applyPdeathsig(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}
