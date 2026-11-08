package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/mcbulazs/lazycompose/internal/compose"
)

func queueOf(m Model) []string {
	var out []string
	for _, e := range m.updating.queue {
		out = append(out, e.Rel)
	}
	return out
}

func TestHeadingActions(t *testing.T) {
	m := newTestModel(t)
	// web/blog runs, web/shop has no containers
	m = send(m,
		servicesMsg{services: map[string][]string{"/srv/web/blog": {"web"}, "/srv/web/shop": {"web"}}},
		statesMsg{states: map[string]*compose.DirState{"/srv/web/blog": running("web")}})
	m.list.selectKey(groupKey(rootKey("/srv"), "web"))

	// u starts every stack in the folder, after asking
	m = send(m, press("u")...)
	if m.confirm == nil || m.confirm.prompt != "Run docker compose up -d on 2 stacks in web/? (y/n)" {
		t.Fatalf("prompt %+v", m.confirm)
	}
	m = send(m, press("y")...)
	if m.updating == nil || !slices.Equal(queueOf(m), []string{"web/blog", "web/shop"}) || !slices.Equal(m.updating.args, upArgs) {
		t.Fatalf("run %+v", m.updating)
	}
	if !strings.Contains(m.status, "up -d 1/2: web/blog") || m.logTitle != "up -d · web/" {
		t.Errorf("status %q, title %q", m.status, m.logTitle)
	}
	m = send(m, updateMsg{env: env("web/blog")})
	m = send(m, updateMsg{env: env("web/shop")})
	if m.updating != nil || m.status != "up -d done: 2 done, 0 failed" {
		t.Errorf("after the queue: status %q", m.status)
	}
	m = send(m, press("esc")...)

	// d and r leave out the stacks that have no containers
	m.list.selectKey(groupKey(rootKey("/srv"), "web"))
	m = send(m, press("d")...)
	if m.confirm == nil || m.confirm.prompt != "Run docker compose down on 1 stack in web/? (y/n)" {
		t.Fatalf("prompt %+v", m.confirm)
	}
	m = send(m, press("n")...)

	// nothing with containers: nothing to ask
	m.list.selectKey(groupKey(rootKey("/srv"), "games"))
	m = send(m, press("r")...)
	if m.confirm != nil || !strings.Contains(m.status, "no stack in games/ has containers") {
		t.Errorf("confirm %+v, status %q", m.confirm, m.status)
	}

	// U updates the running stacks under the heading only
	m.list.selectKey(groupKey(rootKey("/srv"), "web"))
	m = send(m, press("U")...)
	if m.confirm == nil || !strings.Contains(m.confirm.prompt, "Update 1 running stacks in web/") {
		t.Fatalf("prompt %+v", m.confirm)
	}
	m = send(m, press("y")...)
	if !slices.Equal(queueOf(m), []string{"web/blog"}) || m.updating.args != nil {
		t.Errorf("update run %+v", m.updating)
	}

	// the menu shows what they do on the heading, and runs them
	m.updating = nil
	m.list.selectKey(groupKey(rootKey("/srv"), "web"))
	m = send(m, press(":", "restart")...)
	for _, i := range m.menu.shown {
		if en := m.menu.entries[i]; en.name == "restart" && (!en.run || en.short != "docker compose restart (each stack in web/)") {
			t.Errorf("restart in the menu: run %v, %q, why %q", en.run, en.short, en.why)
		}
	}
	m = send(m, press("enter")...)
	if m.confirm == nil || !strings.Contains(m.confirm.prompt, "restart on 1 stack in web/") {
		t.Errorf("running it from the menu: %+v", m.confirm)
	}
	m.confirm = nil

	// other keys do nothing on a heading
	m.updating = nil
	m.list.selectKey(groupKey(rootKey("/srv"), "web"))
	m = send(m, press("e")...)
	if m.edit != nil {
		t.Error("e on a heading should not edit")
	}
}
