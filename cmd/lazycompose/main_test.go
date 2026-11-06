package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mcbulazs/lazycompose/internal/ui"
)

// every action and its defaults must appear in the commented config
func TestDefaultConfigListsEveryAction(t *testing.T) {
	for _, name := range ui.ActionNames() {
		defaults := ui.DefaultKeys(name)
		want := defaults[0]
		if len(defaults) > 1 {
			want = "[" + strings.Join(defaults, ", ") + "]"
		}
		if !strings.Contains(defaultConfig, " "+name+": "+want) &&
			!strings.Contains(defaultConfig, " "+name+": \""+want+"\"") {
			t.Errorf("default config does not list %s: %s", name, want)
		}
	}
}

func TestLoadConfigKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := "keys:\n  recreate: F\n  quit: [q, Q]\n  refresh: []\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.Keys["recreate"], ",") != "F" || strings.Join(cfg.Keys["quit"], ",") != "q,Q" {
		t.Errorf("keys %v", cfg.Keys)
	}
	if ks, ok := cfg.Keys["refresh"]; !ok || len(ks) != 0 {
		t.Errorf("refresh should be an empty list, got %v (present %v)", ks, ok)
	}
}

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

func TestLoadConfigEditor(t *testing.T) {
	dir := t.TempDir()
	tests := map[string]string{
		"roots: [/srv]\n": "vim",
		"editor: nano\n":  "nano",
		"editor: \"\"\n":  "",
	}
	for data, want := range tests {
		path := filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := loadConfig(path)
		if err != nil {
			t.Fatal(err)
		}
		if *cfg.Editor != want {
			t.Errorf("%q: editor %q, want %q", data, *cfg.Editor, want)
		}
	}
}

func TestUpdateEvery(t *testing.T) {
	for in, want := range map[string]time.Duration{"6h": 6 * time.Hour, "30m": 30 * time.Minute, "0": 0, "off": 0, "": 0} {
		if got, err := updateEvery(in); err != nil || got != want {
			t.Errorf("updateEvery(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"soon", "10s", "6"} {
		if _, err := updateEvery(in); err == nil {
			t.Errorf("updateEvery(%q) should fail", in)
		}
	}
}

func TestLoadConfigComposeFilename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	for data, want := range map[string]string{
		"roots: [/srv]\n":                      "docker-compose.yml",
		"new_compose_filename: compose.yaml\n": "compose.yaml",
	} {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := loadConfig(path)
		if err != nil || cfg.Filename != want {
			t.Errorf("%q: filename %q, %v, want %q", data, cfg.Filename, err, want)
		}
	}
	if err := os.WriteFile(path, []byte("new_compose_filename: stack.yml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(path); err == nil || !strings.Contains(err.Error(), "compose.yaml") {
		t.Errorf("a name the scan does not find should be an error listing the names, got %v", err)
	}
}

func TestCheckConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	for data, ok := range map[string]bool{
		"roots: [/srv]\n":                  true,
		"theme: nope\n":                    false,
		"keys:\n  explode: x\n":            false,
		"keys:\n  restart: d\n":            false,
		"roots: [\n":                       false,
		"keys:\n  edit_config: [ctrl+o]\n": true,
	} {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := checkConfig(path); (err == nil) != ok {
			t.Errorf("%q: checkConfig = %v, want ok %v", data, err, ok)
		}
	}
}
