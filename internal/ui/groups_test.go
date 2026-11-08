package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mcbulazs/lazycompose/internal/compose"
	"github.com/mcbulazs/lazycompose/internal/discover"
	"github.com/mcbulazs/lazycompose/internal/history"
)

// rowsOf shows the list rows as text: headings with their mark, and
// environments by name.
func rowsOf(l envList) string {
	var out []string
	for _, r := range l.rows {
		switch r.kind {
		case rowRoot, rowGroup:
			mark := openMark
			if l.closed[r.key] {
				mark = closedMark
			}
			out = append(out, mark+r.label)
		case rowEnv:
			out = append(out, l.items[r.item].env.Name())
		case rowQuick:
			out = append(out, "["+r.label+"]")
		}
	}
	return strings.Join(out, ",")
}

func twoRootEnvs() []discover.Env {
	return []discover.Env{
		{Root: "/b", Rel: "app", Dir: "/b/app", File: "/b/app/compose.yaml"},
		{Root: "/a", Rel: "web/blog", Dir: "/a/web/blog", File: "/a/web/blog/compose.yaml"},
		{Root: "/a", Rel: "web/shop", Dir: "/a/web/shop", File: "/a/web/shop/compose.yaml"},
		{Root: "/a", Rel: "plex", Dir: "/a/plex", File: "/a/plex/compose.yaml"},
	}
}

func TestRootsInConfigOrder(t *testing.T) {
	// /b is configured first, so it comes first, though /a sorts before it
	l := newEnvList(twoRootEnvs(), []string{"/b", "/a"})
	if got := rowsOf(l); got != "▾b,app,▾a,plex,▾web,blog,shop" {
		t.Errorf("rows %s", got)
	}
	// one root: no root heading
	l = newEnvList(twoRootEnvs()[1:], []string{"/a"})
	if got := rowsOf(l); got != "plex,▾web,blog,shop" {
		t.Errorf("one root: rows %s", got)
	}
}

func TestOpenClose(t *testing.T) {
	h := history.Load("")
	m := New(Options{Hostname: "host", Roots: []string{"/b", "/a"}, Envs: twoRootEnvs(), History: h})
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	m.list.selectDir("/a/web/shop")
	m = send(m,
		servicesMsg{services: map[string][]string{"/a/web/blog": {"web"}, "/a/web/shop": {"web", "db"}}},
		statesMsg{states: map[string]*compose.DirState{"/a/web/blog": running("web")}})

	// left closes the folder and stays on its heading, which sums it up
	m = send(m, press("left")...)
	if got := rowsOf(m.list); got != "▾b,app,▾a,plex,▸web" {
		t.Fatalf("rows %s", got)
	}
	if r, ok := m.list.selectedHeading(); !ok || r.label != "web" {
		t.Errorf("the cursor should be on the web heading, got %+v", r)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, closedMark+" web/") || !strings.Contains(v, "1/3") {
		t.Errorf("a closed folder shows its counts:\n%s", v)
	}
	if len(h.Closed) != 1 {
		t.Errorf("closing should be remembered: %v", h.Closed)
	}

	// moving passes over open headings and stops on closed ones
	m = send(m, press("up")...)
	if selected(m) != "plex" {
		t.Errorf("up from the heading: %q", selected(m))
	}
	m = send(m, press("down")...)
	if _, ok := m.list.selectedHeading(); !ok {
		t.Error("down should stop on the closed heading")
	}

	// left on a closed folder closes its root
	m = send(m, press("left")...)
	if got := rowsOf(m.list); got != "▾b,app,▸a" {
		t.Errorf("rows %s", got)
	}
	// enter opens the root again, with the folder still closed
	m = send(m, press("enter")...)
	if got := rowsOf(m.list); got != "▾b,app,▾a,plex,▸web" {
		t.Errorf("rows %s", got)
	}
	m.list.selectKey(groupKey(rootKey("/a"), "web"))
	m = send(m, press("right")...)
	if r, _ := m.list.selectedHeading(); rowsOf(m.list) != "▾b,app,▾a,plex,▾web,blog,shop" || r.label != "web" {
		t.Errorf("rows %s, selected %+v", rowsOf(m.list), r)
	}
	// space closes and opens the selected heading
	m = send(m, press("space")...)
	if got := rowsOf(m.list); got != "▾b,app,▾a,plex,▸web" {
		t.Errorf("space should close it: %s", got)
	}
	m = send(m, press("space")...)
	if got := rowsOf(m.list); got != "▾b,app,▾a,plex,▾web,blog,shop" {
		t.Errorf("space should open it: %s", got)
	}
	if len(h.Closed) != 0 {
		t.Errorf("opening should be remembered: %v", h.Closed)
	}

	// a filter shows what matches in closed headings
	m = send(m, press("left", "/", "sh", "o")...)
	if got := visible(m); len(got) != 1 || got[0] != "web/shop" {
		t.Errorf("filtering into a closed folder: %v", got)
	}

	// the next start remembers what was closed
	again := New(Options{Hostname: "host", Roots: []string{"/b", "/a"}, Envs: twoRootEnvs(), History: h})
	if got := rowsOf(again.list); got != "▾b,app,▾a,plex,▸web" {
		t.Errorf("after a restart: rows %s", got)
	}
}

func TestClickHeading(t *testing.T) {
	m := newTestModel(t)
	// rows: plex, games/, mc, web/, blog, shop from screen row 2
	m = send(m, click(5, 3))
	if got := rowsOf(m.list); got != "plex,▸games,▾web,blog,shop" {
		t.Errorf("a click closes a heading: %s", got)
	}
	m = send(m, click(5, 3))
	if got := rowsOf(m.list); got != "plex,▾games,mc,▾web,blog,shop" {
		t.Errorf("a second click opens it: %s", got)
	}
	// moving stops on headings, open or closed
	m = send(m, press("down")...)
	if selected(m) != "games/mc" {
		t.Errorf("down from the games heading: %q", selected(m))
	}
	m = send(m, press("down")...)
	if r, ok := m.list.selectedHeading(); !ok || r.label != "web" {
		t.Errorf("down should stop on the open web heading, got %+v", r)
	}
}

func TestRemoteRootStartsClosed(t *testing.T) {
	h := history.Load("")
	h.Record("ssh://srv/stacks/web", time.Now())
	roots := []string{"/srv", "ssh://srv/stacks"}
	m := New(Options{Hostname: "pc", Roots: roots, Envs: testEnvs(), History: h, QuickAccess: true})
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 30})

	if len(m.hosts) != 1 || m.hosts[0] != nil {
		t.Fatalf("only this machine should be followed at the start: %v", m.hosts)
	}
	if got := m.activeRoots(); len(got) != 1 || got[0] != "/srv" {
		t.Errorf("only the local root is scanned: %v", got)
	}
	if got := rowsOf(m.list); !strings.HasPrefix(got, "[quick access],web,") || !strings.HasSuffix(got, "▸srv:stacks") {
		t.Errorf("rows %s", got)
	}
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "srv:web") || !strings.Contains(v, "enter connects") {
		t.Errorf("the remote root and its quick access entry should show, offline:\n%s", v)
	}

	// the offline entry is selected first; it is not an environment yet
	if it, ok := m.list.selectedItem(); !ok || !it.offline {
		t.Fatalf("selected %+v", it)
	}
	if _, ok := m.selectedEnv(); ok {
		t.Error("an offline entry cannot be acted on")
	}
	// enter connects
	next, cmd := m.Update(press("enter")[0])
	m = next.(Model)
	if cmd == nil || !m.connecting["ssh://srv"] {
		t.Fatal("enter should start connecting")
	}
	m = send(m, hostCheckedMsg{key: "ssh://srv", err: errors.New("srv: no answer in time")})
	if m.connecting["ssh://srv"] || !strings.Contains(m.status, "no answer") || len(m.hosts) != 1 {
		t.Errorf("a failed connect: status %q, hosts %v", m.status, m.hosts)
	}

	// once connected the root opens and is scanned; its quick entry is real
	m.connecting["ssh://srv"] = true
	m.opts.Find = func(roots []string) ([]discover.Env, error) {
		if len(roots) != 1 || roots[0] != "ssh://srv/stacks" {
			t.Errorf("scanned %v", roots)
		}
		return []discover.Env{remoteEnv("web")}, nil
	}
	next, cmd = m.Update(hostCheckedMsg{key: "ssh://srv"})
	m = next.(Model)
	if len(m.hosts) != 2 || m.list.closed["ssh://srv/stacks"] {
		t.Fatalf("hosts %v, closed %v", m.hosts, m.list.closed)
	}
	m = send(m, envsMsg{envs: []discover.Env{remoteEnv("web")}, roots: []string{"ssh://srv/stacks"}})
	if len(m.opts.Envs) != len(testEnvs())+1 {
		t.Errorf("the local environments should stay: %d", len(m.opts.Envs))
	}
	if it, _ := m.list.selectedItem(); it.offline || it.env.Dir != "ssh://srv/stacks/web" {
		t.Errorf("selected %+v", it)
	}
	_ = cmd
}

func TestMergeEnvs(t *testing.T) {
	old := []discover.Env{{Root: "/a", Dir: "/a/x"}, {Root: "ssh://h/s", Dir: "ssh://h/s/old"}}
	got := mergeEnvs(old, []discover.Env{{Root: "ssh://h/s", Dir: "ssh://h/s/new"}}, []string{"ssh://h/s"})
	if len(got) != 2 || got[0].Dir != "/a/x" || got[1].Dir != "ssh://h/s/new" {
		t.Errorf("merged %+v", got)
	}
}

func TestFolderTree(t *testing.T) {
	envs := []discover.Env{}
	for _, rel := range []string{"infra/traefik", "infra/monitoring/metrics", "infra/monitoring/umami", "infra-x/old", "infra/adguard", "media/arr", "top"} {
		envs = append(envs, discover.Env{Root: "/srv", Rel: rel, Dir: "/srv/" + rel, File: "/srv/" + rel + "/compose.yaml"})
	}
	m := New(Options{Hostname: "host", Roots: []string{"/srv"}, Envs: envs, History: history.Load("")})
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	// subfolders sit inside their folder, after its own stacks
	want := "top,▾infra,adguard,traefik,▾monitoring,metrics,umami,▾infra-x,old,▾media,arr"
	if got := rowsOf(m.list); got != want {
		t.Fatalf("rows\n%s\nwant\n%s", got, want)
	}
	depth := map[string]int{}
	for _, r := range m.list.rows {
		name := r.label
		if r.kind == rowEnv {
			name = m.list.items[r.item].env.Name()
		}
		depth[name] = r.depth
	}
	if depth["infra"] != 0 || depth["traefik"] != 1 || depth["monitoring"] != 1 || depth["metrics"] != 2 {
		t.Errorf("depths %v", depth)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "  ▾ monitoring/") || strings.Contains(v, "infra/monitoring/") {
		t.Errorf("the subfolder should show by its own name, indented:\n%s", v)
	}

	// closing a folder hides its subfolders, and counts them
	m = send(m, servicesMsg{services: map[string][]string{"/srv/infra/monitoring/metrics": {"a", "b"}, "/srv/infra/traefik": {"t"}}},
		statesMsg{states: map[string]*compose.DirState{"/srv/infra/monitoring/metrics": running("a", "b")}})
	m.list.selectDir("/srv/infra/monitoring/umami")
	m.list.selectKey(groupKey(rootKey("/srv"), "infra"))
	m = send(m, press("enter")...)
	if got := rowsOf(m.list); got != "top,▸infra,▾infra-x,old,▾media,arr" {
		t.Errorf("rows %s", got)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "▸ infra/") || !strings.Contains(v, "2/3") {
		t.Errorf("a closed folder should count its subfolders (2 of 3 running):\n%s", v)
	}
	m = send(m, press("enter")...)

	// left on a stack in the subfolder closes the subfolder only, and left
	// again on it closes its parent
	m.list.selectDir("/srv/infra/monitoring/umami")
	m = send(m, press("left")...)
	if got := rowsOf(m.list); got != "top,▾infra,adguard,traefik,▸monitoring,▾infra-x,old,▾media,arr" {
		t.Errorf("rows %s", got)
	}
	m = send(m, press("left")...)
	if r, _ := m.list.selectedHeading(); rowsOf(m.list) != "top,▸infra,▾infra-x,old,▾media,arr" || r.label != "infra" {
		t.Errorf("rows %s, selected %+v", rowsOf(m.list), r)
	}
}
