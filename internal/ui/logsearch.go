package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// logSearch finds text in the log pane. Matches are log line indices.
type logSearch struct {
	query   string
	matches []int
	current int // index into matches
}

func (s logSearch) active() bool { return s.query != "" }

func (m *Model) findMatches() {
	s := &m.search
	s.matches = s.matches[:0]
	if !s.active() {
		return
	}
	q := strings.ToLower(s.query)
	for i, l := range m.logLines {
		if strings.Contains(strings.ToLower(ansi.Strip(l)), q) {
			s.matches = append(s.matches, i)
		}
	}
	s.current = min(s.current, max(len(s.matches)-1, 0))
}

// highlight marks every match in a line. The line loses its own colours,
// which only matters for the few lines that match.
func highlight(line, query string, current bool) string {
	style := lipgloss.NewStyle().Background(colYellowBg).Foreground(colMatchFg)
	if current {
		style = lipgloss.NewStyle().Background(colFocus).Foreground(colMatchFg).Bold(true)
	}
	plain := ansi.Strip(line)
	lower, q := strings.ToLower(plain), strings.ToLower(query)
	var b strings.Builder
	for {
		i := strings.Index(lower, q)
		if i < 0 {
			b.WriteString(plain)
			return b.String()
		}
		b.WriteString(plain[:i])
		b.WriteString(style.Render(plain[i : i+len(q)]))
		plain, lower = plain[i+len(q):], lower[i+len(q):]
	}
}

func (m *Model) startLogSearch() tea.Cmd {
	m.searching = true
	m.searchInput.SetValue(m.search.query)
	m.searchInput.CursorEnd()
	return m.searchInput.Focus()
}

func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.searching = false
		m.searchInput.Blur()
		m.search = logSearch{query: m.searchInput.Value()}
		m.findMatches()
		m.search.current = max(len(m.search.matches)-1, 0)
		m.renderLogs()
		m.jumpToMatch()
		return m, nil
	case "esc":
		m.searching = false
		m.searchInput.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	return m, cmd
}

// moveMatch goes to an older (-1) or newer (+1) match.
func (m *Model) moveMatch(dir int) {
	s := &m.search
	if len(s.matches) == 0 {
		return
	}
	s.current = (s.current + dir + len(s.matches)) % len(s.matches)
	m.renderLogs()
	m.jumpToMatch()
}

func (m *Model) jumpToMatch() {
	s := m.search
	if len(s.matches) == 0 {
		if s.active() {
			m.setStatus("no matches for "+s.query, true)
		}
		return
	}
	row := m.lineStart[s.matches[s.current]]
	m.logs.SetYOffset(max(row-m.logs.Height/2, 0))
	m.setStatus(fmt.Sprintf("match %d/%d", s.current+1, len(s.matches)), false)
}

func (m *Model) clearSearch() {
	m.search = logSearch{}
	m.renderLogs()
	m.setStatus("", false)
}
