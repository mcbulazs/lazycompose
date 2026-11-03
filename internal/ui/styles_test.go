package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestLoadTheme(t *testing.T) {
	th, err := LoadTheme("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if th.Background != "" {
		t.Errorf("terminal theme should not paint the background, got %q", th.Background)
	}

	th, err = LoadTheme("nord", nil, map[string]string{"focus": "#ff0000", "red": "9", "background": "#000"})
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
		{"onedark", map[string]string{"focus": "blue"}},
		{"onedark", map[string]string{"focus": "256"}},
		{"onedark", map[string]string{"background": "4"}},
		{"onedark", map[string]string{"accent": "#fff"}},
	}
	for _, c := range cases {
		if _, err := LoadTheme(c.name, nil, c.overrides); err == nil {
			t.Errorf("LoadTheme(%q, %v): expected error", c.name, c.overrides)
		}
	}
}

func TestLoadCustomTheme(t *testing.T) {
	custom := map[string]map[string]string{
		"sunset": {"base": "onedark", "focus": "#ff9e64"},
		"plain":  {"red": "9"},
		"nord":   {"focus": "#ffffff"},
	}

	th, err := LoadTheme("sunset", custom, map[string]string{"red": "#ff0000"})
	if err != nil {
		t.Fatal(err)
	}
	if th.Focus != lipgloss.Color("#ff9e64") {
		t.Errorf("focus not set from theme: %v", th.Focus)
	}
	if th.Green != Themes["onedark"].Green || th.Background != Themes["onedark"].Background {
		t.Errorf("other colours should come from the base theme")
	}
	if th.Red != lipgloss.Color("#ff0000") {
		t.Errorf("colors overrides should apply on top of the theme: %v", th.Red)
	}

	// without a base, a custom theme starts from the default
	th, err = LoadTheme("plain", custom, nil)
	if err != nil {
		t.Fatal(err)
	}
	if th.Red != lipgloss.Color("9") || th.Green != Themes[DefaultTheme].Green {
		t.Errorf("unexpected plain theme: %+v", th)
	}

	// a custom theme with a built-in name changes that theme
	th, err = LoadTheme("nord", custom, nil)
	if err != nil {
		t.Fatal(err)
	}
	if th.Focus != lipgloss.Color("#ffffff") || th.Background != Themes["nord"].Background {
		t.Errorf("unexpected nord override: %+v", th)
	}
}

func TestLoadCustomThemeErrors(t *testing.T) {
	cases := []map[string]string{
		{"base": "sunset"},
		{"base": "nope"},
		{"focus": "orange"},
		{"accent": "#fff"},
	}
	for _, c := range cases {
		custom := map[string]map[string]string{"mine": c}
		if _, err := LoadTheme("mine", custom, nil); err == nil {
			t.Errorf("theme %v: expected error", c)
		}
	}

	_, err := LoadTheme("other", map[string]map[string]string{"mine": {}}, nil)
	if err == nil || !strings.Contains(err.Error(), "mine") {
		t.Errorf("unknown theme error should list custom themes, got %v", err)
	}
}
