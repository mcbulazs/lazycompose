package ui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestLoadTheme(t *testing.T) {
	th, err := LoadTheme("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if th.Background != "" {
		t.Errorf("terminal theme should not paint the background, got %q", th.Background)
	}

	th, err = LoadTheme("nord", map[string]string{"focus": "#ff0000", "red": "9", "background": "#000"})
	if err != nil {
		t.Fatal(err)
	}
	if th.Focus != lipgloss.Color("#ff0000") || th.Red != lipgloss.Color("9") || th.Background != "#000" {
		t.Errorf("overrides not applied: %+v", th)
	}
	if th.Green != Themes["nord"].Green {
		t.Errorf("other colours should come from the theme")
	}
}

func TestLoadThemeErrors(t *testing.T) {
	cases := []struct {
		name      string
		overrides map[string]string
	}{
		{"solarized", nil},
		{"terminal", map[string]string{"focus": "blue"}},
		{"terminal", map[string]string{"focus": "256"}},
		{"terminal", map[string]string{"background": "4"}},
		{"terminal", map[string]string{"accent": "#fff"}},
	}
	for _, c := range cases {
		if _, err := LoadTheme(c.name, c.overrides); err == nil {
			t.Errorf("LoadTheme(%q, %v): expected error", c.name, c.overrides)
		}
	}
}
