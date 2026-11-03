package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadConfigCreatesDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lazycompose", "config.yaml")

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file not created: %v", err)
	}

	// the written file must load back to the same defaults
	again, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg, again) {
		t.Fatalf("defaults %+v, file gives %+v", cfg, again)
	}
}

func TestLoadConfigKeepsExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("roots: [/srv/stacks]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Roots) != 1 || cfg.Roots[0] != "/srv/stacks" || cfg.MaxDepth != 0 || cfg.Theme != "terminal" {
		t.Fatalf("unexpected config %+v", cfg)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "roots: [/srv/stacks]\n" {
		t.Fatalf("existing file was changed: %q", b)
	}
}

func TestLoadConfigThemes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := `theme: sunset
themes:
  sunset:
    base: onedark
    focus: "#ff9e64"
    dim: 8
`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"base": "onedark", "focus": "#ff9e64", "dim": "8"}
	if cfg.Theme != "sunset" || !reflect.DeepEqual(cfg.Themes["sunset"], want) {
		t.Fatalf("unexpected config %+v", cfg)
	}
}
