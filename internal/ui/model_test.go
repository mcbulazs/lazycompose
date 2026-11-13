package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mcbulazs/lazycompose/internal/compose"
	"github.com/mcbulazs/lazycompose/internal/discover"
	"github.com/mcbulazs/lazycompose/internal/history"
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
		case "up":
			out = append(out, tea.KeyMsg{Type: tea.KeyUp})
		case "left":
			out = append(out, tea.KeyMsg{Type: tea.KeyLeft})
		case "right":
			out = append(out, tea.KeyMsg{Type: tea.KeyRight})
		case "space":
			out = append(out, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
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
	for _, r := range m.list.rows {
		if r.kind == rowEnv {
			out = append(out, m.list.items[r.item].env.Rel)
		}
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
	for _, r := range m.list.rows {
		if r.kind == rowGroup {
			headers = append(headers, r.label)
		}
	}
	if strings.Join(headers, ",") != "games,web" {
		t.Errorf("group headers %v", headers)
	}
}

func TestMoveAndFilter(t *testing.T) {
	m := newTestModel(t)
	m = send(m, press("down", "down", "down", "down")...)
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
	m = send(m, press("down", "down")...)
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
	if rows[1][0] != "mc" || rows[1][1] != "● running" || rows[1][6] != "minecraft-server:latest" || rows[1][7] != "2 hours" {
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

func TestDownService(t *testing.T) {
	m := newTestModel(t)
	m = send(m,
		servicesMsg{services: map[string][]string{"/srv/plex": {"db", "web"}}},
		psMsg{dir: "/srv/plex"},
	)
	m = send(m, press("2")...)
	if m.focus != focusServices || m.selectedService() != "db" {
		t.Fatalf("focus %v, service %q", m.focus, m.selectedService())
	}

	// the menu shows what d runs on the service
	m = send(m, press(":", "down")...)
	found := false
	for _, i := range m.menu.shown {
		if en := m.menu.entries[i]; en.name == "down" {
			found = true
			if en.short != "docker compose rm --stop --force db" {
				t.Errorf("down on a service runs %q", en.short)
			}
		}
	}
	if !found {
		t.Fatal("down should be in the menu")
	}
	m = send(m, press("esc")...)

	m = send(m, press("d")...)
	if m.confirm == nil || m.confirm.prompt != "Stop and remove the plex db container? (y/n)" {
		t.Fatalf("expected a prompt about the service, got %+v", m.confirm)
	}
	m = send(m, press("y")...)
	if m.busy != 1 || !strings.Contains(m.status, "down plex db") {
		t.Errorf("status %q, busy %d", m.status, m.busy)
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
	m := New(Options{Roots: []string{"/srv"}, Find: func([]string) ([]discover.Env, error) { return nil, nil }})
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

func TestColumnsFitContent(t *testing.T) {
	rows := []table.Row{{"app", "● running", "healthy", "0.15%", "52.3MiB", "8443->80/tcp", "nextcloud:30-apache", "2 minutes"}}

	cols := columns(120, rows)
	for i, c := range cols {
		if c.Width < lipgloss.Width(rows[0][i]) {
			t.Errorf("%s is %d wide, cell needs %d", c.Title, c.Width, lipgloss.Width(rows[0][i]))
		}
	}

	// in a narrow pane IMAGE gives up space first
	cols = columns(82, rows)
	if cols[6].Width != 10 || cols[5].Width != lipgloss.Width(rows[0][5]) {
		t.Errorf("IMAGE %d, PORTS %d; want IMAGE shrunk to 10 and PORTS untouched", cols[6].Width, cols[5].Width)
	}

	// and in a very narrow one the total still fits
	cols = columns(70, rows)
	total := 0
	for _, c := range cols {
		if c.Width > 0 {
			total += c.Width + 2
		}
	}
	if cols[3].Width != 0 || cols[4].Width != 0 {
		t.Errorf("CPU and MEM should be hidden first, got %d and %d", cols[3].Width, cols[4].Width)
	}
	if total > 70 {
		t.Errorf("columns take %d, want <= 70", total)
	}
	if cols[1].Width < lipgloss.Width("● running") {
		t.Errorf("STATE should not shrink, got %d", cols[1].Width)
	}
}

func click(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
}

func TestMouse(t *testing.T) {
	m := newTestModel(t)
	// screen: header on row 0, pane border with the title on 1, list from 2:
	// plex, games/, mc, web/, blog, shop
	screen := strings.Split(m.View(), "\n")
	if !strings.Contains(screen[1], "[1] Environments") {
		t.Fatalf("the title should be in the top border: %q", screen[1])
	}
	if !strings.Contains(screen[4], "mc") || !strings.Contains(screen[3], "games/") {
		t.Fatalf("layout changed, rows 3-4 are %q and %q", screen[3], screen[4])
	}
	m = send(m, click(5, 4))
	if selected(m) != "games/mc" {
		t.Errorf("click selected %q, want games/mc", selected(m))
	}
	m = send(m, click(5, 3), click(5, 3)) // a group header closes, then opens again
	if r, ok := m.list.selectedHeading(); !ok || r.label != "games" || m.list.closed[r.key] {
		t.Errorf("clicking a header twice should leave it open and selected, got %+v", r)
	}

	m = send(m, click(5, 6))
	if selected(m) != "web/blog" {
		t.Errorf("click selected %q, want web/blog", selected(m))
	}

	m = send(m,
		servicesMsg{services: map[string][]string{"/srv/web/blog": {"db", "web"}}},
		psMsg{dir: "/srv/web/blog"},
	)
	// services pane: border with title 1, column header 2, rows from 3
	screen = strings.Split(m.View(), "\n")
	if !strings.Contains(screen[4], " web ") {
		t.Fatalf("layout changed, row 4 is %q", screen[4])
	}
	m = send(m, click(m.leftWidth()+5, 4))
	if m.focus != focusServices || m.selectedService() != "web" {
		t.Errorf("focus %v, service %q; want services pane and web", m.focus, m.selectedService())
	}

	m = send(m, click(m.leftWidth()+5, m.servicesHeight()+3))
	if m.focus != focusLogs {
		t.Errorf("click below the services pane should focus logs, got %v", m.focus)
	}
}

func TestEditorResult(t *testing.T) {
	m := newTestModel(t)
	file := filepath.Join(t.TempDir(), "vars")
	if err := os.WriteFile(file, []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	edit := func(m Model) Model {
		m.editID++
		m.edit = &editWatch{id: m.editID, env: env("plex"), file: file, hash: hashFile(file)}
		return m
	}

	// a terminal editor quits: the edit is over
	m = edit(m)
	m = send(m, editorMsg{id: m.editID, took: time.Minute})
	if m.status != "no changes" || m.busy != 0 || m.edit != nil {
		t.Errorf("unchanged file: status %q, busy %d, edit %v", m.status, m.busy, m.edit)
	}
	m = edit(m)
	if err := os.WriteFile(file, []byte("A=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m = send(m, editorMsg{id: m.editID, took: time.Minute})
	if m.busy != 1 || !strings.Contains(m.status, "validating plex") || m.edit != nil {
		t.Errorf("changed file should be validated: status %q, busy %d", m.status, m.busy)
	}

	// a valid config asks before applying
	m = send(m, validateMsg{env: env("plex")})
	if m.confirm == nil || m.confirm.title != "Apply changes" || !strings.Contains(m.confirm.prompt, "config is ok for plex") {
		t.Errorf("expected apply prompt, got %+v", m.confirm)
	}
}

func TestEditorWindow(t *testing.T) {
	m := newTestModel(t)
	file := filepath.Join(t.TempDir(), "compose.yaml")
	if err := os.WriteFile(file, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.editID = 3
	m.edit = &editWatch{id: 3, env: env("plex"), file: file, hash: hashFile(file)}

	// code returns at once and keeps the file in its window
	next, cmd := m.Update(editorMsg{id: 3, took: 100 * time.Millisecond})
	m = next.(Model)
	if m.edit == nil || !m.edit.watching || cmd == nil || !strings.Contains(m.status, "every save") {
		t.Fatalf("a quick editor should start watching: edit %+v, status %q", m.edit, m.status)
	}
	m = send(m, editTickMsg{id: 3})
	if m.busy != 0 {
		t.Errorf("no save, nothing to do: busy %d", m.busy)
	}

	// each save is validated and offered to apply
	for i := range 2 {
		if err := os.WriteFile(file, []byte(fmt.Sprintf("services: {a%d: {}}\n", i)), 0o600); err != nil {
			t.Fatal(err)
		}
		m = send(m, editTickMsg{id: 3})
		if m.busy != 1 || !strings.Contains(m.status, "validating plex") {
			t.Fatalf("save %d: status %q, busy %d", i, m.status, m.busy)
		}
		m = send(m, validateMsg{env: env("plex")})
		if m.confirm == nil {
			t.Fatalf("save %d should ask to apply", i)
		}
		m.confirm = nil
	}
	if m.edit == nil {
		t.Fatal("saving should not end the edit")
	}

	// a new edit replaces the watch; ticks of the old one do nothing
	m.editID, m.edit = 4, &editWatch{id: 4, env: env("plex"), file: file, hash: hashFile(file)}
	if err := os.WriteFile(file, []byte("services: {b: {}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, cmd := m.Update(editTickMsg{id: 3}); cmd != nil {
		t.Error("a tick of an ended edit should do nothing")
	}
}

// enter answers yes, and u, the up key, no longer does
func TestApplyWithEnter(t *testing.T) {
	m := newTestModel(t)
	m = send(m, validateMsg{env: env("plex")})
	if yes, _ := m.confirm.buttons(); yes != " Y yes " {
		t.Errorf("yes button %q", yes)
	}
	if m = send(m, press("u")...); m.confirm == nil || len(m.actions) != 0 {
		t.Error("u should not answer the question")
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.confirm != nil || m.status == "cancelled" || len(m.actions) == 0 {
		t.Errorf("enter should apply: confirm %+v, status %q, actions %d", m.confirm, m.status, len(m.actions))
	}
}

func TestEnvFilePath(t *testing.T) {
	if got := envFile(env("web/blog")); got != filepath.Join("/srv/web/blog", "."+"env") {
		t.Errorf("got %q", got)
	}
}

func TestStatsInTable(t *testing.T) {
	m := newTestModel(t) // plex selected
	m = send(m, psMsg{dir: "/srv/plex", containers: []compose.Container{
		{Name: "plex-plex-1", Service: "plex", State: "running"},
	}})
	if !m.loadingStats {
		t.Fatal("a running container should start a stats sample")
	}
	m = send(m, statsMsg{dir: "/srv/plex", stats: map[string]compose.Stats{
		"plex-plex-1": {CPU: "12.5%", Mem: "310MiB"},
	}})
	row := m.table.Rows()[0]
	if row[3] != "12.5%" || row[4] != "310MiB" {
		t.Errorf("row %v", row)
	}

	// switching env drops the old numbers
	m = send(m, press("down", "down")...)
	if m.stats != nil {
		t.Errorf("stats should be cleared on env switch")
	}
}

func TestLogSearch(t *testing.T) {
	m := newTestModel(t)
	m.appendLog(
		"web-1 | GET / 200",
		"web-1 | ERROR database timeout",
		"db-1  | checkpoint complete",
		"web-1 | error: retrying",
	)
	m = send(m, press("tab", "tab")...) // focus logs
	if m.focus != focusLogs {
		t.Fatalf("focus %v", m.focus)
	}

	m = send(m, press("/", "error", "enter")...)
	if m.filtering {
		t.Fatal("/ in the log pane should search, not filter environments")
	}
	s := m.search
	if len(s.matches) != 2 || s.matches[0] != 1 || s.matches[1] != 3 {
		t.Fatalf("matches %v, want [1 3]", s.matches)
	}
	if s.current != 1 || m.status != "match 2/2" {
		t.Errorf("should start at the newest match: current %d, status %q", s.current, m.status)
	}
	if !strings.Contains(m.View(), "/error 2/2") {
		t.Errorf("title should show the search")
	}

	m = send(m, press("n")...)
	if m.search.current != 0 || m.status != "match 1/2" {
		t.Errorf("n should go to the older match: %d %q", m.search.current, m.status)
	}
	m = send(m, press("n")...)
	if m.search.current != 1 {
		t.Errorf("n should wrap around, got %d", m.search.current)
	}
	m = send(m, press("N")...)
	if m.search.current != 0 {
		t.Errorf("N should go newer (wrapping), got %d", m.search.current)
	}

	m = send(m, press("esc")...)
	if m.search.active() {
		t.Errorf("esc should clear the search")
	}
}

func TestHighlight(t *testing.T) {
	got := ansi.Strip(highlight("web-1 | Error and ERROR", "error", false))
	if got != "web-1 | Error and ERROR" {
		t.Errorf("highlight changed the text: %q", got)
	}
}

func TestForceRecreate(t *testing.T) {
	m := newTestModel(t)
	m = send(m, press("R")...)
	if m.confirm == nil || m.confirm.prompt != "Force recreate plex? (y/n)" {
		t.Fatalf("env prompt %+v", m.confirm)
	}
	m = send(m, press("y")...)
	if m.busy != 1 || m.status != "recreating plex" {
		t.Errorf("busy %d, status %q", m.busy, m.status)
	}

	// with the services pane focused it targets the selected service
	m = newTestModel(t)
	m = send(m,
		servicesMsg{services: map[string][]string{"/srv/plex": {"plex"}}},
		psMsg{dir: "/srv/plex"},
	)
	m = send(m, press("tab", "R")...)
	if m.confirm == nil || m.confirm.prompt != "Force recreate plex plex? (y/n)" {
		t.Errorf("service prompt %+v", m.confirm)
	}
}

func TestLogsFollowSelection(t *testing.T) {
	m := newTestModel(t) // the resize already scheduled plex
	if m.pendingLogs != (logTarget{dir: "/srv/plex"}) {
		t.Fatalf("pending %+v", m.pendingLogs)
	}
	first := m.autoID

	// moving on before the selection settles replaces the pending fetch
	m = send(m, press("down", "down")...)
	if m.pendingLogs.dir != "/srv/games/mc" || m.autoID == first {
		t.Fatalf("pending %+v after moving", m.pendingLogs)
	}
	m = send(m, autoLogsMsg{id: first})
	if m.stream != nil {
		t.Fatal("a stale fetch should be ignored")
	}

	m = send(m, autoLogsMsg{id: m.autoID})
	if m.stream == nil || m.logTarget.dir != "/srv/games/mc" || m.logTitle != "Logs · games/mc" {
		t.Fatalf("stream %v, target %+v, title %q", m.stream, m.logTarget, m.logTitle)
	}

	// in the services pane the selected service is followed
	m = send(m,
		servicesMsg{services: map[string][]string{"/srv/games/mc": {"backup", "mc"}}},
		psMsg{dir: "/srv/games/mc"},
	)
	m = send(m, press("tab")...)
	m = send(m, autoLogsMsg{id: m.autoID})
	if m.logTarget.service != "backup" {
		t.Fatalf("target %+v, want the backup service", m.logTarget)
	}

	// focusing the log pane keeps what it shows
	m = send(m, press("tab")...)
	if m.pendingLogs != (logTarget{}) {
		t.Errorf("focusing logs should not refetch, pending %+v", m.pendingLogs)
	}
}

func TestCommandOutputIsHeld(t *testing.T) {
	m := newTestModel(t)
	m = send(m, press("u")...)
	if !m.holdOutput || m.logTitle != "Output" || len(m.actions) != 1 || m.busy != 1 {
		t.Fatalf("hold %v, title %q, actions %d, busy %d", m.holdOutput, m.logTitle, len(m.actions), m.busy)
	}
	var id int
	for id = range m.actions {
	}

	m = send(m, logLinesMsg{id: id, lines: []string{" Container plex-1 Started"}})
	if !strings.Contains(strings.Join(m.logLines, "\n"), "plex-1 Started") {
		t.Errorf("output not shown: %v", m.logLines)
	}

	// a key while the command runs keeps the output
	m = send(m, press("j")...)
	if !m.holdOutput {
		t.Fatal("output should stay while the command runs")
	}

	m = send(m, logDoneMsg{id: id})
	if m.busy != 0 || len(m.actions) != 0 || !m.holdOutput {
		t.Fatalf("busy %d, actions %d, hold %v", m.busy, len(m.actions), m.holdOutput)
	}
	if m.status != "docker compose up -d (plex) done" {
		t.Errorf("status %q", m.status)
	}

	// scrolling the log pane keeps it too
	m = send(m, press("tab", "tab")...)
	held := m.holdOutput
	m = send(m, press("k")...)
	if !m.holdOutput || !held {
		t.Fatal("scrolling the output should not release it")
	}

	// switching panes keeps it, any other key goes back to the logs
	m = send(m, press("tab")...)
	if !m.holdOutput {
		t.Fatal("tab should not release the output")
	}
	m = send(m, press("j")...)
	if m.holdOutput || m.pendingLogs.dir == "" {
		t.Errorf("hold %v, pending %+v", m.holdOutput, m.pendingLogs)
	}
}

func TestQuitToFolder(t *testing.T) {
	// without -cd-file it explains how to set it up and keeps running
	m := newTestModel(t)
	next, cmd := m.Update(press("Q")[0])
	m = next.(Model)
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("should not quit without -cd-file")
		}
	}
	if !strings.Contains(m.status, "-cd-file") || !m.statusErr {
		t.Errorf("status %q", m.status)
	}

	// with it, the selected folder is handed back and the app quits
	m = New(Options{Hostname: "host", Roots: []string{"/srv"}, Envs: testEnvs(), CdFile: "/tmp/x"})
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	m = send(m, press("down", "down")...)
	next, cmd = m.Update(press("Q")[0])
	m = next.(Model)
	if m.QuitDir() != "/srv/games/mc" {
		t.Errorf("quit dir %q", m.QuitDir())
	}
	if cmd == nil {
		t.Fatal("expected a quit command")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Error("expected the app to quit")
	}

	// a plain q leaves no folder
	m = newTestModel(t)
	m = send(m, press("q")...)
	if m.QuitDir() != "" {
		t.Errorf("q should not set a folder, got %q", m.QuitDir())
	}
}

func TestProgressLinesCollapse(t *testing.T) {
	m := newTestModel(t)
	m = send(m, press("p")...)
	var id int
	for id = range m.actions {
	}
	m = send(m, logLinesMsg{id: id, lines: []string{
		" Image nextcloud:30 Pulling",
		"cce6de6ca5f3 Downloading [=>      ] 1MB/265MB",
		"0aefc10db716 Downloading [=======>] 122B/122B",
		"cce6de6ca5f3 Downloading [==>     ] 2MB/265MB",
	}})
	m = send(m, logLinesMsg{id: id, lines: []string{
		"cce6de6ca5f3 Downloading [====>   ] 9MB/265MB",
		"0aefc10db716 Download complete",
		"some other line",
		" Image nextcloud:30 Pulled",
	}})
	want := []string{
		" Image nextcloud:30 Pulled",
		"cce6de6ca5f3 Downloading [====>   ] 9MB/265MB",
		"0aefc10db716 Download complete",
		"some other line",
	}
	got := m.logLines[1:] // after the "$ docker compose pull" line
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestPaneTitlesShowKeys(t *testing.T) {
	m := newTestModel(t)
	v := m.View()
	for _, want := range []string{"[1] Environments", "[2] Services", "[3] Logs"} {
		if !strings.Contains(v, want) {
			t.Errorf("view should show %q", want)
		}
	}
	m = send(m, press("3")...)
	if m.focus != focusLogs {
		t.Errorf("3 should focus the logs, focus is %v", m.focus)
	}
}

func TestQuickAccess(t *testing.T) {
	h := history.Load("")
	now := time.Now()
	for range 3 {
		h.Record("/srv/web/shop", now)
	}
	h.Record("/srv/plex", now.Add(-time.Hour))
	h.Record("/srv/gone", now) // no longer on disk

	newModel := func(quick bool) Model {
		m := New(Options{Hostname: "host", Roots: []string{"/srv"}, Envs: testEnvs(), QuickAccess: quick, History: h})
		return send(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	}

	m := newModel(true)
	want := []string{"web/shop", "plex", "plex", "games/mc", "web/blog", "web/shop"}
	if got := visible(m); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("visible = %v, want %v", got, want)
	}
	if selected(m) != "web/shop" {
		t.Errorf("should start on the most used env, got %q", selected(m))
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "quick access") {
		t.Errorf("view should show the quick access section:\n%s", v)
	}
	if h := ansi.Strip(m.headerView()); !strings.Contains(h, "○ 4 down") {
		t.Errorf("header should count each env once: %q", h)
	}

	// filtering hides the section, so matches are not listed twice
	f := send(m, press("/", "s", "h", "o")...)
	if got := visible(f); len(got) != 1 || got[0] != "web/shop" {
		t.Errorf("filtered = %v", got)
	}

	// an action counts as a use, moving does not
	m = send(m, press("j", "j", "j", "j", "j")...)
	if selected(m) != "games/mc" {
		t.Fatalf("selected %q, want games/mc", selected(m))
	}
	if len(h.Uses["/srv/games/mc"]) != 0 {
		t.Error("moving should not count as a use")
	}
	m = send(m, press("r")...)
	if len(h.Uses["/srv/games/mc"]) != 1 {
		t.Error("restart should count as a use")
	}

	if got := visible(newModel(false)); len(got) != 4 {
		t.Errorf("quick access off should list each env once, got %v", got)
	}
}

func TestQuickAccessAfterScan(t *testing.T) {
	h := history.Load("")
	h.Record("/srv/web/shop", time.Now())
	m := New(Options{Hostname: "host", Roots: []string{"/srv"}, QuickAccess: true, History: h,
		Find: func([]string) ([]discover.Env, error) { return testEnvs(), nil }})
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 30}, envsMsg{envs: testEnvs()})
	if it, _ := m.list.selectedItem(); !it.quick || it.env.Rel != "web/shop" {
		t.Errorf("after the first scan the cursor should be on quick access web/shop, got %+v", it)
	}
}

func TestPin(t *testing.T) {
	h := history.Load("")
	h.Record("/srv/web/shop", time.Now())
	m := New(Options{Hostname: "host", Roots: []string{"/srv"}, Envs: testEnvs(), QuickAccess: true, History: h})
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 30})

	// games/mc in the main list: pinning shows it first right away
	m = send(m, press("j", "j", "j")...)
	if selected(m) != "games/mc" {
		t.Fatalf("selected %q, want games/mc", selected(m))
	}
	m = send(m, press("f")...)
	want := "games/mc,web/shop,plex,games/mc,web/blog,web/shop"
	if got := strings.Join(visible(m), ","); got != want {
		t.Errorf("visible = %v, want %v", got, want)
	}
	if it, _ := m.list.selectedItem(); it.quick || it.env.Rel != "games/mc" {
		t.Errorf("the cursor should stay on games/mc in the main list, got %+v", it)
	}
	if !strings.Contains(ansi.Strip(m.View()), pinMark+" ● games/mc") && !strings.Contains(ansi.Strip(m.View()), pinMark+" ○ games/mc") {
		t.Errorf("a pinned env should get a star:\n%s", ansi.Strip(m.View()))
	}
	if len(h.Uses["/srv/games/mc"]) != 0 {
		t.Error("pinning should not count as a use")
	}

	// with quick access off only the pins show
	off := New(Options{Hostname: "host", Roots: []string{"/srv"}, Envs: testEnvs(), History: h})
	off = send(off, tea.WindowSizeMsg{Width: 120, Height: 30})
	if got := strings.Join(visible(off), ","); got != "games/mc,plex,games/mc,web/blog,web/shop" {
		t.Errorf("quick access off: visible = %v", got)
	}
	if v := ansi.Strip(off.View()); !strings.Contains(v, pinnedGroup) {
		t.Errorf("the section should be called %q:\n%s", pinnedGroup, v)
	}

	// f again unpins
	m = send(m, press("f")...)
	if h.Pinned("/srv/games/mc") || strings.Join(visible(m), ",") != "web/shop,plex,games/mc,web/blog,web/shop" {
		t.Errorf("after unpinning: pins %v, visible %v", h.Pins, visible(m))
	}
}

func TestUnpinKeepsCursorInQuickAccess(t *testing.T) {
	h := history.Load("")
	for _, d := range []string{"/srv/plex", "/srv/web/blog", "/srv/web/shop"} {
		h.TogglePin(d)
	}
	m := New(Options{Hostname: "host", Roots: []string{"/srv"}, Envs: testEnvs(), QuickAccess: true, History: h})
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 30})

	// unpin all three in a row from the top of the section
	for i, want := range []string{"web/blog", "web/shop"} {
		m = send(m, press("f")...)
		if it, _ := m.list.selectedItem(); !it.quick || it.env.Rel != want {
			t.Fatalf("after unpin %d the cursor should be on quick access %s, got %+v", i+1, want, it)
		}
	}
	m = send(m, press("f")...)
	if len(h.Pins) != 0 {
		t.Fatalf("pins left: %v", h.Pins)
	}
	if it, _ := m.list.selectedItem(); it.quick || it.env.Rel != "web/shop" {
		t.Errorf("with the section empty the cursor should be on web/shop in the list, got %+v", it)
	}
}

func TestNewVersionInHeader(t *testing.T) {
	latest := func(context.Context) (string, error) { return "v0.4.0", nil }
	m := New(Options{Hostname: "host", Roots: []string{"/srv"}, Envs: testEnvs(), Version: "v0.3.0", LatestVersion: latest})
	m = send(m, tea.WindowSizeMsg{Width: 160, Height: 30})
	if m.firstVersionCheck() == nil {
		t.Fatal("a release build should look for new releases")
	}
	m = send(m, m.checkVersion()())
	if h := ansi.Strip(m.headerView()); !strings.Contains(h, "v0.4.0 available, run lazycompose -update") {
		t.Errorf("header = %q", h)
	}

	m = New(Options{Hostname: "host", Envs: testEnvs(), Version: "v0.3.0", LatestVersion: latest, UpdateHint: "update it with your package manager"})
	m = send(m, tea.WindowSizeMsg{Width: 160, Height: 30})
	m = send(m, m.checkVersion()())
	if h := ansi.Strip(m.headerView()); !strings.Contains(h, "v0.4.0 available, update it with your package manager") {
		t.Errorf("header with a hint = %q", h)
	}

	m = New(Options{Hostname: "host", Envs: testEnvs(), Version: "v0.4.0", LatestVersion: latest})
	if m = send(m, m.checkVersion()()); m.newVersion != "" {
		t.Error("the same version should not be mentioned")
	}
	if dev := New(Options{Version: "dev", LatestVersion: latest}); dev.firstVersionCheck() != nil {
		t.Error("development builds should not look for releases")
	}
	if off := New(Options{Version: "v0.3.0"}); off.firstVersionCheck() != nil {
		t.Error("version_check: false should not look")
	}
}
