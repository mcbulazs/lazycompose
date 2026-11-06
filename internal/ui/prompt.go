package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// pathPrompt is a one-line input shown in a box over the panes, used to
// type the folder for a new or moved environment.
type pathPrompt struct {
	title string
	hint  string
	input textinput.Model
	err   string
	// submit runs on enter. An error keeps the box open and shows it.
	submit func(m *Model, value string) (tea.Cmd, error)
}

func (m *Model) openPrompt(title, hint, value string, submit func(m *Model, value string) (tea.Cmd, error)) tea.Cmd {
	in := textinput.New()
	in.Prompt = ""
	in.TextStyle = textStyle
	in.Cursor.Style = textStyle
	in.SetValue(value)
	in.CursorEnd()
	m.prompt = &pathPrompt{title: title, hint: hint, input: in, submit: submit}
	return m.prompt.input.Focus()
}

func (m Model) handlePromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.prompt
	switch msg.String() {
	case "esc":
		m.prompt = nil
		m.setStatus("cancelled", false)
		return m, nil
	case "enter":
		cmd, err := p.submit(&m, strings.TrimSpace(p.input.Value()))
		if err != nil {
			p.err = err.Error()
			return m, nil
		}
		if m.prompt == p { // submit may open a confirm or another prompt
			m.prompt = nil
		}
		return m, cmd
	}
	p.err = ""
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	return m, cmd
}

// promptBox renders the prompt as a bordered box w cells wide.
func (p *pathPrompt) view(w int) string {
	inner := max(w-4, 10)
	p.input.Width = inner - 1
	lines := []string{p.input.View()}
	if p.err != "" {
		// wrapped, not cut: a long path would hide the reason at the end;
		// paths break at their separators
		for _, l := range strings.Split(ansi.Wrap(p.err, inner, `/\`), "\n") {
			lines = append(lines, redStyle.Render(l))
		}
	} else {
		lines = append(lines, dimStyle.Render(ansi.Truncate(p.hint, inner, "…")))
	}
	return pane(p.title, strings.Join(lines, "\n"), w, len(lines)+2, true)
}

// overlay draws fg over bg with its top left corner at column x, row y.
func overlay(bg, fg string, x, y int) string {
	rows := strings.Split(bg, "\n")
	for i, line := range strings.Split(fg, "\n") {
		r := y + i
		if r < 0 || r >= len(rows) {
			continue
		}
		row := rows[r]
		w := lipgloss.Width(line)
		left := ansi.Truncate(row, x, "")
		if pad := x - lipgloss.Width(left); pad > 0 {
			left += strings.Repeat(" ", pad)
		}
		rows[r] = left + line + ansi.Cut(row, x+w, lipgloss.Width(row))
	}
	return strings.Join(rows, "\n")
}

func (m Model) withPrompt(view string) string {
	if m.prompt == nil {
		return view
	}
	x, y, _, box := m.promptBox()
	return overlay(view, box, x, y)
}

// promptBox is where the prompt is on screen: its top left corner, width,
// and the box itself.
func (m Model) promptBox() (x, y, w int, box string) {
	w = min(max(m.width*2/3, 40), m.width-4)
	box = m.prompt.view(w)
	return (m.width - w) / 2, max((m.height-lipgloss.Height(box))/2-2, 1), w, box
}

// handlePromptMouse cancels the prompt on a click outside it.
func (m Model) handlePromptMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	x, y, w, box := m.promptBox()
	if msg.X < x || msg.X >= x+w || msg.Y < y || msg.Y >= y+lipgloss.Height(box) {
		m.prompt = nil
		m.setStatus("cancelled", false)
	}
	return m, nil
}
