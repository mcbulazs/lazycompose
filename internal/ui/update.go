package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mcbulazs/lazycompose/internal/discover"
)

// updateRun updates environments one at a time.
type updateRun struct {
	queue                    []discover.Env
	next                     int
	stopping                 bool
	updated, current, failed int
}

func (m *Model) startUpdateAll() tea.Cmd {
	if m.updating != nil {
		m.setStatus("update already running", true)
		return nil
	}
	var queue []discover.Env
	for _, it := range m.list.items {
		if it.quick {
			continue
		}
		if running, _, _ := m.counts(it.env); running > 0 {
			queue = append(queue, it.env)
		}
	}
	if len(queue) == 0 {
		m.setStatus("nothing running to update", false)
		return nil
	}
	m.confirm = &confirm{
		prompt: fmt.Sprintf("Update %d running environments (pull + up -d)? (y/n)", len(queue)),
		onYes: func(m *Model) tea.Cmd {
			m.updating = &updateRun{queue: queue}
			m.holdOutput = true
			m.stopLogs()
			m.logTitle = "Update all"
			m.logLines = nil
			m.logs.SetContent("")
			m.appendLog(dimStyle.Render(fmt.Sprintf("updating %d environments, esc to stop after the current one", len(queue))))
			return tea.Batch(m.startBusy(m.updateStatus()), updateEnv(queue[0]))
		},
	}
	return nil
}

func (m *Model) updateStatus() string {
	u := m.updating
	return fmt.Sprintf("updating %d/%d: %s", u.next+1, len(u.queue), u.queue[u.next].Rel)
}

func (m *Model) onUpdate(msg updateMsg) tea.Cmd {
	u := m.updating
	if u == nil {
		return nil
	}
	switch {
	case msg.err != nil:
		u.failed++
		m.appendLog(redStyle.Render("failed      ") + msg.env.Rel + dimStyle.Render("  "+lastLine(msg.err.Error())))
	case msg.recreated > 0:
		u.updated++
		m.appendLog(greenStyle.Render("updated     ") + msg.env.Rel + dimStyle.Render(fmt.Sprintf("  %d recreated", msg.recreated)))
	default:
		u.current++
		m.appendLog(dimStyle.Render("up to date  ") + msg.env.Rel)
	}

	u.next++
	if u.next < len(u.queue) && !u.stopping {
		m.setStatus(m.updateStatus(), false)
		return tea.Batch(updateEnv(u.queue[u.next]), m.refreshNow())
	}

	m.busy = max(0, m.busy-1)
	summary := fmt.Sprintf("update done: %d updated, %d up to date, %d failed", u.updated, u.current, u.failed)
	if skipped := len(u.queue) - u.next; skipped > 0 {
		summary += fmt.Sprintf(", %d skipped", skipped)
	}
	m.appendLog(dimStyle.Render(summary))
	m.appendLog(dimStyle.Render("-- press any key to go back to the logs --"))
	m.setStatus(summary, u.failed > 0)
	m.updating = nil
	return tea.Batch(m.refreshNow(), m.recheckUpdates(), findOldImages(true))
}

// lastLine returns the last non-empty line; docker puts the actual error last.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
