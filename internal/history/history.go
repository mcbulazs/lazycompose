// Package history remembers which environments were used and when, to rank
// them by frecency: a mix of how often and how recently they were used.
package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// keep is how many uses are remembered per environment.
const keep = 10

type History struct {
	path string
	Uses map[string][]time.Time `json:"uses"` // env dir -> newest first
}

// DefaultPath is $XDG_STATE_HOME/lazycompose/history.json, or
// ~/.local/state/lazycompose/history.json.
func DefaultPath() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "lazycompose", "history.json")
}

// Load reads the history at path. A missing or broken file gives an empty
// history; an empty path gives one that is never saved.
func Load(path string) *History {
	h := &History{path: path, Uses: map[string][]time.Time{}}
	if path == "" {
		return h
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return h
	}
	var saved History
	if json.Unmarshal(b, &saved) == nil && saved.Uses != nil {
		h.Uses = saved.Uses
	}
	return h
}

// Record notes a use of dir at t.
func (h *History) Record(dir string, t time.Time) {
	uses := append([]time.Time{t}, h.Uses[dir]...)
	if len(uses) > keep {
		uses = uses[:keep]
	}
	h.Uses[dir] = uses
}

// Move carries the uses of dir over to its new place.
func (h *History) Move(from, to string) {
	if uses, ok := h.Uses[from]; ok {
		h.Uses[to] = uses
		delete(h.Uses, from)
	}
}

// Save writes the history, creating its folder.
func (h *History) Save() error {
	if h.path == "" {
		return nil
	}
	b, err := json.Marshal(h)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o700); err != nil {
		return err
	}
	tmp := h.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, h.path)
}

// weight of one use by its age, like Firefox's frecency buckets.
func weight(age time.Duration) float64 {
	day := 24 * time.Hour
	switch {
	case age < 4*time.Hour:
		return 100
	case age < day:
		return 80
	case age < 7*day:
		return 60
	case age < 30*day:
		return 40
	case age < 90*day:
		return 20
	default:
		return 0
	}
}

// Score is the frecency of dir at now.
func (h *History) Score(dir string, now time.Time) float64 {
	s := 0.0
	for _, t := range h.Uses[dir] {
		s += weight(now.Sub(t))
	}
	return s
}

// Top returns up to n of dirs with the highest score, best first. Dirs that
// were not used in the last 90 days are left out.
func (h *History) Top(dirs []string, n int, now time.Time) []string {
	type scored struct {
		dir   string
		score float64
		last  time.Time
	}
	var all []scored
	for _, d := range dirs {
		if s := h.Score(d, now); s > 0 {
			all = append(all, scored{d, s, h.Uses[d][0]})
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].score != all[j].score {
			return all[i].score > all[j].score
		}
		return all[i].last.After(all[j].last)
	})
	out := make([]string, 0, min(n, len(all)))
	for i := 0; i < len(all) && i < n; i++ {
		out = append(out, all[i].dir)
	}
	return out
}
