package ui

import (
	"os/exec"
	"reflect"
	"runtime"
	"testing"

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
