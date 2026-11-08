package remote

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		root      string
		host      Host
		dir, key  string
		wantError string
	}{
		{root: "ssh://server/srv/stacks", host: Host{Name: "server"}, dir: "/srv/stacks", key: "ssh://server/srv/stacks"},
		{root: "ssh://bulazs@192.168.0.100:2222/mnt/data/", host: Host{User: "bulazs", Name: "192.168.0.100", Port: "2222"}, dir: "/mnt/data", key: "ssh://bulazs@192.168.0.100:2222/mnt/data"},
		{root: "ssh://[::1]/a/../b", host: Host{Name: "::1"}, dir: "/b", key: "ssh://[::1]/b"},
		{root: "ssh://h/my%20stacks", host: Host{Name: "h"}, dir: "/my stacks", key: "ssh://h/my stacks"},
		{root: "ssh://h/", host: Host{Name: "h"}, dir: "/", key: "ssh://h/"},
		{root: "ssh://-oProxyCommand=evil/x", wantError: "host"},
		{root: "ssh://-x@h/x", wantError: "user"},
		{root: "ssh://a;b/x", wantError: "host"},
		{root: "ssh://u:pw@h/x", wantError: "password"},
		{root: "ssh://h:abc/x", wantError: "not a remote root"},
		{root: "ssh://h", wantError: "absolute"},
		{root: "ssh://h/x?y=1", wantError: "ssh://host/folder"},
		{root: "ssh://h/a%0Ab", wantError: "control"},
		{root: "http://h/x", wantError: "not a remote root"},
		{root: "ssh:h/x", wantError: "ssh://host/folder"},
	}
	for _, tt := range tests {
		h, dir, err := Parse(tt.root)
		if tt.wantError != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Errorf("%s: error %v, want one about %q", tt.root, err, tt.wantError)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tt.root, err)
			continue
		}
		if *h != tt.host || dir != tt.dir || h.Key(dir) != tt.key {
			t.Errorf("%s: %+v %q key %q, want %+v %q key %q", tt.root, *h, dir, h.Key(dir), tt.host, tt.dir, tt.key)
		}
	}
}

func TestQuote(t *testing.T) {
	tests := map[string]string{
		"docker":                   "docker",
		"/srv/stacks/compose.yaml": "/srv/stacks/compose.yaml",
		"label=com.docker.compose": "label=com.docker.compose",
		"":                         "''",
		"my stacks":                "'my stacks'",
		"it's":                     `'it'\''s'`,
		"$(rm -rf /)":              "'$(rm -rf /)'",
		"a;b|c&d`e`":               "'a;b|c&d`e`'",
		`{{"\t"}}`:                 `'{{"\t"}}'`,
		"-":                        "-",
		"*":                        "'*'",
		"~":                        "'~'",
	}
	for in, want := range tests {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q) = %s, want %s", in, got, want)
		}
	}
}

// TestQuoteShell runs the quoted words through a real shell and checks
// they come back unchanged.
func TestQuoteShell(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil || runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell")
	}
	words := []string{"plain", "with space", "it's", `back\slash`, "$HOME", "`id`", "$(id)", "a\nb", "'", `"`, "*", "~", "!x", "", "--", "-n", "é ü 字"}
	script := "set -- " + Join(words) + `; for a; do printf '%s\0' "$a"; done`
	out, err := exec.Command(sh, "-c", script).Output()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if !slices.Equal(got, words) {
		t.Errorf("the shell read %q, want %q", got, words)
	}
}

func TestSSHArgs(t *testing.T) {
	ControlDir = ""
	h := &Host{User: "me", Name: "server", Port: "2222"}
	got := h.SSHArgs("docker", "compose", "-f", "/srv/my stack/compose.yaml", "ps")
	want := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=2",
		"-p", "2222", "--", "me@server", "docker compose -f '/srv/my stack/compose.yaml' ps"}
	if !slices.Equal(got, want) {
		t.Errorf("args\n%q\nwant\n%q", got, want)
	}

	if runtime.GOOS == "windows" {
		return
	}
	ControlDir = t.TempDir()
	defer func() { ControlDir = "" }()
	got = (&Host{Name: "server"}).SSHArgs("true")
	if !slices.Contains(got, "ControlMaster=auto") || !slices.Contains(got, "ControlPath="+ControlDir+"/%C") {
		t.Errorf("with ControlDir set the connection should be shared: %q", got)
	}
	ControlDir = "/" + strings.Repeat("x", 80)
	if got := (&Host{Name: "server"}).SSHArgs("true"); slices.Contains(got, "ControlMaster=auto") {
		t.Errorf("a socket path that is too long should turn sharing off: %q", got)
	}
}

func TestCommandRunsSSH(t *testing.T) {
	sshProgram = "ssh-test-program"
	defer func() { sshProgram = "ssh" }()
	cmd := (&Host{Name: "server"}).Command(context.Background(), "true")
	if cmd.Args[0] != "ssh-test-program" || cmd.Args[len(cmd.Args)-2] != "server" {
		t.Errorf("args %q", cmd.Args)
	}
}

func TestExplain(t *testing.T) {
	h := &Host{Name: "server"}
	for in, want := range map[string]string{
		"Host key verification failed.":                   "run ssh server once",
		"me@server: Permission denied (publickey).":       "needs a key or ssh-agent",
		"ssh: connect to host server: No route":           "server: ssh: connect to host",
		"signal: killed":                                  "no answer in time",
		`exec: "ssh": executable file not found in $PATH`: "OpenSSH client",
	} {
		if got := Explain(h, errors.New(in)); !strings.Contains(got.Error(), want) {
			t.Errorf("Explain(%q) = %q, want it to say %q", in, got, want)
		}
	}
	if Explain(h, nil) != nil {
		t.Error("Explain(nil) should be nil")
	}
}

// TestQuoteShells checks the quoting in the shells a server may log in
// with, those that are installed here.
func TestQuoteShells(t *testing.T) {
	words := []string{"with space", "it's", `back\slash`, "$HOME", "`id`", "$(id)", "*", "~", "!x", "", `{{"\t"}}`, "é"}
	var want strings.Builder
	for _, w := range words {
		want.WriteString("[" + w + "]")
	}
	for _, sh := range []string{"sh", "bash", "zsh", "dash", "fish"} {
		p, err := exec.LookPath(sh)
		if err != nil || runtime.GOOS == "windows" {
			continue
		}
		out, err := exec.Command(p, "-c", "printf '[%s]' "+Join(words)).Output()
		if err != nil {
			t.Errorf("%s: %v", sh, err)
			continue
		}
		if string(out) != want.String() {
			t.Errorf("%s printed %s, want %s", sh, out, want.String())
		}
	}
}

func TestExplainBanner(t *testing.T) {
	h := &Host{Name: "server"}
	changed := "@@@@@@@@@@@@@@@@@@@@@@@@@@@@\n@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @\n@@@@@@@@@@@@@@@@@@@@@@@@@@@@\nIT IS POSSIBLE THAT SOMEONE IS DOING SOMETHING NASTY!\nHost key verification failed.\n"
	if got := Explain(h, errors.New(changed)).Error(); !strings.Contains(got, "ssh-keygen -R server") {
		t.Errorf("changed host key: %q", got)
	}
	other := "Warning: Permanently added 'x' to the list of known hosts.\r\nssh: connect to host server port 22: Connection refused\n"
	if got := Explain(h, errors.New(other)).Error(); got != "server: ssh: connect to host server port 22: Connection refused" {
		t.Errorf("last line: %q", got)
	}
}
