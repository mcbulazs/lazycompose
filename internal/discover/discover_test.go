package discover

import (
	"os"
	"path/filepath"
	"testing"
)

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFind(t *testing.T) {
	root := t.TempDir()
	touch(t, filepath.Join(root, "web/blogger/compose.yaml"))
	touch(t, filepath.Join(root, "web/shop/docker-compose.yml"))
	touch(t, filepath.Join(root, "game-servers/minecraft/compose.yml"))
	touch(t, filepath.Join(root, "plex/compose.yaml"))
	// nested inside an environment, should be ignored
	touch(t, filepath.Join(root, "plex/config/old/compose.yaml"))
	// too deep for maxDepth 3
	touch(t, filepath.Join(root, "a/b/c/d/compose.yaml"))
	// hidden folders are skipped
	touch(t, filepath.Join(root, ".trash/old/compose.yaml"))
	// not a compose file
	touch(t, filepath.Join(root, "notes/readme.yaml"))

	envs, err := Find([]string{root}, 3)
	if err != nil {
		t.Fatal(err)
	}

	want := []struct{ rel, file, group, name string }{
		{"game-servers/minecraft", "compose.yml", "game-servers", "minecraft"},
		{"plex", "compose.yaml", "", "plex"},
		{"web/blogger", "compose.yaml", "web", "blogger"},
		{"web/shop", "docker-compose.yml", "web", "shop"},
	}
	if len(envs) != len(want) {
		t.Fatalf("got %d envs, want %d: %+v", len(envs), len(want), envs)
	}
	for i, w := range want {
		e := envs[i]
		if e.Rel != w.rel {
			t.Errorf("env %d: rel %q, want %q", i, e.Rel, w.rel)
		}
		if filepath.Base(e.File) != w.file {
			t.Errorf("%s: file %q, want %q", e.Rel, filepath.Base(e.File), w.file)
		}
		if e.Group() != w.group {
			t.Errorf("%s: group %q, want %q", e.Rel, e.Group(), w.group)
		}
		if e.Name() != w.name {
			t.Errorf("%s: name %q, want %q", e.Rel, e.Name(), w.name)
		}
		if e.Dir != filepath.Join(root, w.rel) {
			t.Errorf("%s: dir %q", e.Rel, e.Dir)
		}
	}
}

func TestFindPrefersComposeYaml(t *testing.T) {
	root := t.TempDir()
	touch(t, filepath.Join(root, "app/docker-compose.yml"))
	touch(t, filepath.Join(root, "app/compose.yaml"))

	envs, err := Find([]string{root}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 1 || filepath.Base(envs[0].File) != "compose.yaml" {
		t.Fatalf("unexpected result: %+v", envs)
	}
}

func TestFindDepth(t *testing.T) {
	root := t.TempDir()
	touch(t, filepath.Join(root, "one/compose.yaml"))
	touch(t, filepath.Join(root, "x/two/compose.yaml"))

	envs, err := Find([]string{root}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 1 || envs[0].Rel != "one" {
		t.Fatalf("unexpected result: %+v", envs)
	}
}

func TestFindMultipleRoots(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	touch(t, filepath.Join(a, "app/compose.yaml"))
	touch(t, filepath.Join(b, "other/compose.yaml"))

	envs, err := Find([]string{a, b, a}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 2 {
		t.Fatalf("got %d envs, want 2: %+v", len(envs), envs)
	}
}

func TestFindMissingRoot(t *testing.T) {
	if _, err := Find([]string{filepath.Join(t.TempDir(), "nope")}, 3); err == nil {
		t.Fatal("expected error for missing root")
	}
}
