//go:build linux

package memfd

import (
	"os"

	"golang.org/x/sys/unix"
)

// Options controls creation through [memfd_create(2)]. The zero value creates
// a sealable file with execute permission bits cleared and locked by SealExec.
//
// [memfd_create(2)]: https://man7.org/linux/man-pages/man2/memfd_create.2.html
type Options struct {
	// Executable requests MFD_EXEC instead of MFD_NOEXEC_SEAL. Kernel or
	// namespace policy may reject it; New preserves the error without fallback.
	// This permits execute mode bits but does not execute the file.
	Executable bool
}

// Seals is an inode's irreversible seal bitmask. Combine Seal constants with |.
// Seals returned by File.Seals may contain bits unknown to this version.
type Seals uint32

// SealSeal prevents further AddSeals calls, including calls with a zero mask.
// It wraps Linux F_SEAL_SEAL; see the package's sealing reference.
const SealSeal Seals = unix.F_SEAL_SEAL

// SealShrink prevents reducing the file size (Linux F_SEAL_SHRINK).
const SealShrink Seals = unix.F_SEAL_SHRINK

// SealGrow prevents increasing the file size, including writes beyond its end
// (Linux F_SEAL_GROW).
const SealGrow Seals = unix.F_SEAL_GROW

// SealWrite prevents writes and new shared writable mappings, but permits
// resizing. Adding it fails with EBUSY while a shared writable mapping exists
// (Linux F_SEAL_WRITE).
const SealWrite Seals = unix.F_SEAL_WRITE

// SealFutureWrite prevents writes and new shared writable mappings while
// allowing writes through existing shared writable mappings
// (Linux F_SEAL_FUTURE_WRITE).
const SealFutureWrite Seals = unix.F_SEAL_FUTURE_WRITE

// SealExec locks the file's execute permission bits (Linux F_SEAL_EXEC).
// When execute bits are set, adding it also adds SealShrink, SealGrow,
// SealWrite, and SealFutureWrite. It does not prevent executable mappings.
const SealExec Seals = unix.F_SEAL_EXEC

// New creates an empty, readable and writable memory-backed file using
// [memfd_create(2)]. The caller owns the result and must close it.
//
// The descriptor is atomically close-on-exec and allows sealing. By default,
// MFD_NOEXEC_SEAL clears and locks execute permission bits; Options.Executable
// requests MFD_EXEC instead. There is no fallback to weaker creation flags.
//
// The name is a debugging label, not a path. Empty names are allowed; embedded
// NUL bytes and names longer than 249 bytes fail with EINVAL. Errors wrap the
// underlying errno in an os.PathError. On failure no descriptor is retained.
//
// [memfd_create(2)]: https://man7.org/linux/man-pages/man2/memfd_create.2.html
func New(name string, options Options) (*File, error) {
	flags := unix.MFD_CLOEXEC | unix.MFD_ALLOW_SEALING
	if options.Executable {
		flags |= unix.MFD_EXEC
	} else {
		flags |= unix.MFD_NOEXEC_SEAL
	}

	fd, err := unix.MemfdCreate(name, flags)
	if err != nil {
		return nil, &os.PathError{Op: "memfd_create", Path: name, Err: err}
	}

	return &File{File: os.NewFile(uintptr(fd), name)}, nil
}

// File owns a memory-backed file and adds sealing to the embedded os.File.
// Do not replace the embedded pointer, construct a File from an unrelated file,
// or copy a File after first use. Borrow File.File when an API needs *os.File;
// closing either view closes the same owner.
//
// Methods are safe for concurrent use under os.File's guarantees. Sealing
// operations protect descriptor lifetime against concurrent Close, but sequences
// of operations are not transactions. The zero value owns no descriptor and
// error-returning operations report os.ErrInvalid. After successful creation,
// repeated Close and operations after Close match os.ErrClosed.
//
// Ordinary I/O, sizing, and seeking use os.File semantics. Deadlines are not
// supported. Within a SyscallConn callback, do not close or retain the borrowed
// descriptor; duplicate it if independent ownership is required.
type File struct {
	*os.File
}

// Seals queries the inode's seals with [F_GET_SEALS], preserving unknown bits.
// A concurrent AddSeals can make the returned snapshot stale immediately.
// Errors preserve errno identity through errors.Is and errors.As.
//
// [F_GET_SEALS]: https://man7.org/linux/man-pages/man2/F_GET_SEALS.2const.html
func (f *File) Seals() (Seals, error) {
	seals, err := f.fcntl("F_GET_SEALS", unix.F_GET_SEALS, 0)
	if err != nil {
		return 0, err
	}

	return Seals(seals), nil //nolint:gosec // F_GET_SEALS returns a nonnegative 32-bit mask.
}

// AddSeals irreversibly adds the mask using [F_ADD_SEALS]. Unknown bits are
// rejected with os.ErrInvalid before the syscall. A zero mask changes nothing
// but still reaches the kernel and fails with EPERM after SealSeal is set.
// Adding existing seals otherwise succeeds. Kernel-implied seals are retained.
// Errors preserve errno identity; no requested guarantee is silently weakened.
//
// [F_ADD_SEALS]: https://man7.org/linux/man-pages/man2/F_GET_SEALS.2const.html
func (f *File) AddSeals(seals Seals) error {
	if f.File == nil {
		return os.ErrInvalid
	}

	const supported = SealSeal | SealShrink | SealGrow | SealWrite | SealFutureWrite | SealExec
	if seals & ^supported != 0 {
		return &os.PathError{Op: "F_ADD_SEALS", Path: f.Name(), Err: os.ErrInvalid}
	}

	_, err := f.fcntl("F_ADD_SEALS", unix.F_ADD_SEALS, int(seals))
	return err
}

func (f *File) fcntl(op string, command, argument int) (int, error) {
	conn, err := f.SyscallConn()
	if err != nil {
		return 0, err
	}

	var result int
	var callErr error
	err = conn.Control(func(fd uintptr) {
		result, callErr = unix.FcntlInt(fd, command, argument)
	})

	if err != nil {
		// os.File's raw Control can only fail here if Close prevented the
		// callback. Its internal closing error does not match os.ErrClosed.
		err = os.ErrClosed
	} else {
		err = callErr
	}
	if err != nil {
		return 0, &os.PathError{Op: op, Path: f.Name(), Err: err}
	}

	return result, nil
}
