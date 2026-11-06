package stackfs

import (
	"errors"
	"syscall"
)

// noFollow is not available on Windows. Creating symlinks there needs
// extra rights, so a file swapped for one while copying is unlikely.
const noFollow = 0

// errNotSameDevice is ERROR_NOT_SAME_DEVICE, from a rename to another drive.
const errNotSameDevice = syscall.Errno(17)

// crossDevice reports whether a rename failed because the target is on
// another drive.
func crossDevice(err error) bool { return errors.Is(err, errNotSameDevice) }
