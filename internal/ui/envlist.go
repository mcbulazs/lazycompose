package ui

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mcbulazs/lazycompose/internal/discover"
	"github.com/mcbulazs/lazycompose/internal/remote"
)

type envStatus int

const (
	statusDown envStatus = iota
	statusPartial
	statusUp
)

type envItem struct {
	env    discover.Env
	group  string // parent folder relative to the root, "" for top level
	quick  bool   // a copy shown in the quick access section
	pinned bool   // pinned, shown with a star in the quick access section
	// offline: a quick access entry of a remote host that is not
	// connected yet, known from the history; shown dim, enter connects
	offline bool
}

const (
	quickGroup  = "quick access"
	pinnedGroup = "pinned" // the section when quick access is off
	pinMark     = "★"
	openMark    = "▾"
	closedMark  = "▸"
)

// listRoot is a root, as a heading of the list.
type listRoot struct {
	key   string // the root as environments name it: an absolute folder, or ssh://host/folder
	label string // its folder name, with the host in front for a remote one
	host  *remote.Host
}

type rowKind int

const (
	rowEnv   rowKind = iota
	rowGroup         // a folder heading, can be opened and closed
	rowRoot          // a root heading, when there are several roots
	rowQuick         // the quick access heading
	rowBlank         // after the quick access section
)

type listRow struct {
	kind  rowKind
	item  int    // index into items, for rowEnv
	key   string // what a heading opens and closes: see groupKey
	label string // the heading
	root  int    // index into roots, for rowRoot
	depth int    // how far it is indented
}

// envList is the left pane: environments under their root and parent
// folder. Root and folder headings can be closed; the cursor stops on them
// as on environments.
type envList struct {
	items  []envItem
	roots  []listRoot // in config order
	multi  bool       // several roots: each gets a heading
	rows   []listRow  // what is shown, see rebuild
	cursor int        // index into rows
	offset int        // first rendered row
	query  string
	only   map[string]bool // when set, only these env dirs are shown
	closed map[string]bool // closed headings, by key
	quickN int             // quick access rows that are shown
}

// groupLess orders folders as a tree: a folder's own environments (the
// shorter path) before its subfolders, and names compared a folder level
// at a time.
func groupLess(a, b string) bool {
	if a == "" || b == "" {
		return a == "" && b != ""
	}
	pa, pb := strings.Split(a, "/"), strings.Split(b, "/")
	for i := range min(len(pa), len(pb)) {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return len(pa) < len(pb)
}

// groupKey names a folder heading: its root and the folder.
func groupKey(root, group string) string { return root + "\x00" + group }

// rootKey is a root as environments name it in Env.Root.
func rootKey(r string) string {
	if remote.IsRemote(r) {
		if h, dir, err := remote.Parse(r); err == nil {
			return h.Key(dir)
		}
		return r
	}
	if abs, err := filepath.Abs(r); err == nil {
		return abs
	}
	return r
}

// listRoots turns the configured roots into headings, in config order.
func listRoots(roots []string) []listRoot {
	var out []listRoot
	seen := map[string]bool{}
	for _, r := range roots {
		k := rootKey(r)
		if seen[k] {
			continue
		}
		seen[k] = true
		lr := listRoot{key: k, label: filepath.Base(k)}
		if h, dir, err := remote.Parse(r); err == nil {
			lr.host = h
			lr.label = h.Label() + ":" + path.Base(dir)
		}
		out = append(out, lr)
	}
	return out
}

func newEnvList(envs []discover.Env, roots []string) envList {
	l := envList{roots: listRoots(roots), closed: map[string]bool{}}
	l.multi = len(l.roots) > 1
	l.setEnvs(envs)
	return l
}

// setEnvs replaces the environments, keeping the quick access section,
// what is closed and the selection.
func (l *envList) setEnvs(envs []discover.Env) {
	sel := l.selection()
	order := map[string]int{}
	for i, r := range l.roots {
		order[r.key] = i
	}
	known := func(root string) int {
		if i, ok := order[root]; ok {
			return i
		}
		// a root that is not configured, as in tests: after the others
		l.roots = append(l.roots, listRoot{key: root, label: filepath.Base(root)})
		order[root] = len(l.roots) - 1
		return order[root]
	}
	var items []envItem
	for _, it := range l.items {
		if it.quick {
			items = append(items, it)
		}
	}
	rest := make([]envItem, len(envs))
	for i, e := range envs {
		known(e.Root)
		rest[i] = envItem{env: e, group: e.Group()}
	}
	// roots in config order, top level environments first, then each group
	sort.SliceStable(rest, func(i, j int) bool {
		a, b := rest[i], rest[j]
		if ra, rb := order[a.env.Root], order[b.env.Root]; ra != rb {
			return ra < rb
		}
		if a.group != b.group {
			return groupLess(a.group, b.group)
		}
		return a.env.Name() < b.env.Name()
	})
	l.items = append(items, rest...)
	l.multi = len(l.roots) > 1
	l.rebuildAt(sel)
}

// setQuick shows copies of envs, in order, in a section called group at
// the top of the list. The dirs in pinned get a star; offline ones are
// remote environments that are not connected yet.
func (l *envList) setQuick(envs []envItem, group string) {
	sel := l.selection()
	var items []envItem
	for _, it := range envs {
		it.quick, it.group = true, group
		items = append(items, it)
	}
	for _, it := range l.items {
		if !it.quick {
			items = append(items, it)
		}
	}
	l.items = items
	l.rebuildAt(sel)
}

func (l *envList) setFilter(q string) {
	l.query = q
	l.rebuild()
	l.offset = 0
}

// filtering reports whether a filter shows only some environments; closed
// headings then show what matches inside them.
func (l envList) filtering() bool { return l.query != "" || l.only != nil }

func (l envList) matches(it envItem) bool {
	if l.only != nil && !l.only[it.env.Dir] {
		return false
	}
	// the section would only repeat matches while filtering
	if it.quick && l.filtering() {
		return false
	}
	q := strings.ToLower(l.query)
	g := it.group
	if l.multi && !it.quick {
		g = l.rootLabel(it.env.Root) + "/" + g
	}
	return q == "" || strings.Contains(strings.ToLower(g+"/"+it.env.Name()), q)
}

func (l envList) rootLabel(key string) string {
	for _, r := range l.roots {
		if r.key == key {
			return r.label
		}
	}
	return filepath.Base(key)
}

// selection is what is selected, kept over a rebuild: the row, and the
// environment when it is one.
type selection struct {
	ok  bool
	row listRow
	env envItem
}

func (l envList) selection() selection {
	r, ok := l.selectedRow()
	s := selection{ok: ok, row: r}
	if ok && r.kind == rowEnv {
		s.env = l.items[r.item]
	}
	return s
}

// rebuild lays out the rows and keeps the selection on the same row: the
// same environment, or the heading that now hides it.
func (l *envList) rebuild() { l.rebuildAt(l.selection()) }

func (l *envList) rebuildAt(sel selection) {
	prev, hadPrev, prevEnv := sel.row, sel.ok, sel.env

	l.rows = l.rows[:0]
	l.quickN = 0
	for i, it := range l.items {
		if !it.quick || !l.matches(it) {
			continue
		}
		if l.quickN == 0 {
			l.rows = append(l.rows, listRow{kind: rowQuick, label: it.group})
		}
		l.rows = append(l.rows, listRow{kind: rowEnv, item: i, depth: 1})
		l.quickN++
	}
	if l.quickN > 0 {
		l.rows = append(l.rows, listRow{kind: rowBlank})
	}

	for ri, r := range l.roots {
		var idx []int
		for i, it := range l.items {
			if !it.quick && it.env.Root == r.key && l.matches(it) {
				idx = append(idx, i)
			}
		}
		if l.filtering() && len(idx) == 0 {
			continue
		}
		depth := 0
		if l.multi {
			l.rows = append(l.rows, listRow{kind: rowRoot, key: r.key, label: r.label, root: ri})
			if l.closed[r.key] && !l.filtering() {
				continue
			}
			depth = 1
		}
		// a tree of folders: each level its own heading under its parent,
		// a folder's environments before its subfolders
		var shown []string // the folder path of the last row, by level
		for _, i := range idx {
			it := l.items[i]
			var parts []string
			if it.group != "" {
				parts = strings.Split(it.group, "/")
			}
			same := 0
			for same < len(parts) && same < len(shown) && parts[same] == shown[same] {
				same++
			}
			shown = parts
			hidden := false
			for lvl := range parts {
				k := groupKey(r.key, strings.Join(parts[:lvl+1], "/"))
				if lvl >= same && !hidden {
					l.rows = append(l.rows, listRow{kind: rowGroup, key: k, label: parts[lvl], depth: depth + lvl})
				}
				hidden = hidden || l.closed[k] && !l.filtering()
			}
			if !hidden {
				l.rows = append(l.rows, listRow{kind: rowEnv, item: i, depth: depth + len(parts)})
			}
		}
	}

	l.cursor = 0
	if hadPrev {
		switch {
		case prev.kind == rowEnv && l.selectItem(prevEnv):
		case prev.kind == rowEnv:
			// a heading above it was closed: stay on the nearest one shown
			l.selectNearest(prevEnv.env.Root, prevEnv.group)
		case prev.kind == rowGroup && !l.selectKey(prev.key):
			root, folder, _ := strings.Cut(prev.key, "\x00")
			l.selectNearest(root, folder)
		default:
			l.selectKey(prev.key)
		}
	}
	l.fixCursor()
}

// fixCursor moves the cursor off rows that cannot be selected.
func (l *envList) fixCursor() {
	if len(l.rows) == 0 {
		l.cursor = 0
		return
	}
	l.cursor = max(0, min(l.cursor, len(l.rows)-1))
	if l.selectable(l.cursor) {
		return
	}
	for i := l.cursor; i < len(l.rows); i++ {
		if l.selectable(i) {
			l.cursor = i
			return
		}
	}
	for i := l.cursor; i >= 0; i-- {
		if l.selectable(i) {
			l.cursor = i
			return
		}
	}
}

// clickable reports whether a click selects row i: environments and all
// headings.
func (l envList) clickable(i int) bool {
	k := l.rows[i].kind
	return k == rowEnv || k == rowGroup || k == rowRoot
}

// selectable reports whether the cursor stops on row i: environments and
// headings, which enter and space open and close. While filtering the
// headings only label what matches.
func (l envList) selectable(i int) bool {
	switch l.rows[i].kind {
	case rowEnv:
		return true
	case rowGroup, rowRoot:
		return !l.filtering()
	}
	return false
}

// selectItem moves the cursor to it, or to another copy of its env, and
// reports whether it is shown.
func (l *envList) selectItem(it envItem) bool {
	found := false
	for i, r := range l.rows {
		if r.kind != rowEnv {
			continue
		}
		c := l.items[r.item]
		if c.env.Dir != it.env.Dir {
			continue
		}
		if c.quick == it.quick {
			l.cursor = i
			return true
		}
		if !found {
			l.cursor = i
			found = true
		}
	}
	return found
}

// selectDir selects the environment in dir, outside quick access.
func (l *envList) selectDir(dir string) {
	for i, r := range l.rows {
		if r.kind == rowEnv && !l.items[r.item].quick && l.items[r.item].env.Dir == dir {
			l.cursor = i
			return
		}
	}
}

// selectFirst selects the first environment, the top of quick access.
func (l *envList) selectFirst() {
	for i, r := range l.rows {
		if r.kind == rowEnv {
			l.cursor = i
			return
		}
	}
	l.fixCursor()
}

// selectQuick selects the n-th quick access entry, or the last one.
func (l *envList) selectQuick(n int) {
	seen := -1
	for i, r := range l.rows {
		if r.kind == rowEnv && l.items[r.item].quick {
			seen++
			l.cursor = i
			if seen == n {
				return
			}
		}
	}
}

// selectNearest selects the deepest heading shown of folder in root, or
// the root.
func (l *envList) selectNearest(root, folder string) {
	for f := folder; f != ""; {
		if l.selectKey(groupKey(root, f)) {
			return
		}
		if i := strings.LastIndex(f, "/"); i >= 0 {
			f = f[:i]
		} else {
			f = ""
		}
	}
	l.selectKey(root)
}

// selectKey selects the heading key, and reports whether it is shown.
func (l *envList) selectKey(key string) bool {
	for i, r := range l.rows {
		if (r.kind == rowGroup || r.kind == rowRoot) && r.key == key {
			l.cursor = i
			return true
		}
	}
	return false
}

// quickIndex is the position of the cursor in quick access, or -1.
func (l envList) quickIndex() int {
	n := -1
	for i, r := range l.rows {
		if r.kind == rowEnv && l.items[r.item].quick {
			n++
			if i == l.cursor {
				return n
			}
		}
	}
	return -1
}

func (l envList) selectedRow() (listRow, bool) {
	if l.cursor < 0 || l.cursor >= len(l.rows) {
		return listRow{}, false
	}
	return l.rows[l.cursor], true
}

func (l envList) selectedItem() (envItem, bool) {
	r, ok := l.selectedRow()
	if !ok || r.kind != rowEnv {
		return envItem{}, false
	}
	return l.items[r.item], true
}

// selected is the selected environment; not one that is offline.
func (l envList) selected() (discover.Env, bool) {
	it, ok := l.selectedItem()
	if !ok || it.offline {
		return discover.Env{}, false
	}
	return it.env, true
}

// selectedHeading is the key of the selected heading.
func (l envList) selectedHeading() (listRow, bool) {
	r, ok := l.selectedRow()
	return r, ok && (r.kind == rowGroup || r.kind == rowRoot)
}

// move goes n rows down, or up for a negative n, skipping what cannot be
// selected.
func (l *envList) move(n int) {
	step := 1
	if n < 0 {
		step, n = -1, -n
	}
	for ; n > 0; n-- {
		for i := l.cursor + step; i >= 0 && i < len(l.rows); i += step {
			if l.selectable(i) {
				l.cursor = i
				break
			}
		}
	}
}

// setOpen opens or closes the heading key.
func (l *envList) setOpen(key string, open bool) {
	if open {
		delete(l.closed, key)
	} else {
		l.closed[key] = true
	}
	l.rebuild()
}

// headingOf is the heading row i is under: the folder or root of an
// environment, the root of a folder heading.
func (l envList) headingOf(i int) (string, bool) {
	r := l.rows[i]
	switch {
	case r.kind == rowGroup:
		root, folder, _ := strings.Cut(r.key, "\x00")
		if i := strings.LastIndex(folder, "/"); i >= 0 {
			return groupKey(root, folder[:i]), true
		}
		return root, l.multi
	case r.kind != rowEnv || l.items[r.item].quick:
		return "", false
	}
	it := l.items[r.item]
	if it.group != "" {
		return groupKey(it.env.Root, it.group), true
	}
	if l.multi {
		return it.env.Root, true
	}
	return "", false
}

// rowEnvs are the environments under the heading row r, for its summary.
func (l envList) rowEnvs(r listRow) []discover.Env {
	var out []discover.Env
	for _, it := range l.items {
		if it.quick {
			continue
		}
		g := groupKey(it.env.Root, it.group)
		if r.kind == rowRoot && it.env.Root == r.key || r.kind == rowGroup && (g == r.key || strings.HasPrefix(g, r.key+"/")) {
			out = append(out, it.env)
		}
	}
	return out
}

// quickCount is how many quick access entries are shown; they come first.
func (l envList) quickCount() int { return l.quickN }

// scroll keeps the cursor inside a window of h rows, with the heading
// above it when there is room.
func (l *envList) scroll(h int) {
	if len(l.rows) == 0 {
		l.offset = 0
		return
	}
	top := l.cursor
	if top > 0 && !l.selectable(top-1) && l.rows[top-1].kind == rowQuick {
		top--
	}
	if top < l.offset {
		l.offset = top
	}
	if l.cursor >= l.offset+h {
		l.offset = l.cursor - h + 1
	}
	l.offset = max(0, min(l.offset, max(0, len(l.rows)-h)))
}

type countFunc func(discover.Env) (running, total, problems int)

func statusOf(running, total int) envStatus {
	switch {
	case running == 0:
		return statusDown
	case running < total:
		return statusPartial
	default:
		return statusUp
	}
}

func dot(s envStatus, problems int) string {
	if problems > 0 {
		symbol := map[envStatus]string{statusUp: "●", statusPartial: "◐", statusDown: "○"}[s]
		return redStyle.Render(symbol)
	}
	switch s {
	case statusUp:
		return greenStyle.Render("●")
	case statusPartial:
		return yellowStyle.Render("◐")
	default:
		return dimStyle.Render("○")
	}
}

// listView is what the list needs from the model to draw itself.
type listView struct {
	counts    countFunc
	hasUpdate func(dir string) bool
	// offline reports whether a remote root's host is not connected yet
	offline func(h *remote.Host) bool
}

func (l envList) view(w, h int, focused bool, lv listView) string {
	if len(l.rows) == 0 {
		if l.query != "" {
			return dimStyle.Render("no matches")
		}
		return dimStyle.Render("no environments found")
	}

	var b strings.Builder
	for i := l.offset; i < len(l.rows) && i < l.offset+h; i++ {
		if i > l.offset {
			b.WriteByte('\n')
		}
		r := l.rows[i]
		switch r.kind {
		case rowBlank:
		case rowQuick:
			b.WriteString(groupStyle.Render(ansi.Truncate(r.label, w, "…")))
		case rowGroup, rowRoot:
			b.WriteString(l.headingView(r, w, i == l.cursor, focused, lv))
		case rowEnv:
			b.WriteString(l.envView(r, w, i == l.cursor, focused, lv))
		}
	}
	return b.String()
}

// line lays out an indented label with something right aligned, and
// highlights it when it is the cursor.
func line(indent, mark, label, right string, rightStyle lipgloss.Style, w int, cursor, focused bool, labelStyle lipgloss.Style) string {
	nameW := w - lipgloss.Width(indent) - lipgloss.Width(mark) - lipgloss.Width(right) - 1
	name := ansi.Truncate(label, max(nameW, 1), "…")
	gap := strings.Repeat(" ", max(1, w-lipgloss.Width(indent)-lipgloss.Width(mark)-lipgloss.Width(name)-lipgloss.Width(right)))
	if cursor {
		st := selectedRow
		if !focused {
			st = lipgloss.NewStyle().Foreground(colText).Bold(true)
		}
		return indent + mark + st.Render(ansi.Strip(name)+gap+ansi.Strip(right))
	}
	return indent + mark + labelStyle.Render(name) + gap + rightStyle.Render(right)
}

func (l envList) headingView(r listRow, w int, cursor, focused bool, lv listView) string {
	indent := strings.Repeat("  ", r.depth)
	mark, open := openMark+" ", !l.closed[r.key] || l.filtering()
	if !open {
		mark = closedMark + " "
	}
	label := r.label + "/"
	if r.kind == rowRoot {
		label = r.label
	}
	right, style := "", dimStyle
	switch {
	case r.kind == rowRoot && l.roots[r.root].host != nil && lv.offline(l.roots[r.root].host):
		right = "enter connects"
	case !open:
		running, total, problems, update := 0, 0, 0, false
		for _, e := range l.rowEnvs(r) {
			rn, t, p := lv.counts(e)
			running, total, problems = running+rn, total+t, problems+p
			update = update || lv.hasUpdate(e.Dir)
		}
		right = fmt.Sprintf("%d/%d", running, total)
		if update {
			right = updateMark + " " + right
		}
		switch {
		case problems > 0:
			style = redStyle
		case running > 0:
			style = textStyle
		}
	}
	return line(indent, dimStyle.Render(mark), label, right, style, w, cursor, focused, groupStyle)
}

func (l envList) envView(r listRow, w int, cursor, focused bool, lv listView) string {
	it := l.items[r.item]
	indent := strings.Repeat("  ", r.depth)
	if it.pinned {
		indent = strings.Repeat("  ", max(r.depth-1, 0)) + accentStyle.Render(pinMark) + " "
	}
	label := it.env.Name()
	if it.quick {
		label = it.env.Rel
		if it.env.Remote() {
			label = it.env.Host.Label() + ":" + label
		}
	}
	if it.offline {
		return line(indent, dimStyle.Render("○ "), label, "", dimStyle, w, cursor, focused, dimStyle)
	}
	running, total, problems := lv.counts(it.env)
	count := fmt.Sprintf("%d/%d", running, total)
	countStyle := dimStyle
	if running > 0 {
		countStyle = textStyle
	}
	if problems > 0 {
		countStyle = redStyle
	}
	right := count
	if lv.hasUpdate(it.env.Dir) {
		right = updateMark + " " + count
	}
	out := line(indent, dot(statusOf(running, total), problems)+" ", label, right, countStyle, w, cursor, focused, textStyle)
	if !cursor && strings.HasPrefix(right, updateMark) {
		// the update mark keeps its own colour
		out = strings.Replace(out, countStyle.Render(right), accentStyle.Render(updateMark)+" "+countStyle.Render(count), 1)
	}
	return out
}
