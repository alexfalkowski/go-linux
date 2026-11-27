//go:build linux

package file

import (
	"math"
	"os"

	"golang.org/x/sys/unix"
)

// AllocationMode selects one fallocate operation, optionally modified by KeepSize.
// The zero value is Allocate.
type AllocationMode uint32

// Allocate reserves storage without overwriting existing data. It grows the
// file if the end of the range is beyond EOF, unless KeepSize is set.
const Allocate AllocationMode = unix.FALLOC_FL_ALLOCATE_RANGE

// KeepSize preserves the logical size with Allocate, ZeroRange, or UnshareRange.
// PunchHole requires KeepSize.
const KeepSize AllocationMode = unix.FALLOC_FL_KEEP_SIZE

// PunchHole deallocates whole blocks and zeros any partial blocks in the range.
// Use it with KeepSize. Reads from the hole within the file return zeros.
const PunchHole AllocationMode = unix.FALLOC_FL_PUNCH_HOLE

// ZeroRange zeros the range while allocating storage for it. It can grow the
// file, unless KeepSize is set.
const ZeroRange AllocationMode = unix.FALLOC_FL_ZERO_RANGE

// CollapseRange removes a range strictly before EOF and shifts later bytes
// toward the start of the file. The size decreases by the length of the range.
const CollapseRange AllocationMode = unix.FALLOC_FL_COLLAPSE_RANGE

// InsertRange inserts a hole starting before EOF and shifts later bytes toward
// the end of the file. The size increases by the length of the range.
const InsertRange AllocationMode = unix.FALLOC_FL_INSERT_RANGE

// UnshareRange makes shared blocks private without changing existing contents.
// It reserves storage for the range and can grow the file, unless KeepSize is set.
const UnshareRange AllocationMode = unix.FALLOC_FL_UNSHARE_RANGE

// WriteZeroes zeros the range and prepares its mappings for subsequent
// overwrites. It extends the file if the range ends beyond EOF and must be
// used alone. Filesystem granularity and backing-device support apply.
const WriteZeroes AllocationMode = unix.FALLOC_FL_WRITE_ZEROES

// Fallocate applies mode to length bytes starting at offset on a writable handle.
// The file is borrowed and its current offset is unchanged. Concurrent Close
// either lets an admitted operation finish on the original file or returns
// an error matching os.ErrClosed from Fallocate. Coordinate overlapping I/O
// and range changes with other users of the file.
//
// A nil or zero file, an invalid mode combination, a negative offset, or a
// nonpositive length matches os.ErrInvalid. A range whose end exceeds MaxInt64
// returns EFBIG. Linux checks filesystem support, alignment, and file size
// constraints. Errors are wrapped in os.PathError and preserve errno.
// An operation may block in the kernel. Fallocate performs one kernel call
// for valid input, without retries, emulation, or rollback.
func Fallocate(f *os.File, mode AllocationMode, offset, length int64) error {
	if f == nil || *f == (os.File{}) {
		return &os.PathError{Op: "fallocate", Err: os.ErrInvalid}
	}

	if err := validateAllocation(mode, offset, length); err != nil {
		return &os.PathError{Op: "fallocate", Path: f.Name(), Err: err}
	}

	if err := control(f, func(fd int) error {
		return unix.Fallocate(fd, uint32(mode), offset, length)
	}); err != nil {
		return &os.PathError{Op: "fallocate", Path: f.Name(), Err: err}
	}
	return nil
}

func validateAllocation(mode AllocationMode, offset, length int64) error {
	switch mode {
	case Allocate, KeepSize, PunchHole | KeepSize, ZeroRange, ZeroRange | KeepSize,
		CollapseRange, InsertRange, UnshareRange, UnshareRange | KeepSize, WriteZeroes:
	default:
		return os.ErrInvalid
	}
	if offset < 0 || length <= 0 {
		return os.ErrInvalid
	}
	if offset > math.MaxInt64-length {
		return unix.EFBIG
	}
	return nil
}
