package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mcbulazs/lazycompose/internal/discover"
	"github.com/mcbulazs/lazycompose/internal/remote"
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

// roots are the local roots, as absolute paths.
func (m Model) roots() []string {
	out := make([]string, 0, len(m.opts.Roots))
	for _, r := range m.opts.Roots {
		if remote.IsRemote(r) {
			continue
		}
		if abs, err := filepath.Abs(r); err == nil {
			out = append(out, abs)
		}
	}
	return out
}

const (
	createTitle = "New environment"
	createHint  = "folder to create, enter to create and edit, esc to cancel"
)

func (m *Model) startCreate() tea.Cmd {
	start := ""
	e, ok := m.selectedEnv()
	switch {
	case ok && e.Remote():
		start = e.Host.Key(path.Dir(e.Path))
	case ok:
		start = filepath.Dir(e.Dir)
	case len(m.roots()) > 0:
		start = m.roots()[0]
	case len(m.opts.Roots) > 0:
		start = m.opts.Roots[0]
	}
	sep := string(filepath.Separator)
	if remote.IsRemote(start) {
		sep = "/"
	}
	if start != "" && !strings.HasSuffix(start, sep) {
		start += sep
	}
	return m.openPrompt(createTitle, createHint, start, (*Model).createEnv)
}

// newCompose is the compose file of a new environment: the template, or
// the starter.
func (m *Model) newCompose() (name string, compose []byte, err error) {
	compose = []byte(stackfs.Starter)
	if m.opts.NewTemplate != "" {
		if compose, err = os.ReadFile(m.opts.NewTemplate); err != nil {
			return "", nil, fmt.Errorf("new_env_template: %w", err)
		}
	}
	name = m.opts.NewComposeFilename
	if name == "" {
		name = stackfs.ComposeName
	}
	return name, compose, nil
}

func (m *Model) createEnv(path string) (tea.Cmd, error) {
	if remote.IsRemote(path) {
		name, compose, err := m.newCompose()
		if err != nil {
			return nil, err
		}
		return tea.Batch(m.startBusy("checking "+path), createRemote(path, m.opts.Roots, m.opts.MaxDepth, m.opts.Envs, name, compose, m.opts.NewDotenv)), nil
	}
	t, err := stackfs.Check(path, m.roots(), m.opts.MaxDepth, m.opts.Envs, "")
	if err != nil {
		return nil, err
	}
	name, compose, err := m.newCompose()
	if err != nil {
		return nil, err
	}
	file, err := stackfs.Create(t, name, compose, m.opts.NewDotenv)
	if err != nil {
		return nil, err
	}
	return m.created(t.Env(filepath.Base(file))), nil
}

// created adds the new environment e to the list and opens its compose
// file.
func (m *Model) created(e discover.Env) tea.Cmd {
	envs := append(append([]discover.Env(nil), m.opts.Envs...), e)
	cmd := m.setEnvs(envs, e.Dir)
	m.recordUse(e)
	m.setStatus("created "+e.Rel, false)
	return tea.Batch(cmd, m.startEdit(e, e.File))
}

// remoteTimeout bounds the checks and changes of a remote create or move.
const remoteTimeout = time.Minute

type remoteCreatedMsg struct {
	value   string // what was typed, to show again if the check fails
	env     discover.Env
	checked bool // the check passed; an error came from creating it
	err     error
}

// createRemote checks the folder on the host and creates the environment
// there.
func createRemote(value string, roots []string, maxDepth int, envs []discover.Env, name string, compose []byte, dotenv bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), remoteTimeout)
		defer cancel()
		t, err := stackfs.CheckRemote(ctx, value, roots, maxDepth, envs, "")
		if err != nil {
			return remoteCreatedMsg{value: value, err: err}
		}
		file, err := stackfs.CreateRemote(ctx, t, name, compose, dotenv)
		if err != nil {
			return remoteCreatedMsg{value: value, checked: true, err: err}
		}
		return remoteCreatedMsg{env: t.Env(path.Base(file))}
	}
}

func (m *Model) onRemoteCreated(msg remoteCreatedMsg) tea.Cmd {
	m.busy = max(0, m.busy-1)
	switch {
	case msg.err != nil && !msg.checked:
		// back to the prompt, as for a local folder that does not pass
		cmd := m.openPrompt(createTitle, createHint, msg.value, (*Model).createEnv)
		m.prompt.err = firstLine(msg.err.Error())
		return cmd
	case msg.err != nil:
		m.setStatus("creating "+msg.value+": "+firstLine(msg.err.Error()), true)
		return nil
	}
	return m.created(msg.env)
}

func (m *Model) startMove() tea.Cmd {
	e, ok := m.selectedEnv()
	if !ok {
		m.setStatus("no environment selected", true)
		return nil
	}
	return m.openMovePrompt(e, e.Dir)
}

func (m *Model) openMovePrompt(e discover.Env, value string) tea.Cmd {
	return m.openPrompt("Move or rename "+e.Rel, "new folder, enter to move, esc to cancel", value, func(m *Model, path string) (tea.Cmd, error) {
		return m.moveEnv(e, path)
	})
}

func (m *Model) moveEnv(e discover.Env, value string) (tea.Cmd, error) {
	if e.Remote() || remote.IsRemote(value) {
		h, _, err := remote.Parse(strings.TrimSpace(value))
		if !e.Remote() || !remote.IsRemote(strings.TrimSpace(value)) || err == nil && h.String() != e.Host.String() {
			return nil, errors.New("an environment moves within its machine; moving to another one is not supported")
		}
		if err != nil {
			return nil, err
		}
		return tea.Batch(m.startBusy("checking "+value), checkRemoteMove(e, value, m.opts.Roots, m.opts.MaxDepth, m.opts.Envs)), nil
	}
	t, err := stackfs.Check(value, m.roots(), m.opts.MaxDepth, m.opts.Envs, e.Dir)
	if err != nil {
		return nil, err
	}
	return m.confirmMove(e, t.Env(filepath.Base(e.File)), stackfs.Warnings(e.File, e.Dir, t)), nil
}

type remoteMoveCheckedMsg struct {
	from     discover.Env
	value    string
	to       discover.Env
	warnings []string
	err      error
}

// checkRemoteMove checks the new folder on the host, and reads the compose
// file there for the warnings.
func checkRemoteMove(e discover.Env, value string, roots []string, maxDepth int, envs []discover.Env) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), remoteTimeout)
		defer cancel()
		t, err := stackfs.CheckRemote(ctx, value, roots, maxDepth, envs, e.Path)
		if err != nil {
			return remoteMoveCheckedMsg{from: e, value: value, err: err}
		}
		b, _, _ := e.Host.ReadFile(ctx, e.File, maxComposeFile)
		w := stackfs.ComposeWarnings(b, path.Base(e.Path), path.Base(t.Dir), path.Dir(e.Path) == path.Dir(t.Dir))
		return remoteMoveCheckedMsg{from: e, value: value, to: t.Env(path.Base(e.File)), warnings: w}
	}
}

func (m *Model) onRemoteMoveChecked(msg remoteMoveCheckedMsg) tea.Cmd {
	m.busy = max(0, m.busy-1)
	if msg.err != nil {
		cmd := m.openMovePrompt(msg.from, msg.value)
		m.prompt.err = firstLine(msg.err.Error())
		return cmd
	}
	return m.confirmMove(msg.from, msg.to, msg.warnings)
}

// confirmMove moves e to the checked folder of to, asking first when it
// has containers to stop or the move changes something.
func (m *Model) confirmMove(e, to discover.Env, warnings []string) tea.Cmd {
	// containers of the old project would be left behind, so stop them first
	stop := false
	if st := m.states[e.Dir]; st != nil && st.Total > 0 {
		stop = true
	}
	if !stop && len(warnings) == 0 {
		return m.doMove(e, to, false)
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
	return nil
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
		if from.Remote() {
			ctx, cancel := context.WithTimeout(context.Background(), remoteTimeout)
			defer cancel()
			t := stackfs.Target{Root: to.Root, Dir: to.Path, Rel: to.Rel, Host: to.Host}
			return moveDoneMsg{from: from, to: to, stopped: stop, err: stackfs.MoveRemote(ctx, from.Path, t)}
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
	m.list.setEnvs(envs)
	m.updateQuick()
	m.list.setFilter("")
	m.list.selectDir(dir)
	m.list.setFilter(q)
	if m.problemsOnly {
		m.applyProblemFilter()
	}
	return tea.Batch(loadServices(envs), loadStates(m.hosts), m.onSelect())
}
