package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mcbulazs/lazycompose/internal/compose"
	"github.com/mcbulazs/lazycompose/internal/discover"
)

func env(rel string) discover.Env {
	return discover.Env{Root: "/srv", Rel: rel, Dir: "/srv/" + rel, File: "/srv/" + rel + "/compose.yaml"}
}

func testEnvs() []discover.Env {
	return []discover.Env{env("games/mc"), env("plex"), env("web/blog"), env("web/shop")}
}

func newTestModel(t *testing.T) Model {
	t.Helper()
	m := New(Options{Hostname: "host", Roots: []string{"/srv"}, Envs: testEnvs()})
	return send(m, tea.WindowSizeMsg{Width: 120, Height: 30})
}

func send(m Model, msgs ...tea.Msg) Model {
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

func press(keys ...string) []tea.Msg {
	var out []tea.Msg
	for _, k := range keys {
		switch k {
		case "tab":
			out = append(out, tea.KeyMsg{Type: tea.KeyTab})
		case "down":
			out = append(out, tea.KeyMsg{Type: tea.KeyDown})
		case "esc":
			out = append(out, tea.KeyMsg{Type: tea.KeyEsc})
		case "enter":
			out = append(out, tea.KeyMsg{Type: tea.KeyEnter})
		default:
			for _, r := range k {
				out = append(out, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			}
		}
	}
	return out
}

func selected(m Model) string {
	e, _ := m.selectedEnv()
	return e.Rel
}

func visible(m Model) []string {
	var out []string
	for _, idx := range m.list.visible {
		out = append(out, m.list.items[idx].env.Rel)
	}
	return out
}

func running(services ...string) *compose.DirState {
	st := &compose.DirState{Services: map[string]bool{}}
	for _, s := range services {
		st.Services[s] = true
		st.Running++
		st.Total++
	}
	return st
}

func TestListOrder(t *testing.T) {
	m := newTestModel(t)
	want := []string{"plex", "games/mc", "web/blog", "web/shop"}
	if got := visible(m); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order %v, want %v", got, want)
	}
	var headers []string
	for _, l := range m.list.lines() {
		if l.pos == -1 {
			headers = append(headers, l.header)
		}
	}
	if strings.Join(headers, ",") != "games,web" {
		t.Errorf("group headers %v", headers)
	}
}

func TestMoveAndFilter(t *testing.T) {
	m := newTestModel(t)
	m = send(m, press("down", "down")...)
	if selected(m) != "web/blog" {
		t.Fatalf("selected %q, want web/blog", selected(m))
	}

	m = send(m, press("/", "sh", "o")...)
	if got := visible(m); len(got) != 1 || got[0] != "web/shop" {
		t.Fatalf("filtered %v", got)
	}
	m = send(m, press("enter")...)
	if !strings.Contains(m.View(), "/sho") || strings.Contains(m.View(), "plex") {
		t.Errorf("view should show the filter and hide other envs")
	}

	m = send(m, press("esc")...)
	if len(visible(m)) != 4 {
		t.Errorf("esc should clear the filter, got %v", visible(m))
	}
	if selected(m) != "web/shop" {
		t.Errorf("selection should survive clearing the filter, got %q", selected(m))
	}
}

func TestCountsAndHeader(t *testing.T) {
	m := newTestModel(t)
	m = send(m,
		servicesMsg{services: map[string][]string{
			"/srv/web/blog": {"db", "web"},
			"/srv/plex":     {"plex"},
		}},
		statesMsg{states: map[string]*compose.DirState{
			"/srv/web/blog": running("web"),
			"/srv/plex":     running("plex"),
		}},
	)
	if r, total, _ := m.counts(env("web/blog")); r != 1 || total != 2 {
		t.Errorf("blog counts %d/%d, want 1/2", r, total)
	}
	h := m.headerView()
	for _, want := range []string{"1 up", "1 partial", "2 down"} {
		if !strings.Contains(h, want) {
			t.Errorf("header %q missing %q", h, want)
		}
	}
	if strings.Contains(h, "problems") {
		t.Errorf("header should not mention problems: %q", h)
	}
}

func TestServicesTable(t *testing.T) {
	m := newTestModel(t)
	// switching envs empties the table first, which is where the cursor
	// used to get stuck at -1
	m = send(m, press("down")...)
	if selected(m) != "games/mc" {
		t.Fatalf("selected %q", selected(m))
	}
	m = send(m,
		servicesMsg{services: map[string][]string{"/srv/games/mc": {"mc", "backup"}}},
		psMsg{dir: "/srv/games/mc", containers: []compose.Container{
			{Service: "mc", State: "running", Status: "Up 2 hours", Image: "docker.io/itzg/minecraft-server:latest"},
		}},
	)
	rows := m.table.Rows()
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0][0] != "backup" || rows[0][1] != "○ not created" {
		t.Errorf("backup row %v", rows[0])
	}
	if rows[1][0] != "mc" || rows[1][1] != "● running" || rows[1][4] != "minecraft-server:latest" || rows[1][5] != "2 hours" {
		t.Errorf("mc row %v", rows[1])
	}

	m = send(m, press("tab")...)
	if s := m.selectedService(); s != "backup" {
		t.Errorf("selected service %q, want backup", s)
	}

	// a late reply for another environment is ignored
	m = send(m, psMsg{dir: "/srv/web/blog", containers: []compose.Container{{Service: "web"}}})
	if len(m.table.Rows()) != 2 {
		t.Errorf("stale ps reply changed the table")
	}
}

func TestProblemsFilter(t *testing.T) {
	m := newTestModel(t)
	sick := running("web")
	sick.Problems = 1
	m = send(m, statesMsg{states: map[string]*compose.DirState{
		"/srv/web/blog": sick,
		"/srv/plex":     running("plex"),
	}})
	if !strings.Contains(m.headerView(), "1 with problems") {
		t.Errorf("header should count problems: %q", m.headerView())
	}

	m = send(m, press("!")...)
	if got := visible(m); len(got) != 1 || got[0] != "web/blog" {
		t.Fatalf("problems only shows %v", got)
	}
	if selected(m) != "web/blog" {
		t.Errorf("selection should move to a visible env, got %q", selected(m))
	}

	// the filter follows new states
	m = send(m, statesMsg{states: map[string]*compose.DirState{"/srv/web/blog": running("web")}})
	if len(visible(m)) != 0 {
		t.Errorf("recovered env should drop out of the filter, got %v", visible(m))
	}

	m = send(m, press("esc")...)
	if len(visible(m)) != 4 {
		t.Errorf("esc should show all envs, got %v", visible(m))
	}
}

func TestDownAsksFirst(t *testing.T) {
	m := newTestModel(t)
	m = send(m, press("d")...)
	if m.confirm == nil || !strings.Contains(m.confirm.prompt, "down for plex") {
		t.Fatalf("expected a confirm prompt, got %+v", m.confirm)
	}
	m = send(m, press("n")...)
	if m.confirm != nil || m.busy != 0 {
		t.Errorf("n should cancel without running anything")
	}
}

func TestUpdateAll(t *testing.T) {
	m := newTestModel(t)
	m = send(m, statesMsg{states: map[string]*compose.DirState{
		"/srv/web/blog": running("web"),
		"/srv/plex":     running("plex"),
	}})

	m = send(m, press("U")...)
	if m.confirm == nil || !strings.Contains(m.confirm.prompt, "Update 2 running") {
		t.Fatalf("expected confirm for 2 envs, got %+v", m.confirm)
	}
	m = send(m, press("y")...)
	if m.updating == nil || m.busy != 1 {
		t.Fatalf("update should be running")
	}

	m = send(m, updateMsg{env: env("plex"), recreated: 1})
	if m.updating == nil || !strings.Contains(m.status, "2/2") {
		t.Fatalf("expected second env next, status %q", m.status)
	}
	m = send(m, updateMsg{env: env("web/blog")})
	if m.updating != nil || m.busy != 0 {
		t.Fatalf("update should be finished")
	}
	if m.status != "update done: 1 updated, 1 up to date, 0 failed" {
		t.Errorf("status %q", m.status)
	}
}

func TestUpdateAllNothingRunning(t *testing.T) {
	m := newTestModel(t)
	m = send(m, press("U")...)
	if m.confirm != nil || m.status != "nothing running to update" {
		t.Errorf("confirm %+v, status %q", m.confirm, m.status)
	}
}

func TestScanningState(t *testing.T) {
	m := New(Options{Roots: []string{"/srv"}, Find: func() ([]discover.Env, error) { return nil, nil }})
	m = send(m, tea.WindowSizeMsg{Width: 100, Height: 24})
	if !strings.Contains(m.View(), "scanning…") {
		t.Errorf("should show scanning before the first result")
	}
	m = send(m, envsMsg{envs: testEnvs()})
	if m.scanning || m.busy != 0 || len(visible(m)) != 4 {
		t.Errorf("scan result not applied: scanning=%v busy=%d", m.scanning, m.busy)
	}
}

func TestStateText(t *testing.T) {
	tests := map[string]compose.Container{
		"● running":      {State: "running"},
		"○ exited (0)":   {State: "exited"},
		"○ exited (137)": {State: "exited", ExitCode: 137},
		"✗ exited (2)":   {State: "exited", ExitCode: 2},
		"✗ restarting":   {State: "restarting"},
		"✗ running":      {State: "running", Health: "unhealthy"},
	}
	for want, c := range tests {
		if got := stateText(c); got != want {
			t.Errorf("%+v: got %q, want %q", c, got, want)
		}
	}
}
