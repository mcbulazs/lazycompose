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

// Theme holds the colours of the UI. Background is optional; when set the
// terminal background is painted with it while the app runs.
type Theme struct {
	Border, Focus, Text, Dim lipgloss.TerminalColor
	Green, Yellow, Red       lipgloss.TerminalColor
	SelectBg, SelectFg       lipgloss.TerminalColor
	Background               string
}

// Themes are the built-in themes. "terminal" uses the terminal's own
// 16 colours so it follows whatever theme the terminal is set to.
var Themes = map[string]Theme{
	"terminal": {
		Border: lipgloss.Color("8"), Focus: lipgloss.Color("4"),
		Text: lipgloss.NoColor{}, Dim: lipgloss.Color("8"),
		Green: lipgloss.Color("2"), Yellow: lipgloss.Color("3"), Red: lipgloss.Color("1"),
		SelectBg: lipgloss.Color("4"), SelectFg: lipgloss.Color("0"),
	},
	"tokyonight": hexTheme("#1a1b26", "#3b4261", "#7aa2f7", "#c0caf5", "#565f89", "#9ece6a", "#e0af68", "#f7768e", "#283457"),
	"catppuccin": hexTheme("#1e1e2e", "#45475a", "#89b4fa", "#cdd6f4", "#6c7086", "#a6e3a1", "#f9e2af", "#f38ba8", "#313244"),
	"gruvbox":    hexTheme("#282828", "#504945", "#83a598", "#ebdbb2", "#928374", "#b8bb26", "#fabd2f", "#fb4934", "#3c3836"),
	"nord":       hexTheme("#2e3440", "#4c566a", "#88c0d0", "#d8dee9", "#616e88", "#a3be8c", "#ebcb8b", "#bf616a", "#3b4252"),
}

func hexTheme(bg, border, focus, text, dim, green, yellow, red, selBg string) Theme {
	return Theme{
		Border: lipgloss.Color(border), Focus: lipgloss.Color(focus),
		Text: lipgloss.Color(text), Dim: lipgloss.Color(dim),
		Green: lipgloss.Color(green), Yellow: lipgloss.Color(yellow), Red: lipgloss.Color(red),
		SelectBg: lipgloss.Color(selBg), SelectFg: lipgloss.Color(text),
		Background: bg,
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

// LoadTheme returns a built-in theme with single colours overridden.
func LoadTheme(name string, overrides map[string]string) (Theme, error) {
	if name == "" {
		name = "terminal"
	}
	t, ok := Themes[name]
	if !ok {
		return t, fmt.Errorf("unknown theme %q, available: %s", name, strings.Join(ThemeNames(), ", "))
	}
	for k, v := range overrides {
		if err := t.set(k, v); err != nil {
			return t, err
		}
	}
	return t, nil
}

var hexColor = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

func (t *Theme) set(key, value string) error {
	value = strings.TrimSpace(value)
	if n, err := strconv.Atoi(value); !hexColor.MatchString(value) && (err != nil || n < 0 || n > 255) {
		return fmt.Errorf("colors.%s: %q is not a hex colour or 0-255", key, value)
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
	case "background":
		if !hexColor.MatchString(value) {
			return fmt.Errorf("colors.background: %q must be a hex colour", value)
		}
		t.Background = value
	default:
		return fmt.Errorf("unknown colour %q", key)
	}
	return nil
}

var (
	colBorder, colFocus, colText, colDim lipgloss.TerminalColor

	textStyle, dimStyle, greenStyle, yellowStyle, redStyle, accentStyle lipgloss.Style
	titleStyle, titleDim, selectedRow, groupStyle, confirmStyle         lipgloss.Style
)

func init() { applyTheme(Themes["terminal"]) }

func applyTheme(t Theme) {
	colBorder, colFocus, colText, colDim = t.Border, t.Focus, t.Text, t.Dim

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

func pane(title, body string, w, h int, focused bool) string {
	border := colBorder
	t := titleDim
	if focused {
		border = colFocus
		t = titleStyle
	}
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Width(max(w-2, 0)).
		Height(max(h-2, 0)).
		MaxHeight(max(h, 0))
	title = ansi.Truncate(title, max(w-4, 1), "…")
	return style.Render(t.Render(title) + "\n" + body)
}
