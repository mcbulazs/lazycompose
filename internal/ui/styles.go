package ui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	colBorder   = lipgloss.Color("#3b4261")
	colFocus    = lipgloss.Color("#7aa2f7")
	colText     = lipgloss.Color("#c0caf5")
	colDim      = lipgloss.Color("#565f89")
	colGreen    = lipgloss.Color("#9ece6a")
	colYellow   = lipgloss.Color("#e0af68")
	colRed      = lipgloss.Color("#f7768e")
	colSelectBg = lipgloss.Color("#283457")
)

var (
	textStyle   = lipgloss.NewStyle().Foreground(colText)
	dimStyle    = lipgloss.NewStyle().Foreground(colDim)
	greenStyle  = lipgloss.NewStyle().Foreground(colGreen)
	yellowStyle = lipgloss.NewStyle().Foreground(colYellow)
	redStyle    = lipgloss.NewStyle().Foreground(colRed)
	accentStyle = lipgloss.NewStyle().Foreground(colFocus).Bold(true)

	titleStyle   = lipgloss.NewStyle().Foreground(colFocus).Bold(true)
	titleDim     = lipgloss.NewStyle().Foreground(colDim).Bold(true)
	selectedRow  = lipgloss.NewStyle().Background(colSelectBg).Foreground(colText).Bold(true)
	groupStyle   = lipgloss.NewStyle().Foreground(colDim).Bold(true)
	confirmStyle = lipgloss.NewStyle().Foreground(colYellow).Bold(true)
)

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
