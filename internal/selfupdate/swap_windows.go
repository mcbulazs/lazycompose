package selfupdate

import "os"

// swap puts the new binary at tmp in place of exe. Windows does not let a
// running program be replaced, but it can be renamed: the old one is moved
// aside to exe.old, and moved back if the new one cannot be put in place.
func swap(tmp, exe string) error {
	old := exe + ".old"
	_ = os.Remove(old) // left over from the last update
	if err := os.Rename(exe, old); err != nil {
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Rename(old, exe)
		return err
	}
	_ = os.Remove(old) // fails while the old one still runs; RemoveOld gets it later
	return nil
}

// RemoveOld removes the binary an earlier update moved aside, once it no
// longer runs.
func RemoveOld(exe string) { _ = os.Remove(exe + ".old") }
