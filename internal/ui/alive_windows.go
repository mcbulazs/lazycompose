//go:build windows

package ui

import "os"

// processAlive reports whether a process with this id runs: on Windows
// FindProcess fails for one that does not.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = p.Release()
	return true
}
