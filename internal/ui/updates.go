package ui

import (
	"context"
	"slices"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mcbulazs/lazycompose/internal/compose"
	"github.com/mcbulazs/lazycompose/internal/registry"
)

// Services whose image tag points to a newer image in the registry than the
// one their container runs get a marker. The registry is asked at most once
// per UpdateEvery for each image; after an action only the local side is
// checked again, so a pull and up clears the marker straight away.

const updateMark = "↑"

// firstCheckAfter lets the app start before the first check.
const firstCheckAfter = 5 * time.Second

// registryWorkers is how many registry requests run at once.
const registryWorkers = 4

type remoteDigest struct {
	digest string // "" when the registry could not be asked
	at     time.Time
}

type updateState struct {
	remote   map[string]remoteDigest    // by image ref
	avail    map[string]map[string]bool // env dir -> services with an update
	checking bool
	again    bool // a check was asked for while one was running
}

type updateTickMsg struct{}

type updatesCheckedMsg struct {
	images []compose.ServiceImage
	remote map[string]remoteDigest // refs asked this time
	err    error
}

func (m Model) hasUpdate(dir string) bool { return len(m.upd.avail[dir]) > 0 }

func (m Model) firstUpdateCheck() tea.Cmd {
	if m.opts.UpdateEvery <= 0 {
		return nil
	}
	return tea.Tick(firstCheckAfter, func(time.Time) tea.Msg { return updateTickMsg{} })
}

// recheckUpdates runs a check after an action changed containers or images.
func (m *Model) recheckUpdates() tea.Cmd {
	if m.opts.UpdateEvery <= 0 {
		return nil
	}
	if m.upd.checking {
		m.upd.again = true
		return nil
	}
	return m.startUpdateCheck()
}

func (m *Model) startUpdateCheck() tea.Cmd {
	m.upd.checking = true
	cache := make(map[string]remoteDigest, len(m.upd.remote))
	for k, v := range m.upd.remote {
		cache[k] = v
	}
	return checkUpdates(registry.New(), cache, m.opts.UpdateEvery)
}

func (m Model) handleUpdates(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case updateTickMsg:
		next := tea.Tick(m.opts.UpdateEvery, func(time.Time) tea.Msg { return updateTickMsg{} })
		if m.upd.checking {
			return m, next
		}
		return m, tea.Batch(next, m.startUpdateCheck())

	case updatesCheckedMsg:
		m.upd.checking = false
		if msg.err == nil {
			if m.upd.remote == nil {
				m.upd.remote = map[string]remoteDigest{}
			}
			for k, v := range msg.remote {
				m.upd.remote[k] = v
			}
			m.upd.avail = findUpdates(msg.images, m.upd.remote)
			m.refreshTable()
		}
		if m.upd.again {
			m.upd.again = false
			return m, m.startUpdateCheck()
		}
	}
	return m, nil
}

// findUpdates lists services whose image is not the one the registry has.
// Images without registry digests were built locally and are skipped.
func findUpdates(images []compose.ServiceImage, remote map[string]remoteDigest) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, img := range images {
		r := remote[img.Ref]
		if r.digest == "" || len(img.Digests) == 0 || slices.Contains(img.Digests, r.digest) {
			continue
		}
		if out[img.Dir] == nil {
			out[img.Dir] = map[string]bool{}
		}
		out[img.Dir][img.Service] = true
	}
	return out
}

type digester interface {
	Digest(ctx context.Context, ref registry.Ref) (string, error)
}

// checkUpdates reads the local images and asks the registry about the refs
// that are not in cache or are older than every.
func checkUpdates(reg digester, cache map[string]remoteDigest, every time.Duration) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		images, err := compose.ServiceImages(ctx)
		if err != nil {
			return updatesCheckedMsg{err: err}
		}
		return updatesCheckedMsg{images: images, remote: askRegistries(ctx, reg, images, cache, every)}
	}
}

func askRegistries(ctx context.Context, reg digester, images []compose.ServiceImage, cache map[string]remoteDigest, every time.Duration) map[string]remoteDigest {
	now := time.Now()
	var refs []string
	seen := map[string]bool{}
	for _, img := range images {
		if seen[img.Ref] || len(img.Digests) == 0 {
			continue
		}
		seen[img.Ref] = true
		if c, ok := cache[img.Ref]; ok && now.Sub(c.at) < every {
			continue
		}
		refs = append(refs, img.Ref)
	}

	out := map[string]remoteDigest{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, registryWorkers)
	for _, ref := range refs {
		r, ok := registry.ParseRef(ref)
		if !ok {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			d, err := reg.Digest(ctx, r)
			if err != nil {
				d = "" // private or unreachable: remembered, so it is not retried until next time
			}
			mu.Lock()
			out[ref] = remoteDigest{digest: d, at: now}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}
