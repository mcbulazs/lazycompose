package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mcbulazs/lazycompose/internal/discover"
	"github.com/mcbulazs/lazycompose/internal/history"
)

const testCompose = `services:
  web:
    image: nginx  # the site
    environment:
      PASSWORD: ${DB_PASSWORD}
  db:
    image: "postgres:16#not-a-comment"
volumes:
  data: {}
`

// composeModel has two environments with compose files on disk.
func composeModel(t *testing.T, h *history.History) (Model, string) {
	t.Helper()
	root := t.TempDir()
	var envs []discover.Env
	for _, rel := range []string{"blog", "shop"} {
		dir := filepath.Join(root, rel)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(dir, "compose.yaml")
		if err := os.WriteFile(file, []byte(strings.ReplaceAll(testCompose, "nginx", "nginx-"+rel)), 0o644); err != nil {
			t.Fatal(err)
		}
		envs = append(envs, discover.Env{Root: root, Rel: rel, Dir: dir, File: file})
	}
	m := New(Options{Hostname: "host", Roots: []string{root}, Envs: envs, History: h})
	return send(m, tea.WindowSizeMsg{Width: 120, Height: 40}), root
}

func shown(m Model) string { return ansi.Strip(strings.Join(m.logLines, "\n")) }

func TestComposeView(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	m, root := composeModel(t, history.Load(path))

	m = send(m, press("v")...)
	if m.paneMode != modeCompose || !strings.Contains(shown(m), "image: nginx-blog") {
		t.Fatalf("v should show the compose file of blog:\n%s", shown(m))
	}
	if !strings.Contains(shown(m), "${DB_PASSWORD}") {
		t.Error("the file should be shown as written, with variables not filled in")
	}
	if !strings.HasPrefix(shown(m), "1 services:") {
		t.Errorf("lines should be numbered:\n%s", shown(m))
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Logs │ Compose") {
		t.Errorf("the pane title should show both tabs:\n%s", v)
	}

	// it follows the selection
	m = send(m, press("j")...)
	if !strings.Contains(shown(m), "image: nginx-shop") {
		t.Errorf("moving should show the next compose file:\n%s", shown(m))
	}

	// the mode is remembered for next time
	saved := history.Load(path)
	if saved.Pane != "compose" {
		t.Errorf("saved pane = %q", saved.Pane)
	}
	if m2, _ := composeModel(t, saved); m2.paneMode != modeCompose {
		t.Error("a new start should open in compose mode")
	}

	// an edited file is read again
	if err := os.WriteFile(filepath.Join(root, "shop", "compose.yaml"), []byte("services: {changed: {}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = send(m, editorMsg{env: m.opts.Envs[1], file: filepath.Join(root, "shop", "compose.yaml")})
	if !strings.Contains(shown(m), "changed") {
		t.Errorf("after editing the file should be read again:\n%s", shown(m))
	}

	m = send(m, press("v")...)
	if m.paneMode != modeLogs || strings.Contains(shown(m), "services:") {
		t.Errorf("v again should go back to the logs:\n%s", shown(m))
	}
}

func TestComposeJumpsToService(t *testing.T) {
	m, _ := composeModel(t, nil)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 16}) // too short for the whole file
	m.paneMode = modeCompose
	e := m.opts.Envs[0]
	m.showCompose(e, "db")
	if i := serviceLine(m.logLines, "db"); i != 5 {
		t.Fatalf("db is on line %d, want 5", i)
	}
	if m.logs.YOffset != m.lineStart[5] {
		t.Errorf("the view should start at the db section, offset %d", m.logs.YOffset)
	}
	if serviceLine(m.logLines, "data") != -1 {
		t.Error("a volume is not a service")
	}
}

func TestColourYAML(t *testing.T) {
	line := colourYAML(`    image: "postgres:16#tag"  # comment ${X}`)
	if got := ansi.Strip(line); got != `    image: "postgres:16#tag"  # comment ${X}` {
		t.Errorf("colouring changed the text: %q", got)
	}
	code, comment := splitComment(`image: "a#b"  # real`)
	if code != `image: "a#b"  ` || comment != "# real" {
		t.Errorf("split = %q, %q", code, comment)
	}
	if got := stripControls("bad\x1b]0;title\x07line"); got != "bad]0;titleline" {
		t.Errorf("control characters should go: %q", got)
	}
}

func TestLogTabClick(t *testing.T) {
	m, _ := composeModel(t, nil)
	x, y := screenFind(t, m, "Compose")
	m = send(m, click(x, y))
	if m.paneMode != modeCompose {
		t.Fatal("clicking the Compose tab should switch to it")
	}
	x, y = screenFind(t, m, "Logs │")
	if m = send(m, click(x, y)); m.paneMode != modeLogs {
		t.Error("clicking the Logs tab should switch back")
	}
}
