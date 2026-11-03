package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mcbulazs/lazycompose/internal/compose"
	"github.com/mcbulazs/lazycompose/internal/discover"
)

const maxLogLines = 5000

type focus int

const (
	focusEnvs focus = iota
	focusServices
	focusLogs
)

type Options struct {
	Hostname string
	Roots    []string
	Envs     []discover.Env
	Find     func() ([]discover.Env, error)
	Theme    Theme
	Editor   string
}

type confirm struct {
	prompt string
	onYes  func(m *Model) tea.Cmd
}

type Model struct {
	opts Options

	list      envList
	filter    textinput.Model
	filtering bool

	states   map[string]*compose.DirState
	services map[string][]string

	current    string // dir of the selected environment
	containers []compose.Container
	rowService []string
	table      table.Model

	logs     viewport.Model
	logLines []string
	logTitle string
	stream   *logStream
	streamID int

	spin      spinner.Model
	busy      int
	status    string
	statusErr bool
	confirm   *confirm
	updating  *updateRun

	focus    focus
	help     help.Model
	showHelp bool

	width, height int
	scanning      bool

	events         *eventStream
	eventsStarting bool
	eventsTried    time.Time

	problemsOnly  bool
	loadingStates bool
	loadingPS     bool
}

func New(opts Options) Model {
	if opts.Theme.Focus != nil {
		applyTheme(opts.Theme)
	}
	fi := textinput.New()
	fi.Prompt = "/"
	fi.PromptStyle = accentStyle
	fi.TextStyle = textStyle
	fi.Cursor.Style = textStyle

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colFocus)

	h := help.New()
	h.Styles.ShortKey = lipgloss.NewStyle().Foreground(colFocus)
	h.Styles.ShortDesc = dimStyle
	h.Styles.ShortSeparator = dimStyle
	h.Styles.FullKey = h.Styles.ShortKey
	h.Styles.FullDesc = dimStyle
	h.Styles.FullSeparator = dimStyle

	t := table.New(table.WithColumns(columns(80, nil)))

	m := Model{
		opts:     opts,
		list:     newEnvList(opts.Envs, len(opts.Roots) > 1),
		filter:   fi,
		states:   map[string]*compose.DirState{},
		services: map[string][]string{},
		table:    t,
		logs:     viewport.New(0, 0),
		spin:     sp,
		help:     h,
	}
	if e, ok := m.list.selected(); ok {
		m.current = e.Dir
		m.loadingPS = true
	}
	m.eventsStarting = true // Init starts the event stream
	if opts.Envs == nil && opts.Find != nil {
		m.scanning = true
		m.busy = 1
		m.setStatus("scanning "+strings.Join(opts.Roots, ", "), false)
	}
	m.setTableStyles()
	m.setLogPlaceholder()
	return m
}

func (m Model) Init() tea.Cmd {
	if m.scanning {
		return tea.Batch(m.spin.Tick, rediscover(m.opts.Find), loadStates(), tick(pollEvery), startEvents())
	}
	cmds := []tea.Cmd{loadStates(), loadServices(m.opts.Envs), tick(pollEvery), startEvents()}
	if c := m.selectCurrent(); c != nil {
		cmds = append(cmds, c)
	}
	return tea.Batch(cmds...)
}

func (m Model) selectedEnv() (discover.Env, bool) { return m.list.selected() }

// selectCurrent returns a load command for the selected env. It only changes
// state through pointers, so it is safe to call from Init.
func (m Model) selectCurrent() tea.Cmd {
	e, ok := m.selectedEnv()
	if !ok {
		return nil
	}
	return loadPS(e)
}

func (m *Model) onSelect() tea.Cmd {
	e, ok := m.selectedEnv()
	if !ok {
		m.current = ""
		m.containers = nil
		m.refreshTable()
		return nil
	}
	if e.Dir == m.current {
		return nil
	}
	m.current = e.Dir
	m.containers = nil
	m.table.SetCursor(0)
	m.stopLogs()
	m.setLogPlaceholder()
	m.refreshTable()
	m.loadingPS = true
	return loadPS(e)
}

// counts returns running services, total services and problem containers.
func (m Model) counts(e discover.Env) (int, int, int) {
	running, total := m.serviceCounts(e)
	problems := 0
	if st := m.states[e.Dir]; st != nil {
		problems = st.Problems
	}
	return running, total, problems
}

func (m Model) serviceCounts(e discover.Env) (int, int) {
	st := m.states[e.Dir]
	names, known := m.services[e.Dir]
	if !known {
		if st == nil {
			return 0, 0
		}
		return len(st.Services), max(st.Total, len(st.Services))
	}
	running := 0
	if st != nil {
		for _, n := range names {
			if st.Services[n] {
				running++
			}
		}
	}
	return running, len(names)
}

func (m *Model) setStatus(s string, isErr bool) {
	m.status = s
	m.statusErr = isErr
}

func (m *Model) startBusy(label string) tea.Cmd {
	m.busy++
	m.setStatus(label, false)
	if m.busy == 1 {
		return m.spin.Tick
	}
	return nil
}

func (m *Model) appendLog(lines ...string) {
	atBottom := m.logs.AtBottom() || len(m.logLines) == 0
	m.logLines = append(m.logLines, lines...)
	if len(m.logLines) > maxLogLines {
		m.logLines = m.logLines[len(m.logLines)-maxLogLines:]
	}
	m.renderLogs()
	if atBottom {
		m.logs.GotoBottom()
	}
}

func (m *Model) renderLogs() {
	w := m.logs.Width
	out := make([]string, len(m.logLines))
	for i, l := range m.logLines {
		l = strings.ReplaceAll(l, "\t", "    ")
		if w > 0 {
			l = ansi.Wrap(l, w, "")
		}
		out[i] = l
	}
	m.logs.SetContent(strings.Join(out, "\n"))
}

func (m *Model) setLogPlaceholder() {
	m.logTitle = "Logs"
	m.logLines = nil
	m.logs.SetContent(dimStyle.Render("press l to follow logs"))
}

func (m *Model) stopLogs() {
	m.stream.stop()
	m.stream = nil
}

// target is the selected env and, when the services pane has focus, the
// selected service.
func (m Model) target() (discover.Env, string, bool) {
	e, ok := m.selectedEnv()
	if !ok {
		return e, "", false
	}
	if m.focus == focusServices {
		return e, m.selectedService(), true
	}
	return e, "", true
}

func (m Model) selectedService() string {
	c := m.table.Cursor()
	if c < 0 || c >= len(m.rowService) {
		return ""
	}
	return m.rowService[c]
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	nm := next.(Model)
	nm.list.scroll(nm.listHeight())
	return nm, cmd
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case spinner.TickMsg:
		if m.busy == 0 {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tickMsg:
		every := pollEvery
		if m.events != nil {
			every = pollEveryEvents
		}
		cmds := []tea.Cmd{tick(every), m.refreshIfIdle()}
		if m.events == nil && !m.eventsStarting && time.Since(m.eventsTried) > retryEvents {
			m.eventsStarting = true
			cmds = append(cmds, startEvents())
		}
		return m, tea.Batch(cmds...)

	case eventsStartedMsg:
		m.eventsStarting = false
		m.eventsTried = time.Now()
		if msg.err != nil {
			return m, nil
		}
		m.events = msg.stream
		return m, msg.stream.next()

	case dockerEventMsg:
		if msg.stream != m.events {
			return m, nil
		}
		return m, tea.Batch(m.refreshIfIdle(), msg.stream.next())

	case eventsEndMsg:
		if msg.stream == m.events {
			m.events = nil
			m.eventsTried = time.Now()
		}
		return m, nil

	case statesMsg:
		m.loadingStates = false
		if msg.err != nil {
			m.setStatus("docker: "+firstLine(msg.err.Error()), true)
			return m, nil
		}
		m.states = msg.states
		if m.problemsOnly {
			m.applyProblemFilter()
			return m, m.onSelect()
		}
		return m, nil

	case servicesMsg:
		for dir, names := range msg.services {
			m.services[dir] = names
		}
		m.refreshTable()
		return m, nil

	case psMsg:
		m.loadingPS = false
		if msg.dir != m.current {
			return m, nil
		}
		if msg.err != nil && !m.statusErr {
			m.setStatus("ps: "+firstLine(msg.err.Error()), true)
		}
		m.containers = msg.containers
		m.refreshTable()
		return m, nil

	case envsMsg:
		m.busy = max(0, m.busy-1)
		m.scanning = false
		if msg.err != nil {
			m.setStatus("discover: "+msg.err.Error(), true)
			return m, nil
		}
		m.opts.Envs = msg.envs
		q := m.list.query
		prev := m.list
		m.list = newEnvList(msg.envs, len(m.opts.Roots) > 1)
		if e, ok := prev.selected(); ok {
			for i, idx := range m.list.visible {
				if m.list.items[idx].env.Dir == e.Dir {
					m.list.cursor = i
				}
			}
		}
		m.list.setFilter(q)
		if m.problemsOnly {
			m.applyProblemFilter()
		}
		m.setStatus(fmt.Sprintf("found %d environments", len(msg.envs)), false)
		return m, tea.Batch(loadServices(msg.envs), loadStates(), m.onSelect())

	case actionMsg:
		m.busy = max(0, m.busy-1)
		if msg.err != nil {
			m.setStatus(msg.label+" failed: "+firstLine(msg.err.Error()), true)
		} else {
			m.setStatus(msg.label+" done", false)
		}
		if m.stream == nil {
			m.logTitle = "Output"
			m.appendLog(dimStyle.Render("$ " + msg.label))
			out := strings.TrimRight(msg.output, "\n")
			if msg.err != nil && out == "" {
				out = msg.err.Error()
			}
			if out != "" {
				m.appendLog(strings.Split(out, "\n")...)
			}
		}
		return m, m.refreshNow()

	case editorMsg:
		if msg.err != nil {
			m.setStatus("editor: "+msg.err.Error(), true)
			return m, nil
		}
		if hashFile(msg.env.File) == msg.before {
			m.setStatus("no changes", false)
			return m, nil
		}
		return m, tea.Batch(m.startBusy("validating "+msg.env.Rel), validate(msg.env))

	case validateMsg:
		m.busy = max(0, m.busy-1)
		e := msg.env
		if msg.err != nil {
			m.setStatus("docker compose config failed for "+e.Rel, true)
			m.stopLogs()
			m.logTitle = "Config error"
			m.logLines = nil
			m.logs.SetContent("")
			m.appendLog(strings.Split(redStyle.Render(msg.err.Error()), "\n")...)
			return m, nil
		}
		m.setStatus("config ok", false)
		m.confirm = &confirm{
			prompt: "Apply changes? (y = up -d / n)",
			onYes: func(m *Model) tea.Cmd {
				return tea.Batch(m.startBusy("up -d "+e.Rel),
					runAction(e, "docker compose up -d ("+e.Rel+")", "up", "-d"))
			},
		}
		return m, loadServices([]discover.Env{e})

	case updateMsg:
		return m, m.onUpdate(msg)

	case shellFoundMsg:
		m.busy = max(0, m.busy-1)
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
			return m, nil
		}
		m.setStatus("", false)
		return m, openShell(msg.env, msg.service, msg.shell)

	case folderShellMsg:
		if msg.err != nil {
			m.setStatus("shell: "+msg.err.Error(), true)
		}
		// the compose file may have changed while in the folder
		return m, tea.Batch(m.refreshNow(), loadServices([]discover.Env{msg.env}))

	case shellMsg:
		if msg.err != nil {
			m.setStatus("shell: "+msg.err.Error(), true)
		}
		return m, m.refreshNow()

	case logLinesMsg:
		if m.stream == nil || msg.id != m.stream.id {
			return m, nil
		}
		m.appendLog(msg.lines...)
		return m, m.stream.next()

	case logDoneMsg:
		if m.stream == nil || msg.id != m.stream.id {
			return m, nil
		}
		if msg.err != nil {
			m.appendLog(redStyle.Render(msg.err.Error()))
		}
		m.appendLog(dimStyle.Render("-- log stream ended --"))
		m.stream = nil
		return m, nil
	}
	return m, nil
}

// refreshIfIdle reloads states and the selected env unless a load is already
// running.
func (m *Model) refreshIfIdle() tea.Cmd {
	var cmds []tea.Cmd
	if !m.loadingStates {
		m.loadingStates = true
		cmds = append(cmds, loadStates())
	}
	if e, ok := m.selectedEnv(); ok && !m.loadingPS {
		m.loadingPS = true
		cmds = append(cmds, loadPS(e))
	}
	return tea.Batch(cmds...)
}

func (m *Model) refreshNow() tea.Cmd {
	cmds := []tea.Cmd{loadStates()}
	m.loadingStates = true
	if e, ok := m.selectedEnv(); ok {
		m.loadingPS = true
		cmds = append(cmds, loadPS(e))
	}
	return tea.Batch(cmds...)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		m.stopLogs()
		m.events.stop()
		return m, tea.Quit
	}

	if m.confirm != nil {
		switch msg.String() {
		case "y", "Y":
			c := m.confirm.onYes
			m.confirm = nil
			return m, c(&m)
		case "n", "N", "esc", "q":
			m.confirm = nil
			m.setStatus("cancelled", false)
		}
		return m, nil
	}

	if m.filtering {
		switch msg.String() {
		case "enter":
			m.filtering = false
			m.filter.Blur()
			return m, nil
		case "esc":
			m.filtering = false
			m.filter.Blur()
			m.filter.SetValue("")
			m.list.setFilter("")
			return m, m.onSelect()
		case "up", "down":
			if msg.String() == "up" {
				m.list.move(-1)
			} else {
				m.list.move(1)
			}
			return m, m.onSelect()
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.list.setFilter(m.filter.Value())
		return m, tea.Batch(cmd, m.onSelect())
	}

	switch {
	case key.Matches(msg, keys.Quit):
		m.stopLogs()
		m.events.stop()
		return m, tea.Quit

	case key.Matches(msg, keys.Help):
		m.showHelp = !m.showHelp
		m.help.ShowAll = m.showHelp
		m.layout()
		return m, nil

	case key.Matches(msg, keys.Tab):
		if msg.String() == "shift+tab" {
			m.focus = (m.focus + 2) % 3
		} else {
			m.focus = (m.focus + 1) % 3
		}
		m.setTableStyles()
		return m, nil

	case key.Matches(msg, keys.Up), key.Matches(msg, keys.Down):
		n := 1
		if key.Matches(msg, keys.Up) {
			n = -1
		}
		switch m.focus {
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
				m.logs.ScrollUp(1)
			} else {
				m.logs.ScrollDown(1)
			}
		}
		return m, nil

	case msg.String() == "pgup", msg.String() == "pgdown", msg.String() == "g", msg.String() == "G":
		if m.focus == focusLogs {
			switch msg.String() {
			case "pgup":
				m.logs.PageUp()
			case "pgdown":
				m.logs.PageDown()
			case "g":
				m.logs.GotoTop()
			case "G":
				m.logs.GotoBottom()
			}
		}
		return m, nil

	case key.Matches(msg, keys.Filter):
		m.filtering = true
		m.focus = focusEnvs
		m.setTableStyles()
		m.filter.SetValue(m.list.query)
		m.filter.CursorEnd()
		return m, m.filter.Focus()

	case msg.String() == "esc" && m.updating != nil:
		m.updating.stopping = true
		m.setStatus("stopping after "+m.updating.queue[m.updating.next].Rel, false)
		return m, nil

	case key.Matches(msg, keys.Problems):
		m.problemsOnly = !m.problemsOnly
		if m.problemsOnly {
			m.applyProblemFilter()
		} else {
			m.list.only = nil
			m.list.setFilter(m.list.query)
		}
		return m, m.onSelect()

	case key.Matches(msg, keys.UpdateAll):
		return m, m.startUpdateAll()

	case msg.String() == "esc":
		if m.problemsOnly {
			m.problemsOnly = false
			m.list.only = nil
			m.list.setFilter(m.list.query)
			return m, m.onSelect()
		}
		if m.list.query != "" {
			m.filter.SetValue("")
			m.list.setFilter("")
			return m, m.onSelect()
		}
		if m.stream != nil {
			m.stopLogs()
			m.appendLog(dimStyle.Render("-- stopped following --"))
		}
		return m, nil

	case key.Matches(msg, keys.Refresh):
		cmd := m.refreshNow()
		if m.opts.Find != nil {
			return m, tea.Batch(cmd, m.startBusy("rescanning"), rediscover(m.opts.Find))
		}
		return m, tea.Batch(cmd, loadServices(m.opts.Envs))
	}

	return m.handleAction(msg)
}

func (m Model) handleAction(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e, svc, ok := m.target()
	if !ok {
		return m, nil
	}
	name := e.Rel
	args := func(base ...string) []string {
		if svc != "" {
			name = e.Rel + " " + svc
			return append(base, svc)
		}
		return base
	}

	switch {
	case key.Matches(msg, keys.Edit):
		return m, openEditor(m.opts.Editor, e)

	case key.Matches(msg, keys.Start):
		a := args("up", "-d")
		return m, tea.Batch(m.startBusy("up -d "+name), runAction(e, "docker compose up -d ("+name+")", a...))

	case key.Matches(msg, keys.Restart):
		a := args("restart")
		return m, tea.Batch(m.startBusy("restarting "+name), runAction(e, "docker compose restart ("+name+")", a...))

	case key.Matches(msg, keys.Pull):
		a := args("pull")
		return m, tea.Batch(m.startBusy("pulling "+name), runAction(e, "docker compose pull ("+name+")", a...))

	case key.Matches(msg, keys.Stop):
		m.confirm = &confirm{
			prompt: fmt.Sprintf("Run docker compose down for %s? (y/n)", e.Rel),
			onYes: func(m *Model) tea.Cmd {
				return tea.Batch(m.startBusy("down "+e.Rel),
					runAction(e, "docker compose down ("+e.Rel+")", "down"))
			},
		}
		return m, nil

	case key.Matches(msg, keys.Logs):
		m.stopLogs()
		m.streamID++
		m.logLines = nil
		m.logs.SetContent("")
		m.logTitle = "Logs · " + e.Rel
		if svc != "" {
			m.logTitle += " · " + svc
		}
		s, cmd := startLogs(m.streamID, e, svc)
		m.stream = s
		if s == nil {
			// start failed; keep a dummy stream so the done message is shown
			m.stream = &logStream{id: m.streamID, cancel: func() {}}
		}
		return m, cmd

	case key.Matches(msg, keys.Toggle):
		s := m.selectedService()
		if s == "" {
			m.setStatus("no service selected", true)
			return m, nil
		}
		name := e.Rel + " " + s
		if m.serviceRunning(s) {
			return m, tea.Batch(m.startBusy("stopping "+name),
				runAction(e, "docker compose stop ("+name+")", "stop", s))
		}
		return m, tea.Batch(m.startBusy("starting "+name),
			runAction(e, "docker compose up -d ("+name+")", "up", "-d", s))

	case key.Matches(msg, keys.Folder):
		return m, openFolderShell(e)

	case key.Matches(msg, keys.Shell):
		s := m.selectedService()
		if s == "" {
			m.setStatus("no service selected", true)
			return m, nil
		}
		if !m.serviceRunning(s) {
			m.setStatus(s+" is not running", true)
			return m, nil
		}
		return m, tea.Batch(m.startBusy("opening shell in "+s), findShell(e, s))
	}
	return m, nil
}

func (m *Model) applyProblemFilter() {
	only := map[string]bool{}
	for dir, st := range m.states {
		if st.Problems > 0 {
			only[dir] = true
		}
	}
	m.list.only = only
	m.list.setFilter(m.list.query)
}

func (m Model) serviceRunning(service string) bool {
	for _, c := range m.containers {
		if c.Service == service && c.Running() {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

// services table

var columnTitles = []string{"SERVICE", "STATE", "HEALTH", "PORTS", "IMAGE", "UPTIME"}

// columns sizes each column to its widest cell. When the table does not fit,
// IMAGE, then PORTS, then SERVICE give up space, down to a minimum.
func columns(w int, rows []table.Row) []table.Column {
	limit := []int{24, 14, 9, 32, 40, 18}
	widths := make([]int, len(columnTitles))
	for i, t := range columnTitles {
		widths[i] = len(t)
		for _, r := range rows {
			widths[i] = max(widths[i], lipgloss.Width(r[i]))
		}
		widths[i] = min(widths[i], limit[i])
	}

	// each cell has one column of padding on both sides
	over := len(widths)*2 - w
	for _, wd := range widths {
		over += wd
	}
	for _, c := range []struct{ col, min int }{{4, 10}, {3, 9}, {0, 8}} {
		if over <= 0 {
			break
		}
		cut := min(over, widths[c.col]-c.min)
		if cut > 0 {
			widths[c.col] -= cut
			over -= cut
		}
	}

	// still too wide: trim the widest column until it fits, STATE last
	for over > 0 {
		widest := 4
		for _, i := range []int{4, 3, 0, 5, 2, 1} {
			if widths[i] > widths[widest] {
				widest = i
			}
		}
		if widths[widest] <= 3 {
			break
		}
		widths[widest]--
		over--
	}

	cols := make([]table.Column, len(columnTitles))
	for i, t := range columnTitles {
		cols[i] = table.Column{Title: t, Width: widths[i]}
	}
	return cols
}

func (m *Model) setTableStyles() {
	s := table.DefaultStyles()
	s.Header = s.Header.Foreground(colDim).Bold(true)
	s.Cell = s.Cell.Foreground(colText)
	if m.focus == focusServices {
		s.Selected = selectedRow
	} else {
		s.Selected = lipgloss.NewStyle()
	}
	m.table.SetStyles(s)
}

func stateText(c compose.Container) string {
	if c.Problem() {
		if c.State == "exited" {
			return fmt.Sprintf("✗ exited (%d)", c.ExitCode)
		}
		return "✗ " + c.State
	}
	switch c.State {
	case "running":
		return "● running"
	case "exited":
		return fmt.Sprintf("○ exited (%d)", c.ExitCode)
	case "":
		return "○ -"
	default:
		return "◐ " + c.State
	}
}

func (m *Model) refreshTable() {
	byService := map[string][]compose.Container{}
	for _, c := range m.containers {
		byService[c.Service] = append(byService[c.Service], c)
	}
	names := append([]string(nil), m.services[m.current]...)
	known := map[string]bool{}
	for _, n := range names {
		known[n] = true
	}
	for s := range byService {
		if !known[s] {
			names = append(names, s)
		}
	}
	sort.Strings(names)

	var rows []table.Row
	m.rowService = m.rowService[:0]
	for _, n := range names {
		cs := byService[n]
		if len(cs) == 0 {
			rows = append(rows, table.Row{n, "○ not created", "", "", "", ""})
			m.rowService = append(m.rowService, n)
			continue
		}
		for i, c := range cs {
			label := n
			if len(cs) > 1 {
				label = fmt.Sprintf("%s #%d", n, i+1)
			}
			health := c.Health
			if health == "" {
				health = "-"
			}
			rows = append(rows, table.Row{label, stateText(c), health, c.Ports(), compose.Short(c.Image), c.Uptime()})
			m.rowService = append(m.rowService, n)
		}
	}
	m.table.SetRows(rows)
	// the table leaves its cursor at -1 when it was set while empty
	if c := m.table.Cursor(); c < 0 || c >= len(rows) {
		m.table.SetCursor(max(0, min(c, len(rows)-1)))
	}
	m.layout()
}

// layout

func (m Model) leftWidth() int {
	return min(max(28, m.width*26/100), 40)
}

func (m Model) bodyHeight() int {
	return max(m.height-2-lipgloss.Height(m.footerView()), 6)
}

func (m Model) servicesHeight() int {
	body := m.bodyHeight()
	want := len(m.table.Rows()) + 5
	return max(min(want, body/2), min(7, body-3))
}

func (m Model) listHeight() int {
	h := m.bodyHeight() - 3
	if m.filtering || m.list.query != "" {
		h--
	}
	return max(h, 1)
}

func (m *Model) layout() {
	if m.width == 0 {
		return
	}
	rightW := m.width - m.leftWidth()
	inner := rightW - 4
	sh := m.servicesHeight()

	m.table.SetColumns(columns(inner, m.table.Rows()))
	m.table.SetWidth(inner)
	m.table.SetHeight(max(sh-3, 2))

	m.logs.Width = inner
	m.logs.Height = max(m.bodyHeight()-sh-3, 1)
	if len(m.logLines) > 0 {
		m.renderLogs()
	}
}

// view

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	header := m.headerView()
	footer := m.footerView()
	body := m.bodyHeight()

	lw := m.leftWidth()
	rw := m.width - lw
	sh := m.servicesHeight()

	listH := m.listHeight()
	envTitle := "Environments"
	if m.problemsOnly {
		envTitle += " · problems"
	}
	var listBody string
	if m.filtering || m.list.query != "" {
		f := m.filter.View()
		if !m.filtering {
			f = accentStyle.Render("/") + textStyle.Render(m.list.query)
		}
		listBody = f + "\n"
	}
	if m.scanning && len(m.list.items) == 0 {
		listBody += dimStyle.Render("scanning…")
	} else {
		listBody += m.list.view(lw-4, listH, m.focus == focusEnvs, m.counts)
	}
	left := pane(envTitle, listBody, lw, body, m.focus == focusEnvs)

	svcTitle := "Services"
	if e, ok := m.selectedEnv(); ok {
		svcTitle += " · " + e.Rel
	}
	var svcBody string
	if len(m.table.Rows()) == 0 {
		svcBody = dimStyle.Render("no services")
	} else {
		svcBody = m.table.View()
	}
	services := pane(svcTitle, svcBody, rw, sh, m.focus == focusServices)

	logTitle := m.logTitle
	if m.stream != nil {
		logTitle += " (following)"
	}
	logs := pane(logTitle, m.logs.View(), rw, body-sh, m.focus == focusLogs)

	right := lipgloss.JoinVertical(lipgloss.Left, services, logs)
	main := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	return lipgloss.JoinVertical(lipgloss.Left, header, main, m.statusView(), footer)
}

func (m Model) headerView() string {
	var up, partial, down, problems int
	for _, it := range m.list.items {
		running, total, p := m.counts(it.env)
		if p > 0 {
			problems++
		}
		switch statusOf(running, total) {
		case statusUp:
			up++
		case statusPartial:
			partial++
		default:
			down++
		}
	}
	right := greenStyle.Render(fmt.Sprintf("● %d up", up)) + "  " +
		yellowStyle.Render(fmt.Sprintf("◐ %d partial", partial)) + "  " +
		dimStyle.Render(fmt.Sprintf("○ %d down", down))
	if problems > 0 {
		right += "  " + redStyle.Render(fmt.Sprintf("✗ %d with problems", problems))
	}
	left := accentStyle.Render("lazycompose") + "  " + textStyle.Render(m.opts.Hostname) + "  "
	room := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 4
	roots := strings.Join(m.opts.Roots, ", ")
	if cut := lipgloss.Width(roots) - room; cut > 0 {
		roots = ansi.TruncateLeft(roots, cut+1, "…")
	}
	left += dimStyle.Render(roots)
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		return " " + ansi.Truncate(left+"  "+right, m.width-1, "…")
	}
	return " " + left + strings.Repeat(" ", gap) + right + " "
}

func (m Model) statusView() string {
	var s string
	switch {
	case m.confirm != nil:
		s = confirmStyle.Render(m.confirm.prompt)
	case m.busy > 0:
		s = m.spin.View() + " " + textStyle.Render(m.status)
	case m.statusErr:
		s = redStyle.Render(m.status)
	default:
		s = dimStyle.Render(m.status)
	}
	return " " + ansi.Truncate(s, m.width-2, "…")
}

func (m Model) footerView() string {
	m.help.Width = m.width - 2
	return lipgloss.NewStyle().PaddingLeft(1).Render(m.help.View(keys))
}
