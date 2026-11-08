package remote

import (
	"context"
	"sync"
)

// All commands to a host share one ssh connection, and sshd allows only so
// many sessions on one connection (MaxSessions, 10 by default). Past that,
// ssh falls back to a login of its own, which fails without an agent: the
// key's passphrase went to the shared connection only. So short commands
// take one of a few sessions per host and wait for a free one, which
// leaves room for the long ones: the event stream, followed logs and
// actions.

// shortSessions is how many short commands run on a host at once.
const shortSessions = 5

var (
	sessionsMu sync.Mutex
	sessions   = map[string]chan struct{}{}
)

// Hold takes one of the host's sessions for a short command, waiting for a
// free one, and returns the function that gives it back. A nil host, this
// machine, has no limit.
func (h *Host) Hold(ctx context.Context) (release func(), err error) {
	if h == nil {
		return func() {}, nil
	}
	sessionsMu.Lock()
	slots, ok := sessions[h.String()]
	if !ok {
		slots = make(chan struct{}, shortSessions)
		sessions[h.String()] = slots
	}
	sessionsMu.Unlock()
	select {
	case slots <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-slots }) }, nil
	case <-ctx.Done():
		return func() {}, ctx.Err()
	}
}
