package discover

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/mcbulazs/lazycompose/internal/remote"
)

// tree makes stacks that exercise every rule of Find.
func tree(t *testing.T) string {
	root := t.TempDir()
	for _, f := range []string{
		"web/blog/compose.yaml",
		"web/shop/docker-compose.yml",
		"web/shop/compose.yml", // comes first in ComposeFiles order
		"games/minecraft/compose.yml",
		"jellyfin/compose.yaml",
		"jellyfin/config/old/compose.yaml", // inside an environment
		"a/b/c/d/compose.yaml",             // too deep for 3
		"a/b/c/compose.yaml",
		".trash/old/compose.yaml", // hidden
		"app/node_modules/x/compose.yaml",
		"go/pkg/mod/x/compose.yaml", // the Go module cache
		"notes/readme.yaml",
		"compose.yaml", // the root itself is not an environment
		"with space/it's/compose.yaml",
	} {
		touch(t, filepath.Join(root, filepath.FromSlash(f)))
	}
	return root
}

func TestFromFilesMatchesFind(t *testing.T) {
	root := tree(t)
	// every compose file, as an unfiltered find would list them
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			files = append(files, "/r/"+filepath.ToSlash(rel))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, depth := range []int{0, 1, 3} {
		envs, err := Find([]string{root}, depth)
		if err != nil {
			t.Fatal(err)
		}
		var want, got []string
		for _, e := range envs {
			want = append(want, e.Rel+" "+filepath.Base(e.File))
		}
		for _, f := range FromFiles("/r", files, depth) {
			got = append(got, strings.TrimPrefix(f.Dir, "/r/")+" "+filepath.Base(f.File))
		}
		if !slices.Equal(got, want) {
			t.Errorf("depth %d: FromFiles found\n%q\nFind found\n%q", depth, got, want)
		}
	}
}

// fakeSSH puts an ssh in PATH that runs the remote command here, so the
// real find command and its quoting are tested.
func fakeSSH(t *testing.T) {
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

func TestFindRemote(t *testing.T) {
	fakeSSH(t)
	root := tree(t)
	envs, err := FindRoots([]string{"ssh://server" + filepath.ToSlash(root)}, 3)
	if err != nil {
		t.Fatal(err)
	}
	local, _ := Find([]string{root}, 3)
	if len(envs) != len(local) {
		t.Fatalf("found %d, want %d: %+v", len(envs), len(local), envs)
	}
	for i, e := range envs {
		l := local[i]
		if e.Rel != l.Rel || e.File != l.File || e.Path != l.Dir || e.Dir != "ssh://server"+l.Dir || !e.Remote() {
			t.Errorf("remote %+v, local %+v", e, l)
		}
		if e.Name() != l.Name() || e.RootName() != "server:"+filepath.Base(root) {
			t.Errorf("%s: name %q root %q", e.Rel, e.Name(), e.RootName())
		}
	}

	// a missing remote root is reported, and does not hide the local ones
	envs, err = FindRoots([]string{root, "ssh://server/does/not/exist"}, 3)
	if err == nil || !strings.Contains(err.Error(), "server: ") || len(envs) != len(local) {
		t.Errorf("got %d envs and error %v", len(envs), err)
	}
}
