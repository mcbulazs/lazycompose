package ui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestLoadTheme(t *testing.T) {
	th, err := LoadTheme("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if th.Focus != Themes["terminal"].Focus {
		t.Errorf("default theme should be terminal")
	}

	th, err = LoadTheme("nord", nil, map[string]string{"focus": "#ff0000", "red": "9", "background": "#000"}) // background is ignored
	if err != nil {
		t.Fatal(err)
	}
	if th.Focus != themeColor("focus", "#ff0000") || th.Red != lipgloss.Color("9") {
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
	if th.Focus != themeColor("focus", "#ff9e64") {
		t.Errorf("focus not set from theme: %v", th.Focus)
	}
	if th.Green != Themes["onedark"].Green || th.Border != Themes["onedark"].Border {
		t.Errorf("other colours should come from the base theme")
	}
	if th.Red != themeColor("red", "#ff0000") {
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
	if th.Focus != themeColor("focus", "#ffffff") || th.Green != Themes["nord"].Green {
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

// every built-in theme keeps its colours apart from the background when the
// terminal has only 256 or 16 colours
func TestThemeFallbacks(t *testing.T) {
	for name, th := range Themes {
		for key, c := range map[string]lipgloss.TerminalColor{
			"border": th.Border, "focus": th.Focus, "text": th.Text, "dim": th.Dim,
			"green": th.Green, "yellow": th.Yellow, "red": th.Red, "select_fg": th.SelectFg,
		} {
			cc, ok := c.(lipgloss.CompleteColor)
			if !ok {
				continue // the terminal theme uses the terminal's own colours
			}
			n, _ := strconv.Atoi(cc.ANSI256)
			r, g, b := xterm256(n)
			if _, _, l := hsl(r, g, b); l < 0.2 {
				t.Errorf("%s %s: %s becomes %d (%d,%d,%d) with 256 colours, too dark", name, key, cc.TrueColor, n, r, g, b)
			}
			if cc.ANSI == "0" {
				t.Errorf("%s %s: black with 16 colours", name, key)
			}
		}
	}
}

func TestNearest256(t *testing.T) {
	for hex, want := range map[string]int{
		"#e06c75": 168, // onedark red, was 232 (almost black) through lipgloss
		"#000000": 16,
		"#ffffff": 231,
		"#ff0000": 196,
		"#808080": 244,
	} {
		r, g, b, _ := parseHex(hex)
		if got := nearest256(r, g, b); got != want {
			t.Errorf("nearest256(%s) = %d, want %d", hex, got, want)
		}
	}
}

func TestRoleFallbacks(t *testing.T) {
	for key, want := range map[string]string{"red": "1", "green": "2", "yellow": "3", "text": "7", "dim": "8"} {
		// nord's text is a light blue-grey; it must stay plain text
		c := themeColor(key, "#d8dee9").(lipgloss.CompleteColor)
		if c.ANSI != want {
			t.Errorf("%s: 16 colour fallback %s, want %s", key, c.ANSI, want)
		}
	}
	if c := themeColor("focus", "#61afef").(lipgloss.CompleteColor); c.ANSI != "4" {
		t.Errorf("a blue focus should fall back to blue, got %s", c.ANSI)
	}
	if c := themeColor("red", "9"); c != lipgloss.Color("9") {
		t.Errorf("a 0-255 colour is kept as it is, got %v", c)
	}
}

// xterm256 is the RGB of a colour of the cube or the grey ramp.
func xterm256(n int) (r, g, b int) {
	if n >= 232 {
		v := 8 + 10*(n-232)
		return v, v, v
	}
	n -= 16
	return cubeLevels[n/36], cubeLevels[n/6%6], cubeLevels[n%6]
}
