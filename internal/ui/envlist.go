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
	env   discover.Env
	group string
}

// envList is the left pane: environments grouped by parent folder.
type envList struct {
	items   []envItem
	visible []int // indices into items that match the filter
	cursor  int   // index into visible
	offset  int   // first rendered line
	query   string
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

func (l *envList) setFilter(q string) {
	prev, hadPrev := l.selected()
	l.query = q
	q = strings.ToLower(q)
	l.visible = l.visible[:0]
	for i, it := range l.items {
		if q == "" || strings.Contains(strings.ToLower(it.group+"/"+it.env.Name()), q) {
			l.visible = append(l.visible, i)
		}
	}
	l.cursor = 0
	if hadPrev {
		for i, idx := range l.visible {
			if l.items[idx].env.Dir == prev.Dir {
				l.cursor = i
			}
		}
	}
	l.offset = 0
}

func (l envList) selected() (discover.Env, bool) {
	if l.cursor < 0 || l.cursor >= len(l.visible) {
		return discover.Env{}, false
	}
	return l.items[l.visible[l.cursor]].env, true
}

func (l *envList) move(n int) {
	if len(l.visible) == 0 {
		return
	}
	l.cursor = max(0, min(len(l.visible)-1, l.cursor+n))
}

type listLine struct {
	header string
	pos    int // position in visible, -1 for headers
}

func (l envList) lines() []listLine {
	var out []listLine
	group := ""
	for pos, idx := range l.visible {
		it := l.items[idx]
		if it.group != "" && it.group != group {
			out = append(out, listLine{header: it.group, pos: -1})
		}
		group = it.group
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

type countFunc func(discover.Env) (running, total int)

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

func dot(s envStatus) string {
	switch s {
	case statusUp:
		return greenStyle.Render("●")
	case statusPartial:
		return yellowStyle.Render("◐")
	default:
		return dimStyle.Render("○")
	}
}

func (l envList) view(w, h int, focused bool, counts countFunc) string {
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
			b.WriteString(groupStyle.Render(ansi.Truncate(ln.header+"/", w, "…")))
			continue
		}
		it := l.items[l.visible[ln.pos]]
		running, total := counts(it.env)

		indent := ""
		if it.group != "" {
			indent = "  "
		}
		count := fmt.Sprintf("%d/%d", running, total)
		nameW := w - len(indent) - 2 - len(count) - 1
		name := ansi.Truncate(it.env.Name(), max(nameW, 1), "…")
		gap := strings.Repeat(" ", max(1, w-len(indent)-2-lipgloss.Width(name)-len(count)))

		if ln.pos == l.cursor {
			st := selectedRow
			if !focused {
				st = lipgloss.NewStyle().Foreground(colText).Bold(true)
			}
			row := indent + dot(statusOf(running, total)) + " " + st.Render(name+gap+count)
			b.WriteString(row)
			continue
		}
		countStyle := dimStyle
		if running > 0 {
			countStyle = textStyle
		}
		b.WriteString(indent + dot(statusOf(running, total)) + " " + textStyle.Render(name) + gap + countStyle.Render(count))
	}
	return b.String()
}
