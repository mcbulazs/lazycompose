package ui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mcbulazs/lazycompose/internal/remote"
)

// Temporary files and folders carry the id of the lazycompose process that
// made them: copies of remote files being edited, and the log pane opened
// in the editor. They are removed when it quits; RemoveLeftovers removes
// those of a lazycompose that did not get to, after a crash.

// tempPrefixes start the names of the temporary files and folders.
var tempPrefixes = []string{editDirPrefix, logsFilePrefix, remote.AskpassDirPrefix}

// RemoveLeftovers removes the temporary files and folders whose process is
// no longer running.
func RemoveLeftovers() {
	removeLeftovers(os.TempDir(), processAlive)
}

func removeLeftovers(dir string, alive func(pid int) bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, en := range entries {
		for _, prefix := range tempPrefixes {
			rest, ok := strings.CutPrefix(en.Name(), prefix)
			if !ok {
				continue
			}
			id, _, ok := strings.Cut(rest, "-")
			pid, err := strconv.Atoi(id)
			if !ok || err != nil || pid <= 0 || pid == os.Getpid() || alive(pid) {
				continue
			}
			_ = os.RemoveAll(filepath.Join(dir, en.Name()))
		}
	}
}
