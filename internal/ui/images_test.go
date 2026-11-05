package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/mcbulazs/lazycompose/internal/compose"
)

func TestOldImages(t *testing.T) {
	m := newTestModel(t)
	m.busy = 1
	imgs := []compose.OldImage{
		{ID: "sha256:aaaaaaaaaaaaaaaa", Repo: "nginx", Size: 45_000_000, Created: time.Now().Add(-21 * 24 * time.Hour)},
		{ID: "sha256:bbbbbbbbbbbbbbbb", Size: 5_000_000, Created: time.Now()},
	}
	m = send(m, oldImagesMsg{images: imgs})
	if m.confirm == nil || !strings.Contains(m.confirm.prompt, "Remove 2 old images (up to 50.0MB)") {
		t.Fatalf("should ask before removing: %+v", m.confirm)
	}
	out := ansi.Strip(strings.Join(m.logLines, "\n"))
	for _, want := range []string{"nginx", "45.0MB", "3 weeks ago", "aaaaaaaaaaaa", "(unknown)"} {
		if !strings.Contains(out, want) {
			t.Errorf("the list should show %q:\n%s", want, out)
		}
	}

	m = newTestModel(t)
	m.busy = 1
	if m = send(m, oldImagesMsg{}); m.confirm != nil || m.status != "no old images to remove" {
		t.Errorf("no images: status %q", m.status)
	}

	// after update all only the status mentions them
	m.setStatus("update done: 1 updated", false)
	m = send(m, oldImagesMsg{images: imgs, hint: true})
	if m.confirm != nil || !strings.Contains(m.status, "2 old images (50.0MB), P removes them") {
		t.Errorf("hint status %q", m.status)
	}
}

func TestFormatSize(t *testing.T) {
	for b, want := range map[int64]string{999: "999B", 1500: "1.5kB", 45_000_000: "45.0MB", 2_300_000_000: "2.3GB"} {
		if got := formatSize(b); got != want {
			t.Errorf("formatSize(%d) = %q, want %q", b, got, want)
		}
	}
}
