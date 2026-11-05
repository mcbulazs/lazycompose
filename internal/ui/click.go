package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The bottom line and the yes / no of a question can be clicked, so the
// whole program works with the mouse alone.

const footerSep = " • "

// footerItems are the keys shown in the bottom line.
func footerItems() []key.Binding {
	var out []key.Binding
	for _, b := range []key.Binding{keys.Help, keys.Commands, keys.Filter, keys.Quit} {
		if b.Enabled() {
			out = append(out, b)
		}
	}
	return out
}

func footerLabel(b key.Binding) string { return b.Help().Key + " " + b.Help().Desc }

// footerAt returns the key under column x of the bottom line.
func (m Model) footerAt(x int) (key.Binding, bool) {
	pos := 1
	for i, b := range footerItems() {
		if i > 0 {
			pos += lipgloss.Width(footerSep)
		}
		w := lipgloss.Width(footerLabel(b))
		if x >= pos && x < pos+w && pos+w <= m.width-1 {
			return b, true
		}
		pos += w
	}
	return key.Binding{}, false
}

var confirmButtons = []struct{ key, label string }{{"y", " y yes "}, {"n", " n no "}}

// text is the question without its "(y/n)", which the buttons replace.
func (c *confirm) text() string { return strings.TrimSuffix(c.prompt, " (y/n)") }

func (m Model) confirmView() string {
	s := confirmStyle.Render(m.confirm.text()) + " "
	for _, b := range confirmButtons {
		s += " " + selectedRow.Render(b.label)
	}
	return s
}

// confirmAt returns the answer under column x of the status line.
func (m Model) confirmAt(x int) string {
	pos := 1 + lipgloss.Width(m.confirm.text()) + 1
	for _, b := range confirmButtons {
		pos++ // the space before a button
		w := lipgloss.Width(b.label)
		if x >= pos && x < pos+w && pos+w <= m.width-1 {
			return b.key
		}
		pos += w
	}
	return ""
}

// handleBarClick handles a click on the bottom line or on an answer of a
// question, and reports whether it was one.
func (m Model) handleBarClick(msg tea.MouseMsg) (tea.Model, tea.Cmd, bool) {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil, false
	}
	switch msg.Y {
	case m.height - 1:
		if m.filtering || m.confirm != nil {
			return m, nil, true
		}
		if b, ok := m.footerAt(msg.X); ok {
			if k, ok := keyMsg(b.Keys()[0]); ok {
				next, cmd := m.handleKey(k)
				return next, cmd, true
			}
		}
		return m, nil, true
	case m.height - 2:
		if m.confirm == nil {
			return m, nil, false
		}
		if a := m.confirmAt(msg.X); a != "" {
			next, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(a)})
			return next, cmd, true
		}
		return m, nil, true
	}
	return m, nil, false
}
