package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mcbulazs/lazycompose/internal/discover"
	"github.com/mcbulazs/lazycompose/internal/stackfs"
)

// Creating and moving environments. Both ask for a folder in a prompt; the
// folder is checked by stackfs.Check so it stays inside the scanned roots
// and is found again by the next scan.

type moveDoneMsg struct {
	from    discover.Env
	to      discover.Env
	stopped bool
	err     error
}

func (m Model) roots() []string {
	out := make([]string, 0, len(m.opts.Roots))
	for _, r := range m.opts.Roots {
		if abs, err := filepath.Abs(r); err == nil {
			out = append(out, abs)
		}
	}
	return out
}

func (m *Model) startCreate() tea.Cmd {
	start := ""
	if e, ok := m.selectedEnv(); ok {
		start = filepath.Dir(e.Dir)
	} else if roots := m.roots(); len(roots) > 0 {
		start = roots[0]
	}
	if start != "" && !strings.HasSuffix(start, "/") {
		start += "/"
	}
	return m.openPrompt("New environment", "folder to create, enter to create and edit, esc to cancel", start, (*Model).createEnv)
}

func (m *Model) createEnv(path string) (tea.Cmd, error) {
	t, err := stackfs.Check(path, m.roots(), m.opts.MaxDepth, m.opts.Envs, "")
	if err != nil {
		return nil, err
	}
	compose := []byte(stackfs.Starter)
	if m.opts.NewTemplate != "" {
		if compose, err = os.ReadFile(m.opts.NewTemplate); err != nil {
			return nil, fmt.Errorf("new_env_template: %w", err)
		}
	}
	name := m.opts.NewComposeFilename
	if name == "" {
		name = stackfs.ComposeName
	}
	file, err := stackfs.Create(t, name, compose, m.opts.NewDotenv)
	if err != nil {
		return nil, err
	}
	e := t.Env(filepath.Base(file))
	envs := append(append([]discover.Env(nil), m.opts.Envs...), e)
	cmd := m.setEnvs(envs, e.Dir)
	m.recordUse(e)
	m.setStatus("created "+e.Rel, false)
	return tea.Batch(cmd, openEditor(m.opts.Editor, e, e.File)), nil
}

func (m *Model) startMove() tea.Cmd {
	e, ok := m.selectedEnv()
	if !ok {
		m.setStatus("no environment selected", true)
		return nil
	}
	return m.openPrompt("Move or rename "+e.Rel, "new folder, enter to move, esc to cancel", e.Dir, func(m *Model, path string) (tea.Cmd, error) {
		return m.moveEnv(e, path)
	})
}

func (m *Model) moveEnv(e discover.Env, path string) (tea.Cmd, error) {
	t, err := stackfs.Check(path, m.roots(), m.opts.MaxDepth, m.opts.Envs, e.Dir)
	if err != nil {
		return nil, err
	}
	to := t.Env(filepath.Base(e.File))
	warnings := stackfs.Warnings(e.File, e.Dir, t)
	// containers of the old project would be left behind, so stop them first
	stop := false
	if st := m.states[e.Dir]; st != nil && st.Total > 0 {
		stop = true
	}
	if !stop && len(warnings) == 0 {
		return m.doMove(e, to, false), nil
	}

	m.stopLogs()
	m.holdOutput = true
	m.logTitle = "Move"
	m.logLines = nil
	m.search = logSearch{}
	m.logs.SetContent("")
	m.appendLog(textStyle.Render(e.Dir + " → " + to.Dir))
	if stop {
		m.appendLog(yellowStyle.Render("• it has containers, so it is stopped with docker compose down first; start it again with u"))
	}
	for _, w := range warnings {
		m.appendLog(yellowStyle.Render("• " + w))
	}
	verb := "Move"
	if stop {
		verb = "Stop and move"
	}
	m.confirm = &confirm{
		prompt: fmt.Sprintf("%s %s to %s? (y/n)", verb, e.Rel, to.Rel),
		onYes:  func(m *Model) tea.Cmd { return m.doMove(e, to, stop) },
	}
	return nil, nil
}

func (m *Model) doMove(from, to discover.Env, stop bool) tea.Cmd {
	busy := m.startBusy("moving " + from.Rel)
	return tea.Batch(busy, func() tea.Msg {
		if stop {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if _, err := project(from).Run(ctx, "down"); err != nil {
				return moveDoneMsg{from: from, to: to, err: fmt.Errorf("docker compose down: %w", err)}
			}
		}
		t := stackfs.Target{Root: to.Root, Dir: to.Dir, Rel: to.Rel}
		return moveDoneMsg{from: from, to: to, stopped: stop, err: stackfs.Move(from.Dir, t)}
	})
}

func (m *Model) moveDone(msg moveDoneMsg) tea.Cmd {
	m.busy = max(0, m.busy-1)
	if msg.err != nil {
		m.setStatus("move failed: "+firstLine(msg.err.Error()), true)
		m.appendLog(redStyle.Render("move failed: " + msg.err.Error()))
		return m.refreshNow()
	}
	if h := m.opts.History; h != nil {
		h.Move(msg.from.Dir, msg.to.Dir)
		if err := h.Save(); err != nil {
			m.setStatus("saving history: "+err.Error(), true)
		}
	}
	envs := make([]discover.Env, 0, len(m.opts.Envs))
	for _, e := range m.opts.Envs {
		if e.Dir == msg.from.Dir {
			e = msg.to
		}
		envs = append(envs, e)
	}
	status := "moved " + msg.from.Rel + " to " + msg.to.Rel
	if msg.stopped {
		status += ", press u to start it again"
	}
	if m.holdOutput {
		m.appendLog(greenStyle.Render(status))
		m.appendLog(dimStyle.Render("-- press any key to go back to the logs --"))
	}
	cmd := m.setEnvs(envs, msg.to.Dir)
	m.setStatus(status, false)
	return tea.Batch(cmd, m.refreshNow())
}

// setEnvs replaces the environment list and selects dir.
func (m *Model) setEnvs(envs []discover.Env, dir string) tea.Cmd {
	sort.SliceStable(envs, func(i, j int) bool {
		if envs[i].Root != envs[j].Root {
			return envs[i].Root < envs[j].Root
		}
		return envs[i].Rel < envs[j].Rel
	})
	m.opts.Envs = envs
	q := m.list.query
	m.list = newEnvList(envs, len(m.opts.Roots) > 1)
	m.updateQuick()
	m.list.setFilter("")
	for i, idx := range m.list.visible {
		if it := m.list.items[idx]; it.env.Dir == dir && !it.quick {
			m.list.cursor = i
		}
	}
	m.list.setFilter(q)
	if m.problemsOnly {
		m.applyProblemFilter()
	}
	return tea.Batch(loadServices(envs), loadStates(), m.onSelect())
}
