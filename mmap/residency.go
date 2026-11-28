//go:build linux

package mmap

import (
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// LockMode selects when the region's pages are locked. The zero mode is
// LockImmediate.
type LockMode uint8

// LockImmediate brings accessible pages into memory and locks them (mlock).
const LockImmediate LockMode = 0

// LockOnFault locks pages already present in the mapping and locks additional
// pages as they fault in (mlock2 with MLOCK_ONFAULT). A page present only in the
// file cache is locked when it faults into the mapping.
const LockOnFault LockMode = unix.MLOCK_ONFAULT

// Residency is a caller-owned snapshot of the page status reported by Linux.
type Residency struct {
	// Resident contains one entry per intersecting page in address order,
	// starting at offset zero. The last entry may cover a partial page.
	Resident []bool

	// PageSize is the system page size in bytes.
	PageSize int
}

// Lock applies [mlock(2)] or mlock2 to all pages intersecting the region,
// including a partial final page. Both modes charge the entire range against
// RLIMIT_MEMLOCK unless the caller can bypass that limit. Locks are released
// by Unlock or successful Close. Repeated locks on the same pages do not stack.
//
// Lock can block. Owner-state errors take precedence over mode validation.
// Unknown modes match os.ErrInvalid. Kernel errors preserve errno, including
// unavailability of mlock2, and may follow a change to the mapping's lock state.
//
// [mlock(2)]: https://man7.org/linux/man-pages/man2/mlock.2.html
func (r *Region) Lock(mode LockMode) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.stateError(); err != nil {
		return &os.PathError{Op: "mlock", Path: "mmap", Err: err}
	}

	switch mode {
	case LockImmediate:
		if err := unix.Mlock(r.data); err != nil {
			return &os.PathError{Op: "mlock", Path: "mmap", Err: err}
		}
	case LockOnFault:
		_, _, errno := unix.Syscall(unix.SYS_MLOCK2, uintptr(unsafe.Pointer(&r.data[0])), uintptr(len(r.data)), unix.MLOCK_ONFAULT)
		if errno != 0 {
			return &os.PathError{Op: "mlock2", Path: "mmap", Err: errno}
		}
	default:
		return &os.PathError{Op: "mlock", Path: "mmap", Err: os.ErrInvalid}
	}

	return nil
}

// Unlock releases locks on all pages intersecting the region through
// munlock. The region remains mapped and pages may remain resident. Kernel
// errors preserve errno and may follow partial application.
func (r *Region) Unlock() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.stateError(); err != nil {
		return &os.PathError{Op: "munlock", Path: "mmap", Err: err}
	}
	if err := unix.Munlock(r.data); err != nil {
		return &os.PathError{Op: "munlock", Path: "mmap", Err: err}
	}

	return nil
}

// Residency reports page status through [mincore(2)] without touching the
// mapped bytes. Results can become stale during the call, and Linux may report
// all pages resident for file mappings whose page-cache state it withholds.
// A resident page may still incur a page fault on access. Each call returns
// independent storage, which remains valid after Close. Errors return a zero
// Residency and preserve the underlying error identity.
//
// [mincore(2)]: https://man7.org/linux/man-pages/man2/mincore.2.html
func (r *Region) Residency() (Residency, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.stateError(); err != nil {
		return Residency{}, &os.PathError{Op: "mincore", Path: "mmap", Err: err}
	}

	pageSize := os.Getpagesize()
	pageCount := 1 + (len(r.data)-1)/pageSize
	status := make([]byte, pageCount)
	_, _, errno := unix.Syscall(unix.SYS_MINCORE, uintptr(unsafe.Pointer(&r.data[0])), uintptr(len(r.data)), uintptr(unsafe.Pointer(&status[0])))
	if errno != 0 {
		return Residency{}, &os.PathError{Op: "mincore", Path: "mmap", Err: errno}
	}

	resident := make([]bool, pageCount)
	for page, flags := range status {
		resident[page] = flags&1 != 0
	}
	return Residency{PageSize: pageSize, Resident: resident}, nil
}
