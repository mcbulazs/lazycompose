package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mcbulazs/lazycompose/internal/discover"
	"github.com/mcbulazs/lazycompose/internal/remote"
)

// The log pane shows either the logs of the selection or its compose file,
// as written: never with the .env values filled in, so no secrets end up on
// screen.

type paneMode int

const (
	modeLogs paneMode = iota
	modeCompose
)

// paneName is how a mode is saved in the history file.
func (p paneMode) name() string {
	if p == modeCompose {
		return "compose"
	}
	return "logs"
}

// maxComposeFile limits how much of a compose file is shown.
const maxComposeFile = 1 << 20

// overrideFiles are merged on top of the compose file by docker compose.
var overrideFiles = []string{"compose.override.yaml", "compose.override.yml", "docker-compose.override.yaml", "docker-compose.override.yml"}

// composeFile is a file shown in the compose view: the compose file, or
// the override file merged on top of it.
type composeFile struct {
	name string // the override's file name; "" for the compose file
	data []byte
	cut  bool // over maxComposeFile, only the start is in data
	err  error
}

// composeReadMsg brings the compose file of a remote environment.
type composeReadMsg struct {
	target logTarget
	files  []composeFile
}

// showCompose puts the compose file of e in the log pane, scrolled to the
// section of svc if one is given. A remote file is read in the background,
// by the command it returns.
func (m *Model) showCompose(e discover.Env, svc string) tea.Cmd {
	m.stopLogs()
	m.search = logSearch{}
	m.outputKeys = nil
	m.composeTarget = logTarget{dir: e.Dir, service: svc}
	m.logTitle = "Compose · " + e.Rel + "/" + path.Base(filepath.ToSlash(e.File))
	if e.Remote() {
		m.logLines = nil
		m.logs.SetContent(dimStyle.Render("reading the compose file…"))
		target := m.composeTarget
		return func() tea.Msg { return composeReadMsg{target: target, files: readRemoteCompose(e)} }
	}
	m.renderCompose(svc, readLocalCompose(e))
	return nil
}

func (m *Model) onComposeRead(msg composeReadMsg) {
	if m.paneMode != modeCompose || msg.target != m.composeTarget {
		return // the selection moved on
	}
	m.renderCompose(msg.target.service, msg.files)
}

func (m *Model) renderCompose(svc string, files []composeFile) {
	main := files[0]
	if main.err != nil {
		m.logLines = []string{redStyle.Render(main.err.Error())}
		m.renderLogs()
		m.logs.GotoTop()
		return
	}
	lines := composeText(main.data, main.cut)
	for _, f := range files[1:] {
		lines = append(lines, "", dimStyle.Render("── "+f.name+", applied on top ──"))
		if f.err != nil {
			lines = append(lines, redStyle.Render(f.err.Error()))
		} else {
			lines = append(lines, composeText(f.data, f.cut)...)
		}
	}
	m.logLines = lines
	m.renderLogs()
	m.logs.GotoTop()
	if svc != "" {
		if i := serviceLine(m.logLines, svc); i >= 0 && i < len(m.lineStart) {
			m.logs.SetYOffset(m.lineStart[i])
		}
	}
}

// readLocalCompose reads the compose file and the first override file
// there is.
func readLocalCompose(e discover.Env) []composeFile {
	data, cut, err := readLimited(e.File)
	files := []composeFile{{data: data, cut: cut, err: err}}
	for _, name := range overrideFiles {
		p := filepath.Join(e.Dir, name)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		data, cut, err := readLimited(p)
		return append(files, composeFile{name: name, data: data, cut: cut, err: err})
	}
	return files
}

// readRemoteCompose is readLocalCompose over ssh, with no more than the
// limit coming over.
func readRemoteCompose(e discover.Env) []composeFile {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	read := func(p string) (composeFile, bool) {
		data, exists, err := e.Host.ReadFile(ctx, p, maxComposeFile)
		if errors.Is(err, remote.ErrTooBig) {
			return composeFile{data: data, cut: true}, true // the start, as for a local file
		}
		return composeFile{data: data, err: err}, exists || err != nil
	}
	main, ok := read(e.File)
	if !ok {
		main.err = errors.New(e.File + ": no such file")
	}
	files := []composeFile{main}
	if main.err != nil {
		return files
	}
	for _, name := range overrideFiles {
		if f, ok := read(path.Join(e.Path, name)); ok {
			f.name = name
			return append(files, f)
		}
	}
	return files
}

func readLimited(p string) ([]byte, bool, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxComposeFile+1))
	if err != nil {
		return nil, false, err
	}
	if len(b) > maxComposeFile {
		return b[:maxComposeFile], true, nil
	}
	return b, false, nil
}

// composeLines reads a compose file and colours it, with line numbers.
func composeLines(p string) ([]string, error) {
	b, cut, err := readLimited(p)
	if err != nil {
		return nil, err
	}
	return composeText(b, cut), nil
}

// composeText colours a compose file, with line numbers.
func composeText(b []byte, cut bool) []string {
	raw := strings.Split(strings.TrimRight(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n"), "\n")
	width := len(fmt.Sprint(len(raw)))
	out := make([]string, len(raw), len(raw)+1)
	for i, l := range raw {
		// the file is shown as text: no escape sequence in it reaches the
		// terminal
		l = stripControls(l)
		out[i] = dimStyle.Render(fmt.Sprintf("%*d ", width, i+1)) + colourYAML(l)
	}
	if cut {
		out = append(out, dimStyle.Render("… the rest of the file is not shown, it is over 1MB"))
	}
	return out
}

// stripControls removes control characters, keeping tabs.
func stripControls(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || (r >= 0x20 && r != 0x7f && (r < 0x80 || r >= 0xa0)) {
			return r
		}
		return -1
	}, s)
}

var (
	yamlKey      = regexp.MustCompile(`^(\s*(?:-\s+)?)([^\s#'"][^:#]*?|"[^"]*"|'[^']*'):(\s|$)`)
	yamlVariable = regexp.MustCompile(`\$\{[^}]*\}|\$[A-Za-z_][A-Za-z0-9_]*`)
)

// colourYAML colours one line of YAML: keys, comments and ${variables}.
// It is a simple line based colourer, not a parser.
func colourYAML(line string) string {
	code, comment := splitComment(line)
	var b strings.Builder
	if m := yamlKey.FindStringSubmatchIndex(code); m != nil {
		b.WriteString(dimStyle.Render(code[m[2]:m[3]]))
		b.WriteString(accentStyle.Render(code[m[4]:m[5]]))
		b.WriteString(textStyle.Render(":"))
		code = code[m[5]+1:]
	} else if t := strings.TrimLeft(code, " \t"); strings.HasPrefix(t, "- ") || t == "-" {
		indent := code[:len(code)-len(t)]
		b.WriteString(indent + dimStyle.Render("-"))
		code = t[1:]
	}
	last := 0
	for _, v := range yamlVariable.FindAllStringIndex(code, -1) {
		b.WriteString(textStyle.Render(code[last:v[0]]))
		b.WriteString(yellowStyle.Render(code[v[0]:v[1]]))
		last = v[1]
	}
	b.WriteString(textStyle.Render(code[last:]))
	if comment != "" {
		b.WriteString(dimStyle.Render(comment))
	}
	return b.String()
}

// splitComment splits a line at a # that starts a comment: at the start or
// after a space, and not inside quotes.
func splitComment(line string) (code, comment string) {
	var quote rune
	for i, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			return line[:i], line[i:]
		}
	}
	return line, ""
}

// serviceLine finds the line where svc is defined under services:.
func serviceLine(lines []string, svc string) int {
	def := regexp.MustCompile(`^\s+` + regexp.QuoteMeta(svc) + `:\s*(#.*)?$`)
	inServices := false
	for i, l := range lines {
		plain := numberless(l)
		if strings.HasPrefix(plain, "services:") {
			inServices = true
			continue
		}
		if inServices && plain != "" && plain[0] != ' ' && plain[0] != '#' {
			inServices = false // the next top level key
		}
		if inServices && def.MatchString(plain) {
			return i
		}
	}
	return -1
}

// numberless is a shown line without its colours and line number.
func numberless(l string) string {
	plain := ansi.Strip(l)
	t := strings.TrimLeft(plain, " ")
	if i := strings.IndexByte(t, ' '); i >= 0 {
		return t[i+1:]
	}
	return ""
}

// logTabNames are the two tabs in the log pane title, in order.
var logTabNames = [2]string{"Logs", "Compose"}

const logTabSep = " │ "

// logTabsTitle is the log pane title with both modes as tabs, the active
// one marked: "[3] Logs │ Compose · web/blog". title starts with the name
// of the mode, which the tabs replace.
func (m Model) logTabsTitle(title string, w int) string {
	focused := m.focus == focusLogs
	t := paneTitleStyle(focused)
	rest := strings.TrimPrefix(strings.TrimPrefix(title, logTabNames[0]), logTabNames[1])
	s := t.Render(paneKey(keys.FocusLogs))
	for i, name := range logTabNames {
		if i > 0 {
			s += dimStyle.Render(logTabSep)
		}
		if paneMode(i) == m.paneMode {
			s += t.Underline(true).Render(name)
		} else {
			s += dimStyle.Render(name)
		}
	}
	return ansi.Truncate(s+t.Render(rest), max(w-6, 1), "…")
}

// logTabAt returns the tab under column x of the log pane's top border,
// where the pane starts at column paneX.
func logTabAt(x, paneX int) (paneMode, bool) {
	pos := paneX + lipgloss.Width("╭─ ") + lipgloss.Width(paneKey(keys.FocusLogs))
	for i, name := range logTabNames {
		if i > 0 {
			pos += lipgloss.Width(logTabSep)
		}
		w := lipgloss.Width(name)
		if x >= pos && x < pos+w {
			return paneMode(i), true
		}
		pos += w
	}
	return 0, false
}
