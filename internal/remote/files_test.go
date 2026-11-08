package remote

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeSSH puts an ssh in PATH that runs the remote command here, so the
// real commands and their quoting are tested.
func fakeSSH(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell")
	}
	bin := t.TempDir()
	script := "#!/bin/sh\neval \"last=\\${$#}\"\nexec sh -c \"$last\"\n"
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ControlDir = ""
}

func TestReadWriteFile(t *testing.T) {
	fakeSSH(t)
	ctx := context.Background()
	h := &Host{Name: "server"}
	dir := t.TempDir()
	p := filepath.Join(dir, "it's here", "app.conf")

	if _, exists, err := h.ReadFile(ctx, p, 100); exists || err != nil {
		t.Fatalf("missing file: exists %v, err %v", exists, err)
	}
	if err := os.Mkdir(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := h.WriteFile(ctx, p, []byte("A=1\n"), "600"); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("a new file should be 0600, got %v", fi.Mode().Perm())
	}
	// an existing file keeps its permissions
	if err := os.Chmod(p, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := h.WriteFile(ctx, p, []byte("B=2\n"), ""); err != nil {
		t.Fatal(err)
	}
	data, exists, err := h.ReadFile(ctx, p, 100)
	if err != nil || !exists || string(data) != "B=2\n" {
		t.Errorf("read back %q %v %v", data, exists, err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o640 {
		t.Errorf("writing should keep the permissions, got %v", fi.Mode().Perm())
	}
	if _, _, err := h.ReadFile(ctx, p, 2); !errors.Is(err, ErrTooBig) {
		t.Errorf("over the limit: %v", err)
	}
	if err := h.WriteFile(ctx, filepath.Join(dir, "no/such/dir/f"), nil, "600"); err == nil || !strings.HasPrefix(err.Error(), "server: ") {
		t.Errorf("writing into a missing folder: %v", err)
	}
}

func TestTerminalArgs(t *testing.T) {
	ControlDir = ""
	got := (&Host{Name: "server"}).TerminalArgs("docker", "compose", "exec", "web", "bash")
	if i := slices.Index(got, "-t"); i < 0 || i > slices.Index(got, "--") {
		t.Errorf("-t should come before the host: %q", got)
	}
	if slices.Contains((&Host{Name: "server"}).SSHArgs("true"), "-t") {
		t.Error("only terminal commands get -t")
	}
}

func TestExistsRealPath(t *testing.T) {
	fakeSSH(t)
	ctx := context.Background()
	h := &Host{Name: "server"}
	dir := t.TempDir()
	if ok, err := h.Exists(ctx, filepath.Join(dir, "nope")); ok || err != nil {
		t.Errorf("missing: %v %v", ok, err)
	}
	if ok, err := h.Exists(ctx, dir); !ok || err != nil {
		t.Errorf("folder: %v %v", ok, err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink("/", link); err != nil {
		t.Fatal(err)
	}
	if ok, _ := h.Exists(ctx, link); !ok {
		t.Error("a link counts as there")
	}
	if real, err := h.RealPath(ctx, link); err != nil || real != "/" {
		t.Errorf("RealPath = %q, %v", real, err)
	}
	if err := h.Run(ctx, "mkdir", "--", filepath.Join(dir, "a b")); err != nil {
		t.Fatal(err)
	}
	if err := h.Run(ctx, "mkdir", "--", filepath.Join(dir, "a b")); err == nil || !strings.HasPrefix(err.Error(), "server: ") {
		t.Errorf("mkdir twice: %v", err)
	}
}

func TestLoginArgs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no shared connections on Windows")
	}
	ControlDir = t.TempDir()
	defer func() { ControlDir = "" }()
	h := &Host{Name: "server"}
	login := h.LoginCommand().Args[1:]
	if slices.Contains(login, "BatchMode=yes") || !slices.Contains(login, "ControlMaster=yes") || login[len(login)-1] != "true" {
		t.Errorf("login lets ssh ask and opens the connection: %q", login)
	}
	if batch := h.SSHArgs("true"); !slices.Contains(batch, "BatchMode=yes") || !slices.Contains(batch, "ControlMaster=auto") {
		t.Errorf("other commands never ask and join it: %q", batch)
	}
}

func TestHold(t *testing.T) {
	h := &Host{Name: "hold-test"}
	ctx := context.Background()
	var releases []func()
	for range shortSessions {
		r, err := h.Hold(ctx)
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, r)
	}
	// all taken: the next one waits until one comes back
	short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, err := h.Hold(short); err == nil {
		t.Fatal("a host should not get more than shortSessions at once")
	}
	releases[0]()
	releases[0]() // giving back twice is harmless
	if r, err := h.Hold(ctx); err != nil {
		t.Errorf("a freed session should be taken: %v", err)
	} else {
		r()
	}
	// other hosts and this machine have their own
	if r, err := (&Host{Name: "other"}).Hold(ctx); err != nil {
		t.Errorf("another host: %v", err)
	} else {
		r()
	}
	var local *Host
	if r, err := local.Hold(ctx); err != nil {
		t.Errorf("this machine has no limit: %v", err)
	} else {
		r()
	}
}

func TestExplainRefusedSession(t *testing.T) {
	out := "mux_client_request_session: session request failed: Session open refused by peer\nroot@h: Permission denied (publickey).\n"
	err := Explain(&Host{Name: "h"}, errors.New(out))
	if !strings.Contains(err.Error(), "MaxSessions") || !errors.Is(err, ErrSessionRefused) {
		t.Errorf("a refused session should name MaxSessions, got %q", err)
	}
}
