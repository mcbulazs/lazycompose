package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRestartAsks(t *testing.T) {
	m := newTestModel(t)
	m = send(m, press("r")...)
	if m.confirm == nil || m.confirm.title != "Restart" || m.confirm.prompt != "Restart plex? (y/n)" {
		t.Fatalf("r should ask first, got %+v", m.confirm)
	}
	if yes, _ := m.confirm.buttons(); yes != " y/r yes " {
		t.Errorf("r again should answer yes: %q", yes)
	}
	// r again restarts
	m = send(m, press("r")...)
	if m.confirm != nil || len(m.actions) != 1 || !strings.Contains(m.status, "restarting plex") {
		t.Errorf("confirm %+v, actions %d, status %q", m.confirm, len(m.actions), m.status)
	}
}

func TestNoConfirm(t *testing.T) {
	m := New(Options{Hostname: "host", Roots: []string{"/srv"}, Envs: testEnvs(), NoConfirm: true})
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	for _, k := range []string{"r", "d", "R"} {
		next, _ := m.Update(press(k)[0])
		nm := next.(Model)
		if nm.confirm != nil || len(nm.actions) != 1 {
			t.Errorf("%s with confirm off should run at once: confirm %+v, actions %d", k, nm.confirm, len(nm.actions))
		}
	}
}

func TestAfterEdit(t *testing.T) {
	for _, tc := range []struct {
		mode    string
		ask     bool
		actions int
		status  string
	}{
		{"", true, 0, "config ok"},
		{AfterEditAsk, true, 0, "config ok"},
		{AfterEditApply, false, 1, "up -d plex"},
		{AfterEditCheck, false, 0, "config ok; u applies it"},
	} {
		m := New(Options{Hostname: "host", Roots: []string{"/srv"}, Envs: testEnvs(), AfterEdit: tc.mode, NoConfirm: true})
		m = send(m, tea.WindowSizeMsg{Width: 120, Height: 30})
		m = send(m, validateMsg{env: env("plex")})
		if (m.confirm != nil) != tc.ask || len(m.actions) != tc.actions || !strings.Contains(m.status, tc.status) {
			t.Errorf("after_edit %q: confirm %v, actions %d, status %q", tc.mode, m.confirm != nil, len(m.actions), m.status)
		}
	}
}
