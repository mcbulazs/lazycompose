package stackfs

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mcbulazs/lazycompose/internal/discover"
)

func setup(t *testing.T) (root string, envs []discover.Env) {
	t.Helper()
	root = t.TempDir()
	for _, rel := range []string{"web/blog", "plex"} {
		dir := filepath.Join(root, rel)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(dir, "compose.yaml")
		if err := os.WriteFile(file, []byte("services: {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		envs = append(envs, discover.Env{Root: root, Rel: rel, Dir: dir, File: file})
	}
	return root, envs
}

func TestCheck(t *testing.T) {
	root, envs := setup(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	ok, err := Check(root+"/web/shop/", []string{root}, 0, envs, "")
	if err != nil || ok.Rel != "web/shop" || ok.Dir != filepath.Join(root, "web/shop") {
		t.Fatalf("Check = %+v, %v", ok, err)
	}
	if _, err := Check(root+"/new/deep/app", []string{root}, 0, envs, ""); err != nil {
		t.Errorf("missing parent folders should be fine: %v", err)
	}

	bad := map[string]string{
		"":                         "type a folder",
		"web/shop":                 "full path",
		outside + "/x":             "not inside",
		root + "/":                 "folder name",
		root + "/web/blog":         "already exists",
		root + "/web/../plex":      "already exists",
		root + "/plex/sub":         "inside environment plex",
		root + "/.hidden/app":      "skips",
		root + "/a/node_modules/b": "skips",
		root + "/link/app":         "symlink",
		root + "/../escape":        "not inside",
	}
	for path, want := range bad {
		_, err := Check(path, []string{root}, 0, envs, "")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Check(%q) = %v, want an error with %q", path, err, want)
		}
	}
	if _, err := Check(root+"/a/b/c", []string{root}, 2, envs, ""); err == nil || !strings.Contains(err.Error(), "max_depth") {
		t.Errorf("too deep for max_depth should fail, got %v", err)
	}

	// moving
	blog := envs[0].Dir
	if _, err := Check(blog, []string{root}, 0, envs, blog); err == nil {
		t.Error("moving to the same place should fail")
	}
	if _, err := Check(blog+"/inner", []string{root}, 0, envs, blog); err == nil || !strings.Contains(err.Error(), "into itself") {
		t.Errorf("moving into itself should fail, got %v", err)
	}
	if _, err := Check(root+"/blog", []string{root}, 0, envs, blog); err != nil {
		t.Errorf("a plain move should pass: %v", err)
	}
}

func TestCreate(t *testing.T) {
	root, envs := setup(t)
	target, err := Check(root+"/apps/new", []string{root}, 0, envs, "")
	if err != nil {
		t.Fatal(err)
	}
	file, err := Create(target, "compose.yaml", []byte(Starter), true)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(file); string(b) != Starter || filepath.Base(file) != "compose.yaml" {
		t.Errorf("compose file %s = %q", file, b)
	}
	info, err := os.Stat(filepath.Join(target.Dir, ".env"))
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Errorf("the env file should be created with mode 600: %v %v", info, err)
	}
	if found, _ := discover.Find([]string{root}, 0); len(found) != 3 {
		t.Errorf("the scan should find the new environment, found %d", len(found))
	}
	if _, err := Create(target, ComposeName, nil, false); err == nil {
		t.Error("creating over an existing folder should fail")
	}
	other, _ := Check(root+"/apps/other", []string{root}, 0, envs, "")
	if _, err := Create(other, "stack.yml", nil, false); err == nil {
		t.Error("a name the scan does not find should fail")
	}
	if _, err := os.Stat(other.Dir); !os.IsNotExist(err) {
		t.Error("a bad name should fail before creating the folder")
	}
}

func TestMove(t *testing.T) {
	root, envs := setup(t)
	blog := envs[0]
	if err := os.WriteFile(filepath.Join(blog.Dir, "data.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	target, err := Check(root+"/sites/blog2", []string{root}, 0, envs, blog.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := Move(blog.Dir, target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(blog.Dir); !os.IsNotExist(err) {
		t.Error("the old folder should be gone")
	}
	if b, _ := os.ReadFile(filepath.Join(target.Dir, "data.txt")); string(b) != "keep" {
		t.Error("files should move with the folder")
	}
}

func TestCopyTree(t *testing.T) {
	from := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(filepath.Join(from, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(from, "sub", "a"), []byte("a"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("sub/a", filepath.Join(from, "link")); err != nil {
		t.Fatal(err)
	}
	to := filepath.Join(t.TempDir(), "dst")
	if err := os.Mkdir(to, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(from, to); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(to, "sub", "a"))
	// Windows has no Unix file modes to keep
	if info == nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o640) {
		t.Errorf("file mode not kept: %v", info)
	}
	if l, _ := os.Readlink(filepath.Join(to, "link")); l != filepath.FromSlash("sub/a") {
		t.Errorf("symlink = %q", l)
	}
}

func TestWarnings(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "compose.yaml")
	compose := "services:\n  app:\n    volumes:\n      - ../shared:/data\n      - ./local:/x\n    env_file: \"../common.env\"\n"
	if err := os.WriteFile(file, []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}

	rename := Warnings(file, "/srv/web/My Blog", Target{Dir: "/srv/web/blog2"})
	if len(rename) != 1 || !strings.Contains(rename[0], `"myblog" to "blog2"`) {
		t.Errorf("rename warnings = %v", rename)
	}
	move := Warnings(file, "/srv/web/blog", Target{Dir: "/srv/sites/blog"})
	if len(move) != 1 || !strings.Contains(move[0], "../common.env, ../shared") {
		t.Errorf("move warnings = %v", move)
	}

	if err := os.WriteFile(file, []byte("name: blog\nservices: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if w := Warnings(file, "/srv/web/blog", Target{Dir: "/srv/web/other"}); len(w) != 0 {
		t.Errorf("a set name: keeps the project, got %v", w)
	}
}
