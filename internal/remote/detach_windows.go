//go:build windows

package remote

import "os/exec"

// detach does nothing on Windows, where logins use ssh-agent instead, since
// its ssh cannot keep a connection open for the commands after it.
func detach(*exec.Cmd) {}
