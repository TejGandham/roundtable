//go:build !linux && !windows

package roundtable

import (
	"os/exec"
	"syscall"
)

// applyPdeathsig: Darwin/BSD have no PR_SET_PDEATHSIG. We set Setpgid so
// the codex child lives in its own process group (enables group kill
// from Stop()), but have no atomic orphan-prevention if the parent dies
// uncleanly. The happy-path Stop() handler covers normal shutdown; the
// gap is documented in INSTALL.md troubleshooting (Phase E).
func applyPdeathsig(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}
