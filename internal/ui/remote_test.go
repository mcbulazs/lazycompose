package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mcbulazs/lazycompose/internal/compose"
	"github.com/mcbulazs/lazycompose/internal/discover"
	"github.com/mcbulazs/lazycompose/internal/remote"
)

func remoteEnv(rel string) discover.Env {
	h := &remote.Host{Name: "srv"}
	return discover.Env{Root: "ssh://srv/stacks", Rel: rel, Dir: "ssh://srv/stacks/" + rel,
		File: "/stacks/" + rel + "/compose.yaml", Host: h, Path: "/stacks/" + rel}
}

func remoteModel(t *testing.T) Model {
	t.Helper()
	envs := append(testEnvs(), remoteEnv("web"))
	m := New(Options{Hostname: "pc", Roots: []string{"/srv", "ssh://srv/stacks"}, Envs: envs, Editor: "nano", CdFile: "x"})
	m = send(m, tea.WindowSizeMsg{Width: 160, Height: 30})
	m.list.selectDir(remoteEnv("web").Dir)
	if e, ok := m.selectedEnv(); !ok || !e.Remote() {
		t.Fatalf("could not select the remote env")
	}
	return m
}

func TestHostsOf(t *testing.T) {
	got := hostsOf([]string{"ssh://a/x", "/srv", "ssh://a/y", "ssh://me@b:2222/z", "ssh://-bad/x"})
	if len(got) != 3 || got[0] != nil || got[1].String() != "ssh://a" || got[2].String() != "ssh://me@b:2222" {
		t.Errorf("hosts %v", got)
	}
	if got := hostsOf([]string{"ssh://a/x"}); len(got) != 1 || got[0] == nil {
		t.Errorf("only remote roots should not read local docker: %v", got)
	}
	if got := hostsOf(nil); len(got) != 1 || got[0] != nil {
		t.Errorf("no roots: %v", got)
	}
}

func TestRemoteStates(t *testing.T) {
	m := remoteModel(t)
	if h := m.hostNames(); h != "pc, srv" {
		t.Errorf("header hosts %q", h)
	}
	key := "ssh://srv/stacks/web"
	m = send(m, statesMsg{states: map[string]*compose.DirState{
		"/srv/web/blog": running("web"),
		key:             running("app"),
	}})
	if r, total, _ := m.counts(remoteEnv("web")); r != 1 || total != 1 {
		t.Errorf("remote counts %d/%d", r, total)
	}

	// the host goes away: what was known stays, and the error shows once
	fail := statesMsg{states: map[string]*compose.DirState{"/srv/web/blog": running("web")},
		errs: map[string]error{"ssh://srv": errors.New("srv: no answer in time")}}
	m = send(m, fail)
	if m.states[key] == nil || !m.statusErr || !strings.Contains(m.status, "no answer") {
		t.Errorf("after an error: state %v, status %q", m.states[key], m.status)
	}
	m.setStatus("something else", false)
	m = send(m, fail)
	if m.status != "something else" {
		t.Errorf("the same error should not be shown again, status %q", m.status)
	}
	m = send(m, statesMsg{states: map[string]*compose.DirState{}})
	if m.status != "srv is reachable again" || m.states[key] != nil {
		t.Errorf("after recovery: status %q, state %v", m.status, m.states[key])
	}

	// this machine's docker failing leaves everything as it was
	m = send(m, statesMsg{states: map[string]*compose.DirState{key: running("app")}})
	m = send(m, statesMsg{errs: map[string]error{"": errors.New("cannot connect")}})
	if m.states[key] == nil || !strings.HasPrefix(m.status, "docker: ") {
		t.Errorf("local error: state %v, status %q", m.states[key], m.status)
	}
}

func TestRemoteLocalOnly(t *testing.T) {
	m := remoteModel(t)
	next, _ := m.Update(press("Q")[0])
	nm := next.(Model)
	if nm.quitDir != "" || !strings.Contains(nm.status, "does not work on remote roots yet") {
		t.Errorf("Q on a remote env: status %q", nm.status)
	}
	m = send(m, press(":", "quit to folder")...)
	for _, i := range m.menu.shown {
		if en := m.menu.entries[i]; en.name == "quit_to_folder" && (en.run || !strings.Contains(en.why, "remote")) {
			t.Errorf("quit to folder in the menu: run %v, why %q", en.run, en.why)
		}
	}
}

func TestRemoteCreateMovePrompts(t *testing.T) {
	m := remoteModel(t)

	// n starts in the remote env's parent folder, on its host
	m = send(m, press("n")...)
	if m.prompt == nil || m.prompt.input.Value() != "ssh://srv/stacks/" {
		t.Fatalf("new prompt %v", m.prompt)
	}
	m.prompt = nil

	// a failed check opens the prompt again, with what was typed
	m.busy = 1
	m = send(m, remoteCreatedMsg{value: "ssh://srv/stacks/web", err: errors.New("/stacks/web already exists on srv")})
	if m.prompt == nil || m.prompt.input.Value() != "ssh://srv/stacks/web" || !strings.Contains(m.prompt.err, "already exists") || m.busy != 0 {
		t.Errorf("after a failed check: prompt %+v, busy %d", m.prompt, m.busy)
	}
	m.prompt = nil

	// m starts with where it is, and stays on its machine
	m = send(m, press("m")...)
	if m.prompt == nil || m.prompt.input.Value() != "ssh://srv/stacks/web" {
		t.Fatalf("move prompt %v", m.prompt)
	}
	e, _ := m.selectedEnv()
	for _, to := range []string{"/srv/web/elsewhere", "ssh://other/stacks/web2"} {
		if _, err := m.moveEnv(e, to); err == nil || !strings.Contains(err.Error(), "within its machine") {
			t.Errorf("move to %s: %v", to, err)
		}
	}
	local := env("plex")
	if _, err := m.moveEnv(local, "ssh://srv/stacks/plex"); err == nil || !strings.Contains(err.Error(), "within its machine") {
		t.Errorf("local to remote: %v", err)
	}

	// a checked move asks first when it has containers
	m.prompt = nil
	m.states[e.Dir] = running("app")
	to := remoteEnv("web2")
	m.busy = 1
	m = send(m, remoteMoveCheckedMsg{from: e, value: to.Dir, to: to})
	if m.confirm == nil || !strings.Contains(m.confirm.prompt, "Stop and move web to web2") {
		t.Errorf("confirm %+v", m.confirm)
	}
}

func TestRemoteGroups(t *testing.T) {
	l := newEnvList([]discover.Env{remoteEnv("web"), remoteEnv("games/mc")}, []string{"/srv", "ssh://srv/stacks"})
	var rows []string
	for _, r := range l.rows {
		switch r.kind {
		case rowRoot, rowGroup:
			rows = append(rows, r.label)
		case rowEnv:
			rows = append(rows, l.items[r.item].env.Name())
		}
	}
	// roots in config order, the empty local one too
	if strings.Join(rows, ",") != "srv,srv:stacks,web,games,mc" {
		t.Errorf("rows %q", rows)
	}
}
