//go:build linux

package mmap

import (
	"math"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

// Mode selects sharing behavior. Modes are exclusive, not a bitmask.
type Mode uint8

// Private creates a copy-on-write mapping (MAP_PRIVATE). Writes do not change
// the source file. Later file changes have unspecified visibility in the view.
const Private Mode = 0

// Shared shares changes with other mappings and the source file (MAP_SHARED).
// Visibility does not imply synchronization between readers and writers or
// durable storage. Anonymous Shared mappings have no source file.
const Shared Mode = 1

// Protection is a bitmask of requested page permissions. Combine Read and Write
// with |. Actual permissions may be broader on some architectures or process
// personalities; protection is not a security boundary.
type Protection uint8

// NoAccess prohibits memory access (PROT_NONE), and is the zero protection.
const NoAccess Protection = unix.PROT_NONE

// Read requests readable pages (PROT_READ).
const Read Protection = unix.PROT_READ

// Write requests writable pages (PROT_WRITE).
const Write Protection = unix.PROT_WRITE

// Options controls a new mapping. The zero value selects Private and NoAccess.
// Executable mappings and additional Linux mapping flags are not supported.
type Options struct {
	// Mode selects private copy-on-write or shared backing; zero is Private.
	Mode Mode

	// Protection requests page permissions; zero is NoAccess. Set Read and/or
	// Write explicitly before accessing Bytes.
	Protection Protection
}

// Advice selects one whole-region [madvise(2)] operation, not a bitmask.
// Advice can affect performance and, for DontNeed, the mapped contents.
//
// [madvise(2)]: https://man7.org/linux/man-pages/man2/madvise.2.html
type Advice uint8

// Normal restores the default access-pattern advice (MADV_NORMAL).
const Normal Advice = unix.MADV_NORMAL

// Random suggests that accesses will be random (MADV_RANDOM).
const Random Advice = unix.MADV_RANDOM

// Sequential suggests that accesses will be sequential (MADV_SEQUENTIAL).
const Sequential Advice = unix.MADV_SEQUENTIAL

// WillNeed suggests bringing pages into memory (MADV_WILLNEED). Success does
// not promise that all pages are resident or that later access cannot block.
const WillNeed Advice = unix.MADV_WILLNEED

// DontNeed discards this mapping's resident pages (MADV_DONTNEED). Subsequent
// access to private anonymous pages sees zeros; private file changes are lost
// and pages are reloaded from the file. Shared pages are repopulated from their
// backing object, which need not be freed immediately. This does not truncate
// the file or promise writeback, durability, or zeroing of shared contents.
const DontNeed Advice = unix.MADV_DONTNEED

// Map creates a file-backed region using [mmap(2)]. It borrows file only during
// the call, protecting descriptor lifetime against concurrent file.Close. The
// caller owns the returned region and must close it independently of the file.
// Closing file after success does not invalidate the mapping.
//
// The length must be positive and offset nonnegative and page-aligned, using
// os.Getpagesize. Invalid arguments or unknown options match os.ErrInvalid;
// offset+length beyond int64 matches EOVERFLOW. Validation precedes file access.
// A nil file matches os.ErrInvalid; a closed file matches os.ErrClosed.
// Errors wrap an os.PathError and preserve underlying errno identity.
//
// The file must permit reading; Shared with Write also requires write access.
// Mapping does not extend the file or freeze its size: accessing whole pages
// beyond EOF, including after truncation, can cause a fatal SIGBUS. File size
// checks cannot prevent concurrent truncation; callers own that coordination.
// Close does not synchronize dirty pages to durable storage.
//
// [mmap(2)]: https://man7.org/linux/man-pages/man2/mmap.2.html
func Map(file *os.File, offset int64, length int, options Options) (*Region, error) {
	flags, err := mappingFlags(offset, length, options)
	if err != nil {
		return nil, &os.PathError{Op: "mmap", Path: "mmap", Err: err}
	}

	conn, err := file.SyscallConn()
	if err != nil {
		return nil, &os.PathError{Op: "mmap", Path: "mmap", Err: err}
	}

	var data []byte
	var callErr error
	err = conn.Control(func(fd uintptr) {
		data, callErr = unix.Mmap(int(fd), offset, length, int(options.Protection), flags)
	})
	if err != nil {
		// os.File's raw Control reports its private closing error when Close
		// prevents the callback; normalize it to the public owner error.
		err = os.ErrClosed
	} else {
		err = callErr
	}
	if err != nil {
		return nil, &os.PathError{Op: "mmap", Path: file.Name(), Err: err}
	}

	return &Region{data: data}, nil
}

// Anonymous creates a zero-initialized region using [mmap(2)] with
// MAP_ANONYMOUS. The caller owns it and must close it. Options and positive
// length are validated as in Map. Shared selects a shared anonymous backing
// object; separate Anonymous calls always create independent objects.
//
// [mmap(2)]: https://man7.org/linux/man-pages/man2/mmap.2.html
func Anonymous(length int, options Options) (*Region, error) {
	flags, err := mappingFlags(0, length, options)
	if err != nil {
		return nil, &os.PathError{Op: "mmap", Path: "anonymous", Err: err}
	}

	data, err := unix.Mmap(-1, 0, length, int(options.Protection), flags|unix.MAP_ANONYMOUS)
	if err != nil {
		return nil, &os.PathError{Op: "mmap", Path: "anonymous", Err: err}
	}

	return &Region{data: data}, nil
}

// Region owns one complete mapping. Do not copy a Region after first use.
// Nil receivers are unsupported. The zero value owns nothing: Bytes returns
// nil and error-returning methods match os.ErrInvalid. After successful Close,
// Bytes returns nil and all other methods, including Close, match os.ErrClosed.
//
// Methods serialize their operations, but returned slices escape that lock.
// Callers must coordinate ALL slice use with Close, Protect, and Advise, and
// synchronize conflicting memory accesses. A concurrent Bytes call does not
// keep its result mapped. There is no finalizer: unmapping is explicit.
type Region struct {
	data   []byte
	mu     sync.Mutex
	closed bool
}

// Bytes returns a borrowed view with length and capacity equal to the requested
// mapping length. It does not copy or check page permissions. Normal Go bounds
// checks apply; page rounding does not permit extending the slice.
//
// Every view, subslice, or pointer becomes invalid after successful Close.
// Access then can fault or silently corrupt an unrelated mapping at a reused
// address. Keeping a slice or Region reachable does not prevent explicit Close.
func (r *Region) Bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.data
}

// Protect changes permissions on all pages intersecting the region through
// [mprotect(2)]. Only NoAccess, Read, Write, or Read|Write are accepted; unknown
// bits match os.ErrInvalid without changing permissions. Owner-state errors
// take precedence. Kernel errors retain their errno; do not assume a failed
// kernel call changed no pages. NoAccess views still have their original length
// but cannot be accessed until protection is changed again.
//
// [mprotect(2)]: https://man7.org/linux/man-pages/man2/mprotect.2.html
func (r *Region) Protect(protection Protection) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.stateError(); err != nil {
		return &os.PathError{Op: "mprotect", Path: "mmap", Err: err}
	}
	if protection & ^(Read|Write) != 0 {
		return &os.PathError{Op: "mprotect", Path: "mmap", Err: os.ErrInvalid}
	}
	if err := unix.Mprotect(r.data, int(protection)); err != nil {
		return &os.PathError{Op: "mprotect", Path: "mmap", Err: err}
	}

	return nil
}

// Advise applies one supported advice to all pages intersecting the region
// through [madvise(2)]. Unknown values match os.ErrInvalid without a syscall;
// owner-state errors take precedence. Kernel errors preserve errno and may
// follow partial application. Advice is synchronous as a syscall and may block;
// no context, deadline, or residency guarantee is offered.
//
// [madvise(2)]: https://man7.org/linux/man-pages/man2/madvise.2.html
func (r *Region) Advise(advice Advice) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.stateError(); err != nil {
		return &os.PathError{Op: "madvise", Path: "mmap", Err: err}
	}
	switch advice {
	case Normal, Random, Sequential, WillNeed, DontNeed:
	default:
		return &os.PathError{Op: "madvise", Path: "mmap", Err: os.ErrInvalid}
	}
	if err := unix.Madvise(r.data, int(advice)); err != nil {
		return &os.PathError{Op: "madvise", Path: "mmap", Err: err}
	}

	return nil
}

// Close unmaps the complete original region using [munmap(2)], invalidating all
// escaped views. Callers must stop all view access before calling it. On error
// the owner retains its mapping so the error can be addressed and Close retried.
// Close does not close the source file or promise durable writeback.
//
// [munmap(2)]: https://man7.org/linux/man-pages/man2/mmap.2.html
func (r *Region) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.stateError(); err != nil {
		return &os.PathError{Op: "munmap", Path: "mmap", Err: err}
	}
	if err := unix.Munmap(r.data); err != nil {
		return &os.PathError{Op: "munmap", Path: "mmap", Err: err}
	}

	r.data = nil
	r.closed = true
	return nil
}

// stateError requires r.mu to be held; nil data alone cannot distinguish a
// never-initialized owner from a successfully closed one.
func (r *Region) stateError() error {
	if r.closed {
		return os.ErrClosed
	}
	if r.data == nil {
		return os.ErrInvalid
	}

	return nil
}

func mappingFlags(offset int64, length int, options Options) (int, error) {
	if length <= 0 || offset < 0 || offset%int64(os.Getpagesize()) != 0 || options.Protection & ^(Read|Write) != 0 {
		return 0, os.ErrInvalid
	}
	if offset > math.MaxInt64-int64(length) {
		return 0, unix.EOVERFLOW
	}

	switch options.Mode {
	case Private:
		return unix.MAP_PRIVATE, nil
	case Shared:
		return unix.MAP_SHARED, nil
	default:
		return 0, os.ErrInvalid
	}
}
