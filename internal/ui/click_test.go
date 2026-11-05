package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// screenFind returns the column and row where text first appears on screen.
func screenFind(t *testing.T, m Model, text string) (int, int) {
	t.Helper()
	for y, line := range strings.Split(ansi.Strip(m.View()), "\n") {
		if i := strings.Index(line, text); i >= 0 {
			return ansi.StringWidth(line[:i]), y
		}
	}
	t.Fatalf("%q is not on screen:\n%s", text, ansi.Strip(m.View()))
	return 0, 0
}

func TestFooterClicks(t *testing.T) {
	m := newTestModel(t)
	x, y := screenFind(t, m, ": run")
	if y != m.height-1 {
		t.Fatalf("footer on row %d, want %d", y, m.height-1)
	}
	m = send(m, click(x, y))
	if m.menu == nil || m.menu.all {
		t.Fatal("clicking : run should open the action menu")
	}
	m = send(m, click(0, 0))
	if m.menu != nil {
		t.Fatal("a click outside should close the menu")
	}
	x, y = screenFind(t, m, "? keys")
	if m = send(m, click(x+2, y)); m.menu == nil || !m.menu.all {
		t.Fatal("clicking ? keys should open the keys window")
	}
}

func TestMenuMouse(t *testing.T) {
	m := newTestModel(t)
	m = send(m, press(":")...)
	m = send(m, tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if m.menu.cursor != 1 {
		t.Errorf("the wheel should move the cursor, got %d", m.menu.cursor)
	}
	// "Pin to" is on screen; a click selects it, a second click runs it
	x, y := screenFind(t, m, "Pin to quick access")
	m = send(m, click(x, y))
	if m.menu == nil || m.menu.entries[m.menu.shown[m.menu.cursor]].name != "pin" {
		t.Fatal("a click should select the entry under it")
	}
	m = send(m, click(x, y))
	if m.menu != nil {
		t.Error("a second click should run the entry and close the menu")
	}
}

func TestPromptClickOutside(t *testing.T) {
	m := newTestModel(t)
	m = send(m, press("n")...)
	if m.prompt == nil {
		t.Fatal("n should open the prompt")
	}
	x, y := screenFind(t, m, "New environment")
	if m = send(m, click(x, y+1)); m.prompt == nil {
		t.Error("a click inside should keep the prompt")
	}
	if m = send(m, click(0, m.height-3)); m.prompt != nil {
		t.Error("a click outside should cancel the prompt")
	}
}

func TestConfirmButtons(t *testing.T) {
	m := newTestModel(t)
	m = send(m, press("R")...)
	if m.confirm == nil {
		t.Fatal("R should ask first")
	}
	if v := ansi.Strip(m.statusView()); strings.Contains(v, "(y/n)") || !strings.Contains(v, "y yes") {
		t.Errorf("the question should show buttons: %q", v)
	}
	x, y := screenFind(t, m, "n no")
	if m = send(m, click(x, y)); m.confirm != nil || m.status != "cancelled" {
		t.Errorf("clicking no should cancel, status %q", m.status)
	}
}
