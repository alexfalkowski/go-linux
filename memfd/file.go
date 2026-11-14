//go:build linux

package memfd

import (
	"io"
	"os"
	"time"

	"github.com/alexfalkowski/go-linux/mmap"
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

	return &File{file: os.NewFile(uintptr(fd), name)}, nil
}

// File owns a memory-backed file with ordinary file I/O and Linux sealing.
// Methods are safe for concurrent use under os.File's guarantees. Do not copy
// a File after first use. Sequences of operations are not transactions.
// The zero value owns nothing and error-returning operations match os.ErrInvalid.
// I/O and sealing after Close, including repeated Close, match os.ErrClosed.
// Deadline methods retain os.File errors. Nil receivers are unsupported.
type File struct {
	file *os.File
}

// Name returns the debugging label supplied to New, including after Close.
func (f *File) Name() string {
	return f.file.Name()
}

// Read reads bytes at the current offset, as in os.File.Read.
func (f *File) Read(p []byte) (int, error) {
	return f.file.Read(p)
}

// ReadAt reads at offset without changing the current file offset.
func (f *File) ReadAt(p []byte, offset int64) (int, error) {
	return f.file.ReadAt(p, offset)
}

// Write writes at the current offset, subject to the inode's seals.
func (f *File) Write(p []byte) (int, error) {
	return f.file.Write(p)
}

// WriteAt writes at offset without changing the current file offset.
func (f *File) WriteAt(p []byte, offset int64) (int, error) {
	return f.file.WriteAt(p, offset)
}

// WriteString writes s at the current offset, as in os.File.WriteString.
func (f *File) WriteString(s string) (int, error) {
	return f.file.WriteString(s)
}

// ReadFrom copies r into the file at its current offset until EOF or an error.
func (f *File) ReadFrom(r io.Reader) (int64, error) {
	// Expose only Read: os.File's copy fallback could otherwise call r.WriteTo
	// with a writer exposing the underlying file's Fd and SyscallConn methods.
	return f.file.ReadFrom(struct{ io.Reader }{r})
}

// WriteTo copies from the current offset into w until EOF or an error.
func (f *File) WriteTo(w io.Writer) (int64, error) {
	// Expose only Write: os.File's copy fallback could otherwise call w.ReadFrom
	// with a reader exposing the underlying file's Fd and SyscallConn methods.
	return f.file.WriteTo(struct{ io.Writer }{w})
}

// Seek sets the current offset, as in os.File.Seek.
func (f *File) Seek(offset int64, whence int) (int64, error) {
	return f.file.Seek(offset, whence)
}

// Stat returns a snapshot of the file's metadata, as in os.File.Stat.
func (f *File) Stat() (os.FileInfo, error) {
	return f.file.Stat()
}

// Truncate sets the size, subject to the inode's seals. Growth is zero-filled.
// It does not change the current offset. Coordinate size changes with mappings
// to avoid accessing pages beyond EOF.
func (f *File) Truncate(size int64) error {
	return f.file.Truncate(size)
}

// Chmod changes permission bits, subject to seals and kernel policy.
func (f *File) Chmod(mode os.FileMode) error {
	return f.file.Chmod(mode)
}

// Chown changes ownership, as in os.File.Chown. A value of -1 leaves that ID
// unchanged. Kernel permission checks apply.
func (f *File) Chown(uid, gid int) error {
	return f.file.Chown(uid, gid)
}

// Sync invokes os.File.Sync. A memory-backed file has no persistent storage.
func (f *File) Sync() error {
	return f.file.Sync()
}

// Map borrows this file to create a region with mmap.Map's semantics. The file
// must be sized before accessing mapped pages. The caller owns the region and
// must close it independently; closing this file leaves the region valid.
// Failure returns nil and leaves this file open. Seals can prohibit Shared
// writable mappings. Mapping options and offsets follow mmap.Map.
func (f *File) Map(offset int64, length int, options mmap.Options) (*mmap.Region, error) {
	return mmap.Map(f.file, offset, length, options)
}

// SetDeadline follows os.File.SetDeadline. Memory-backed files do not support
// deadlines; an open file returns os.ErrNoDeadline.
func (f *File) SetDeadline(deadline time.Time) error {
	return f.file.SetDeadline(deadline)
}

// SetReadDeadline follows os.File.SetReadDeadline; deadlines are unsupported.
func (f *File) SetReadDeadline(deadline time.Time) error {
	return f.file.SetReadDeadline(deadline)
}

// SetWriteDeadline follows os.File.SetWriteDeadline; deadlines are unsupported.
func (f *File) SetWriteDeadline(deadline time.Time) error {
	return f.file.SetWriteDeadline(deadline)
}

// Close releases the file. Existing mappings retain their own references.
func (f *File) Close() error {
	return f.file.Close()
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
	if f.file == nil {
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
	conn, err := f.file.SyscallConn()
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
