package ui

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const DefaultTheme = "terminal"

// Theme holds the colours of the UI. The terminal background is never
// changed, so transparent terminals keep working.
type Theme struct {
	Border, Focus, Text, Dim lipgloss.TerminalColor
	Green, Yellow, Red       lipgloss.TerminalColor
	SelectBg, SelectFg       lipgloss.TerminalColor
}

// Themes are the built-in themes. "terminal" only uses the terminal's own
// palette, in the style of lazygit: green active border, blue selected line.
var Themes = map[string]Theme{
	"terminal": {
		Border: lipgloss.NoColor{}, Focus: lipgloss.Color("2"),
		Text: lipgloss.NoColor{}, Dim: lipgloss.Color("8"),
		Green: lipgloss.Color("2"), Yellow: lipgloss.Color("3"), Red: lipgloss.Color("1"),
		SelectBg: lipgloss.Color("4"), SelectFg: lipgloss.NoColor{},
	},
	"onedark":    hexTheme("#4b5263", "#61afef", "#abb2bf", "#5c6370", "#98c379", "#e5c07b", "#e06c75", "#3e4451"),
	"tokyonight": hexTheme("#3b4261", "#7aa2f7", "#c0caf5", "#565f89", "#9ece6a", "#e0af68", "#f7768e", "#283457"),
	"catppuccin": hexTheme("#45475a", "#89b4fa", "#cdd6f4", "#6c7086", "#a6e3a1", "#f9e2af", "#f38ba8", "#313244"),
	"gruvbox":    hexTheme("#504945", "#83a598", "#ebdbb2", "#928374", "#b8bb26", "#fabd2f", "#fb4934", "#3c3836"),
	"nord":       hexTheme("#4c566a", "#88c0d0", "#d8dee9", "#616e88", "#a3be8c", "#ebcb8b", "#bf616a", "#3b4252"),
}

func hexTheme(border, focus, text, dim, green, yellow, red, selBg string) Theme {
	return Theme{
		Border: lipgloss.Color(border), Focus: lipgloss.Color(focus),
		Text: lipgloss.Color(text), Dim: lipgloss.Color(dim),
		Green: lipgloss.Color(green), Yellow: lipgloss.Color(yellow), Red: lipgloss.Color(red),
		SelectBg: lipgloss.Color(selBg), SelectFg: lipgloss.Color(text),
	}
}

// ThemeNames lists the built-in themes, sorted.
func ThemeNames() []string {
	names := make([]string, 0, len(Themes))
	for n := range Themes {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// LoadTheme picks a theme by name from the built-in ones or the user's own,
// then applies single colour overrides. A user theme starts from a built-in
// theme given by its "base" key, or from the default theme.
func LoadTheme(name string, custom map[string]map[string]string, overrides map[string]string) (Theme, error) {
	if name == "" {
		name = DefaultTheme
	}

	var t Theme
	if colors, ok := custom[name]; ok {
		base := colors["base"]
		if base == "" {
			base = DefaultTheme
			if _, builtin := Themes[name]; builtin {
				base = name
			}
		}
		bt, ok := Themes[base]
		if !ok {
			return t, fmt.Errorf("themes.%s.base: %q is not a built-in theme, use one of: %s",
				name, base, strings.Join(ThemeNames(), ", "))
		}
		t = bt
		for _, k := range sortedKeys(colors) {
			if k == "base" {
				continue
			}
			if err := t.set(k, colors[k]); err != nil {
				return t, fmt.Errorf("themes.%s.%s: %w", name, k, err)
			}
		}
	} else if bt, ok := Themes[name]; ok {
		t = bt
	} else {
		names := ThemeNames()
		names = append(names, sortedKeys(custom)...)
		return t, fmt.Errorf("unknown theme %q, available: %s", name, strings.Join(names, ", "))
	}

	for _, k := range sortedKeys(overrides) {
		if err := t.set(k, overrides[k]); err != nil {
			return t, fmt.Errorf("colors.%s: %w", k, err)
		}
	}
	return t, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ColorKeys are the colour names a theme can set.
var ColorKeys = []string{"border", "focus", "text", "dim", "green", "yellow", "red", "select_bg", "select_fg"}

var hexColor = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

func (t *Theme) set(key, value string) error {
	if key == "background" {
		// older versions painted the terminal background; ignore the key
		return nil
	}
	value = strings.TrimSpace(value)
	if n, err := strconv.Atoi(value); !hexColor.MatchString(value) && (err != nil || n < 0 || n > 255) {
		return fmt.Errorf("%q is not a hex colour or 0-255", value)
	}
	c := lipgloss.Color(value)
	switch key {
	case "border":
		t.Border = c
	case "focus":
		t.Focus = c
	case "text":
		t.Text = c
	case "dim":
		t.Dim = c
	case "green":
		t.Green = c
	case "yellow":
		t.Yellow = c
	case "red":
		t.Red = c
	case "select_bg":
		t.SelectBg = c
	case "select_fg":
		t.SelectFg = c
	default:
		return fmt.Errorf("unknown colour key, use one of: %s", strings.Join(ColorKeys, ", "))
	}
	return nil
}

var (
	colBorder, colFocus, colText, colDim lipgloss.TerminalColor
	colYellowBg, colMatchFg              lipgloss.TerminalColor

	textStyle, dimStyle, greenStyle, yellowStyle, redStyle, accentStyle lipgloss.Style
	titleStyle, titleDim, selectedRow, groupStyle, confirmStyle         lipgloss.Style
)

func init() { applyTheme(Themes[DefaultTheme]) }

func applyTheme(t Theme) {
	colBorder, colFocus, colText, colDim = t.Border, t.Focus, t.Text, t.Dim
	colYellowBg, colMatchFg = t.Yellow, lipgloss.Color("0")

	textStyle = lipgloss.NewStyle().Foreground(t.Text)
	dimStyle = lipgloss.NewStyle().Foreground(t.Dim)
	greenStyle = lipgloss.NewStyle().Foreground(t.Green)
	yellowStyle = lipgloss.NewStyle().Foreground(t.Yellow)
	redStyle = lipgloss.NewStyle().Foreground(t.Red)
	accentStyle = lipgloss.NewStyle().Foreground(t.Focus).Bold(true)

	titleStyle = lipgloss.NewStyle().Foreground(t.Focus).Bold(true)
	titleDim = lipgloss.NewStyle().Foreground(t.Dim).Bold(true)
	selectedRow = lipgloss.NewStyle().Background(t.SelectBg).Foreground(t.SelectFg).Bold(true)
	groupStyle = lipgloss.NewStyle().Foreground(t.Dim).Bold(true)
	confirmStyle = lipgloss.NewStyle().Foreground(t.Yellow).Bold(true)
}

// pane draws a bordered box of w by h cells with its title in the top
// border, like ╭─ [2] Services ──╮.
func pane(title, body string, w, h int, focused bool) string {
	border := colBorder
	t := titleDim
	if focused {
		border = colFocus
		t = titleStyle
	}
	bs := lipgloss.NewStyle().Foreground(border)
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderTop(false).
		BorderForeground(border).
		Padding(0, 1).
		Width(max(w-2, 0)).
		Height(max(h-2, 0)).
		MaxHeight(max(h-1, 0))

	title = ansi.Truncate(title, max(w-6, 1), "…")
	fill := max(w-lipgloss.Width(title)-5, 0)
	top := bs.Render("╭─ ") + t.Render(title) + bs.Render(" "+strings.Repeat("─", fill)+"╮")
	if w < 7 {
		top = bs.Render("╭" + strings.Repeat("─", max(w-2, 0)) + "╮")
	}
	return top + "\n" + style.Render(body)
}
