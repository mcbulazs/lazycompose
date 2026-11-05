package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestZoom(t *testing.T) {
	m := newTestModel(t)
	m = send(m, press("3", "z")...)
	if !m.zoomed || m.logs.Width != m.width-4 {
		t.Fatalf("z should zoom the log pane to full width, width %d", m.logs.Width)
	}
	v := ansi.Strip(m.View())
	if strings.Contains(v, "Environments") || !strings.Contains(v, "zoomed") {
		t.Errorf("only the zoomed pane should show:\n%s", v)
	}
	if v := ansi.Strip(send(m, press("1")...).View()); !strings.Contains(v, "Environments") || strings.Contains(v, "Services") {
		t.Errorf("switching panes while zoomed should show that pane:\n%s", v)
	}
	if m = send(m, press("esc")...); m.zoomed {
		t.Error("esc should leave the zoom")
	}
}
