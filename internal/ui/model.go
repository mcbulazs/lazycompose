package ui

import (
	"fmt"
	"sort"
	"strings"

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

	focus    focus
	help     help.Model
	showHelp bool

	width, height int
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

	t := table.New(table.WithColumns(columns(80)))

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
	m.setTableStyles()
	m.setLogPlaceholder()
	return m
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{loadStates(), loadServices(m.opts.Envs), tick()}
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

// running and total service counts for an environment
func (m Model) counts(e discover.Env) (int, int) {
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

	case spinner.TickMsg:
		if m.busy == 0 {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tickMsg:
		cmds := []tea.Cmd{tick()}
		if !m.loadingStates {
			m.loadingStates = true
			cmds = append(cmds, loadStates())
		}
		if e, ok := m.selectedEnv(); ok && !m.loadingPS {
			m.loadingPS = true
			cmds = append(cmds, loadPS(e))
		}
		return m, tea.Batch(cmds...)

	case statesMsg:
		m.loadingStates = false
		if msg.err != nil {
			m.setStatus("docker: "+firstLine(msg.err.Error()), true)
			return m, nil
		}
		m.states = msg.states
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

	case msg.String() == "esc":
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
		return m, openEditor(e)

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

	case key.Matches(msg, keys.Shell):
		s := m.selectedService()
		if s == "" {
			m.setStatus("no service selected", true)
			return m, nil
		}
		return m, openShell(e, s)
	}
	return m, nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

// services table

func columns(w int) []table.Column {
	// each cell has one column of padding on both sides
	fixed := map[string]int{"SERVICE": 16, "STATE": 13, "HEALTH": 9, "UPTIME": 14}
	used := 6 * 2
	for _, v := range fixed {
		used += v
	}
	rest := max(w-used, 20)
	ports := rest * 45 / 100
	return []table.Column{
		{Title: "SERVICE", Width: fixed["SERVICE"]},
		{Title: "STATE", Width: fixed["STATE"]},
		{Title: "HEALTH", Width: fixed["HEALTH"]},
		{Title: "PORTS", Width: ports},
		{Title: "IMAGE", Width: rest - ports},
		{Title: "UPTIME", Width: fixed["UPTIME"]},
	}
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
	if m.table.Cursor() >= len(rows) {
		m.table.SetCursor(max(0, len(rows)-1))
	}
	m.layout()
}

// layout

func (m Model) leftWidth() int {
	return min(max(28, m.width*30/100), 48)
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

	m.table.SetColumns(columns(inner))
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
	var listBody string
	if m.filtering || m.list.query != "" {
		f := m.filter.View()
		if !m.filtering {
			f = accentStyle.Render("/") + textStyle.Render(m.list.query)
		}
		listBody = f + "\n"
	}
	listBody += m.list.view(lw-4, listH, m.focus == focusEnvs, m.counts)
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
	var up, partial, down int
	for _, it := range m.list.items {
		switch statusOf(m.counts(it.env)) {
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
