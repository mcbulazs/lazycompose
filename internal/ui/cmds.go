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
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mcbulazs/lazycompose/internal/compose"
	"github.com/mcbulazs/lazycompose/internal/discover"
	"github.com/mcbulazs/lazycompose/internal/remote"
)

type (
	tickMsg   struct{}
	statesMsg struct {
		states map[string]*compose.DirState
		// errs are the hosts that could not be read, by hostKey
		errs map[string]error
	}
	servicesMsg struct {
		services map[string][]string
		// err is a host refusing sessions; other errors, like a broken
		// compose file, show when the environment is selected
		err error
	}
	psMsg struct {
		dir        string
		containers []compose.Container
		err        error
	}
	envsMsg struct {
		envs  []discover.Env
		roots []string // the roots that were scanned
		err   error
	}
	configEditedMsg struct {
		changed bool
		quick   bool // the editor returned at once, its window has the file
		err     error
	}
	logsEditedMsg struct {
		file string // left for the editor window when it returned quickly
		err  error
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
	return compose.Project{Dir: e.Folder(), File: e.File, Host: e.Host}
}

// hostsOf lists the machines the roots are on: nil for this one, when a
// root is a local folder or there are no roots, then each remote host once.
func hostsOf(roots []string) []*remote.Host {
	var hosts []*remote.Host
	local := len(roots) == 0
	seen := map[string]bool{}
	for _, r := range roots {
		if !remote.IsRemote(r) {
			local = true
			continue
		}
		h, _, err := remote.Parse(r)
		if err != nil || seen[h.String()] {
			continue
		}
		seen[h.String()] = true
		hosts = append(hosts, h)
	}
	if local {
		hosts = append([]*remote.Host{nil}, hosts...)
	}
	return hosts
}

func tick(every time.Duration) tea.Cmd {
	return tea.Tick(every, func(time.Time) tea.Msg { return tickMsg{} })
}

// loadStates reads the containers of every host at the same time.
func loadStates(hosts []*remote.Host) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		msg := statesMsg{states: map[string]*compose.DirState{}, errs: map[string]error{}}
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, h := range hosts {
			wg.Add(1)
			go func() {
				defer wg.Done()
				st, err := compose.StateByDir(ctx, h)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					if h != nil && !strings.HasPrefix(err.Error(), h.Label()+": ") {
						err = fmt.Errorf("%s: %w", h.Label(), err)
					}
					msg.errs[hostKey(h)] = err
					return
				}
				for dir, s := range st {
					msg.states[dir] = s
				}
			}()
		}
		wg.Wait()
		return msg
	}
}

func loadServices(envs []discover.Env) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		res := map[string][]string{}
		var refused error
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
					if errors.Is(err, remote.ErrSessionRefused) {
						mu.Lock()
						refused = err
						mu.Unlock()
					}
					return
				}
				mu.Lock()
				res[e.Dir] = names
				mu.Unlock()
			}(e)
		}
		wg.Wait()
		return servicesMsg{services: res, err: refused}
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

// rediscover scans roots; the environments of other roots stay.
func rediscover(find func(roots []string) ([]discover.Env, error), roots []string) tea.Cmd {
	return func() tea.Msg {
		envs, err := find(roots)
		return envsMsg{envs: envs, roots: roots, err: err}
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

// editorCommand uses the configured editor, then $VISUAL, then $EDITOR,
// then the first of nvim or vi (or notepad on Windows) that is installed.
// The editor may include arguments, like "code --wait".
func editorCommand(editor, file string) (*exec.Cmd, error) {
	for _, e := range []string{editor, os.Getenv("VISUAL"), os.Getenv("EDITOR")} {
		if parts := strings.Fields(e); len(parts) > 0 {
			return exec.Command(parts[0], append(parts[1:], file)...), nil
		}
	}
	editors := []string{"nvim", "vi"}
	if runtime.GOOS == "windows" {
		editors = append(editors, "notepad")
	}
	for _, name := range editors {
		if p, err := exec.LookPath(name); err == nil {
			return exec.Command(p, file), nil
		}
	}
	return nil, errors.New("no editor found, set editor in the config or $EDITOR")
}

// openConfigEditor edits the lazycompose config file.
func openConfigEditor(editor, file string) tea.Cmd {
	cmd, err := editorCommand(editor, file)
	if err != nil {
		return func() tea.Msg { return configEditedMsg{err: err} }
	}
	before, start := hashFile(file), time.Now()
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return configEditedMsg{changed: hashFile(file) != before, quick: time.Since(start) < quickEditor, err: err}
	})
}

// logsFilePrefix starts the temporary files the log pane is written to;
// the process id follows. logsFilePattern is how they are shown.
const (
	logsFilePrefix  = "lazycompose-logs-"
	logsFilePattern = logsFilePrefix + "*.log"
)

// openLogsEditor writes the log pane's lines, without colours, to a
// temporary file only the user can read and opens it in the editor, where
// the text can be searched and copied. The file is removed when the editor
// exits; an editor that returns at once has a window that still shows it,
// so then it is removed when lazycompose quits.
func openLogsEditor(editor string, lines []string) tea.Cmd {
	name, err := writeLogsFile(lines)
	var cmd *exec.Cmd
	if err == nil {
		cmd, err = editorCommand(editor, name)
		if err != nil {
			_ = os.Remove(name)
		}
	}
	if err != nil {
		return func() tea.Msg { return logsEditedMsg{err: err} }
	}
	start := time.Now()
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err == nil && time.Since(start) < quickEditor {
			return logsEditedMsg{file: name}
		}
		_ = os.Remove(name)
		return logsEditedMsg{err: err}
	})
}

// writeLogsFile writes lines without their colours to a new temporary file
// and returns its name.
func writeLogsFile(lines []string) (string, error) {
	f, err := os.CreateTemp("", fmt.Sprintf("%s%d-*.log", logsFilePrefix, os.Getpid()))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(ansi.Strip(l))
		b.WriteByte('\n')
	}
	_, err = f.WriteString(b.String())
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// envFile is the file docker compose reads variables from by default.
func envFile(e discover.Env) string {
	if e.Remote() {
		return path.Join(e.Path, ".env") // a path on the host
	}
	return filepath.Join(e.Dir, ".env")
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
	cmd := project(e).Terminal("exec", service, shell)
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
	args := slices.Clone(logArgs)
	if service != "" {
		args = append(args, service)
	}
	return startStream(id, ctx, cancel, project(e).Command(ctx, args...))
}

// startAction runs a docker compose command and streams its output. It is
// never cancelled: stopping an up or down halfway would do more harm.
func startAction(id int, e discover.Env, args ...string) (*logStream, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	return startStream(id, ctx, cancel, project(e).Command(ctx, args...))
}

// startDocker runs a docker command and streams its output, like
// startAction.
func startDocker(id int, args ...string) (*logStream, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	return startStream(id, ctx, cancel, exec.CommandContext(ctx, "docker", args...))
}

// startStream sends the stdout and stderr of cmd line by line. The process
// starts when the returned command runs, not before.
func startStream(id int, ctx context.Context, cancel context.CancelFunc, cmd *exec.Cmd) (*logStream, tea.Cmd) {
	s := &logStream{id: id, cancel: cancel, lines: make(chan string, 512)}
	start := func() tea.Msg {
		pr, pw := io.Pipe()
		cmd.Stdout = pw
		cmd.Stderr = pw
		if err := cmd.Start(); err != nil {
			cancel()
			return logDoneMsg{id: id, err: err}
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
				s.lines <- cleanLine(sc.Text())
			}
			if err := sc.Err(); err != nil && ctx.Err() == nil {
				s.err = err
			}
		}()
		return s.next()()
	}
	return s, start
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

// update all

type updateMsg struct {
	env       discover.Env
	recreated int
	err       error
}

// updateEnv pulls new images and recreates whatever changed.
func updateEnv(e discover.Env) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		if _, err := project(e).Run(ctx, "pull", "-q"); err != nil {
			return updateMsg{env: e, err: err}
		}
		out, err := project(e).Run(ctx, "up", "-d")
		if err != nil {
			return updateMsg{env: e, err: err}
		}
		return updateMsg{env: e, recreated: countRecreated(out)}
	}
}

func countRecreated(out string) int {
	n := 0
	for _, l := range strings.Split(out, "\n") {
		if strings.HasSuffix(strings.TrimSpace(l), " Recreated") {
			n++
		}
	}
	return n
}

type folderShellMsg struct {
	env discover.Env
	err error
}

type tmuxWindowMsg struct {
	env discover.Env
	err error
}

// tmuxNewWindow is replaced in tests.
var tmuxNewWindow = func(dir, name string) *exec.Cmd {
	return exec.Command("tmux", "new-window", "-c", dir, "-n", name)
}

// openFolder opens a shell in the environment's folder: a new tmux window
// when running inside tmux, so lazycompose keeps running, otherwise a
// nested shell.
func openFolder(e discover.Env) tea.Cmd {
	if os.Getenv("TMUX") == "" {
		return openFolderShell(e)
	}
	return func() tea.Msg {
		cmd := tmuxNewWindow(e.Dir, e.Name())
		if e.Remote() {
			cmd = tmuxRemoteWindow(e)
		}
		out, err := cmd.CombinedOutput()
		if err != nil && len(out) > 0 {
			err = errors.New(strings.TrimSpace(string(out)))
		}
		return tmuxWindowMsg{env: e, err: err}
	}
}

// userShell is $SHELL, or sh. On Windows, where $SHELL is usually not set,
// it is PowerShell (pwsh, then the built-in one), then %ComSpec%.
func userShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	if runtime.GOOS != "windows" {
		return "sh"
	}
	for _, name := range []string{"pwsh", "powershell"} {
		if _, err := exec.LookPath(name); err == nil {
			return name
		}
	}
	if sh := os.Getenv("ComSpec"); sh != "" {
		return sh
	}
	return "cmd"
}

// remoteFolderShell is the login shell on the host, started in the
// environment's folder. The folder goes to sh as an argument, not into the
// script.
func remoteFolderShell(e discover.Env) []string {
	return []string{"sh", "-c", `cd -- "$1" && exec "${SHELL:-sh}" -l`, "sh", e.Path}
}

// tmuxRemoteWindow opens a tmux window here with the shell on the host.
func tmuxRemoteWindow(e discover.Env) *exec.Cmd {
	args := append([]string{"new-window", "-n", e.Name(), "ssh"}, e.Host.TerminalArgs(remoteFolderShell(e)...)...)
	return exec.Command("tmux", args...)
}

// openFolderShell starts the user's shell in the environment's folder, or
// the login shell on the host for a remote one.
func openFolderShell(e discover.Env) tea.Cmd {
	cmd := exec.Command(userShell())
	cmd.Dir = e.Dir
	if e.Remote() {
		cmd = e.Host.TerminalCommand(remoteFolderShell(e)...)
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		// a non-zero exit just means the last command failed
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			err = nil
		}
		return folderShellMsg{env: e, err: err}
	})
}

const statsPause = time.Second

type (
	statsMsg struct {
		dir   string
		stats map[string]compose.Stats
		err   error
	}
	statsTickMsg struct{ dir string }
)

func loadStats(e discover.Env, names []string) tea.Cmd {
	dir := e.Dir
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		st, err := compose.ContainerStats(ctx, e.Host, names)
		return statsMsg{dir: dir, stats: st, err: err}
	}
}
