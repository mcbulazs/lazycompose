package ui

import (
	"regexp"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mcbulazs/lazycompose/internal/discover"
)

// The log pane follows the selection: the selected environment, or the
// selected service while the services pane has focus. Commands show their
// output there instead, which stays until the next key press.

// logSettle is how long the selection has to rest before logs are fetched,
// so scrolling through the list does not start a process per row.
const logSettle = 250 * time.Millisecond

type logTarget struct {
	dir, service string
}

type autoLogsMsg struct{ id int }

type actionRun struct {
	label  string
	stream *logStream
}

// wantedLogs is what the log pane should follow right now.
func (m Model) wantedLogs() (discover.Env, string, bool) {
	e, ok := m.selectedEnv()
	if !ok {
		return e, "", false
	}
	switch m.focus {
	case focusServices:
		return e, m.selectedService(), true
	case focusLogs:
		// reading the logs should not switch them
		if m.logTarget.dir == e.Dir {
			return e, m.logTarget.service, true
		}
	}
	return e, "", true
}

// scheduleLogs fetches logs for the selection once it has settled.
func (m *Model) scheduleLogs() tea.Cmd {
	if m.holdOutput || m.width == 0 {
		return nil
	}
	e, svc, ok := m.wantedLogs()
	if !ok {
		return nil
	}
	want := logTarget{dir: e.Dir, service: svc}
	if want == m.logTarget || want == m.pendingLogs {
		return nil
	}
	m.pendingLogs = want
	m.autoID++
	id := m.autoID
	return tea.Tick(logSettle, func(time.Time) tea.Msg { return autoLogsMsg{id: id} })
}

func (m *Model) followLogs(e discover.Env, svc string) tea.Cmd {
	m.stopLogs()
	m.streamID++
	m.logLines = nil
	m.search = logSearch{}
	m.logs.SetContent("")
	m.logTitle = "Logs · " + e.Rel
	if svc != "" {
		m.logTitle += " · " + svc
	}
	m.logTarget = logTarget{dir: e.Dir, service: svc}
	m.pendingLogs = logTarget{}
	s, cmd := startLogs(m.streamID, e, svc)
	m.stream = s
	return cmd
}

// runAction runs a docker compose command with its output streamed into the
// log pane.
func (m *Model) runAction(e discover.Env, busy, label string, args ...string) tea.Cmd {
	spin := m.startBusy(busy)
	m.stopLogs()
	if !m.holdOutput {
		m.logLines = nil
		m.search = logSearch{}
		m.logs.SetContent("")
	}
	m.holdOutput = true
	m.logTitle = "Output"
	m.outputKeys = nil
	m.appendLog(dimStyle.Render("$ " + label))

	m.streamID++
	s, cmd := startAction(m.streamID, e, args...)
	if m.actions == nil {
		m.actions = map[int]*actionRun{}
	}
	m.actions[m.streamID] = &actionRun{label: label, stream: s}
	return tea.Batch(spin, cmd)
}

func (m *Model) actionDone(id int, a *actionRun, err error) tea.Cmd {
	delete(m.actions, id)
	m.busy = max(0, m.busy-1)
	if err != nil {
		m.setStatus(a.label+" failed: "+firstLine(err.Error()), true)
		m.appendLog(redStyle.Render(a.label + " failed: " + err.Error()))
	} else {
		m.setStatus(a.label+" done", false)
	}
	if len(m.actions) == 0 && m.updating == nil {
		m.appendLog(dimStyle.Render("-- press any key to go back to the logs --"))
	}
	return tea.Batch(m.refreshNow(), m.recheckUpdates())
}

// releaseOutput goes back to following logs on a key press, once nothing is
// running any more. Switching panes and scrolling the log pane keep it.
func (m *Model) releaseOutput(msg tea.KeyMsg) {
	if !m.holdOutput || len(m.actions) > 0 || m.updating != nil || m.confirm != nil {
		return
	}
	if key.Matches(msg, keys.NextPane, keys.PrevPane, keys.FocusEnvs, keys.FocusServices, keys.FocusLogs) {
		return
	}
	if m.focus == focusLogs {
		if key.Matches(msg, keys.MoveUp, keys.MoveDown, keys.Filter) {
			return
		}
		switch msg.String() {
		case "pgup", "pgdown", "g", "G", "n", "N":
			return
		}
	}
	m.holdOutput = false
	m.logTarget = logTarget{}
	m.pendingLogs = logTarget{}
}

// Docker redraws progress in place on a terminal, but into a pipe it writes
// every update as a new line. Lines about the same layer or object replace
// the previous one, so a pull shows one growing bar per layer.
var progressKey = regexp.MustCompile(`^\s*(?:([0-9a-f]{12})\s|((?:Container|Image|Network|Volume)\s+\S+)\s)`)

func outputKey(line string) string {
	m := progressKey.FindStringSubmatch(line)
	if m == nil {
		return ""
	}
	return m[1] + m[2]
}

func (m *Model) appendOutput(lines ...string) {
	if m.outputKeys == nil {
		m.outputKeys = map[string]int{}
	}
	atBottom := m.logs.AtBottom() || len(m.logLines) == 0
	for _, l := range lines {
		k := outputKey(l)
		if i, ok := m.outputKeys[k]; ok && i < len(m.logLines) && outputKey(m.logLines[i]) == k {
			m.logLines[i] = l
			continue
		}
		if k != "" {
			m.outputKeys[k] = len(m.logLines)
		}
		m.logLines = append(m.logLines, l)
	}
	if over := len(m.logLines) - maxLogLines; over > 0 {
		m.logLines = m.logLines[over:]
		m.outputKeys = map[string]int{} // positions moved
	}
	m.renderLogs()
	if atBottom {
		m.logs.GotoBottom()
	}
}
