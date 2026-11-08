package ui

import (
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

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
	"github.com/mcbulazs/lazycompose/internal/history"
	"github.com/mcbulazs/lazycompose/internal/remote"
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
	// Find scans the given roots
	Find   func(roots []string) ([]discover.Env, error)
	Theme  Theme
	Editor string
	// CdFile receives the selected folder when quitting with quit_to_folder
	CdFile string
	// Mouse is set when the program runs with mouse support
	Mouse bool
	// MaxDepth is the scan depth, so new folders are placed where the scan
	// finds them; 0 is no limit
	MaxDepth int
	// NewTemplate is a compose file to start new environments from
	NewTemplate string
	// NewDotenv creates an empty env file in new environments
	NewDotenv bool
	// NewComposeFilename is the compose file name of new environments; ""
	// means stackfs.ComposeName
	NewComposeFilename string
	// Version is this build's version, like v0.3.0
	Version string
	// LatestVersion looks up the latest release, to mention it in the
	// header; nil never looks
	LatestVersion func(context.Context) (string, error)
	// UpdateHint says how to update, after the new version in the header;
	// "" means "run lazycompose -update"
	UpdateHint string
	// UpdateEvery is how often to ask registries for newer images; 0 is never
	UpdateEvery time.Duration
	// ConfigPath is the config file, edited with edit_config; "" means none
	ConfigPath string
	// CheckConfig reads the config file again after it was edited, to report
	// mistakes; nil skips the check
	CheckConfig func() error
	// QuickAccess shows the most used environments at the top of the list
	QuickAccess bool
	// History records environment use for quick access; nil records nothing
	History *history.History
}

// quickSize is how many environments quick access shows at most.
const quickSize = 5

type confirm struct {
	prompt string
	onYes  func(m *Model) tea.Cmd
	// yes is another key that answers yes, like the up key for applying
	// with up -d; nil for none
	yes *key.Binding
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

	logs viewport.Model
	// lineStart is the first wrapped row of each log line
	lineStart []int
	logTarget logTarget // what the log stream follows
	paneMode  paneMode  // the log pane shows logs or the compose file
	// composeTarget is the compose file shown in compose mode
	composeTarget logTarget
	pendingLogs   logTarget // waiting for the selection to settle
	autoID        int
	holdOutput    bool           // keep command output until a key is pressed
	outputKeys    map[string]int // progress line positions in command output
	actions       map[int]*actionRun
	search        logSearch
	searching     bool
	searchInput   textinput.Model
	logLines      []string
	logTitle      string
	stream        *logStream
	streamID      int

	spin      spinner.Model
	busy      int
	status    string
	statusErr bool
	confirm   *confirm
	updating  *updateRun

	focus  focus
	zoomed bool // only the focused pane is shown, full size
	// newVersion is a newer lazycompose release, shown in the header
	newVersion string
	menu       *actionMenu // the keys window or the action menu, when open
	recent     []string    // actions run from the menu, newest first

	width, height int
	scanning      bool
	quitDir       string                   // set when quitting to a folder
	stats         map[string]compose.Stats // by container name, for the selected env
	loadingStats  bool

	// hosts are the machines with roots: nil for this one, if it has
	// roots, and each remote host
	hosts          []*remote.Host
	hostErrs       map[string]string // the last error of each host, by hostKey
	connecting     map[string]bool   // hosts being connected, by hostKey
	afterScan      envItem           // what to select once a scan of a new host is in
	edit           *editWatch        // the file being edited, if any
	leftovers      []string          // temporary files and folders to remove when quitting
	editID         int
	events         map[string]*eventStream
	eventsStarting map[string]bool
	eventsTried    map[string]time.Time

	upd updateState

	prompt *pathPrompt

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

	si := textinput.New()
	si.Prompt = "/"
	si.PromptStyle = accentStyle
	si.TextStyle = textStyle
	si.Placeholder = "search logs"

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colFocus)

	t := table.New(table.WithColumns(columns(80, nil)))

	m := Model{
		opts:        opts,
		list:        newEnvList(opts.Envs, opts.Roots),
		filter:      fi,
		searchInput: si,
		states:      map[string]*compose.DirState{},
		services:    map[string][]string{},
		table:       t,
		logs:        viewport.New(0, 0),
		spin:        sp,
	}
	m.updateQuick()
	m.list.selectFirst() // start on the most used environment
	if opts.History != nil && opts.History.Pane == modeCompose.name() {
		m.paneMode = modeCompose
	}
	if e, ok := m.list.selected(); ok {
		m.current = e.Dir
		m.loadingPS = true
	}
	m.hosts = startHosts(opts.Roots, opts.Envs)
	m.connecting = map[string]bool{}
	m.closeAtStart()
	m.hostErrs = map[string]string{}
	m.events = map[string]*eventStream{}
	m.eventsStarting = map[string]bool{}
	m.eventsTried = map[string]time.Time{}
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
		return tea.Batch(m.spin.Tick, rediscover(m.opts.Find, m.activeRoots()), loadStates(m.hosts), tick(pollEvery), m.startAllEvents(true), m.firstUpdateCheck(), m.firstVersionCheck())
	}
	cmds := []tea.Cmd{loadStates(m.hosts), loadServices(m.opts.Envs), tick(pollEvery), m.startAllEvents(true), m.firstUpdateCheck(), m.firstVersionCheck()}
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
	if it, _ := m.list.selectedItem(); it.offline {
		m.setStatus(it.env.Host.Label()+" is not connected yet; enter connects", false)
	}
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
	m.stats = nil
	m.table.SetCursor(0)
	if !m.holdOutput {
		m.stopLogs()
		m.setLogPlaceholder()
	}
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
	m.findMatches()
	isMatch := map[int]bool{}
	for _, i := range m.search.matches {
		isMatch[i] = true
	}
	current := -1
	if len(m.search.matches) > 0 {
		current = m.search.matches[m.search.current]
	}

	w := m.logs.Width
	out := make([]string, len(m.logLines))
	m.lineStart = m.lineStart[:0]
	row := 0
	for i, l := range m.logLines {
		l = strings.ReplaceAll(l, "\t", "    ")
		if isMatch[i] {
			l = highlight(l, m.search.query, i == current)
		}
		if w > 0 {
			l = ansi.Wrap(l, w, "")
		}
		out[i] = l
		m.lineStart = append(m.lineStart, row)
		row += strings.Count(l, "\n") + 1
	}
	m.logs.SetContent(strings.Join(out, "\n"))
}

func (m *Model) setLogPlaceholder() {
	m.search = logSearch{}
	m.logTitle = "Logs"
	m.logLines = nil
	m.logs.SetContent(dimStyle.Render("loading logs…"))
}

func (m *Model) stopLogs() {
	m.stream.stop()
	m.stream = nil
	m.logTarget = logTarget{}
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
	if c := nm.scheduleLogs(); c != nil {
		cmd = tea.Batch(cmd, c)
	}
	if nm.opts.Mouse && handedOver(msg) {
		// Bubble Tea turns the mouse off when it hands the terminal to
		// another program and does not turn it back on afterwards.
		cmd = tea.Batch(cmd, tea.EnableMouseCellMotion)
	}
	return nm, cmd
}

// handedOver reports messages sent when the editor or a shell has exited.
func handedOver(msg tea.Msg) bool {
	switch msg.(type) {
	case editorMsg, configEditedMsg, logsEditedMsg, shellMsg, folderShellMsg, hostLoginMsg:
		return true
	}
	return false
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case oldImagesMsg:
		return m, m.onOldImages(msg)

	case composeReadMsg:
		m.onComposeRead(msg)
		return m, nil

	case versionTickMsg:
		return m, m.checkVersion()

	case versionMsg:
		return m, m.onVersion(msg)

	case tea.MouseMsg:
		switch {
		case m.prompt != nil:
			return m.handlePromptMouse(msg)
		case m.menu != nil:
			return m.handleMenuMouse(msg)
		}
		return m.handleMouse(msg)

	case spinner.TickMsg:
		if m.busy == 0 {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case moveDoneMsg:
		return m, m.moveDone(msg)

	case hostCheckedMsg:
		return m, m.onHostChecked(msg)

	case hostLoginMsg:
		return m, m.onHostLogin(msg)

	case remoteCreatedMsg:
		return m, m.onRemoteCreated(msg)

	case remoteMoveCheckedMsg:
		return m, m.onRemoteMoveChecked(msg)

	case updateTickMsg, updatesCheckedMsg:
		return m.handleUpdates(msg)

	case tickMsg:
		every := pollEvery
		if m.allEvents() {
			every = pollEveryEvents
		}
		return m, tea.Batch(tick(every), m.refreshIfIdle(), m.startAllEvents(false))

	case eventsStartedMsg:
		m.eventsStarting[msg.host] = false
		m.eventsTried[msg.host] = time.Now()
		if msg.err != nil {
			return m, nil
		}
		m.events[msg.host] = msg.stream
		return m, msg.stream.next()

	case dockerEventMsg:
		if msg.stream != m.events[msg.stream.host] {
			return m, nil
		}
		return m, tea.Batch(m.refreshIfIdle(), msg.stream.next())

	case eventsEndMsg:
		if msg.stream == m.events[msg.stream.host] {
			delete(m.events, msg.stream.host)
			m.eventsTried[msg.stream.host] = time.Now()
		}
		return m, nil

	case statesMsg:
		m.loadingStates = false
		if m.onStates(msg) {
			return m, nil
		}
		// a log stream ends when its containers stop; pick it up again
		// once they run
		if m.stream == nil && !m.holdOutput && m.logTarget.dir != "" {
			st := m.states[m.logTarget.dir]
			svc := m.logTarget.service
			if st != nil && st.Running > 0 && (svc == "" || st.Services[svc]) {
				m.logTarget = logTarget{}
			}
		}
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
		return m, m.sampleStats()

	case statsMsg:
		m.loadingStats = false
		if msg.dir != m.current {
			return m, nil
		}
		if msg.err == nil {
			m.stats = msg.stats
			m.refreshTable()
		}
		// sample again shortly; docker stats itself takes about two seconds
		dir := msg.dir
		return m, tea.Tick(statsPause, func(time.Time) tea.Msg { return statsTickMsg{dir: dir} })

	case statsTickMsg:
		if msg.dir != m.current {
			return m, nil
		}
		return m, m.sampleStats()

	case envsMsg:
		m.busy = max(0, m.busy-1)
		m.scanning = false
		if msg.err != nil && len(msg.envs) == 0 {
			m.setStatus("discover: "+firstLine(msg.err.Error()), true)
			return m, nil
		}
		first := len(m.opts.Envs) == 0
		envs := mergeEnvs(m.opts.Envs, msg.envs, msg.roots)
		m.opts.Envs = envs
		m.list.setEnvs(envs)
		m.updateQuick()
		if first {
			m.list.selectFirst() // start on the most used environment
		}
		if m.afterScan.env.Dir != "" {
			m.list.selectItem(m.afterScan)
			m.afterScan = envItem{}
		}
		if m.problemsOnly {
			m.applyProblemFilter()
		}
		if msg.err != nil {
			m.setStatus("discover: "+firstLine(msg.err.Error()), true)
		} else {
			m.setStatus(fmt.Sprintf("found %d environments", len(envs)), false)
		}
		return m, tea.Batch(loadServices(msg.envs), loadStates(m.hosts), m.onSelect())

	case autoLogsMsg:
		if msg.id != m.autoID {
			return m, nil
		}
		m.pendingLogs = logTarget{}
		if m.holdOutput || m.paneMode == modeCompose {
			return m, nil
		}
		if e, svc, ok := m.wantedLogs(); ok {
			return m, m.followLogs(e, svc)
		}
		return m, nil

	case editorMsg:
		return m, m.onEditor(msg)

	case editTickMsg:
		return m, m.onEditTick(msg)

	case editReadyMsg:
		return m, m.onEditReady(msg)

	case uploadedMsg:
		return m, m.onUploaded(msg)

	case configEditedMsg:
		switch {
		case msg.err != nil:
			m.setStatus("editor: "+msg.err.Error(), true)
		case !msg.changed && msg.quick:
			m.setStatus("config opened in the editor; restart lazycompose after saving it", false)
		case !msg.changed:
			m.setStatus("no changes", false)
		default:
			if m.opts.CheckConfig != nil {
				if err := m.opts.CheckConfig(); err != nil {
					m.setStatus("config: "+firstLine(err.Error())+"; fix it, or lazycompose will not start", true)
					return m, nil
				}
			}
			m.setStatus("config saved; restart lazycompose to use it", false)
		}
		return m, nil

	case validateMsg:
		m.busy = max(0, m.busy-1)
		e := msg.env
		if msg.err != nil {
			m.setStatus("docker compose config failed for "+e.Rel, true)
			m.stopLogs()
			m.holdOutput = true
			m.logTitle = "Config error"
			m.logLines = nil
			m.logs.SetContent("")
			m.appendLog(strings.Split(redStyle.Render(msg.err.Error()), "\n")...)
			return m, nil
		}
		m.setStatus("config ok", false)
		yes := "y"
		if keys.Start.Enabled() {
			// the up key applies too, it is what the question offers to run
			yes += " or " + keys.Start.Help().Key
		}
		m.confirm = &confirm{
			prompt: "Apply changes? (" + yes + " = up -d / n)",
			onYes: func(m *Model) tea.Cmd {
				return m.runAction(e, "up -d "+e.Rel, "docker compose up -d ("+e.Rel+")", "up", "-d")
			},
			yes: &keys.Start,
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

	case tmuxWindowMsg:
		if msg.err != nil {
			// tmux did not work out; fall back to a nested shell
			m.setStatus("tmux: "+firstLine(msg.err.Error()), true)
			return m, openFolderShell(msg.env)
		}
		m.setStatus("opened a tmux window in "+msg.env.Rel, false)
		return m, nil

	case logsEditedMsg:
		if msg.file != "" {
			m.leftovers = append(m.leftovers, msg.file)
		}
		if msg.err != nil {
			m.setStatus("editor: "+msg.err.Error(), true)
		}
		return m, nil

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
		if a, ok := m.actions[msg.id]; ok {
			m.appendOutput(msg.lines...)
			return m, a.stream.next()
		}
		if m.stream == nil || msg.id != m.stream.id {
			return m, nil
		}
		m.appendLog(msg.lines...)
		return m, m.stream.next()

	case logDoneMsg:
		if a, ok := m.actions[msg.id]; ok {
			return m, m.actionDone(msg.id, a, msg.err)
		}
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
		cmds = append(cmds, loadStates(m.hosts))
	}
	if e, ok := m.selectedEnv(); ok && !m.loadingPS {
		m.loadingPS = true
		cmds = append(cmds, loadPS(e))
	}
	return tea.Batch(cmds...)
}

func (m *Model) refreshNow() tea.Cmd {
	cmds := []tea.Cmd{loadStates(m.hosts)}
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
		m.stopEvents()
		m.cleanup()
		return m, tea.Quit
	}

	if m.prompt != nil {
		return m.handlePromptKey(msg)
	}
	if m.menu != nil {
		return m.handleMenuKey(msg)
	}

	m.releaseOutput(msg)

	if m.confirm != nil {
		switch msg.String() {
		case "y", "Y":
			c := m.confirm.onYes
			m.confirm = nil
			return m, c(&m)
		case "n", "N", "esc", "q":
			m.confirm = nil
			m.setStatus("cancelled", false)
		default:
			if y := m.confirm.yes; y != nil && y.Enabled() && key.Matches(msg, *y) {
				c := m.confirm.onYes
				m.confirm = nil
				return m, c(&m)
			}
		}
		return m, nil
	}

	if m.searching {
		return m.handleSearchKey(msg)
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
		m.stopEvents()
		m.cleanup()
		return m, tea.Quit

	case key.Matches(msg, keys.QuitToFolder):
		if m.opts.CdFile == "" {
			m.setStatus(keys.QuitToFolder.Help().Key+` needs lazycompose -cd-file, see "Change directory on quit" in the README`, true)
			return m, nil
		}
		e, ok := m.selectedEnv()
		if !ok {
			m.setStatus("no environment selected", true)
			return m, nil
		}
		if e.Remote() {
			m.setStatus(notOnRemote(keys.QuitToFolder), true)
			return m, nil
		}
		m.quitDir = e.Dir
		m.stopLogs()
		m.stopEvents()
		m.cleanup()
		return m, tea.Quit

	// n is also the next match while searching the logs
	case key.Matches(msg, keys.NewEnv) && !(m.focus == focusLogs && m.search.active()):
		return m, m.startCreate()

	case key.Matches(msg, keys.Pin):
		m.togglePin()
		return m, m.onSelect()

	case key.Matches(msg, keys.MoveEnv):
		return m, m.startMove()

	case key.Matches(msg, keys.Help):
		return m, m.openMenu(true)

	case key.Matches(msg, keys.Commands):
		return m, m.openMenu(false)

	case key.Matches(msg, keys.NextPane, keys.PrevPane, keys.FocusEnvs, keys.FocusServices, keys.FocusLogs):
		switch {
		case key.Matches(msg, keys.NextPane):
			m.focus = (m.focus + 1) % 3
		case key.Matches(msg, keys.PrevPane):
			m.focus = (m.focus + 2) % 3
		case key.Matches(msg, keys.FocusEnvs):
			m.focus = focusEnvs
		case key.Matches(msg, keys.FocusServices):
			m.focus = focusServices
		case key.Matches(msg, keys.FocusLogs):
			m.focus = focusLogs
		}
		m.setTableStyles()
		return m, nil

	case m.focus == focusEnvs && slices.Contains([]string{"enter", " ", "right", "left"}, msg.String()):
		return m, m.headingKey(msg.String())

	case key.Matches(msg, keys.MoveUp, keys.MoveDown):
		n := 1
		if key.Matches(msg, keys.MoveUp) {
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

	case key.Matches(msg, keys.Filter) && m.focus == focusLogs:
		return m, m.startLogSearch()

	case m.focus == focusLogs && m.search.active() && (msg.String() == "n" || msg.String() == "N"):
		if msg.String() == "n" {
			m.moveMatch(-1)
		} else {
			m.moveMatch(1)
		}
		return m, nil

	case msg.String() == "esc" && m.focus == focusLogs && m.search.active():
		m.clearSearch()
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

	case key.Matches(msg, keys.CleanImages):
		return m, m.startCleanup()

	case key.Matches(msg, keys.Zoom):
		m.zoomed = !m.zoomed
		m.layout()
		return m, nil

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
		if m.zoomed {
			m.zoomed = false
			m.layout()
		}
		return m, nil

	case key.Matches(msg, keys.ComposeView):
		if m.paneMode == modeCompose {
			m.setPaneMode(modeLogs)
		} else {
			m.setPaneMode(modeCompose)
		}
		return m, nil

	case key.Matches(msg, keys.EditConfig):
		if _, _, why := m.configCommand(""); why != "" {
			m.setStatus(why, true)
			return m, nil
		}
		return m, openConfigEditor(m.opts.Editor, m.opts.ConfigPath)

	case key.Matches(msg, keys.OpenLogs):
		if _, _, why := m.logsCommand(""); why != "" {
			m.setStatus(why, true)
			return m, nil
		}
		return m, openLogsEditor(m.opts.Editor, m.logLines)

	case key.Matches(msg, keys.Refresh):
		m.composeTarget = logTarget{} // read the compose file again
		cmd := m.refreshNow()
		if m.opts.Find != nil {
			return m, tea.Batch(cmd, m.startBusy("rescanning"), rediscover(m.opts.Find, m.activeRoots()))
		}
		return m, tea.Batch(cmd, loadServices(m.opts.Envs))
	}

	return m.handleAction(msg)
}

// handleAction runs an action on the selected environment, and counts it as
// a use for quick access.
func (m Model) handleAction(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	next, cmd := m.envAction(msg)
	nm := next.(Model)
	if cmd != nil || nm.confirm != nil && m.confirm == nil {
		if e, ok := nm.selectedEnv(); ok {
			nm.recordUse(e)
		}
	}
	return nm, cmd
}

// updateQuick fills the quick access section with the pinned environments
// and the most used ones. It runs on start, after a rescan and after a pin
// changes, so the list does not move while in use.
func (m *Model) updateQuick() {
	h := m.opts.History
	if h == nil {
		return
	}
	byDir := map[string]envItem{}
	dirs := make([]string, 0, len(m.opts.Envs))
	for _, e := range m.opts.Envs {
		dirs = append(dirs, e.Dir)
		byDir[e.Dir] = envItem{env: e}
	}
	// remote environments of hosts that are not connected, shown dim
	known := append(slices.Collect(maps.Keys(h.Uses)), h.Pins...)
	for d, it := range m.offlineQuick(known) {
		if _, ok := byDir[d]; !ok {
			byDir[d] = it
			dirs = append(dirs, d)
		}
	}
	group := quickGroup
	if !m.opts.QuickAccess {
		group = pinnedGroup
	}
	var items []envItem
	for _, d := range h.Quick(dirs, quickSize, time.Now(), m.opts.QuickAccess) {
		it := byDir[d]
		it.pinned = h.Pinned(d)
		items = append(items, it)
	}
	m.list.setQuick(items, group)
}

// togglePin pins or unpins the selected environment.
func (m *Model) togglePin() {
	e, ok := m.selectedEnv()
	h := m.opts.History
	if !ok || h == nil {
		return
	}
	status := "unpinned " + e.Rel
	if h.TogglePin(e.Dir) {
		status = "pinned " + e.Rel
	}
	if err := h.Save(); err != nil {
		m.setStatus("saving history: "+err.Error(), true)
		return
	}
	before, _ := m.list.selectedItem()
	pos := m.list.quickIndex()
	m.updateQuick()
	// unpinned in the section: stay there, on the next one, so several can
	// be unpinned in a row
	if now, _ := m.list.selectedItem(); before.quick && !now.quick && m.list.quickCount() > 0 {
		m.list.selectQuick(pos)
	}
	m.setStatus(status, false)
}

func (m *Model) recordUse(e discover.Env) {
	h := m.opts.History
	if h == nil {
		return
	}
	h.Record(e.Dir, time.Now())
	if err := h.Save(); err != nil {
		m.setStatus("saving history: "+err.Error(), true)
	}
}

func (m Model) envAction(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		return m, m.startEdit(e, e.File)

	case key.Matches(msg, keys.EditEnv):
		return m, m.startEdit(e, envFile(e))

	case key.Matches(msg, keys.Start):
		a := args(upArgs...)
		return m, m.runAction(e, "up -d "+name, "docker compose up -d ("+name+")", a...)

	case key.Matches(msg, keys.Restart):
		a := args(restartArgs...)
		return m, m.runAction(e, "restarting "+name, "docker compose restart ("+name+")", a...)

	case key.Matches(msg, keys.Pull):
		a := args(pullArgs...)
		return m, m.runAction(e, "pulling "+name, "docker compose pull ("+name+")", a...)

	case key.Matches(msg, keys.Stop):
		m.confirm = &confirm{
			prompt: fmt.Sprintf("Run docker compose down for %s? (y/n)", e.Rel),
			onYes: func(m *Model) tea.Cmd {
				return m.runAction(e, "down "+e.Rel, "docker compose down ("+e.Rel+")", downArgs...)
			},
		}
		return m, nil

	case key.Matches(msg, keys.Recreate):
		a := args(recreateArgs...)
		target := name
		m.confirm = &confirm{
			prompt: fmt.Sprintf("Force recreate %s? (y/n)", target),
			onYes: func(m *Model) tea.Cmd {
				return m.runAction(e, "recreating "+target, "docker compose up -d --force-recreate ("+target+")", a...)
			},
		}
		return m, nil

	case key.Matches(msg, keys.Logs):
		if m.paneMode == modeCompose {
			m.setPaneMode(modeLogs) // fetching logs means wanting to see them
		}
		m.holdOutput = false
		return m, m.followLogs(e, svc)

	case key.Matches(msg, keys.Toggle):
		s := m.selectedService()
		if s == "" {
			m.setStatus("no service selected", true)
			return m, nil
		}
		name := e.Rel + " " + s
		if m.serviceRunning(s) {
			return m, m.runAction(e, "stopping "+name, "docker compose stop ("+name+")", "stop", s)
		}
		return m, m.runAction(e, "starting "+name, "docker compose up -d ("+name+")", append(slices.Clone(upArgs), s)...)

	case key.Matches(msg, keys.Folder):
		return m, openFolder(e)

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

// sampleStats starts a stats sample for the running containers of the
// selected env, unless one is already running.
func (m *Model) sampleStats() tea.Cmd {
	if m.loadingStats {
		return nil
	}
	var names []string
	for _, c := range m.containers {
		if c.Running() {
			names = append(names, c.Name)
		}
	}
	if len(names) == 0 {
		m.stats = nil
		return nil
	}
	e, ok := m.selectedEnv()
	if !ok || e.Dir != m.current {
		return nil
	}
	m.loadingStats = true
	return loadStats(e, names)
}

// QuitDir is the folder chosen with quit_to_folder, or "".
func (m Model) QuitDir() string { return m.quitDir }

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

var columnTitles = []string{"SERVICE", "STATE", "HEALTH", "CPU", "MEM", "PORTS", "IMAGE", "UPTIME"}

// columns sizes each column to its widest cell. When the table does not fit,
// IMAGE, then PORTS, then SERVICE give up space down to a minimum, and then
// the least important columns are hidden.
func columns(w int, rows []table.Row) []table.Column {
	limit := []int{24, 14, 9, 8, 9, 32, 40, 18}
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
	for _, c := range []struct{ col, min int }{{6, 10}, {5, 9}, {0, 8}} {
		if over <= 0 {
			break
		}
		cut := min(over, widths[c.col]-c.min)
		if cut > 0 {
			widths[c.col] -= cut
			over -= cut
		}
	}

	// still too wide: hide the least important columns; the table skips
	// columns with no width
	for _, col := range []int{3, 4, 2, 7, 5, 6} {
		if over <= 0 {
			break
		}
		over -= widths[col] + 2
		widths[col] = 0
	}

	// and as a last resort trim the widest column, STATE last
	for over > 0 {
		widest := 4
		for _, i := range []int{6, 5, 0, 7, 4, 3, 2, 1} {
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
			rows = append(rows, table.Row{n, "○ not created", "", "", "", "", "", ""})
			m.rowService = append(m.rowService, n)
			continue
		}
		for i, c := range cs {
			label := n
			if len(cs) > 1 {
				label = fmt.Sprintf("%s #%d", n, i+1)
			}
			if m.upd.avail[m.current][n] {
				label += " " + updateMark
			}
			health := c.Health
			if health == "" {
				health = "-"
			}
			st := m.stats[c.Name]
			rows = append(rows, table.Row{label, stateText(c), health, st.CPU, st.Mem, c.Ports(), compose.Short(c.Image), c.Uptime()})
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
	want := len(m.table.Rows()) + 4
	return max(min(want, body/2), min(7, body-3))
}

func (m Model) listHeight() int {
	h := m.bodyHeight() - 2
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
	sh := m.servicesHeight()
	logsH := m.bodyHeight() - sh
	if m.zoomed {
		rightW, sh, logsH = m.width, m.bodyHeight(), m.bodyHeight()
	}
	inner := rightW - 4

	m.table.SetColumns(columns(inner, m.table.Rows()))
	m.table.SetWidth(inner)
	m.table.SetHeight(max(sh-2, 2))

	m.logs.Width = inner
	m.logs.Height = max(logsH-2, 1)
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
	logsH := body - sh
	if m.zoomed {
		lw, rw, sh, logsH = m.width, m.width, body, body
	}
	zoomNote := func(title string) string {
		if m.zoomed {
			return title + " · zoomed, " + keys.Zoom.Help().Key + " to go back"
		}
		return title
	}

	listH := m.listHeight()
	envTitle := paneKey(keys.FocusEnvs) + "Environments"
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
		listBody += m.list.view(lw-4, listH, m.focus == focusEnvs, listView{counts: m.counts, hasUpdate: m.hasUpdate, offline: m.hostOffline})
	}
	left := pane(zoomNote(envTitle), listBody, lw, body, m.focus == focusEnvs)

	svcTitle := paneKey(keys.FocusServices) + "Services"
	if e, ok := m.selectedEnv(); ok {
		svcTitle += " · " + e.Rel
	}
	var svcBody string
	if len(m.table.Rows()) == 0 {
		svcBody = dimStyle.Render("no services")
	} else {
		svcBody = m.table.View()
	}
	services := pane(zoomNote(svcTitle), svcBody, rw, sh, m.focus == focusServices)

	logTitle := m.logTitle
	if m.stream != nil {
		logTitle += " (following)"
	}
	if s := m.search; s.active() {
		if len(s.matches) > 0 {
			logTitle += fmt.Sprintf(" · /%s %d/%d", s.query, s.current+1, len(s.matches))
		} else {
			logTitle += fmt.Sprintf(" · /%s no matches", s.query)
		}
	}
	var logs string
	if m.holdOutput {
		logs = pane(zoomNote(paneKey(keys.FocusLogs)+logTitle), m.logs.View(), rw, logsH, m.focus == focusLogs)
	} else {
		logs = paneStyled(m.logTabsTitle(zoomNote(logTitle), rw), m.logs.View(), rw, logsH, m.focus == focusLogs)
	}

	var main string
	switch {
	case !m.zoomed:
		main = lipgloss.JoinHorizontal(lipgloss.Top, left, lipgloss.JoinVertical(lipgloss.Left, services, logs))
	case m.focus == focusEnvs:
		main = left
	case m.focus == focusServices:
		main = services
	default:
		main = logs
	}

	return m.withMenu(m.withPrompt(lipgloss.JoinVertical(lipgloss.Left, header, main, m.statusView(), footer)))
}

func (m Model) headerView() string {
	var up, partial, down, problems, updates int
	for _, it := range m.list.items {
		if it.quick {
			continue
		}
		if m.hasUpdate(it.env.Dir) {
			updates++
		}
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
	if updates > 0 {
		right += "  " + accentStyle.Render(fmt.Sprintf("↑ %d with updates", updates))
	}
	left := accentStyle.Render("lazycompose") + "  "
	if m.newVersion != "" {
		hint := m.opts.UpdateHint
		if hint == "" {
			hint = "run lazycompose -update"
		}
		left += yellowStyle.Render(m.newVersion+" available, "+hint) + "  "
	}
	left += textStyle.Render(m.hostNames()) + "  "
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
	case m.searching:
		s = m.searchInput.View()
	case m.confirm != nil:
		s = m.confirmView()
	case m.busy > 0:
		s = m.spin.View() + " " + textStyle.Render(m.status)
	case m.statusErr:
		s = redStyle.Render(m.status)
	default:
		s = dimStyle.Render(m.status)
	}
	return " " + ansi.Truncate(s, m.width-2, "…")
}

// footerView points to the keys window and the action menu, which list
// everything else.
func (m Model) footerView() string {
	var parts []string
	for _, b := range footerItems() {
		parts = append(parts, accentStyle.Render(b.Help().Key)+" "+dimStyle.Render(b.Help().Desc))
	}
	return " " + ansi.Truncate(strings.Join(parts, dimStyle.Render(footerSep)), m.width-2, "…")
}

// paneKey is the "[1] " prefix of a pane title, showing the key that jumps to
// the pane, like lazygit.
func paneKey(b key.Binding) string {
	if !b.Enabled() || len(b.Keys()) == 0 {
		return ""
	}
	return "[" + b.Keys()[0] + "] "
}

// onStates takes in the container states of every host. A host that could
// not be read keeps what was known about it, and its error is shown once,
// until it changes. It reports true when this machine's docker failed,
// which leaves everything as it was, as before there were remote hosts.
func (m *Model) onStates(msg statesMsg) bool {
	if err := msg.errs[""]; err != nil {
		m.setStatus("docker: "+firstLine(err.Error()), true)
		return true
	}
	errs := map[string]string{}
	for k, err := range msg.errs {
		errs[k] = firstLine(err.Error())
		for dir, st := range m.states {
			if remote.HostOf(dir) == k {
				msg.states[dir] = st
			}
		}
		if m.hostErrs[k] != errs[k] {
			m.setStatus(errs[k], true)
		}
	}
	for k := range m.hostErrs {
		if _, still := errs[k]; !still {
			m.setStatus(strings.TrimPrefix(k, remote.Prefix)+" is reachable again", false)
		}
	}
	m.hostErrs = errs
	m.states = msg.states
	return false
}

// localOnly are the actions that do not work on remote environments yet.
var localOnly = map[string]bool{"quit_to_folder": true}

// notOnRemote says that the action of b cannot be used on a remote
// environment yet.
func notOnRemote(b key.Binding) string {
	return b.Help().Desc + " does not work on remote roots yet"
}

// hostNames are the machines shown in the header: this one when it has
// roots, then the remote hosts.
func (m Model) hostNames() string {
	names := make([]string, 0, len(m.hosts))
	for _, h := range m.hosts {
		if h == nil {
			names = append(names, m.opts.Hostname)
		} else {
			names = append(names, h.Label())
		}
	}
	if len(names) == 0 {
		return m.opts.Hostname
	}
	return strings.Join(names, ", ")
}

// cleanup removes the temporary files before quitting.
func (m *Model) cleanup() {
	m.endEdit()
	for _, f := range m.leftovers {
		_ = os.RemoveAll(f)
	}
	m.leftovers = nil
}

// mergeEnvs replaces the environments of the scanned roots in envs with
// found, and keeps those of other roots.
func mergeEnvs(envs, found []discover.Env, roots []string) []discover.Env {
	scanned := map[string]bool{}
	for _, r := range roots {
		scanned[rootKey(r)] = true
	}
	var out []discover.Env
	for _, e := range envs {
		if !scanned[e.Root] {
			out = append(out, e)
		}
	}
	return append(out, found...)
}

// closeAtStart closes the roots of hosts that are not connected, and the
// headings the history says were closed.
func (m *Model) closeAtStart() {
	for _, r := range m.opts.Roots {
		if h, _, err := remote.Parse(r); err == nil && !m.connected(h) {
			m.list.closed[rootKey(r)] = true
		}
	}
	if h := m.opts.History; h != nil {
		for _, k := range h.Closed {
			m.list.closed[k] = true
		}
	}
	m.list.rebuild()
	m.list.selectFirst()
}

// headingKey handles enter, space and the arrows in the list: they open
// and close headings, and enter connects an offline host.
func (m *Model) headingKey(k string) tea.Cmd {
	if it, ok := m.list.selectedItem(); ok {
		switch {
		case it.offline && (k == "enter" || k == " "):
			m.afterScan = it // selected again once the scan finds it
			return m.connect(it.env.Host)
		case k == "left":
			// close the heading it is under, and stay on that heading
			if hk, ok := m.list.headingOf(m.list.cursor); ok {
				open := false
				return tea.Batch(m.setHeading(hk, &open), m.onSelect())
			}
		}
		return nil
	}
	if _, ok := m.list.selectedHeading(); !ok {
		return nil
	}
	if k == "left" && m.list.closed[m.list.rows[m.list.cursor].key] {
		// already closed: close the root it is in
		if hk, ok := m.list.headingOf(m.list.cursor); ok {
			f := false
			return tea.Batch(m.setHeading(hk, &f), m.onSelect())
		}
		return nil
	}
	var open *bool
	switch k {
	case "right":
		t := true
		open = &t
	case "left":
		f := false
		open = &f
	}
	return tea.Batch(m.toggleHeading(open), m.onSelect())
}

// toggleHeading opens or closes the selected heading, or sets it to open
// when that is given. Opening the root of a host that is not connected
// connects it.
func (m *Model) toggleHeading(open *bool) tea.Cmd {
	r, ok := m.list.selectedHeading()
	if !ok {
		return nil
	}
	if r.kind == rowRoot {
		if h := m.list.roots[r.root].host; m.hostOffline(h) {
			if open != nil && !*open {
				return nil
			}
			return m.connect(h)
		}
	}
	want := m.list.closed[r.key]
	if open != nil {
		want = *open
	}
	return m.setHeading(r.key, &want)
}

// setHeading opens or closes the heading key and remembers it, except for
// remote roots, which always start closed.
func (m *Model) setHeading(key string, open *bool) tea.Cmd {
	m.list.setOpen(key, *open)
	m.list.selectKey(key)
	m.list.fixCursor() // an open heading is passed over: on to what is in it
	remoteRoot := remote.IsRemote(key) && !strings.Contains(key, "\x00")
	if h := m.opts.History; h != nil && !remoteRoot {
		h.Closed = slices.DeleteFunc(h.Closed, func(k string) bool { return k == key })
		if !*open {
			h.Closed = append(h.Closed, key)
		}
		if err := h.Save(); err != nil {
			m.setStatus("saving history: "+err.Error(), true)
		}
	}
	return nil
}
