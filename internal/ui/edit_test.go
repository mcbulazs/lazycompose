package ui

import (
	"errors"
	"fmt"
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

// serverEnv is a remote environment whose host is this machine, in dir.
func serverEnv(dir string) discover.Env {
	h := &remote.Host{Name: "server"}
	return discover.Env{Root: h.Key(filepath.Dir(dir)), Rel: filepath.Base(dir), Dir: h.Key(dir),
		File: filepath.Join(dir, "compose.yaml"), Host: h, Path: dir}
}

func TestRemoteEdit(t *testing.T) {
	fakeSSH(t)
	tempDir(t)
	stack := filepath.Join(t.TempDir(), "my stack")
	if err := os.Mkdir(stack, 0o755); err != nil {
		t.Fatal(err)
	}
	e := serverEnv(stack)
	if err := os.WriteFile(e.File, []byte("services: {}\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	ready := copyForEdit(1, e, e.File)().(editReadyMsg)
	if ready.err != nil {
		t.Fatal(ready.err)
	}
	if !strings.HasPrefix(filepath.Base(ready.dir), fmt.Sprintf("%s%d-", editDirPrefix, os.Getpid())) || filepath.Base(ready.file) != "compose.yaml" {
		t.Errorf("copy at %s", ready.file)
	}
	if fi, _ := os.Stat(ready.dir); fi.Mode().Perm() != 0o700 {
		t.Errorf("the folder of the copy should be private, got %v", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(ready.file); fi.Mode().Perm() != 0o600 {
		t.Errorf("the copy should be private, got %v", fi.Mode().Perm())
	}

	// a save is written back, keeping the file's permissions
	saved := []byte("services: {web: {}}\n")
	up := upload(1, e, e.File, saved, ready.server, ready.exists)().(uploadedMsg)
	if up.err != nil {
		t.Fatal(up.err)
	}
	if b, _ := os.ReadFile(e.File); string(b) != string(saved) {
		t.Errorf("server file holds %q", b)
	}
	if fi, _ := os.Stat(e.File); fi.Mode().Perm() != 0o640 {
		t.Errorf("writing back should keep the permissions, got %v", fi.Mode().Perm())
	}

	// a change on the server meanwhile is not overwritten
	if err := os.WriteFile(e.File, []byte("services: {other: {}}\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	up = upload(1, e, e.File, []byte("mine"), up.server, true)().(uploadedMsg)
	if !errors.Is(up.err, errChanged) {
		t.Errorf("a file changed on the server: %v", up.err)
	}
	if b, _ := os.ReadFile(e.File); string(b) == "mine" {
		t.Error("the change on the server was overwritten")
	}

	// a file that is not there yet is made on the first save, private
	vars := filepath.Join(stack, "vars")
	ready = copyForEdit(2, e, vars)().(editReadyMsg)
	if ready.err != nil || ready.exists {
		t.Fatalf("missing file: %+v", ready)
	}
	if up := upload(2, e, vars, []byte("A=1\n"), ready.server, false)().(uploadedMsg); up.err != nil {
		t.Fatal(up.err)
	}
	if fi, err := os.Stat(vars); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("a new file should be 0600: %v %v", fi, err)
	}
}

func TestRemoteEditConflictKeepsCopy(t *testing.T) {
	m := newTestModel(t)
	dir := t.TempDir()
	copyFile := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(copyFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.busy = 1
	m.editID = 5
	m.edit = &editWatch{id: 5, env: serverEnv("/stacks/web"), file: copyFile, remote: "/stacks/web/compose.yaml", dir: dir, uploading: true}
	m = send(m, uploadedMsg{id: 5, err: errChanged})
	if m.edit != nil || !strings.Contains(m.status, copyFile) || !m.statusErr || len(m.leftovers) != 1 {
		t.Errorf("status %q, edit %v", m.status, m.edit)
	}
	if _, err := os.Stat(copyFile); err != nil {
		t.Error("the copy with the user's work should be kept")
	}

	// an ended edit removes its copy
	m.editID = 6
	m.edit = &editWatch{id: 6, file: copyFile, remote: "/x", dir: dir}
	m.endEdit()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the copy should be gone: %v", err)
	}
}

func TestRemoveLeftovers(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		editDirPrefix + "111-abc",                         // dead
		editDirPrefix + "222-abc",                         // alive
		logsFilePrefix + "111-x.log",                      // dead
		editDirPrefix + "notapid-x",                       // not ours
		"other-111-x",                                     // not ours
		fmt.Sprintf("%s%d-x", editDirPrefix, os.Getpid()), // this one
	}
	for _, n := range names {
		if err := os.Mkdir(filepath.Join(dir, n), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	removeLeftovers(dir, func(pid int) bool { return pid == 222 })
	for i, n := range names {
		_, err := os.Stat(filepath.Join(dir, n))
		gone := os.IsNotExist(err)
		if want := i == 0 || i == 2; gone != want {
			t.Errorf("%s: removed %v, want %v", n, gone, want)
		}
	}
	if !processAlive(os.Getpid()) {
		t.Error("this process should count as alive")
	}
}
