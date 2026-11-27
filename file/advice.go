//go:build linux

package file

import (
	"math"
	"os"
	"runtime"
	"strconv"

	"golang.org/x/sys/unix"
)

// Advice selects a single Linux file access hint. The zero value is Normal.
// The values follow Linux's [fadvise.h] on the supported architectures.
//
// [fadvise.h]: https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/fadvise.h
type Advice int

// Normal restores the default readahead and page-reuse policy for the open file.
const Normal Advice = 0

// Random signals random access and disables automatic file readahead.
const Random Advice = 1

// Sequential signals sequential access and increases the readahead window.
const Sequential Advice = 2

// WillNeed requests advance reads into the page cache for the range.
const WillNeed Advice = 3

// DontNeed asks Linux to reclaim cached pages for the range.
const DontNeed Advice = 4

// NoReuse tells Linux that accesses on the open file need not protect pages
// from reclamation.
const NoReuse Advice = 5

// Fadvise passes advice to Linux for length bytes starting at offset. A zero
// length means through EOF. Normal, Random, Sequential, and NoReuse affect
// policy for the open file description, including any duplicates. WillNeed
// and DontNeed act on the filesystem's page cache for the range. The kernel
// and filesystem may limit or ignore these hints.
//
// Unknown advice, a nil or zero file, or a negative offset or length matches
// os.ErrInvalid. A range end beyond MaxInt64 returns EFBIG. The file is
// borrowed, its offset is preserved, and errors use *os.PathError.
func Fadvise(f *os.File, advice Advice, offset, length int64) error {
	if f == nil || *f == (os.File{}) {
		return &os.PathError{Op: "fadvise", Err: os.ErrInvalid}
	}
	switch advice {
	case Normal, Random, Sequential, WillNeed, DontNeed, NoReuse:
	default:
		return &os.PathError{Op: "fadvise", Path: f.Name(), Err: os.ErrInvalid}
	}
	if err := validateAdviceRange(offset, length); err != nil {
		return &os.PathError{Op: "fadvise", Path: f.Name(), Err: err}
	}

	nativeAdvice := int(advice)
	// Linux assigns values 6 and 7 to DontNeed and NoReuse on s390x.
	if runtime.GOARCH == "s390x" && (advice == DontNeed || advice == NoReuse) {
		nativeAdvice += 2
	}
	if err := control(f, func(fd int) error {
		return unix.Fadvise(fd, offset, length, nativeAdvice)
	}); err != nil {
		return &os.PathError{Op: "fadvise", Path: f.Name(), Err: err}
	}
	return nil
}

// Readahead requests advance reads for length bytes starting at offset on a
// readable file. A zero length asks Linux to consider the range through EOF.
// Linux can round the request to page boundaries, limit the work, and block
// on filesystem metadata. Cached pages remain subject to reclamation.
//
// A nil or zero file, or a negative offset or length, matches os.ErrInvalid.
// A range end beyond MaxInt64 returns EFBIG. The file is borrowed, its offset
// is preserved, and errors use *os.PathError.
func Readahead(f *os.File, offset, length int64) error {
	if f == nil || *f == (os.File{}) {
		return &os.PathError{Op: "readahead", Err: os.ErrInvalid}
	}
	if err := validateAdviceRange(offset, length); err != nil {
		return &os.PathError{Op: "readahead", Path: f.Name(), Err: err}
	}

	if err := control(f, func(fd int) error {
		// On 32-bit Linux the 64-bit offset needs a different syscall layout.
		if strconv.IntSize != 64 {
			return unix.ENOSYS
		}
		//nolint:gosec // The descriptor is live and the range is nonnegative and bounded.
		_, _, errno := unix.Syscall(unix.SYS_READAHEAD, uintptr(fd), uintptr(offset), uintptr(length))
		if errno != 0 {
			return errno
		}
		return nil
	}); err != nil {
		return &os.PathError{Op: "readahead", Path: f.Name(), Err: err}
	}
	return nil
}

func validateAdviceRange(offset, length int64) error {
	if offset < 0 || length < 0 {
		return os.ErrInvalid
	}
	if offset > math.MaxInt64-length {
		return unix.EFBIG
	}
	return nil
}
