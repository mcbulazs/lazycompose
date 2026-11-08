package stackfs

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mcbulazs/lazycompose/internal/discover"
	"github.com/mcbulazs/lazycompose/internal/remote"
)

// fakeSSH puts an ssh in PATH that runs the remote command here.
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
	remote.ControlDir = ""
}

func TestCheckRemote(t *testing.T) {
	fakeSSH(t)
	ctx := context.Background()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.MkdirAll(filepath.Join(root, "web", "blog"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "out")); err != nil {
		t.Fatal(err)
	}
	h := &remote.Host{Name: "server"}
	roots := []string{"/local/stacks", "ssh://server" + root, "ssh://other" + root}
	blog := discover.Env{Rel: "web/blog", Dir: h.Key(root + "/web/blog"), Host: h, Path: root + "/web/blog"}
	envs := []discover.Env{blog}

	tests := []struct {
		target, self, wantErr string
	}{
		{target: "ssh://server" + root + "/games/mc"},
		{target: "ssh://server" + root + "/new stack"},
		{target: "ssh://server/elsewhere/x", wantErr: "not inside the scanned folders"},
		{target: "ssh://nobody" + root + "/x", wantErr: "not a scanned host"},
		{target: "ssh://server" + root, wantErr: "folder name after"},
		{target: "ssh://server" + root + "/a/b/c/d", wantErr: "too deep"},
		{target: "ssh://server" + root + "/.hidden/x", wantErr: "skips"},
		{target: "ssh://server" + root + "/web/blog/sub", wantErr: "inside environment web/blog"},
		{target: "ssh://server" + root + "/web", wantErr: "already exists on server"},
		{target: "ssh://server" + root + "/out/x", wantErr: "through a symlink"},
		{target: "ssh://server" + root + "/web/blog", self: root + "/web/blog", wantErr: "already is"},
		{target: "ssh://server" + root + "/web/blog/in", self: root + "/web/blog", wantErr: "into itself"},
		{target: "ssh://server" + root + "/blog2", self: root + "/web/blog"},
	}
	for _, tt := range tests {
		got, err := CheckRemote(ctx, tt.target, roots, 3, envs, tt.self)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%s: error %v, want %q", tt.target, err, tt.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tt.target, err)
			continue
		}
		if got.Host == nil || got.Root != "ssh://server"+root || !strings.HasPrefix(tt.target, "ssh://server"+got.Dir) {
			t.Errorf("%s: target %+v", tt.target, got)
		}
	}
}

func TestCreateMoveRemote(t *testing.T) {
	fakeSSH(t)
	ctx := context.Background()
	root := t.TempDir()
	h := &remote.Host{Name: "server"}
	roots := []string{"ssh://server" + root}

	tg, err := CheckRemote(ctx, "ssh://server"+root+"/apps/my web", roots, 0, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	file, err := CreateRemote(ctx, tg, "compose.yaml", []byte(Starter), true)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(file); string(b) != Starter {
		t.Errorf("compose file holds %q", b)
	}
	if fi, _ := os.Stat(file); fi.Mode().Perm() != 0o644 {
		t.Errorf("compose file mode %v", fi.Mode().Perm())
	}
	if fi, err := os.Stat(filepath.Join(tg.Dir, "."+"env")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("the env file should be private: %v %v", fi, err)
	}
	e := tg.Env("compose.yaml")
	if e.Dir != h.Key(root+"/apps/my web") || e.Path != tg.Dir || e.File != file || e.Rel != "apps/my web" {
		t.Errorf("env %+v", e)
	}
	if _, err := CreateRemote(ctx, tg, "compose.yaml", nil, false); err == nil {
		t.Error("creating it twice should fail")
	}
	if _, err := CreateRemote(ctx, tg, "stack.yaml", nil, false); err == nil {
		t.Error("a name the scan does not find should be refused")
	}

	to, err := CheckRemote(ctx, "ssh://server"+root+"/moved/web", roots, 0, []discover.Env{e}, e.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := MoveRemote(ctx, e.Path, to); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "moved", "web", "compose.yaml")); err != nil {
		t.Errorf("not moved: %v", err)
	}
	if _, err := os.Stat(e.Path); !os.IsNotExist(err) {
		t.Errorf("the old folder should be gone: %v", err)
	}
	// a folder that appeared meanwhile is not moved into
	if err := os.MkdirAll(filepath.Join(root, "late"), 0o755); err != nil {
		t.Fatal(err)
	}
	late := Target{Root: tg.Root, Dir: root + "/late", Rel: "late", Host: h}
	if err := MoveRemote(ctx, to.Dir, late); err == nil || !strings.Contains(err.Error(), "meanwhile") {
		t.Errorf("moving onto a folder that is there: %v", err)
	}
}

func TestComposeWarnings(t *testing.T) {
	b := []byte("services:\n  web:\n    volumes:\n      - ../shared:/data\n")
	w := ComposeWarnings(b, "blog", "blog2", false)
	if len(w) != 2 || !strings.Contains(w[0], `"blog" to "blog2"`) || !strings.Contains(w[1], "../shared") {
		t.Errorf("warnings %q", w)
	}
	if w := ComposeWarnings([]byte("name: x\n"), "a", "b", true); len(w) != 0 {
		t.Errorf("a set name keeps the project: %q", w)
	}
}
