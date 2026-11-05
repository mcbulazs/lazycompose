package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mcbulazs/lazycompose/internal/history"
)

func shownNames(m Model) []string {
	var out []string
	for _, i := range m.menu.shown {
		out = append(out, m.menu.entries[i].name)
	}
	return out
}

func TestEveryActionInMenu(t *testing.T) {
	for _, a := range actions {
		if _, ok := menuInfos[a.name]; !ok {
			t.Errorf("%s has no menu description", a.name)
		}
		for _, k := range a.keys {
			msg, ok := keyMsg(k)
			if !ok || msg.String() != k {
				t.Errorf("key %q of %s comes back as %q", k, a.name, msg.String())
			}
		}
	}
	for _, k := range []string{"ctrl+r", "shift+tab", "f5", "pgup", "ctrl+d"} {
		if msg, ok := keyMsg(k); !ok || msg.String() != k {
			t.Errorf("key %q comes back as %q", k, msg.String())
		}
	}
}

func TestMenuSearch(t *testing.T) {
	m := newTestModel(t)
	m = send(m, press(":")...)
	if m.menu == nil || m.menu.all {
		t.Fatal(": should open the action menu")
	}
	for _, name := range shownNames(m) {
		if menuInfos[name].nav {
			t.Errorf("the action menu should not list %s", name)
		}
	}

	m = send(m, press("e")...)
	if got := shownNames(m); len(got) < 2 || got[0] != "edit" || got[1] != "edit_env" {
		t.Errorf("e should list edit first, then E: %v", got)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyBackspace})
	// typed fast, "up" arrives as one message named like the arrow key
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("favourite")})
	if got := shownNames(m); len(got) != 1 || got[0] != "pin" {
		t.Errorf("favourite should find pin: %v", got)
	}
	m = send(m, press("esc")...)
	if m.menu != nil {
		t.Error("esc should close the menu")
	}

	m = send(m, press(":", ":")...)
	if m.menu != nil {
		t.Error("the key that opened the menu should close it")
	}
}

func TestMenuCommands(t *testing.T) {
	m := newTestModel(t)
	e, _ := m.selectedEnv()
	m = send(m, servicesMsg{services: map[string][]string{e.Dir: {"app"}}})
	m = send(m, press(":")...)
	byName := map[string]menuEntry{}
	for _, en := range m.menu.entries {
		byName[en.name] = en
	}
	if got, want := byName["up"].command, "docker compose -f "+e.File+" up -d"; got != want {
		t.Errorf("up runs %q, want %q", got, want)
	}
	if got := byName["up"].short; got != "docker compose up -d" {
		t.Errorf("up short = %q", got)
	}
	if !byName["up"].run || byName["shell"].run {
		t.Error("up should run; shell should not, its service is not running")
	}
	if !strings.Contains(byName["shell"].why, "not running") {
		t.Errorf("shell should say why it cannot run: %q", byName["shell"].why)
	}

	// in the services pane actions run on the selected service
	m = send(m, press("esc", "2", ":")...)
	if !strings.Contains(m.menu.title, e.Rel+" app") {
		t.Errorf("title = %q", m.menu.title)
	}
	for _, en := range m.menu.entries {
		if en.name == "restart" && !strings.HasSuffix(en.command, " restart app") {
			t.Errorf("restart on a service runs %q", en.command)
		}
		if en.name == "down" && strings.HasSuffix(en.command, " app") {
			t.Errorf("down always works on the whole environment: %q", en.command)
		}
	}
}

func TestMenuRuns(t *testing.T) {
	h := history.Load("")
	m := New(Options{Hostname: "host", Roots: []string{"/srv"}, Envs: testEnvs(), QuickAccess: true, History: h})
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	e, _ := m.selectedEnv()
	m = send(m, press(":", "pin", "enter")...)
	if m.menu != nil || !h.Pinned(e.Dir) {
		t.Fatalf("enter should run pin and close the menu, pins %v", h.Pins)
	}
	m = send(m, press(":")...)
	if got := shownNames(m); got[0] != "pin" {
		t.Errorf("the last action run should come first, got %v", got)
	}
}

func TestKeysWindow(t *testing.T) {
	m := newTestModel(t)
	m = send(m, press("?")...)
	if m.menu == nil || !m.menu.all {
		t.Fatal("? should open the keys window")
	}
	v := ansi.Strip(m.View())
	for _, want := range []string{"Keys", "Move down", "docker compose up -d"} {
		if !strings.Contains(v, want) {
			t.Errorf("keys window should show %q:\n%s", want, v)
		}
	}
	m = send(m, press("ctrl")...)
	if got := shownNames(m); len(got) == 0 {
		t.Error("searching ctrl should find ctrl+r and ctrl+c")
	}
	if !strings.Contains(ansi.Strip(m.footerView()), "? keys") {
		t.Errorf("footer = %q", m.footerView())
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"/srv/web/compose.yaml": "/srv/web/compose.yaml",
		"/srv/my stack":         "'/srv/my stack'",
		"it's":                  `'it'\''s'`,
		"bad\x1b]0;x\x07":       `$"bad\x1b]0;x\a"`,
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
