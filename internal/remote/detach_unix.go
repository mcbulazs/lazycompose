//go:build !windows

package remote

import (
	"os/exec"
	"syscall"
)

// detach starts cmd in its own session, without the terminal, so ssh asks
// through SSH_ASKPASS even where it does not know SSH_ASKPASS_REQUIRE.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
