package ui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mcbulazs/lazycompose/internal/discover"
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

// showCompose puts the compose file of e in the log pane, scrolled to the
// section of svc if one is given.
func (m *Model) showCompose(e discover.Env, svc string) {
	m.stopLogs()
	m.search = logSearch{}
	m.outputKeys = nil
	m.composeTarget = logTarget{dir: e.Dir, service: svc}
	m.logTitle = "Compose · " + e.Rel + "/" + filepath.Base(e.File)

	lines, err := composeLines(e.File)
	if err != nil {
		m.logLines = []string{redStyle.Render(err.Error())}
		m.renderLogs()
		m.logs.GotoTop()
		return
	}
	for _, name := range overrideFiles {
		path := filepath.Join(e.Dir, name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		more, err := composeLines(path)
		lines = append(lines, "", dimStyle.Render("── "+name+", applied on top ──"))
		if err != nil {
			lines = append(lines, redStyle.Render(err.Error()))
		}
		lines = append(lines, more...)
		break
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

// composeLines reads a compose file and colours it, with line numbers.
func composeLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxComposeFile+1))
	if err != nil {
		return nil, err
	}
	cut := len(b) > maxComposeFile
	if cut {
		b = b[:maxComposeFile]
	}
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
	return out, nil
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
