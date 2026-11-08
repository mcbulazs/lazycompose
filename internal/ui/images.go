package ui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mcbulazs/lazycompose/internal/compose"
)

type oldImagesMsg struct {
	images []compose.OldImage
	err    error
	hint   bool // only mention them in the status, after update all
}

func findOldImages(hint bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		images, err := compose.OldImages(ctx)
		return oldImagesMsg{images: images, err: err, hint: hint}
	}
}

// startCleanup looks for old images to remove.
func (m *Model) startCleanup() tea.Cmd {
	if m.updating != nil {
		m.setStatus("wait for update all to finish", true)
		return nil
	}
	return tea.Batch(m.startBusy("looking for old images"), findOldImages(false))
}

// onOldImages lists the old images in the log pane and asks before
// removing them.
func (m *Model) onOldImages(msg oldImagesMsg) tea.Cmd {
	var total int64
	for _, img := range msg.images {
		total += img.Size
	}
	if msg.hint {
		if msg.err == nil && len(msg.images) > 0 && m.updating == nil && !m.statusErr {
			m.setStatus(fmt.Sprintf("%s · %d old images (%s), %s removes them",
				m.status, len(msg.images), formatSize(total), keys.CleanImages.Help().Key), false)
		}
		return nil
	}
	m.busy = max(0, m.busy-1)
	if msg.err != nil {
		m.setStatus("looking for old images: "+firstLine(msg.err.Error()), true)
		return nil
	}
	if len(msg.images) == 0 {
		m.setStatus("no old images to remove", false)
		return nil
	}

	m.stopLogs()
	m.holdOutput = true
	m.logTitle = "Old images"
	m.logLines = nil
	m.search = logSearch{}
	m.logs.SetContent("")
	m.appendLog(dimStyle.Render("untagged images no container uses, mostly older versions left by pulls:"))
	ids := make([]string, len(msg.images))
	for i, img := range msg.images {
		ids[i] = img.ID
		repo := cleanLine(img.Repo) // from docker, so kept away from the terminal
		if repo == "" {
			repo = "(unknown)"
		}
		m.appendLog(fmt.Sprintf("  %-30s %8s  %-14s %s", repo, formatSize(img.Size),
			ago(time.Since(img.Created)), dimStyle.Render(shortID(img.ID))))
	}
	return m.ask(&confirm{
		title:  "Remove old images",
		prompt: fmt.Sprintf("Remove %d old images (up to %s)? (y/n)", len(ids), formatSize(total)),
		onYes: func(m *Model) tea.Cmd {
			label := fmt.Sprintf("docker image rm (%d old images)", len(ids))
			return m.runStream("removing old images", label, func(id int) (*logStream, tea.Cmd) {
				return startDocker(id, append([]string{"image", "rm"}, ids...)...)
			})
		},
	})
}

func shortID(id string) string {
	const prefix = "sha256:"
	if len(id) > len(prefix)+12 {
		return id[len(prefix) : len(prefix)+12]
	}
	return id
}

// formatSize writes bytes the way docker does, in powers of 1000.
func formatSize(b int64) string {
	units := []string{"B", "kB", "MB", "GB", "TB"}
	f := float64(b)
	i := 0
	for f >= 1000 && i < len(units)-1 {
		f /= 1000
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%dB", b)
	}
	return fmt.Sprintf("%.1f%s", f, units[i])
}

// ago writes a duration roughly, like "3 weeks ago".
func ago(d time.Duration) string {
	day := 24 * time.Hour
	n, unit := 0, ""
	switch {
	case d < time.Hour:
		return "just now"
	case d < day:
		n, unit = int(d/time.Hour), "hour"
	case d < 14*day:
		n, unit = int(d/day), "day"
	case d < 60*day:
		n, unit = int(d/(7*day)), "week"
	case d < 730*day:
		n, unit = int(d/(30*day)), "month"
	default:
		n, unit = int(d/(365*day)), "year"
	}
	if n != 1 {
		unit += "s"
	}
	return fmt.Sprintf("%d %s ago", n, unit)
}
