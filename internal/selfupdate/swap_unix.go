//go:build !windows

package selfupdate

import "os"

// swap moves the new binary at tmp over exe in one rename, so a failure
// leaves the old one in place.
func swap(tmp, exe string) error { return os.Rename(tmp, exe) }

// RemoveOld removes what a Windows update leaves behind; there is nothing
// to remove elsewhere.
func RemoveOld(string) {}
