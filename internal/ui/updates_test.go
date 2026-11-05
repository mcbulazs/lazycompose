package ui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/mcbulazs/lazycompose/internal/compose"
	"github.com/mcbulazs/lazycompose/internal/registry"
)

type fakeRegistry struct {
	mu      sync.Mutex
	digests map[string]string // repo -> digest; missing repos fail
	asked   []string
}

func (f *fakeRegistry) Digest(_ context.Context, r registry.Ref) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, r.Repo)
	if d, ok := f.digests[r.Repo]; ok {
		return d, nil
	}
	return "", errors.New("401")
}

func testImages() []compose.ServiceImage {
	return []compose.ServiceImage{
		{Dir: "/srv/plex", Service: "plex", Ref: "plexinc/pms-docker", Digests: []string{"sha256:old"}},
		{Dir: "/srv/web/blog", Service: "web", Ref: "nginx:alpine", Digests: []string{"sha256:new"}},
		{Dir: "/srv/web/shop", Service: "web", Ref: "nginx:alpine", Digests: []string{"sha256:new"}},
		{Dir: "/srv/web/shop", Service: "app", Ref: "shop-app"}, // built locally
		{Dir: "/srv/games/mc", Service: "mc", Ref: "ghcr.io/me/private", Digests: []string{"sha256:x"}},
	}
}

func TestAskRegistries(t *testing.T) {
	reg := &fakeRegistry{digests: map[string]string{"plexinc/pms-docker": "sha256:newer", "library/nginx": "sha256:new"}}
	got := askRegistries(context.Background(), reg, testImages(), nil, time.Hour)
	if len(reg.asked) != 3 {
		t.Errorf("asked %v; each ref once, and not locally built images", reg.asked)
	}
	if got["plexinc/pms-docker"].digest != "sha256:newer" || got["ghcr.io/me/private"].digest != "" {
		t.Errorf("remote = %v", got)
	}
	if _, ok := got["ghcr.io/me/private"]; !ok {
		t.Error("failures should be remembered so they are not retried every time")
	}

	updates := findUpdates(testImages(), got)
	if !updates["/srv/plex"]["plex"] || len(updates) != 1 {
		t.Errorf("updates = %v, want only plex", updates)
	}

	// fresh answers are not asked again, old ones are
	reg.asked = nil
	cache := map[string]remoteDigest{
		"plexinc/pms-docker": {digest: "sha256:newer", at: time.Now()},
		"nginx:alpine":       {digest: "sha256:new", at: time.Now().Add(-2 * time.Hour)},
		"ghcr.io/me/private": {at: time.Now()},
	}
	askRegistries(context.Background(), reg, testImages(), cache, time.Hour)
	if len(reg.asked) != 1 || reg.asked[0] != "library/nginx" {
		t.Errorf("asked %v, want only the stale nginx", reg.asked)
	}
}

func TestUpdateMarkers(t *testing.T) {
	m := newTestModel(t)
	m.opts.UpdateEvery = time.Hour
	remote := map[string]remoteDigest{"plexinc/pms-docker": {digest: "sha256:newer", at: time.Now()}}
	m = send(m, updatesCheckedMsg{images: testImages(), remote: remote})

	if !m.hasUpdate("/srv/plex") || m.hasUpdate("/srv/web/blog") {
		t.Fatal("only plex should have an update")
	}
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "↑ 1 with updates") {
		t.Errorf("header should count updates:\n%s", v)
	}
	var plexLine string
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "plex") && strings.Contains(l, "0/0") {
			plexLine = l
		}
	}
	if !strings.Contains(plexLine, "↑") {
		t.Errorf("plex row should have the marker: %q", plexLine)
	}

	// after a pull and up the container runs the new image
	images := testImages()
	images[0].Digests = []string{"sha256:newer"}
	m = send(m, updatesCheckedMsg{images: images})
	if m.hasUpdate("/srv/plex") {
		t.Error("marker should go once the new image runs")
	}
}

func TestRecheckWhileChecking(t *testing.T) {
	m := newTestModel(t)
	m.opts.UpdateEvery = time.Hour
	m.upd.checking = true
	if cmd := m.recheckUpdates(); cmd != nil || !m.upd.again {
		t.Error("a recheck during a check should wait for it")
	}
	m.opts.UpdateEvery = 0
	m.upd = updateState{}
	if m.recheckUpdates() != nil || m.firstUpdateCheck() != nil {
		t.Error("update_check 0 should never check")
	}
}
