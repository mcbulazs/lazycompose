//go:build !windows

package stackfs

import (
	"errors"
	"syscall"
)

// noFollow makes OpenFile fail on a symlink, so a file swapped for one
// while copying is not followed.
const noFollow = syscall.O_NOFOLLOW

// crossDevice reports whether a rename failed because the target is on
// another filesystem.
func crossDevice(err error) bool { return errors.Is(err, syscall.EXDEV) }
