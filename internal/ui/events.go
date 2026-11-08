package ui

import (
	"bufio"
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mcbulazs/lazycompose/internal/compose"
	"github.com/mcbulazs/lazycompose/internal/remote"
)

const (
	pollEvery       = 3 * time.Second  // without docker events
	pollEveryEvents = 15 * time.Second // with docker events, keeps uptime fresh
	retryEvents     = 30 * time.Second
	eventsSettle    = 300 * time.Millisecond
)

// eventStream follows `docker events` for compose containers on one host so
// the UI can refresh as soon as something changes instead of waiting for the
// next poll.
type eventStream struct {
	host   string // hostKey of the host
	cancel context.CancelFunc
	events chan struct{}
}

type (
	eventsStartedMsg struct {
		host   string
		stream *eventStream
		err    error
	}
	dockerEventMsg struct{ stream *eventStream }
	eventsEndMsg   struct{ stream *eventStream }
)

// hostKey names a host in maps: "" for this machine.
func hostKey(h *remote.Host) string {
	if h == nil {
		return ""
	}
	return h.String()
}

func startEvents(h *remote.Host) tea.Cmd {
	key := hostKey(h)
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		cmd := compose.Docker(ctx, h, "events",
			"--filter", "type=container",
			"--filter", "label=com.docker.compose.project",
			"--format", "{{.Action}}")
		out, err := cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err != nil {
			cancel()
			return eventsStartedMsg{host: key, err: err}
		}

		s := &eventStream{host: key, cancel: cancel, events: make(chan struct{}, 1)}
		go func() {
			defer close(s.events)
			sc := bufio.NewScanner(out)
			for sc.Scan() {
				select {
				case s.events <- struct{}{}:
				default: // a refresh is already pending
				}
			}
			_ = cmd.Wait()
		}()
		return eventsStartedMsg{host: key, stream: s}
	}
}

// next waits for an event, then lets a burst settle so a restart of a whole
// stack causes one refresh rather than dozens.
func (s *eventStream) next() tea.Cmd {
	return func() tea.Msg {
		if _, ok := <-s.events; !ok {
			return eventsEndMsg{stream: s}
		}
		deadline := time.After(eventsSettle)
		for {
			select {
			case _, ok := <-s.events:
				if !ok {
					return eventsEndMsg{stream: s}
				}
			case <-deadline:
				return dockerEventMsg{stream: s}
			}
		}
	}
}

func (s *eventStream) stop() {
	if s != nil {
		s.cancel()
	}
}

// startAllEvents starts the event streams of the hosts that have none, if
// the last try was long enough ago or force is set.
func (m *Model) startAllEvents(force bool) tea.Cmd {
	var cmds []tea.Cmd
	for _, h := range m.hosts {
		k := hostKey(h)
		if m.events[k] != nil || m.eventsStarting[k] || !force && time.Since(m.eventsTried[k]) <= retryEvents {
			continue
		}
		m.eventsStarting[k] = true
		cmds = append(cmds, startEvents(h))
	}
	return tea.Batch(cmds...)
}

// allEvents reports whether every host has an event stream.
func (m Model) allEvents() bool {
	for _, h := range m.hosts {
		if m.events[hostKey(h)] == nil {
			return false
		}
	}
	return true
}

func (m *Model) stopEvents() {
	for _, s := range m.events {
		s.stop()
	}
}
