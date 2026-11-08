package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mcbulazs/lazycompose/internal/discover"
)

// updateRun runs an action on environments one at a time: an update (pull,
// then up -d) for update all, or one docker compose command for the
// stacks under a heading.
type updateRun struct {
	queue    []discover.Env
	next     int
	stopping bool
	// args is the docker compose command; nil is an update
	args                     []string
	verb                     string // how the command is called in messages, like "up -d"
	updated, current, failed int
}

func (m *Model) startUpdateAll() tea.Cmd {
	var envs []discover.Env
	for _, it := range m.list.items {
		if !it.quick && !it.offline {
			envs = append(envs, it.env)
		}
	}
	return m.startUpdate(envs, "Update all", "environments")
}

// startUpdate asks to update the running ones of envs, then does.
func (m *Model) startUpdate(envs []discover.Env, title, what string) tea.Cmd {
	if m.updating != nil {
		m.setStatus("update already running", true)
		return nil
	}
	var queue []discover.Env
	for _, e := range envs {
		if running, _, _ := m.counts(e); running > 0 {
			queue = append(queue, e)
		}
	}
	if len(queue) == 0 {
		m.setStatus("nothing running to update", false)
		return nil
	}
	return m.ask(&confirm{
		title:  "Update",
		prompt: fmt.Sprintf("Update %d running %s (pull + up -d)? (y/n)", len(queue), what),
		onYes: func(m *Model) tea.Cmd {
			return m.runQueue(&updateRun{queue: queue, verb: "updating"}, title)
		},
	})
}

// headingActions are the actions that run on every stack under a heading:
// the docker compose command, and whether stacks without containers are
// left out, since the command would do nothing there.
var headingActions = []struct {
	name       string // the action's name, as in the config
	key        *key.Binding
	args       []string
	verb       string
	containers bool
}{
	{"up", &keys.Start, upArgs, "up -d", false},
	{"pull", &keys.Pull, pullArgs, "pull", false},
	{"down", &keys.Stop, downArgs, "down", true},
	{"restart", &keys.Restart, restartArgs, "restart", true},
	{"recreate", &keys.Recreate, recreateArgs, "up -d --force-recreate", true},
}

// headingCommand is what the action name does on the heading r, for the
// menu, or false when it does nothing there.
func (m Model) headingCommand(name string, r listRow) (string, bool) {
	label := r.label + "/"
	if r.kind == rowRoot {
		label = r.label
	}
	if name == "update_all" {
		return "pull + up -d (each running stack in " + label + ")", true
	}
	for _, a := range headingActions {
		if a.name == name {
			return "docker compose " + a.verb + " (each stack in " + label + ")", true
		}
	}
	return "", false
}

// headingAction runs the action of msg on the stacks under the selected
// heading, after asking. It reports false for keys that are not such an
// action.
func (m *Model) headingAction(msg tea.KeyMsg, r listRow) (tea.Cmd, bool) {
	envs := m.list.rowEnvs(r)
	label := r.label + "/"
	if r.kind == rowRoot {
		label = r.label
	}
	if key.Matches(msg, keys.UpdateAll) {
		return m.startUpdate(envs, "Update · "+label, "stacks in "+label), true
	}
	for _, a := range headingActions {
		if !key.Matches(msg, *a.key) {
			continue
		}
		if m.updating != nil {
			m.setStatus("an action on several stacks is already running", true)
			return nil, true
		}
		var queue []discover.Env
		for _, e := range envs {
			if st := m.states[e.Dir]; !a.containers || st != nil && st.Total > 0 {
				queue = append(queue, e)
			}
		}
		if len(queue) == 0 {
			m.setStatus("no stack in "+label+" has containers", false)
			return nil, true
		}
		run := &updateRun{queue: queue, args: a.args, verb: a.verb}
		stacks := "stacks"
		if len(queue) == 1 {
			stacks = "stack"
		}
		return m.ask(&confirm{
			title:  "docker compose " + a.verb,
			prompt: fmt.Sprintf("Run docker compose %s on %d %s in %s? (y/n)", a.verb, len(queue), stacks, label),
			onYes:  func(m *Model) tea.Cmd { return m.runQueue(run, a.verb+" · "+label) },
		}), true
	}
	return nil, false
}

// runQueue starts u, showing its progress in the log pane.
func (m *Model) runQueue(u *updateRun, title string) tea.Cmd {
	m.updating = u
	m.holdOutput = true
	m.stopLogs()
	m.logTitle = title
	m.logLines = nil
	m.logs.SetContent("")
	what := "updating"
	if u.args != nil {
		what = "running docker compose " + u.verb + " on"
	}
	m.appendLog(dimStyle.Render(fmt.Sprintf("%s %d environments, esc to stop after the current one", what, len(u.queue))))
	return tea.Batch(m.startBusy(m.updateStatus()), m.runNext())
}

// runNext runs u's action on its next environment.
func (m *Model) runNext() tea.Cmd {
	u := m.updating
	e := u.queue[u.next]
	if u.args == nil {
		return updateEnv(e)
	}
	args := u.args
	return func() tea.Msg {
		_, err := project(e).Run(context.Background(), args...)
		return updateMsg{env: e, err: err}
	}
}

func (m *Model) updateStatus() string {
	u := m.updating
	return fmt.Sprintf("%s %d/%d: %s", u.verb, u.next+1, len(u.queue), u.queue[u.next].Rel)
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
	case u.args != nil:
		u.updated++
		m.appendLog(greenStyle.Render("done        ") + msg.env.Rel)
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
		return tea.Batch(m.runNext(), m.refreshNow())
	}

	m.busy = max(0, m.busy-1)
	summary := fmt.Sprintf("update done: %d updated, %d up to date, %d failed", u.updated, u.current, u.failed)
	if u.args != nil {
		summary = fmt.Sprintf("%s done: %d done, %d failed", u.verb, u.updated, u.failed)
	}
	if skipped := len(u.queue) - u.next; skipped > 0 {
		summary += fmt.Sprintf(", %d skipped", skipped)
	}
	m.appendLog(dimStyle.Render(summary))
	m.appendLog(dimStyle.Render("-- press any key to go back to the logs --"))
	m.setStatus(summary, u.failed > 0)
	m.updating = nil
	if u.args != nil && u.verb != "pull" {
		return m.refreshNow() // no new images that leave old ones behind
	}
	return tea.Batch(m.refreshNow(), m.recheckUpdates(), findOldImages(true))
}

// lastLine returns the last non-empty line; docker puts the actual error last.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
