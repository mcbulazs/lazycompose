package ui

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The docker compose arguments of the actions, shared by the actions
// themselves and the command shown for them in the menu.
var (
	upArgs       = []string{"up", "-d"}
	downArgs     = []string{"down"}
	restartArgs  = []string{"restart"}
	recreateArgs = []string{"up", "-d", "--force-recreate"}
	pullArgs     = []string{"pull"}
	logArgs      = []string{"logs", "-f", "--tail", "200"}
)

// menuInfo describes an action in the menu: what it does and extra words
// to find it by.
type menuInfo struct {
	desc  string
	words string
	nav   bool // moves around instead of doing something; only in the keys list
}

var menuInfos = map[string]menuInfo{
	"move_up":            {"Move up, or scroll the log pane", "previous", true},
	"move_down":          {"Move down, or scroll the log pane", "next", true},
	"next_pane":          {"Next pane", "switch focus", true},
	"prev_pane":          {"Previous pane", "switch focus", true},
	"focus_environments": {"Jump to the environments pane", "focus list", true},
	"focus_services":     {"Jump to the services pane", "focus containers", true},
	"focus_logs":         {"Jump to the logs pane", "focus output", true},
	"filter":             {"Filter environments, or search the logs", "find search", false},
	"problems":           {"Show only environments with problems", "errors unhealthy failed", false},
	"edit":               {"Edit the compose file", "open editor yaml", false},
	"edit_env":           {"Edit the .env file, created if missing", "variables environment secrets", false},
	"edit_config":        {"Edit the lazycompose config file", "settings options preferences configuration yaml", false},
	"new_env":            {"Create an environment", "add new folder stack", false},
	"move_env":           {"Move or rename the environment", "rename folder", false},
	"pin":                {"Pin to quick access, or unpin", "favourite favorite star top", false},
	"up":                 {"Start or update the containers", "start create run", false},
	"down":               {"Stop and remove the containers (asks first)", "stop remove", false},
	"stop_start":         {"Stop the selected service, or start it", "toggle", false},
	"restart":            {"Restart the containers", "reboot", false},
	"recreate":           {"Force recreate the containers (asks first)", "force", false},
	"pull":               {"Pull newer images", "download update", false},
	"update_all":         {"Update every running environment (asks first)", "upgrade everything", false},
	"clean_images":       {"Remove old images that no container uses (asks first)", "prune clean disk space untagged dangling delete", false},
	"zoom":               {"Make the focused pane full screen, or back", "fullscreen maximize enlarge", false},
	"compose_view":       {"Show the compose file in the log pane, or the logs again", "view preview yaml file config", false},
	"logs":               {"Fetch the logs again", "output follow", false},
	"open_logs":          {"Open the log pane's text in your editor, to search or copy it", "copy select clipboard export save view pager", false},
	"shell":              {"Open a shell in the selected service", "exec bash sh terminal container", false},
	"folder_shell":       {"Open your shell in the environment's folder", "terminal cd tmux", false},
	"refresh":            {"Refresh and rescan folders", "reload scan", false},
	"commands":           {"Search and run an action", "palette menu", true},
	"help":               {"Search all keys", "keys help", true},
	"quit":               {"Quit", "exit close", false},
	"quit_to_folder":     {"Quit and cd into the environment's folder", "exit cd", false},
}

// fixedEntries are keys that cannot be rebound, listed in the keys window.
var fixedEntries = []menuEntry{
	{keys: "/", desc: "In the log pane: search the logs"},
	{keys: "n / N", desc: "While searching the logs: older / newer match"},
	{keys: "g / G", desc: "In the log pane: jump to the top / bottom"},
	{keys: "pgup / pgdown", desc: "Scroll a page"},
	{keys: "y / n", desc: "Answer a question"},
	{keys: "enter", desc: "Confirm input"},
	{keys: "esc", desc: "Cancel or clear: a filter, a search, a running update all"},
	{keys: "ctrl+c", desc: "Quit"},
	{keys: "shift+drag", desc: "Select text while the mouse is on"},
}

type menuEntry struct {
	name    string // action name, "" for fixed keys
	keys    string
	desc    string
	words   string
	short   string // the command, without the compose file
	command string // the exact command, "" if it runs none
	why     string // why it cannot run now
	key     tea.KeyMsg
	run     bool // can be run from the menu
}

// actionMenu is the window opened with : (actions to run) or ? (all keys).
type actionMenu struct {
	all     bool
	title   string
	input   textinput.Model
	entries []menuEntry
	shown   []int
	cursor  int
	offset  int
}

func (m *Model) openMenu(all bool) tea.Cmd {
	in := textinput.New()
	in.Prompt = "> "
	in.PromptStyle = accentStyle
	in.TextStyle = textStyle
	in.Cursor.Style = textStyle
	in.Placeholder = "type to search"
	title := "Keys"
	if !all {
		title = "Run"
		if e, svc, ok := m.target(); ok {
			title += " on " + e.Rel
			if svc != "" {
				title += " " + svc
			}
		}
	}
	m.menu = &actionMenu{all: all, title: title, input: in, entries: m.menuEntries(all)}
	m.menu.filter()
	return m.menu.input.Focus()
}

// menuEntries lists the actions, the ones used recently first, with the
// command each runs on the current selection.
func (m Model) menuEntries(all bool) []menuEntry {
	var out []menuEntry
	for _, a := range actions {
		info := menuInfos[a.name]
		b := a.get(&keys)
		if !all && (info.nav || !b.Enabled()) {
			continue
		}
		en := menuEntry{name: a.name, desc: info.desc, words: a.name + " " + a.help + " " + info.words}
		if b.Enabled() {
			en.keys = b.Help().Key
			if k, ok := keyMsg(b.Keys()[0]); ok && !info.nav {
				en.key, en.run = k, true
			}
		} else {
			en.keys = "-"
			en.why = "no key, it was turned off in the config"
		}
		if info.nav {
			en.run = false
		}
		en.short, en.command, en.why = m.commandFor(a.name, en.why)
		if en.why != "" {
			en.run = false
		}
		out = append(out, en)
	}
	if !all {
		slices.SortStableFunc(out, func(a, b menuEntry) int {
			return recentRank(m.recent, a.name) - recentRank(m.recent, b.name)
		})
	} else {
		out = append(out, fixedEntries...)
	}
	return out
}

// recentRank is the place of name in recent, or past the end if it is not
// there.
func recentRank(recent []string, name string) int {
	if i := slices.Index(recent, name); i >= 0 {
		return i
	}
	return len(recent)
}

// commandFor returns the command an action runs on the current selection,
// short and in full, or why it cannot run.
func (m Model) commandFor(name, why string) (short, full, reason string) {
	switch name {
	case "edit_config":
		return m.configCommand(why)
	case "open_logs":
		return m.logsCommand(why)
	}
	e, svc, ok := m.target()
	needsEnv := !slices.Contains([]string{"filter", "problems", "new_env", "update_all", "clean_images", "zoom", "compose_view", "refresh", "quit", "commands", "help"}, name) && !menuInfos[name].nav
	if !ok {
		if needsEnv && why == "" {
			why = "no environment selected"
		}
		return "", "", why
	}
	compose := func(args ...string) (string, string, string) {
		if svc != "" {
			args = append(slices.Clone(args), svc)
		}
		return "docker compose " + shellJoin(args), shellJoin(project(e).Command(context.Background(), args...).Args), why
	}
	service := func() (string, bool) {
		s := m.selectedService()
		if s == "" && why == "" {
			why = "no service selected"
		}
		return s, s != ""
	}
	switch name {
	case "edit", "edit_env":
		file := e.File
		if name == "edit_env" {
			file = envFile(e)
		}
		cmd, err := editorCommand(m.opts.Editor, file)
		if err != nil {
			return "", "", err.Error()
		}
		full := shellJoin(cmd.Args)
		return shellJoin(append(cmd.Args[:len(cmd.Args)-1:len(cmd.Args)-1], filepath.Base(file))), full, why
	case "up":
		return compose(upArgs...)
	case "restart":
		return compose(restartArgs...)
	case "recreate":
		return compose(recreateArgs...)
	case "pull":
		return compose(pullArgs...)
	case "logs":
		return compose(logArgs...)
	case "down":
		svc = "" // down always works on the whole environment
		return compose(downArgs...)
	case "stop_start":
		s, ok := service()
		if !ok {
			return "", "", why
		}
		svc = ""
		if m.serviceRunning(s) {
			return compose("stop", s)
		}
		return compose(append(slices.Clone(upArgs), s)...)
	case "shell":
		s, ok := service()
		if !ok {
			return "", "", why
		}
		if !m.serviceRunning(s) && why == "" {
			why = s + " is not running"
		}
		svc = ""
		short, full, why := compose("exec", s, "bash")
		return short + " (or sh)", full + " (or sh)", why
	case "folder_shell":
		if os.Getenv("TMUX") != "" {
			return "tmux new-window in the folder", shellJoin(tmuxNewWindow(e.Dir, e.Name()).Args), why
		}
		sh := userShell()
		return sh + " (in the folder)", shellJoin([]string{sh}) + " in " + shellQuote(e.Dir), why
	case "update_all":
		return "docker compose pull -q, up -d (each running env)", "for each running environment: docker compose -f FILE pull -q, then docker compose -f FILE up -d", why
	case "clean_images":
		return "docker image rm (old images)", "docker image rm, after listing the untagged images no container uses", why
	case "move_env":
		return "", "", why
	}
	return "", "", why
}

// shellJoin writes args as a shell would need them, for showing only.
func shellJoin(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = shellQuote(a)
	}
	return strings.Join(out, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("-_./:=@+,%", r)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	if !strings.ContainsFunc(s, func(r rune) bool { return !strconv.IsPrint(r) }) {
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	// control characters in a folder name must not reach the terminal
	return "$" + strconv.Quote(s)
}

// keyMsg builds the key press a binding like "ctrl+r" or "R" stands for.
func keyMsg(s string) (tea.KeyMsg, bool) {
	if r := []rune(s); len(r) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: r}, true
	}
	t, ok := keyTypes[s]
	return tea.KeyMsg{Type: t}, ok
}

// keyTypes maps key names like "ctrl+r" and "tab" to their key type.
var keyTypes = func() map[string]tea.KeyType {
	out := map[string]tea.KeyType{}
	for t := tea.KeyType(-200); t <= 200; t++ {
		if t == tea.KeyRunes {
			continue
		}
		if name := (tea.Key{Type: t}).String(); name != "" && !strings.HasPrefix(name, "%!") {
			if _, seen := out[name]; !seen {
				out[name] = t
			}
		}
	}
	return out
}()

// filter shows the entries matching the typed text, best matches first.
func (mn *actionMenu) filter() {
	raw := strings.TrimSpace(mn.input.Value())
	type hit struct{ idx, rank int }
	var hits []hit
	for i, en := range mn.entries {
		if r, ok := matchRank(raw, en); ok {
			hits = append(hits, hit{i, r})
		}
	}
	slices.SortStableFunc(hits, func(a, b hit) int { return a.rank - b.rank })
	mn.shown = mn.shown[:0]
	for _, h := range hits {
		mn.shown = append(mn.shown, h.idx)
	}
	mn.cursor, mn.offset = 0, 0
}

// matchRank ranks how well raw matches an entry, lower is better: its key
// exactly, its key in any case, the start of a word, a part of the text,
// and the letters in order. One letter only matches keys and word starts.
func matchRank(raw string, en menuEntry) (int, bool) {
	if raw == "" {
		return 0, true
	}
	q := strings.ToLower(raw)
	keyList := strings.FieldsFunc(en.keys, func(r rune) bool { return r == '/' || r == ' ' })
	if slices.Contains(keyList, raw) {
		return 0, true
	}
	for _, k := range keyList {
		if strings.EqualFold(k, raw) {
			return 1, true
		}
	}
	desc := strings.ToLower(en.desc)
	text := desc + " " + strings.ToLower(en.words)
	for _, w := range strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && r != '.' }) {
		if strings.HasPrefix(w, q) {
			return 2, true
		}
	}
	if len([]rune(q)) < 2 {
		return 0, false
	}
	if strings.Contains(text+" "+strings.ToLower(en.short), q) {
		return 3, true
	}
	if len([]rune(q)) >= 3 && subsequence(q, desc) {
		return 4, true
	}
	return 0, false
}

func subsequence(q, s string) bool {
	i := 0
	for _, r := range s {
		if i < len(q) && r == rune(q[i]) {
			i++
		}
	}
	return i == len(q)
}

func (m Model) handleMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	mn := m.menu
	// by type, since typed text like "up" has the same name as a key
	switch msg.Type {
	case tea.KeyEsc:
		m.menu = nil
		return m, nil
	case tea.KeyUp, tea.KeyCtrlP, tea.KeyCtrlK:
		mn.cursor = max(0, mn.cursor-1)
		return m, nil
	case tea.KeyDown, tea.KeyCtrlN, tea.KeyCtrlJ:
		mn.cursor = max(0, min(len(mn.shown)-1, mn.cursor+1))
		return m, nil
	case tea.KeyPgUp:
		mn.cursor = max(0, mn.cursor-m.menuRows())
		return m, nil
	case tea.KeyPgDown:
		mn.cursor = max(0, min(len(mn.shown)-1, mn.cursor+m.menuRows()))
		return m, nil
	case tea.KeyEnter:
		return m.runMenuEntry()
	}
	// the key that opened the window closes it again
	if mn.input.Value() == "" && (mn.all && key.Matches(msg, keys.Help) || !mn.all && key.Matches(msg, keys.Commands)) {
		m.menu = nil
		return m, nil
	}
	var cmd tea.Cmd
	before := mn.input.Value()
	mn.input, cmd = mn.input.Update(msg)
	if mn.input.Value() != before {
		mn.filter()
	}
	return m, cmd
}

// runMenuEntry runs the selected entry and closes the menu.
func (m Model) runMenuEntry() (tea.Model, tea.Cmd) {
	mn := m.menu
	if mn.cursor >= len(mn.shown) {
		return m, nil
	}
	en := mn.entries[mn.shown[mn.cursor]]
	if !en.run {
		if en.why != "" {
			m.setStatus(en.why, true)
		}
		return m, nil
	}
	m.menu = nil
	m.recent = append([]string{en.name}, slices.DeleteFunc(m.recent, func(s string) bool { return s == en.name })...)
	if en.name == "new_env" {
		// its key is also the next match while searching the logs
		return m, m.startCreate()
	}
	return m.handleKey(en.key)
}

// menuBox is where the window is on screen: its top left corner and size.
func (m Model) menuBox() (x, y, w, h int) {
	w, h = m.menuSize()
	return (m.width - w) / 2, max((m.height-h)/2, 1), w, h
}

// handleMenuMouse scrolls with the wheel, selects an entry on click and
// runs it on a second click. A click outside closes the window.
func (m Model) handleMenuMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}
	mn := m.menu
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		mn.cursor = max(0, mn.cursor-1)
	case tea.MouseButtonWheelDown:
		mn.cursor = max(0, min(len(mn.shown)-1, mn.cursor+1))
	case tea.MouseButtonLeft:
		x, y, w, h := m.menuBox()
		if msg.X < x || msg.X >= x+w || msg.Y < y || msg.Y >= y+h {
			m.menu = nil
			return m, nil
		}
		// the top border, the search line and a blank line, then the entries
		row := msg.Y - y - 3
		if row < 0 || row >= m.menuRows() {
			return m, nil
		}
		i := mn.offset + row
		if i >= len(mn.shown) {
			return m, nil
		}
		if i == mn.cursor {
			return m.runMenuEntry()
		}
		mn.cursor = i
	}
	return m, nil
}

// menuSize is the width and height of the window.
func (m Model) menuSize() (int, int) {
	return min(max(m.width*4/5, 50), m.width-2, 110), max(min(m.height-4, 30), 8)
}

// detailLines is the room for the selected entry's command under the list.
const detailLines = 3

// menuRows is how many entries fit in the window.
func (m Model) menuRows() int {
	_, h := m.menuSize()
	return h - 4 - detailLines // borders, input, blank line, and the details
}

func (m Model) menuView() string {
	mn := m.menu
	w, h := m.menuSize()
	inner := w - 4
	rows := m.menuRows()
	if mn.cursor < mn.offset {
		mn.offset = mn.cursor
	}
	if mn.cursor >= mn.offset+rows {
		mn.offset = mn.cursor - rows + 1
	}

	keyW := 6
	for _, en := range mn.entries {
		keyW = max(keyW, lipgloss.Width(en.keys))
	}
	keyW = min(keyW, 14)
	descW := 0
	for _, en := range mn.entries {
		descW = max(descW, lipgloss.Width(en.desc))
	}
	descW = min(descW, max(inner-keyW-2-32, 20))
	cmdW := inner - keyW - 2 - descW - 2

	lines := []string{mn.input.View(), ""}
	if len(mn.shown) == 0 {
		lines = append(lines, dimStyle.Render("nothing matches"))
	}
	for i := mn.offset; i < len(mn.shown) && i < mn.offset+rows; i++ {
		en := mn.entries[mn.shown[i]]
		k := ansi.Truncate(en.keys, keyW, "…")
		k += strings.Repeat(" ", keyW-lipgloss.Width(k))
		d := ansi.Truncate(en.desc, descW, "…")
		d += strings.Repeat(" ", descW-lipgloss.Width(d))
		c := ""
		if cmdW > 8 {
			c = ansi.Truncate(en.short, cmdW, "…")
		}
		c += strings.Repeat(" ", max(0, cmdW-lipgloss.Width(c)))
		if i == mn.cursor {
			lines = append(lines, selectedRow.Render(k+"  "+d+"  "+c))
			continue
		}
		ks, ds := accentStyle, textStyle
		if !en.run && !mn.all {
			ks, ds = dimStyle, dimStyle
		}
		lines = append(lines, ks.Render(k)+"  "+ds.Render(d)+"  "+dimStyle.Render(c))
	}

	// the selected entry in full: the exact command, or why it cannot run
	detail := dimStyle.Render("↑/↓ move · enter run · esc close")
	if mn.cursor < len(mn.shown) {
		en := mn.entries[mn.shown[mn.cursor]]
		switch {
		case en.why != "":
			detail = yellowStyle.Render(ansi.Truncate(en.why, inner, "…"))
		case en.command != "":
			wrapped := strings.Split(ansi.Hardwrap("runs "+en.command, inner, false), "\n")
			if len(wrapped) > detailLines {
				wrapped = wrapped[:detailLines]
				wrapped[detailLines-1] = ansi.Truncate(wrapped[detailLines-1], inner-1, "") + "…"
			}
			detail = dimStyle.Render(strings.Join(wrapped, "\n"))
		case !en.run:
			detail = dimStyle.Render("press the key itself")
		}
	}
	for len(lines) < rows+2+detailLines-lipgloss.Height(detail) {
		lines = append(lines, "")
	}
	lines = append(lines, detail)
	return pane(mn.title, strings.Join(lines, "\n"), w, h, true)
}

func (m Model) withMenu(view string) string {
	if m.menu == nil {
		return view
	}
	x, y, _, _ := m.menuBox()
	return overlay(view, m.menuView(), x, y)
}

// logsCommand is the editor command for the log pane's text, which is
// written to a temporary file first.
func (m Model) logsCommand(why string) (short, full, reason string) {
	if len(m.logLines) == 0 {
		if why == "" {
			why = "the log pane is empty"
		}
		return "", "", why
	}
	cmd, err := editorCommand(m.opts.Editor, logsFilePattern)
	if err != nil {
		return "", "", err.Error()
	}
	full = shellJoin(cmd.Args)
	return full, full, why
}

// configCommand is the editor command for the config file, shown with the
// full path so the menu also tells where the config lives.
func (m Model) configCommand(why string) (short, full, reason string) {
	if m.opts.ConfigPath == "" {
		if why == "" {
			why = "no config file, lazycompose was started with -config \"\""
		}
		return "", "", why
	}
	cmd, err := editorCommand(m.opts.Editor, m.opts.ConfigPath)
	if err != nil {
		return "", "", err.Error()
	}
	full = shellJoin(cmd.Args)
	return full, full, why
}
