package history

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestTop(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	h := Load("")
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	day := 24 * time.Hour

	// often, but weeks ago
	for range 5 {
		h.Record("/old", ago(20*day))
	}
	// once, just now
	h.Record("/new", ago(time.Minute))
	// often and recent
	for range 3 {
		h.Record("/busy", ago(time.Hour))
	}
	// too long ago to count
	h.Record("/gone", ago(200*day))

	dirs := []string{"/old", "/new", "/busy", "/gone", "/never"}
	got := h.Top(dirs, 5, now)
	want := []string{"/busy", "/old", "/new"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Top = %v, want %v", got, want)
	}
	if got := h.Top(dirs, 2, now); len(got) != 2 {
		t.Errorf("Top(2) gave %d dirs", len(got))
	}
	// dirs that no longer exist are not listed
	if got := h.Top([]string{"/new"}, 5, now); !reflect.DeepEqual(got, []string{"/new"}) {
		t.Errorf("Top of one dir = %v", got)
	}
}

func TestKeepsLastUses(t *testing.T) {
	h := Load("")
	now := time.Now()
	for i := range 15 {
		h.Record("/a", now.Add(time.Duration(i)*time.Second))
	}
	if len(h.Uses["/a"]) != keep {
		t.Fatalf("kept %d uses, want %d", len(h.Uses["/a"]), keep)
	}
	if !h.Uses["/a"][0].Equal(now.Add(14 * time.Second)) {
		t.Error("newest use should come first")
	}
}

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "history.json")
	h := Load(path)
	now := time.Now().Truncate(time.Second)
	h.Record("/a", now)
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}
	got := Load(path)
	if len(got.Uses["/a"]) != 1 || !got.Uses["/a"][0].Equal(now) {
		t.Errorf("loaded %v", got.Uses)
	}
}

func TestLoadMissingOrBroken(t *testing.T) {
	dir := t.TempDir()
	if h := Load(filepath.Join(dir, "missing.json")); len(h.Uses) != 0 {
		t.Error("missing file should give an empty history")
	}
}

func TestPins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	h := Load(path)
	now := time.Now()
	for _, d := range []string{"/a", "/b", "/c", "/d"} {
		h.Record(d, now)
	}
	h.Record("/a", now) // most used

	if !h.TogglePin("/c") || !h.TogglePin("/gone") || !h.TogglePin("/b") {
		t.Fatal("pinning should report pinned")
	}
	dirs := []string{"/a", "/b", "/c", "/d", "/e"}
	if got, want := h.Quick(dirs, 3, now, true), []string{"/c", "/b", "/a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Quick = %v, want pins first, then by use: %v", got, want)
	}
	if got, want := h.Quick(dirs, 5, now, false), []string{"/c", "/b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Quick without use = %v, want only pins %v", got, want)
	}
	if got := h.Quick(dirs, 1, now, true); len(got) != 2 {
		t.Errorf("all pins should show even past n, got %v", got)
	}

	if h.TogglePin("/c") || h.Pinned("/c") {
		t.Error("toggling again should unpin")
	}
	h.Move("/b", "/b2")
	if !h.Pinned("/b2") || h.Pinned("/b") {
		t.Error("the pin should follow a move")
	}
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}
	if got := Load(path).Pins; !reflect.DeepEqual(got, []string{"/gone", "/b2"}) {
		t.Errorf("loaded pins %v", got)
	}
}
