//go:build linux

package file

import (
	"os"

	"golang.org/x/sys/unix"
)

// LockMode selects one whole-file lock operation and an optional nonblocking
// modifier. The zero value is invalid.
type LockMode int

// LockShared acquires a lock that permits other shared locks.
const LockShared LockMode = unix.LOCK_SH

// LockExclusive acquires a lock that excludes other shared and exclusive locks.
const LockExclusive LockMode = unix.LOCK_EX

// Unlock releases the lock on the open file description.
const Unlock LockMode = unix.LOCK_UN

// LockNonblock makes an acquisition fail with EWOULDBLOCK on contention.
// Combine it with a lock operation.
const LockNonblock LockMode = unix.LOCK_NB

// Flock acquires, converts, or releases a Linux whole-file advisory lock.
// Use exactly one of [LockShared], [LockExclusive], or [Unlock], optionally
// combined with [LockNonblock]. Filesystems can impose additional access and
// locking rules.
//
// Duplicates share one lock, which [Unlock] on any duplicate or closure of the
// final reference releases. Separate opens can contend with each other even
// in one process. Converting a lock can release the old lock before acquiring
// the new one, so a failed conversion can leave the file unlocked.
//
// Flock borrows f. A blocking acquisition can occupy an OS thread and cannot
// be canceled by closing f or setting its deadline. A nil or zero file or an
// invalid mode matches [os.ErrInvalid]. With a valid mode, a closed file matches
// [os.ErrClosed]. Errors use [os.PathError] and preserve errno.
func Flock(f *os.File, mode LockMode) error {
	if f == nil || *f == (os.File{}) {
		return &os.PathError{Op: "flock", Err: os.ErrInvalid}
	}
	switch mode &^ LockNonblock {
	case LockShared, LockExclusive, Unlock:
	default:
		return &os.PathError{Op: "flock", Path: f.Name(), Err: os.ErrInvalid}
	}

	if err := control(f, func(fd int) error {
		return unix.Flock(fd, int(mode))
	}); err != nil {
		return &os.PathError{Op: "flock", Path: f.Name(), Err: err}
	}
	return nil
}
