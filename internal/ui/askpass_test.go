package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mcbulazs/lazycompose/internal/remote"
)

func TestLoginQuestion(t *testing.T) {
	m := newTestModel(t)
	m.connecting["ssh://srv"] = true
	m.opts.Roots = append(m.opts.Roots, "ssh://srv/stacks")

	// a passphrase: hidden, and passed on with its spaces
	q, reply := remote.NewQuestion("Enter passphrase for key '/home/me/.ssh/id_ed25519':", true)
	m = send(m, questionMsg{q: q})
	if m.prompt == nil || m.prompt.title != "ssh login · srv" || m.prompt.input.EchoMode != textinput.EchoPassword {
		t.Fatalf("prompt %+v", m.prompt)
	}
	m = send(m, press(" ", "s", "e", "c", " ")...)
	if v := ansi.Strip(m.View()); strings.Contains(v, " sec ") || !strings.Contains(v, "id_ed25519") {
		t.Errorf("the passphrase should not show, the question should:\n%s", v)
	}
	m = send(m, press("enter")...)
	if got := reply(); got != "ok  sec \n" {
		t.Errorf("reply %q", got)
	}
	if m.prompt != nil || !m.askWaiting && m.asker != nil {
		t.Errorf("the popup should close")
	}

	// a host key question shows what is typed; esc cancels it
	q, reply = remote.NewQuestion("Are you sure you want to continue connecting (yes/no/[fingerprint])?", false)
	m = send(m, questionMsg{q: q})
	if m.prompt == nil || m.prompt.input.EchoMode != textinput.EchoNormal {
		t.Fatalf("prompt %+v", m.prompt)
	}
	m = send(m, press("esc")...)
	if got := reply(); got != "cancel" || m.prompt != nil {
		t.Errorf("reply %q, prompt %v", got, m.prompt)
	}
}

func TestSingleRemoteRootConnects(t *testing.T) {
	m := New(Options{Hostname: "pc", Roots: []string{"ssh://srv/stacks"}})
	if m.autoConnect == nil || m.autoConnect.String() != "ssh://srv" || len(m.hosts) != 0 {
		t.Fatalf("autoConnect %v, hosts %v", m.autoConnect, m.hosts)
	}
	if m.Init() == nil {
		t.Fatal("Init should start connecting")
	}
	next, cmd := m.Update(connectMsg{host: m.autoConnect})
	if cmd == nil || !next.(Model).connecting["ssh://srv"] {
		t.Error("connectMsg should start connecting")
	}
	_ = tea.Quit
}
