package ui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mcbulazs/lazycompose/internal/discover"
)

func TestEditorCommand(t *testing.T) {
	tests := []struct {
		config, visual, editor string
		want                   []string
	}{
		{"code --wait", "vim", "nano", []string{"code", "--wait", "f.yaml"}},
		{"", "vim", "nano", []string{"vim", "f.yaml"}},
		{"", "", "nano -w", []string{"nano", "-w", "f.yaml"}},
		{"  ", "", "nano", []string{"nano", "f.yaml"}},
	}
	for _, tt := range tests {
		t.Setenv("VISUAL", tt.visual)
		t.Setenv("EDITOR", tt.editor)
		cmd, err := editorCommand(tt.config, "f.yaml")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(cmd.Args, tt.want) {
			t.Errorf("config %q: got %v, want %v", tt.config, cmd.Args, tt.want)
		}
	}
}

func TestCountRecreated(t *testing.T) {
	out := ` Container demo-web-1 Recreate 
 Container demo-web-1 Recreated 
 Container demo-db-1 Running 
 Container demo-worker-1 Recreated 
 Container demo-web-1 Started 
`
	if n := countRecreated(out); n != 2 {
		t.Errorf("got %d, want 2", n)
	}
	if n := countRecreated(" Container demo-web-1 Running \n"); n != 0 {
		t.Errorf("got %d, want 0", n)
	}
}

func TestOpenFolderInTmux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no tmux on Windows; the fakes use sh")
	}
	var gotDir, gotName string
	orig := tmuxNewWindow
	t.Cleanup(func() { tmuxNewWindow = orig })
	tmuxNewWindow = func(dir, name string) *exec.Cmd {
		gotDir, gotName = dir, name
		return exec.Command("true")
	}
	t.Setenv("TMUX", "/tmp/tmux-1000/default,1,0")

	e := discover.Env{Rel: "web/blog", Dir: "/srv/web/blog"}
	msg := openFolder(e)()
	w, ok := msg.(tmuxWindowMsg)
	if !ok || w.err != nil {
		t.Fatalf("got %#v", msg)
	}
	if gotDir != "/srv/web/blog" || gotName != "blog" {
		t.Errorf("new-window -c %q -n %q", gotDir, gotName)
	}

	// a failing tmux reports its output
	tmuxNewWindow = func(dir, name string) *exec.Cmd {
		return exec.Command("sh", "-c", "echo 'no server running' >&2; exit 1")
	}
	if w := openFolder(e)().(tmuxWindowMsg); w.err == nil || w.err.Error() != "no server running" {
		t.Errorf("err %v", w.err)
	}
}

func TestOpenFolderOutsideTmux(t *testing.T) {
	t.Setenv("TMUX", "")
	called := false
	orig := tmuxNewWindow
	t.Cleanup(func() { tmuxNewWindow = orig })
	tmuxNewWindow = func(dir, name string) *exec.Cmd { called = true; return exec.Command("true") }
	_ = openFolder(discover.Env{Dir: "/srv/x"}) // a nested shell, not run here
	if called {
		t.Error("tmux should not be used outside tmux")
	}
}

func TestUserShell(t *testing.T) {
	t.Setenv("SHELL", "/usr/bin/fish")
	if got := userShell(); got != "/usr/bin/fish" {
		t.Errorf("userShell() = %q, want $SHELL", got)
	}
	t.Setenv("SHELL", "")
	got := userShell()
	switch {
	case runtime.GOOS == "windows" && got == "":
		t.Error("userShell() is empty, want PowerShell or %ComSpec%")
	case runtime.GOOS != "windows" && got != "sh":
		t.Errorf("userShell() = %q, want sh", got)
	}
}

// tempDir points os.TempDir at a test folder, on every system.
func tempDir(t *testing.T) string {
	dir := t.TempDir()
	for _, v := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(v, dir)
	}
	return dir
}

func TestWriteLogsFile(t *testing.T) {
	dir := tempDir(t)
	name, err := writeLogsFile([]string{"\x1b[31merror\x1b[0m: boom", "plain\tline"})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(name) != dir || !strings.HasSuffix(name, ".log") {
		t.Errorf("file %q, want a .log in %q", name, dir)
	}
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if want := "error: boom\nplain\tline\n"; string(b) != want {
		t.Errorf("file holds %q, want %q", b, want)
	}
	if fi, err := os.Stat(name); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode %v, want 0600: logs can hold secrets", fi.Mode().Perm())
	}
}

func TestOpenLogs(t *testing.T) {
	tempDir(t)
	m := New(Options{Hostname: "host", Roots: []string{"/srv"}, Editor: "nano"})
	m = send(m, tea.WindowSizeMsg{Width: 160, Height: 30})
	m = send(m, press("o")...)
	if m.status != "the log pane is empty" || !m.statusErr {
		t.Errorf("o with no logs: status %q (error %v)", m.status, m.statusErr)
	}

	m.logLines = []string{"hello"}
	m.setStatus("", false)
	next, cmd := m.Update(press("o")[0])
	if cmd == nil || next.(Model).status != "" {
		t.Errorf("o with logs should open the editor, status %q", next.(Model).status)
	}
	m = send(m, press(":", "copy")...)
	if names := shownNames(m); len(names) == 0 || names[0] != "open_logs" {
		t.Errorf("searching copy should find open_logs first, got %v", names)
	}

	m = send(New(Options{}), logsEditedMsg{err: errors.New("no editor found")})
	if !strings.Contains(m.status, "editor: no editor found") {
		t.Errorf("status %q", m.status)
	}
}
