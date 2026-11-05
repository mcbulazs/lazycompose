package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mcbulazs/lazycompose/internal/selfupdate"
)

// How soon after start, and then how often, to look for a new release.
const (
	versionFirst = 10 * time.Second
	versionEvery = 24 * time.Hour
)

type (
	versionTickMsg struct{}
	versionMsg     struct {
		tag string
		err error
	}
)

// firstVersionCheck schedules the first look for a new lazycompose release.
// Development builds are not checked.
func (m Model) firstVersionCheck() tea.Cmd {
	if m.opts.LatestVersion == nil || !selfupdate.IsRelease(m.opts.Version) {
		return nil
	}
	return tea.Tick(versionFirst, func(time.Time) tea.Msg { return versionTickMsg{} })
}

func (m Model) checkVersion() tea.Cmd {
	latest := m.opts.LatestVersion
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		tag, err := latest(ctx)
		return versionMsg{tag: tag, err: err}
	}
}

// onVersion remembers a newer release for the header and checks again
// later. A failed check is silent: no network is not worth a message.
func (m *Model) onVersion(msg versionMsg) tea.Cmd {
	if msg.err == nil && selfupdate.Newer(msg.tag, m.opts.Version) {
		m.newVersion = msg.tag
	}
	return tea.Tick(versionEvery, func(time.Time) tea.Msg { return versionTickMsg{} })
}
