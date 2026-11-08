// Package remote runs commands on other machines over ssh, for roots like
// ssh://user@host/srv/stacks.
package remote

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Prefix starts a remote root.
const Prefix = "ssh://"

// IsRemote reports whether root is a remote root.
func IsRemote(root string) bool { return strings.HasPrefix(root, Prefix) }

// Host is a machine reached over ssh. Name may be a Host from ~/.ssh/config.
type Host struct {
	User, Name, Port string
}

// Parse splits a root like ssh://user@host:22/srv/stacks into the host and
// the absolute folder on it. Names that ssh could read as options, and
// anything that is not a plain host, user or port, are refused.
func Parse(root string) (*Host, string, error) {
	u, err := url.Parse(root)
	if err != nil || u.Scheme != "ssh" {
		return nil, "", fmt.Errorf("%q is not a remote root like ssh://host/srv/stacks", root)
	}
	bad := func(why string) (*Host, string, error) {
		return nil, "", fmt.Errorf("%s: %s", root, why)
	}
	if u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" {
		return bad("write it as ssh://host/folder")
	}
	h := &Host{Name: u.Hostname(), Port: u.Port()}
	if u.User != nil {
		if _, has := u.User.Password(); has {
			return bad("passwords are not supported, use an ssh key")
		}
		h.User = u.User.Username()
	}
	if !plainName(h.Name, true) {
		return bad("the host should be a name or address from ~/.ssh/config or DNS")
	}
	if h.User != "" && !plainName(h.User, false) {
		return bad("the user name has characters ssh does not take")
	}
	if h.Port != "" && !digits(h.Port) {
		return bad("the port should be a number")
	}
	if u.Path == "" || u.Path[0] != '/' {
		return bad("the folder should be an absolute path, like ssh://host/srv/stacks")
	}
	if strings.ContainsFunc(u.Path, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return bad("the folder has control characters")
	}
	return h, path.Clean(u.Path), nil
}

// plainName allows letters, digits, dots, underscores and dashes, not
// starting with a dash so ssh cannot take it as an option; for hosts also
// the colons of IPv6 addresses.
func plainName(s string, host bool) bool {
	if s == "" || s[0] == '-' {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
		case r == ':' && host:
		default:
			return false
		}
	}
	return true
}

func digits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// String is the host as a root prefix, like ssh://user@host:22.
func (h *Host) String() string {
	s := Prefix
	if h.User != "" {
		s += h.User + "@"
	}
	if strings.Contains(h.Name, ":") {
		s += "[" + h.Name + "]"
	} else {
		s += h.Name
	}
	if h.Port != "" {
		s += ":" + h.Port
	}
	return s
}

// Key names a folder on this host the way lazycompose keys environments:
// ssh://host/srv/stacks/web.
func (h *Host) Key(dir string) string { return h.String() + dir }

// HostOf is the host part of a key made by Key, like ssh://host, or "" for
// a local folder.
func HostOf(key string) string {
	if !IsRemote(key) {
		return ""
	}
	rest := key[len(Prefix):]
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}
	return Prefix + rest
}

// Label is a short name for the host, for the list and messages.
func (h *Host) Label() string { return h.Name }

// ControlDir holds the sockets of the shared ssh connections, one per
// host, so polling does not log in again every few seconds. Empty turns
// sharing off.
var ControlDir string

// sshProgram is the ssh client; tests replace it.
var sshProgram = "ssh"

var controlOnce sync.Once

// sharing reports whether connections can be shared: Windows' ssh cannot,
// and a socket path longer than about 100 bytes does not work on macOS.
func sharing() bool {
	if ControlDir == "" || runtime.GOOS == "windows" || len(ControlDir)+42 > 100 {
		return false
	}
	ok := true
	controlOnce.Do(func() {
		// the sockets let anyone who can reach them use the connection
		if os.MkdirAll(ControlDir, 0o700) != nil || os.Chmod(ControlDir, 0o700) != nil {
			ok = false
		}
	})
	if fi, err := os.Stat(ControlDir); err != nil || !fi.IsDir() {
		return false
	}
	return ok
}

// SSHArgs are the arguments for ssh to run args on the host. Passwords and
// questions are turned off, since a prompt would break the screen.
func (h *Host) SSHArgs(args ...string) []string { return h.sshArgs(sshBatch, args) }

// TerminalArgs are SSHArgs for a command that gets the terminal, like a
// shell: ssh gives it one with -t.
func (h *Host) TerminalArgs(args ...string) []string { return h.sshArgs(sshTerminal, args) }

type sshMode int

const (
	sshBatch    sshMode = iota // no questions, may share the connection
	sshTerminal                // the same, with a terminal for the command
	sshLogin                   // ssh may ask, and opens the shared connection
)

func (h *Host) sshArgs(mode sshMode, args []string) []string {
	var out []string
	if mode != sshLogin {
		out = append(out, "-o", "BatchMode=yes")
	}
	out = append(out,
		"-o", "ConnectTimeout=5",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=2")
	if sharing() {
		master := "auto"
		if mode == sshLogin {
			master = "yes"
		}
		out = append(out,
			"-o", "ControlMaster="+master,
			"-o", "ControlPath="+filepath.Join(ControlDir, "%C"),
			"-o", "ControlPersist=60")
	}
	if h.Port != "" {
		out = append(out, "-p", h.Port)
	}
	if mode == sshTerminal {
		out = append(out, "-t")
	}
	dest := h.Name
	if h.User != "" {
		dest = h.User + "@" + h.Name
	}
	// "--" so nothing after it is read as an option
	return append(out, "--", dest, Join(args))
}

// Command runs args on the host through ssh.
func (h *Host) Command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, sshProgram, h.SSHArgs(args...)...)
	// a shared connection started by this command keeps running; do not
	// wait for it if it holds on to the output
	cmd.WaitDelay = 2 * time.Second
	return cmd
}

// TerminalCommand runs args on the host with the terminal, for programs
// the user works in, like a shell.
func (h *Host) TerminalCommand(args ...string) *exec.Cmd {
	return exec.Command(sshProgram, h.TerminalArgs(args...)...)
}

// Join makes the command line the remote shell runs. ssh sends one string,
// so each argument is quoted for the shell there.
func Join(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = Quote(a)
	}
	return strings.Join(q, " ")
}

// Quote quotes s for a POSIX shell. Plain words stay as they are; the rest
// goes in single quotes, inside which nothing is special but the quote.
func Quote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Explain turns ssh's own error into advice when it is a common one.
func Explain(h *Host, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "remote host identification has changed"):
		return fmt.Errorf("%s: its host key changed since the last login, so ssh refuses it; if you expect that, like after a reinstall, remove the old key with ssh-keygen -R %s", h.Label(), h.Name)
	case strings.Contains(low, "host key verification failed"):
		return fmt.Errorf("%s: the host key is not known yet; run ssh %s once to check and trust it", h.Label(), h.Name)
	case strings.Contains(low, "permission denied"):
		return fmt.Errorf("%s: ssh login failed; lazycompose needs a key or ssh-agent, it cannot ask for a password", h.Label())
	case strings.Contains(low, "signal: killed"):
		return fmt.Errorf("%s: no answer in time; check that it is on and reachable", h.Label())
	case strings.Contains(low, "executable file not found"):
		return errors.New("ssh was not found in PATH; remote roots need the OpenSSH client")
	}
	return fmt.Errorf("%s: %s", h.Label(), lastLine(msg))
}

// lastLine is the last line of ssh's output that says something: ssh ends
// with the error, after warnings and banners of @ signs. Control characters
// are dropped, so it is safe to print.
func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return -1
			}
			return r
		}, lines[i]))
		if strings.Trim(l, "@ ") != "" {
			return l
		}
	}
	return ""
}
