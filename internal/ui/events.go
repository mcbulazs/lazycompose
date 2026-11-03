package ui

import (
	"bufio"
	"context"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	pollEvery       = 3 * time.Second  // without docker events
	pollEveryEvents = 15 * time.Second // with docker events, keeps uptime fresh
	retryEvents     = 30 * time.Second
	eventsSettle    = 300 * time.Millisecond
)

// eventStream follows `docker events` for compose containers so the UI can
// refresh as soon as something changes instead of waiting for the next poll.
type eventStream struct {
	cancel context.CancelFunc
	events chan struct{}
}

type (
	eventsStartedMsg struct {
		stream *eventStream
		err    error
	}
	dockerEventMsg struct{ stream *eventStream }
	eventsEndMsg   struct{ stream *eventStream }
)

func startEvents() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		cmd := exec.CommandContext(ctx, "docker", "events",
			"--filter", "type=container",
			"--filter", "label=com.docker.compose.project",
			"--format", "{{.Action}}")
		out, err := cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err != nil {
			cancel()
			return eventsStartedMsg{err: err}
		}

		s := &eventStream{cancel: cancel, events: make(chan struct{}, 1)}
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
		return eventsStartedMsg{stream: s}
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
