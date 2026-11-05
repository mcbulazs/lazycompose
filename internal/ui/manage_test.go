package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mcbulazs/lazycompose/internal/compose"
	"github.com/mcbulazs/lazycompose/internal/discover"
	"github.com/mcbulazs/lazycompose/internal/history"
)

// diskModel is a model over real folders: root/web/blog and root/plex.
func diskModel(t *testing.T) (Model, string) {
	t.Helper()
	root := t.TempDir()
	for _, rel := range []string{"web/blog", "plex"} {
		dir := filepath.Join(root, rel)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services: {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	envs, err := discover.Find([]string{root}, 0)
	if err != nil {
		t.Fatal(err)
	}
	m := New(Options{Hostname: "host", Roots: []string{root}, Envs: envs, Editor: "true", History: history.Load("")})
	return send(m, tea.WindowSizeMsg{Width: 120, Height: 30}), root
}

func typeInPrompt(t *testing.T, m Model, value string) Model {
	t.Helper()
	if m.prompt == nil {
		t.Fatal("no prompt open")
	}
	m.prompt.input.SetValue(value)
	return send(m, press("enter")...)
}

func TestCreateEnv(t *testing.T) {
	m, root := diskModel(t)
	if selected(m) != "plex" {
		t.Fatalf("selected %q", selected(m))
	}
	m = send(m, press("n")...)
	if m.prompt == nil || m.prompt.input.Value() != root+"/" {
		t.Fatalf("prompt should start at the parent of the selection, got %+v", m.prompt)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "New environment") {
		t.Errorf("the prompt should be drawn:\n%s", v)
	}

	// an error keeps the prompt open and shows why
	m = typeInPrompt(t, m, root+"/plex")
	if m.prompt == nil || !strings.Contains(m.prompt.err, "already exists") {
		t.Fatalf("existing folder should be refused inline, prompt %+v", m.prompt)
	}
	if !strings.Contains(ansi.Strip(m.View()), "already exists") {
		t.Error("the error should be shown in the prompt")
	}

	m = typeInPrompt(t, m, root+"/web/shop")
	if m.prompt != nil {
		t.Fatalf("prompt should close, err %q", m.prompt.err)
	}
	if selected(m) != "web/shop" {
		t.Errorf("new environment should be selected, got %q", selected(m))
	}
	if _, err := os.Stat(filepath.Join(root, "web/shop/docker-compose.yml")); err != nil {
		t.Error(err)
	}
	if len(m.opts.Envs) != 3 {
		t.Errorf("envs = %d, want 3", len(m.opts.Envs))
	}
	if _, err := os.Stat(filepath.Join(root, "web/shop", ".env")); err == nil {
		t.Error("no env file unless new_env_create_dotenv is set")
	}
}

func TestCreateEnvTemplateAndEsc(t *testing.T) {
	m, root := diskModel(t)
	tpl := filepath.Join(t.TempDir(), "tpl.yml")
	if err := os.WriteFile(tpl, []byte("services:\n  x:\n    image: busybox\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.opts.NewTemplate = tpl
	m.opts.NewDotenv = true

	m = send(m, press("n")...)
	m = send(m, press("esc")...)
	if m.prompt != nil {
		t.Fatal("esc should close the prompt")
	}

	m = send(m, press("n")...)
	m = typeInPrompt(t, m, root+"/apps/x")
	b, err := os.ReadFile(filepath.Join(root, "apps/x/docker-compose.yml"))
	if err != nil || !strings.Contains(string(b), "busybox") {
		t.Errorf("compose file should come from the template: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(root, "apps/x", ".env")); err != nil {
		t.Errorf("env file should be created: %v", err)
	}
}

func TestMoveEnv(t *testing.T) {
	m, root := diskModel(t)
	blog := filepath.Join(root, "web/blog")
	m.opts.History.Record(blog, time.Now())
	m = send(m, press("j")...) // web/blog
	if selected(m) != "web/blog" {
		t.Fatalf("selected %q", selected(m))
	}

	m = send(m, press("m")...)
	if m.prompt == nil || m.prompt.input.Value() != blog {
		t.Fatalf("prompt should start at the current folder, got %+v", m.prompt)
	}
	m = typeInPrompt(t, m, root+"/plex")
	if m.prompt == nil || m.prompt.err == "" {
		t.Fatal("moving onto an existing folder should be refused")
	}

	// a stack with containers asks first, and warns about the project name
	m.states[blog] = &compose.DirState{Total: 1, Services: map[string]bool{}}
	m = typeInPrompt(t, m, root+"/web/journal")
	if m.confirm == nil || !strings.Contains(m.confirm.prompt, "Stop and move web/blog to web/journal") {
		t.Fatalf("should confirm stopping first, got %+v", m.confirm)
	}
	if out := strings.Join(m.logLines, "\n"); !strings.Contains(out, "project name changes") {
		t.Errorf("the log pane should warn about the project name:\n%s", out)
	}
	m = send(m, press("n")...)
	if m.confirm != nil {
		t.Fatal("n should cancel")
	}

	// after the move the list, selection and history follow the new folder
	to := discover.Env{Root: root, Rel: "web/journal", Dir: filepath.Join(root, "web/journal"), File: filepath.Join(root, "web/journal/compose.yaml")}
	if err := os.Rename(blog, to.Dir); err != nil {
		t.Fatal(err)
	}
	from := m.opts.Envs[1]
	m = send(m, moveDoneMsg{from: from, to: to, stopped: true})
	if selected(m) != "web/journal" {
		t.Errorf("moved environment should be selected, got %q", selected(m))
	}
	if !strings.Contains(m.status, "press u to start it again") {
		t.Errorf("status %q should say how to start it", m.status)
	}
	h := m.opts.History
	if len(h.Uses[to.Dir]) != 1 || len(h.Uses[blog]) != 0 {
		t.Errorf("history should follow the move: %v", h.Uses)
	}
	for _, e := range m.opts.Envs {
		if e.Dir == blog {
			t.Error("the old folder is still listed")
		}
	}
}

func TestMoveEnvDirect(t *testing.T) {
	m, root := diskModel(t)
	// plex has no containers and the name stays, so it moves without asking
	m = send(m, press("m")...)
	m.prompt.input.SetValue(root + "/media/plex")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.confirm != nil || m.prompt != nil || cmd == nil {
		t.Errorf("should move straight away: confirm %+v prompt %+v", m.confirm, m.prompt)
	}
}

func TestOverlay(t *testing.T) {
	bg := "aaaaaaaa\nbbbbbbbb\ncccccccc"
	got := overlay(bg, "XY\nZW", 3, 1)
	want := "aaaaaaaa\nbbbXYbbb\ncccZWccc"
	if got != want {
		t.Errorf("overlay = %q, want %q", got, want)
	}
}
