package remote

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
)

// Logging in at the start. Commands to a host run with BatchMode, so ssh
// never asks anything while the screen belongs to lazycompose. A key with
// a passphrase, or a host key that is not trusted yet, then needs a login
// first: ssh runs in the terminal before the screen opens, asks what it
// needs itself, and leaves the shared connection open for the rest. The
// passphrase goes to ssh only; lazycompose never sees it.

// CanLogin reports whether the shared connection with h can be opened at
// the start, which is what a passphrase prompt needs: Windows' ssh cannot
// share one.
func CanLogin() bool { return sharing() }

// NeedsLogin tries to reach h without questions. It reports true when ssh
// would have to ask something: a passphrase, a password or whether to trust
// the host key. Other errors, like an unreachable host, are returned; the
// screen shows those later.
func (h *Host) NeedsLogin(ctx context.Context) (bool, error) {
	cmd := h.Command(ctx, "true")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return false, nil
	}
	low := strings.ToLower(stderr.String())
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 255 &&
		(strings.Contains(low, "permission denied") || strings.Contains(low, "host key verification failed") ||
			strings.Contains(low, "passphrase")) &&
		!strings.Contains(low, "remote host identification has changed") {
		return true, nil
	}
	return false, h.fail(err, stderr.String())
}

// LoginCommand is ssh in the terminal, which may ask for a passphrase or
// to trust the host key, and keeps the connection open for the commands
// after it.
func (h *Host) LoginCommand() *exec.Cmd {
	return exec.Command(sshProgram, h.sshArgs(sshLogin, []string{"true"})...)
}
