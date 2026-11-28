//go:build linux

package file

import (
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// RecordLockScope selects the Linux owner of a record lock. Its zero value
// is invalid, so each operation requires an explicit ownership choice.
type RecordLockScope int

// POSIXLocks are process-associated. Closing any descriptor for the same
// inode in the process releases all of its POSIX locks on that inode.
const POSIXLocks RecordLockScope = 1

// OFDLocks belong to the open file description. Duplicates share the locks,
// and closing the final reference releases them. Separate opens compete.
const OFDLocks RecordLockScope = 2

// RecordLockType selects read access, write access, or release for a range.
// Its zero value is [LockRead].
type RecordLockType int16

// LockRead requests a shared record lock. Acquisition requires read access.
const LockRead RecordLockType = unix.F_RDLCK

// LockWrite requests an exclusive record lock. Acquisition requires write access.
const LockWrite RecordLockType = unix.F_WRLCK

// LockRelease releases the caller's lock on the specified range.
const LockRelease RecordLockType = unix.F_UNLCK

// LockRange identifies a byte range. Its zero value covers the entire file.
type LockRange struct {
	// Start is the signed byte offset relative to Whence.
	Start int64
	// Length counts bytes forward from Start if positive, or backward up to
	// but excluding Start if negative. Zero covers all bytes from Start,
	// including any future growth.
	Length int64
	// Whence is [io.SeekStart] (the default), [io.SeekCurrent], or [io.SeekEnd].
	// The kernel resolves it without changing the file offset.
	Whence int
}

// RecordLock describes a Linux advisory lock request or a reported conflict.
type RecordLock struct {
	// Range identifies the affected bytes.
	Range LockRange
	// Type selects the lock or release operation. The default is [LockRead].
	Type RecordLockType
}

// RecordLockConflict describes one incompatible lock returned by the kernel.
type RecordLockConflict struct {
	// Lock describes the conflicting lock with an absolute byte range.
	Lock RecordLock
	// PID is the POSIX lock owner's PID in the caller's PID namespace, zero
	// for an invisible owner, or -1 for an OFD lock. Remote filesystems can
	// report filesystem-specific owner values.
	PID int
}

// QueryRecordLock asks whether a [LockRead] or [LockWrite] request would
// conflict under scope. It returns one conflicting lock with true, or a zero
// conflict with false. The result is a snapshot and reserves no bytes.
// The kernel considers conflicting locks from either scope.
//
// QueryRecordLock borrows f and wraps errors as described by [SetRecordLock].
func QueryRecordLock(f *os.File, scope RecordLockScope, lock RecordLock) (RecordLockConflict, bool, error) {
	native, err := recordLock(f, scope, lock, unix.F_GETLK, unix.F_OFD_GETLK)
	if err != nil {
		return RecordLockConflict{}, false, err
	}
	if native.Type == unix.F_UNLCK {
		return RecordLockConflict{}, false, nil
	}

	return RecordLockConflict{
		Lock: RecordLock{
			Range: LockRange{Start: native.Start, Length: native.Len, Whence: int(native.Whence)},
			Type:  RecordLockType(native.Type),
		},
		PID: int(native.Pid),
	}, true, nil
}

// SetRecordLock acquires, converts, or releases the requested range under
// scope without waiting for a conflicting lock. Contention matches EACCES
// or EAGAIN. Use [LockRelease] to release the specified bytes.
//
// SetRecordLock borrows f. A nil or zero file, unknown scope, invalid lock
// type, or invalid origin matches [os.ErrInvalid]. With valid options a
// closed file matches [os.ErrClosed]. Linux validates the resolved byte
// offsets and access mode. Errors use [os.PathError] and preserve errno.
func SetRecordLock(f *os.File, scope RecordLockScope, lock RecordLock) error {
	_, err := recordLock(f, scope, lock, unix.F_SETLK, unix.F_OFD_SETLK)
	return err
}

// WaitRecordLock applies the same request as [SetRecordLock], waiting for
// conflicts on acquisition or conversion. A wait can occupy an OS thread.
// Closing f or setting a Go deadline cannot cancel it. Kernel errors such
// as EINTR and, for [POSIXLocks], EDEADLK are returned without retry.
func WaitRecordLock(f *os.File, scope RecordLockScope, lock RecordLock) error {
	_, err := recordLock(f, scope, lock, unix.F_SETLKW, unix.F_OFD_SETLKW)
	return err
}

func recordLock(f *os.File, scope RecordLockScope, lock RecordLock, posixCommand, ofdCommand int) (unix.Flock_t, error) {
	if f == nil || *f == (os.File{}) {
		return unix.Flock_t{}, &os.PathError{Op: "fcntl", Err: os.ErrInvalid}
	}
	var command int
	switch scope {
	case POSIXLocks:
		command = posixCommand
	case OFDLocks:
		command = ofdCommand
	default:
		return unix.Flock_t{}, &os.PathError{Op: "fcntl", Path: f.Name(), Err: os.ErrInvalid}
	}

	if !lock.Type.validFor(posixCommand) {
		return unix.Flock_t{}, &os.PathError{Op: "fcntl", Path: f.Name(), Err: os.ErrInvalid}
	}

	span := lock.Range
	if span.Whence < io.SeekStart || span.Whence > io.SeekEnd {
		return unix.Flock_t{}, &os.PathError{Op: "fcntl", Path: f.Name(), Err: os.ErrInvalid}
	}
	native := unix.Flock_t{Type: int16(lock.Type), Whence: int16(span.Whence), Start: span.Start, Len: span.Length}
	if err := control(f, func(fd int) error {
		return unix.FcntlFlock(uintptr(fd), command, &native)
	}); err != nil {
		return unix.Flock_t{}, &os.PathError{Op: "fcntl", Path: f.Name(), Err: err}
	}
	return native, nil
}

func (kind RecordLockType) validFor(posixCommand int) bool {
	switch kind {
	case LockRead, LockWrite:
		return true
	case LockRelease:
		return posixCommand != unix.F_GETLK
	default:
		return false
	}
}
