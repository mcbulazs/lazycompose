package ui

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mcbulazs/lazycompose/internal/discover"
	"github.com/mcbulazs/lazycompose/internal/remote"
)

// Remote hosts are connected on demand. With several roots, a remote root
// starts closed and lazycompose does not touch its host until the root is
// opened: then it logs in, scans the root and follows the host like this
// one. A single remote root is connected right at the start. When ssh has
// to ask something to log in, like a key's passphrase, the question comes
// through remote.Asker and is asked in a popup.

type (
	hostCheckedMsg struct {
		key  string
		need bool // ssh has to ask something
		err  error
	}
	hostLoginMsg struct {
		key string
		err error
	}
	connectMsg  struct{ host *remote.Host }
	questionMsg struct{ q remote.Question }
)

// startHosts are the hosts followed from the start: this machine when a
// root is local or there are none, the host of a single remote root, and
// the hosts of environments that are already known.
func startHosts(roots []string, envs []discover.Env) []*remote.Host {
	all := hostsOf(roots)
	var out []*remote.Host
	known := map[string]bool{}
	for _, e := range envs {
		if e.Host != nil {
			known[e.Host.String()] = true
		}
	}
	for _, h := range all {
		if h == nil || known[h.String()] {
			out = append(out, h)
		}
	}
	return out
}

// hostOf is the host of a configured remote root by its key, ssh://host.
func (m Model) hostOf(key string) *remote.Host {
	for _, h := range hostsOf(m.opts.Roots) {
		if h != nil && h.String() == key {
			return h
		}
	}
	return nil
}

func (m Model) connected(h *remote.Host) bool {
	return slices.ContainsFunc(m.hosts, func(c *remote.Host) bool { return hostKey(c) == hostKey(h) })
}

// activeRoots are the roots that are scanned: local ones and those of
// connected hosts.
func (m Model) activeRoots() []string {
	var out []string
	for _, r := range m.opts.Roots {
		h, _, err := remote.Parse(r)
		if !remote.IsRemote(r) || err == nil && m.connected(h) {
			out = append(out, r)
		}
	}
	return out
}

// rootsOf are the configured roots on h.
func (m Model) rootsOf(h *remote.Host) []string {
	var out []string
	for _, r := range m.opts.Roots {
		if rh, _, err := remote.Parse(r); err == nil && rh.String() == h.String() {
			out = append(out, r)
		}
	}
	return out
}

// connect starts connecting to h, unless it is connected or connecting.
func (m *Model) connect(h *remote.Host) tea.Cmd {
	k := hostKey(h)
	if m.connected(h) || m.connecting[k] {
		return nil
	}
	m.connecting[k] = true
	return tea.Batch(m.startBusy("connecting to "+h.Label()), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		need, err := h.NeedsLogin(ctx)
		return hostCheckedMsg{key: k, need: need, err: err}
	})
}

func (m *Model) onHostChecked(msg hostCheckedMsg) tea.Cmd {
	m.busy = max(0, m.busy-1)
	h := m.hostOf(msg.key)
	if h == nil {
		return nil
	}
	switch {
	case msg.err != nil:
		m.connecting[msg.key] = false
		m.setStatus(firstLine(msg.err.Error()), true)
		return nil
	case msg.need && !remote.CanLogin():
		m.connecting[msg.key] = false
		m.setStatus(h.Label()+" needs a key passphrase or a password; add the key to ssh-agent with ssh-add", true)
		return nil
	case msg.need:
		if m.asker == nil {
			a, err := remote.NewAsker()
			if err != nil {
				m.connecting[msg.key] = false
				m.setStatus("logging in to "+h.Label()+": "+err.Error(), true)
				return nil
			}
			m.asker = a
		}
		ctx, cancel := context.WithCancel(context.Background())
		cmd, err := h.AskingLoginCommand(ctx, m.asker)
		if err != nil {
			cancel()
			m.connecting[msg.key] = false
			m.setStatus("logging in to "+h.Label()+": "+err.Error(), true)
			return nil
		}
		m.logins[msg.key] = cancel
		return tea.Batch(m.startBusy("logging in to "+h.Label()), m.waitQuestion(), runLogin(msg.key, h, cmd))
	}
	return m.onConnected(h)
}

// runLogin runs the login ssh, which opens the shared connection.
func runLogin(key string, h *remote.Host, cmd *exec.Cmd) tea.Cmd {
	return func() tea.Msg {
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		if err != nil && stderr.Len() > 0 {
			err = remote.Explain(h, errors.New(stderr.String()))
		}
		return hostLoginMsg{key: key, err: err}
	}
}

// waitQuestion waits for the next question of a login, unless something
// already does.
func (m *Model) waitQuestion() tea.Cmd {
	if m.asker == nil || m.askWaiting {
		return nil
	}
	m.askWaiting = true
	a := m.asker
	return func() tea.Msg { return questionMsg{q: <-a.Questions} }
}

// onQuestion asks ssh's question in a popup. A passphrase or password is
// typed hidden and passed on exactly as typed.
func (m *Model) onQuestion(msg questionMsg) tea.Cmd {
	m.askWaiting = false
	q := msg.q
	var hosts []string
	for k, on := range m.connecting {
		if h := m.hostOf(k); on && h != nil {
			hosts = append(hosts, h.Label())
		}
	}
	sort.Strings(hosts)
	title := "ssh login"
	if len(hosts) > 0 {
		title += " · " + strings.Join(hosts, ", ")
	}
	if m.prompt != nil && m.prompt.cancel != nil {
		m.prompt.cancel(m) // only one popup: an earlier question gives way
	}
	cmd := m.openPrompt(title, q.Prompt, "", func(m *Model, value string) (tea.Cmd, error) {
		q.Answer(value)
		return m.waitQuestion(), nil
	})
	m.prompt.cancel = func(m *Model) tea.Cmd {
		q.Cancel()
		// esc means no login: otherwise ssh goes on to ask for a password
		for k, cancel := range m.logins {
			cancel()
			delete(m.logins, k)
		}
		return m.waitQuestion()
	}
	if q.Secret {
		m.prompt.raw = true
		m.prompt.input.EchoMode = textinput.EchoPassword
		m.prompt.input.EchoCharacter = '•'
	}
	return cmd
}

func (m *Model) onHostLogin(msg hostLoginMsg) tea.Cmd {
	m.busy = max(0, m.busy-1)
	cancel, running := m.logins[msg.key]
	if running {
		cancel()
		delete(m.logins, msg.key)
	}
	h := m.hostOf(msg.key)
	if h == nil {
		return nil
	}
	switch {
	case msg.err != nil && !running:
		m.connecting[msg.key] = false
		m.setStatus("login to "+h.Label()+" cancelled", false)
		return nil
	case msg.err != nil:
		m.connecting[msg.key] = false
		m.setStatus("could not log in: "+firstLine(msg.err.Error()), true)
		return nil
	}
	return m.onConnected(h)
}

// onConnected follows h from now on: its roots are opened and scanned,
// and its containers polled and their events followed.
func (m *Model) onConnected(h *remote.Host) tea.Cmd {
	k := hostKey(h)
	m.connecting[k] = false
	if !m.connected(h) {
		m.hosts = append(m.hosts, h)
	}
	roots := m.rootsOf(h)
	for _, r := range roots {
		delete(m.list.closed, rootKey(r))
	}
	m.list.rebuild()
	m.updateQuick()
	if len(roots) > 0 {
		m.list.selectKey(rootKey(roots[0])) // until the scan fills it in
	}
	m.setStatus("scanning "+strings.Join(roots, ", "), false)
	cmds := []tea.Cmd{m.startBusy("scanning " + h.Label()), loadStates(m.hosts), m.startAllEvents(true), m.recheckUpdates()}
	if m.opts.Find != nil {
		cmds = append(cmds, rediscover(m.opts.Find, roots))
	}
	return tea.Batch(cmds...)
}

// hostOffline reports whether h is a configured host that is not connected.
func (m Model) hostOffline(h *remote.Host) bool { return h != nil && !m.connected(h) }

// offlineQuick are quick access entries of hosts that are not connected:
// remote environments known from the history, under a configured root.
func (m Model) offlineQuick(dirs []string) map[string]envItem {
	out := map[string]envItem{}
	for _, d := range dirs {
		hk := remote.HostOf(d)
		if hk == "" {
			continue
		}
		h := m.hostOf(hk)
		if h == nil || m.connected(h) {
			continue
		}
		p := strings.TrimPrefix(d, hk)
		for _, r := range m.rootsOf(h) {
			_, rdir, _ := remote.Parse(r)
			rel, ok := strings.CutPrefix(p, strings.TrimSuffix(rdir, "/")+"/")
			if !ok || rel == "" {
				continue
			}
			out[d] = envItem{offline: true, env: discover.Env{Root: h.Key(rdir), Rel: rel, Dir: d, Host: h, Path: p, File: path.Join(p, "compose.yaml")}}
			break
		}
	}
	return out
}
