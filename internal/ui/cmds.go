package ui

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mcbulazs/lazycompose/internal/compose"
	"github.com/mcbulazs/lazycompose/internal/discover"
)

const refreshEvery = 3 * time.Second

type (
	tickMsg   struct{}
	statesMsg struct {
		states map[string]*compose.DirState
		err    error
	}
	servicesMsg struct{ services map[string][]string }
	psMsg       struct {
		dir        string
		containers []compose.Container
		err        error
	}
	envsMsg struct {
		envs []discover.Env
		err  error
	}
	actionMsg struct {
		label  string
		output string
		err    error
	}
	editorMsg struct {
		env    discover.Env
		before [32]byte
		err    error
	}
	validateMsg struct {
		env discover.Env
		err error
	}
	shellMsg      struct{ err error }
	shellFoundMsg struct {
		env     discover.Env
		service string
		shell   string
		err     error
	}
)

func project(e discover.Env) compose.Project {
	return compose.Project{Dir: e.Dir, File: e.File}
}

func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

func loadStates() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		st, err := compose.StateByDir(ctx)
		return statesMsg{states: st, err: err}
	}
}

func loadServices(envs []discover.Env) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		res := map[string][]string{}
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 8)
		for _, e := range envs {
			wg.Add(1)
			go func(e discover.Env) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				names, err := project(e).Services(ctx)
				if err != nil {
					return
				}
				mu.Lock()
				res[e.Dir] = names
				mu.Unlock()
			}(e)
		}
		wg.Wait()
		return servicesMsg{services: res}
	}
}

func loadPS(e discover.Env) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cs, err := project(e).PS(ctx)
		return psMsg{dir: e.Dir, containers: cs, err: err}
	}
}

func rediscover(find func() ([]discover.Env, error)) tea.Cmd {
	return func() tea.Msg {
		envs, err := find()
		return envsMsg{envs: envs, err: err}
	}
}

func runAction(e discover.Env, label string, args ...string) tea.Cmd {
	return func() tea.Msg {
		out, err := project(e).Run(context.Background(), args...)
		return actionMsg{label: label, output: out, err: err}
	}
}

func validate(e discover.Env) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return validateMsg{env: e, err: project(e).Validate(ctx)}
	}
}

func hashFile(path string) [32]byte {
	b, err := os.ReadFile(path)
	if err != nil {
		return [32]byte{}
	}
	return sha256.Sum256(b)
}

func editorCommand(file string) (*exec.Cmd, error) {
	if parts := strings.Fields(os.Getenv("EDITOR")); len(parts) > 0 {
		return exec.Command(parts[0], append(parts[1:], file)...), nil
	}
	for _, name := range []string{"nvim", "vi"} {
		if p, err := exec.LookPath(name); err == nil {
			return exec.Command(p, file), nil
		}
	}
	return nil, errors.New("no editor found, set $EDITOR")
}

func openEditor(e discover.Env) tea.Cmd {
	cmd, err := editorCommand(e.File)
	if err != nil {
		return func() tea.Msg { return editorMsg{env: e, err: err} }
	}
	before := hashFile(e.File)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorMsg{env: e, before: before, err: err}
	})
}

// shells are tried in this order
var shells = []string{"bash", "sh"}

// findShell checks which shell the container has before handing over the
// terminal, so a missing shell gives a message instead of a broken screen.
func findShell(e discover.Env, service string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var lastErr error
		for _, sh := range shells {
			_, err := project(e).Run(ctx, "exec", "-T", service, sh, "-c", "exit 0")
			if err == nil {
				return shellFoundMsg{env: e, service: service, shell: sh}
			}
			lastErr = err
		}
		return shellFoundMsg{err: fmt.Errorf("no shell in %s (tried %s): %s",
			service, strings.Join(shells, ", "), firstLine(lastErr.Error()))}
	}
}

func openShell(e discover.Env, service, shell string) tea.Cmd {
	cmd := project(e).Command(context.Background(), "exec", service, shell)
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return shellMsg{err: err} })
}

// log streaming

type (
	logLinesMsg struct {
		id    int
		lines []string
	}
	logDoneMsg struct {
		id  int
		err error
	}
)

type logStream struct {
	id     int
	cancel context.CancelFunc
	lines  chan string
	err    error
}

func startLogs(id int, e discover.Env, service string) (*logStream, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	args := []string{"logs", "-f", "--tail", "200"}
	if service != "" {
		args = append(args, service)
	}
	cmd := project(e).Command(ctx, args...)

	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	s := &logStream{id: id, cancel: cancel, lines: make(chan string, 512)}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, func() tea.Msg { return logDoneMsg{id: id, err: err} }
	}

	go func() {
		err := cmd.Wait()
		pw.CloseWithError(err)
	}()
	go func() {
		defer close(s.lines)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			s.lines <- sc.Text()
		}
		if err := sc.Err(); err != nil && ctx.Err() == nil {
			s.err = err
		}
	}()
	return s, s.next()
}

// next waits for at least one line, then drains whatever else is ready so
// busy containers do not cause one render per line.
func (s *logStream) next() tea.Cmd {
	return func() tea.Msg {
		line, ok := <-s.lines
		if !ok {
			return logDoneMsg{id: s.id, err: s.err}
		}
		batch := []string{line}
		for len(batch) < 500 {
			select {
			case l, ok := <-s.lines:
				if !ok {
					return logLinesMsg{id: s.id, lines: batch}
				}
				batch = append(batch, l)
			default:
				return logLinesMsg{id: s.id, lines: batch}
			}
		}
		return logLinesMsg{id: s.id, lines: batch}
	}
}

func (s *logStream) stop() {
	if s != nil {
		s.cancel()
	}
}
