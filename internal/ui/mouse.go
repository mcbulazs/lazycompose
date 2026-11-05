package ui

import tea "github.com/charmbracelet/bubbletea"

// paneAt reports which pane is under the given screen cell, and the row
// inside that pane counted from its top border.
func (m Model) paneAt(x, y int) (focus, int, bool) {
	body := m.bodyHeight()
	if y < 1 || y > body {
		return 0, 0, false
	}
	row := y - 1
	if m.zoomed {
		return m.focus, row, true
	}
	if x < m.leftWidth() {
		return focusEnvs, row, true
	}
	if sh := m.servicesHeight(); row < sh {
		return focusServices, row, true
	} else {
		return focusLogs, row - sh, true
	}
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if next, cmd, ok := m.handleBarClick(msg); ok {
		return next, cmd
	}
	if m.confirm != nil || m.filtering {
		return m, nil
	}
	pane, row, ok := m.paneAt(msg.X, msg.Y)
	if !ok || msg.Action != tea.MouseActionPress {
		return m, nil
	}

	switch msg.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		n := 1
		if msg.Button == tea.MouseButtonWheelUp {
			n = -1
		}
		switch pane {
		case focusEnvs:
			m.list.move(n)
			return m, m.onSelect()
		case focusServices:
			if n < 0 {
				m.table.MoveUp(1)
			} else {
				m.table.MoveDown(1)
			}
		case focusLogs:
			if n < 0 {
				m.logs.ScrollUp(3)
			} else {
				m.logs.ScrollDown(3)
			}
		}
		return m, nil

	case tea.MouseButtonLeft:
		m.focus = pane
		m.setTableStyles()
		switch pane {
		case focusEnvs:
			// the top border holds the title, then the filter line if shown
			first := 1
			if m.list.query != "" {
				first++
			}
			lines := m.list.lines()
			if i := row - first + m.list.offset; row >= first && i < len(lines) && lines[i].pos >= 0 {
				m.list.cursor = lines[i].pos
				return m, m.onSelect()
			}
		case focusServices:
			// the top border and the column header, then the rows; only
			// when all rows fit, since the table does not expose its scroll
			rows := len(m.table.Rows())
			if i := row - 2; i >= 0 && i < rows && rows < m.table.Height() {
				m.table.SetCursor(i)
			}
		}
		return m, nil
	}
	return m, nil
}
