package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

// text is the question without its "(y/n)", which the buttons replace.
func (c *confirm) text() string { return strings.TrimSuffix(c.prompt, " (y/n)") }

// buttons are the answers as shown. Yes is the default that enter gives,
// so its key is a capital, as in a shell's [Y/n].
func (c *confirm) buttons() (yes, no string) { return " Y yes ", " n no " }

// confirmBox is the open question drawn as a box in the middle of the
// screen, so it cannot be missed, with where it and its buttons are.
type confirmBox struct {
	x, y, w    int
	box        string
	row        int // screen row of the buttons
	yesX, yesW int
	noX, noW   int
}

const buttonGap = 3

func (m Model) confirmBox() confirmBox {
	c := m.confirm
	text := c.text()
	w := min(max(lipgloss.Width(text)+4, 44), max(m.width*2/3, 44), m.width-4)
	inner := max(w-4, 10)
	lines := strings.Split(ansi.Wrap(text, inner, " "), "\n")
	yes, no := c.buttons()
	pad := max((inner-lipgloss.Width(yes)-buttonGap-lipgloss.Width(no))/2, 0)
	body := make([]string, 0, len(lines)+2)
	for _, l := range lines {
		body = append(body, textStyle.Render(l))
	}
	body = append(body, "", strings.Repeat(" ", pad)+selectedRow.Render(yes)+strings.Repeat(" ", buttonGap)+selectedRow.Render(no))
	title := c.title
	if title == "" {
		title = "Are you sure?"
	}
	b := confirmBox{w: w, box: pane(title, strings.Join(body, "\n"), w, len(body)+2, true)}
	b.x = (m.width - w) / 2
	b.y = max((m.height-lipgloss.Height(b.box))/2-2, 1)
	// the box has its title in the top border and a space inside each side
	b.row = b.y + 1 + len(lines) + 1
	b.yesX, b.yesW = b.x+2+pad, lipgloss.Width(yes)
	b.noX, b.noW = b.yesX+b.yesW+buttonGap, lipgloss.Width(no)
	return b
}

func (m Model) withConfirm(view string) string {
	if m.confirm == nil {
		return view
	}
	b := m.confirmBox()
	return overlay(view, b.box, b.x, b.y)
}

// confirmAt returns the answer under a click, if it is on a button.
func (m Model) confirmAt(x, y int) string {
	b := m.confirmBox()
	switch {
	case y != b.row:
	case x >= b.yesX && x < b.yesX+b.yesW:
		return "y"
	case x >= b.noX && x < b.noX+b.noW:
		return "n"
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
	}
	if m.confirm != nil {
		// only the buttons of the question can be clicked
		if a := m.confirmAt(msg.X, msg.Y); a != "" {
			next, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(a)})
			return next, cmd, true
		}
		return m, nil, true
	}
	return m, nil, false
}
