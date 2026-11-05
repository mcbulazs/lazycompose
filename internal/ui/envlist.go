package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mcbulazs/lazycompose/internal/discover"
)

type envStatus int

const (
	statusDown envStatus = iota
	statusPartial
	statusUp
)

type envItem struct {
	env    discover.Env
	group  string
	quick  bool // a copy shown in the quick access section
	pinned bool // pinned, shown with a star in the quick access section
}

const (
	quickGroup  = "quick access"
	pinnedGroup = "pinned" // the section when quick access is off
	pinMark     = "★"
)

// envList is the left pane: environments grouped by parent folder.
type envList struct {
	items   []envItem
	visible []int // indices into items that match the filter
	cursor  int   // index into visible
	offset  int   // first rendered line
	query   string
	only    map[string]bool // when set, only these env dirs are shown
}

func newEnvList(envs []discover.Env, multiRoot bool) envList {
	items := make([]envItem, len(envs))
	for i, e := range envs {
		g := e.Group()
		if multiRoot {
			g = strings.Trim(filepath.Base(e.Root)+"/"+g, "/")
		}
		items[i] = envItem{env: e, group: g}
	}
	// top level environments first, then each group
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if (a.group == "") != (b.group == "") {
			return a.group == ""
		}
		if a.group != b.group {
			return a.group < b.group
		}
		return a.env.Name() < b.env.Name()
	})
	l := envList{items: items}
	l.setFilter("")
	return l
}

// setQuick shows copies of dirs, in order, in a section called group at
// the top of the list. The dirs in pinned get a star.
func (l *envList) setQuick(dirs []string, group string, pinned func(string) bool) {
	prev, hadPrev := l.selectedItem()
	var rest []envItem
	byDir := map[string]envItem{}
	for _, it := range l.items {
		if !it.quick {
			rest = append(rest, it)
			byDir[it.env.Dir] = it
		}
	}
	var items []envItem
	for _, d := range dirs {
		if it, ok := byDir[d]; ok {
			items = append(items, envItem{env: it.env, group: group, quick: true, pinned: pinned(d)})
		}
	}
	l.items = append(items, rest...)
	l.setFilter(l.query)
	if hadPrev {
		l.selectItem(prev)
	}
}

func (l *envList) setFilter(q string) {
	prev, hadPrev := l.selectedItem()
	l.query = q
	q = strings.ToLower(q)
	l.visible = l.visible[:0]
	for i, it := range l.items {
		if l.only != nil && !l.only[it.env.Dir] {
			continue
		}
		// the section would only repeat matches while filtering
		if it.quick && (q != "" || l.only != nil) {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(it.group+"/"+it.env.Name()), q) {
			l.visible = append(l.visible, i)
		}
	}
	l.cursor = 0
	if hadPrev {
		l.selectItem(prev)
	}
	l.offset = 0
}

// selectItem moves the cursor to it, or to another copy of its env.
func (l *envList) selectItem(it envItem) {
	found := false
	for i, idx := range l.visible {
		c := l.items[idx]
		if c.env.Dir != it.env.Dir {
			continue
		}
		if c.quick == it.quick {
			l.cursor = i
			return
		}
		if !found {
			l.cursor = i
			found = true
		}
	}
}

func (l envList) selectedItem() (envItem, bool) {
	if l.cursor < 0 || l.cursor >= len(l.visible) {
		return envItem{}, false
	}
	return l.items[l.visible[l.cursor]], true
}

func (l envList) selected() (discover.Env, bool) {
	it, ok := l.selectedItem()
	return it.env, ok
}

func (l *envList) move(n int) {
	if len(l.visible) == 0 {
		return
	}
	l.cursor = max(0, min(len(l.visible)-1, l.cursor+n))
}

type listLine struct {
	header string
	quick  bool // the quick access header
	pos    int  // position in visible, -1 for headers and blank lines
}

func (l envList) lines() []listLine {
	var out []listLine
	group, inQuick := "", false
	for pos, idx := range l.visible {
		it := l.items[idx]
		if inQuick && !it.quick {
			out = append(out, listLine{pos: -1}) // blank line after the section
			group = ""
		}
		if it.group != "" && (it.group != group || it.quick != inQuick) {
			out = append(out, listLine{header: it.group, quick: it.quick, pos: -1})
		}
		group, inQuick = it.group, it.quick
		out = append(out, listLine{pos: pos})
	}
	return out
}

// scroll keeps the cursor (and its group header) inside a window of h lines.
func (l *envList) scroll(h int) {
	lines := l.lines()
	if len(lines) == 0 {
		l.offset = 0
		return
	}
	cur := 0
	for i, ln := range lines {
		if ln.pos == l.cursor {
			cur = i
		}
	}
	top := cur
	if cur > 0 && lines[cur-1].pos == -1 {
		top = cur - 1
	}
	if top < l.offset {
		l.offset = top
	}
	if cur >= l.offset+h {
		l.offset = cur - h + 1
	}
	l.offset = max(0, min(l.offset, max(0, len(lines)-h)))
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

func (l envList) view(w, h int, focused bool, counts countFunc, hasUpdate func(dir string) bool) string {
	if len(l.visible) == 0 {
		if l.query != "" {
			return dimStyle.Render("no matches")
		}
		return dimStyle.Render("no environments found")
	}

	lines := l.lines()
	var b strings.Builder
	for i := l.offset; i < len(lines) && i < l.offset+h; i++ {
		ln := lines[i]
		if i > l.offset {
			b.WriteByte('\n')
		}
		if ln.pos == -1 {
			switch {
			case ln.quick:
				b.WriteString(groupStyle.Render(ansi.Truncate(ln.header, w, "…")))
			case ln.header != "":
				b.WriteString(groupStyle.Render(ansi.Truncate(ln.header+"/", w, "…")))
			}
			continue
		}
		it := l.items[l.visible[ln.pos]]
		running, total, problems := counts(it.env)

		indent := ""
		if it.group != "" {
			indent = "  "
		}
		if it.pinned {
			indent = accentStyle.Render(pinMark) + " "
		}
		count := fmt.Sprintf("%d/%d", running, total)
		mark := ""
		if hasUpdate(it.env.Dir) {
			mark = updateMark + " "
		}
		nameW := w - lipgloss.Width(indent) - 2 - len(count) - lipgloss.Width(mark) - 1
		label := it.env.Name()
		if it.quick {
			label = it.env.Rel
		}
		name := ansi.Truncate(label, max(nameW, 1), "…")
		gap := strings.Repeat(" ", max(1, w-lipgloss.Width(indent)-2-lipgloss.Width(name)-lipgloss.Width(mark)-len(count)))

		if ln.pos == l.cursor {
			st := selectedRow
			if !focused {
				st = lipgloss.NewStyle().Foreground(colText).Bold(true)
			}
			row := indent + dot(statusOf(running, total), problems) + " " + st.Render(name+gap+mark+count)
			b.WriteString(row)
			continue
		}
		countStyle := dimStyle
		if running > 0 {
			countStyle = textStyle
		}
		if problems > 0 {
			countStyle = redStyle
		}
		if mark != "" {
			mark = accentStyle.Render(updateMark) + " "
		}
		b.WriteString(indent + dot(statusOf(running, total), problems) + " " + textStyle.Render(name) + gap + mark + countStyle.Render(count))
	}
	return b.String()
}
