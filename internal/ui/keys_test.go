package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

func resetKeys(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		if err := SetKeys(nil); err != nil {
			t.Fatal(err)
		}
	})
}

func runes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestDefaultKeysHaveNoClashes(t *testing.T) {
	if _, err := buildKeys(nil); err != nil {
		t.Fatal(err)
	}
}

func TestSetKeys(t *testing.T) {
	resetKeys(t)
	err := SetKeys(map[string][]string{
		"recreate": {"F"},
		"quit":     {"q", "ctrl+d"},
		"refresh":  {}, // unbound
	})
	if err != nil {
		t.Fatal(err)
	}
	if !key.Matches(runes("F"), keys.Recreate) || key.Matches(runes("R"), keys.Recreate) {
		t.Error("recreate should move from R to F")
	}
	if !key.Matches(tea.KeyMsg{Type: tea.KeyCtrlD}, keys.Quit) {
		t.Error("quit should accept ctrl+d")
	}
	if keys.Refresh.Enabled() {
		t.Error("refresh should be off")
	}
	if label := keys.Quit.Help().Key; label != "q/ctrl+d" {
		t.Errorf("help label %q", label)
	}
}

func TestSetKeysErrors(t *testing.T) {
	resetKeys(t)
	tests := map[string]map[string][]string{
		"unknown action":  {"explode": {"z"}},
		"clash":           {"restart": {"d"}},
		"fixed key":       {"quit": {"ctrl+c"}},
		"clash in config": {"pull": {"z"}, "logs": {"z"}},
	}
	for name, over := range tests {
		if err := SetKeys(over); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	// a failed load keeps the previous keys
	if !key.Matches(runes("r"), keys.Restart) {
		t.Error("defaults should survive a bad config")
	}
}

func TestRemappedKeyRuns(t *testing.T) {
	resetKeys(t)
	if err := SetKeys(map[string][]string{"down": {"D"}}); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	m = send(m, press("d")...)
	if m.confirm != nil {
		t.Fatal("d should no longer run down")
	}
	m = send(m, press("D")...)
	if m.confirm == nil || !strings.Contains(m.confirm.prompt, "down for plex") {
		t.Errorf("D should ask for down, got %+v", m.confirm)
	}
}

func TestPaneKeys(t *testing.T) {
	m := newTestModel(t)
	for _, tt := range []struct {
		key  string
		want focus
	}{{"3", focusLogs}, {"1", focusEnvs}, {"2", focusServices}} {
		m = send(m, press(tt.key)...)
		if m.focus != tt.want {
			t.Errorf("%s: focus %v, want %v", tt.key, m.focus, tt.want)
		}
	}
}
