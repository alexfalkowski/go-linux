//go:build linux

package procfs

import (
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"golang.org/x/sys/unix"
)

// OpenFlags is a Linux fdinfo flags word. Unknown output bits are preserved.
// The flags are observations and cannot be used here to change a descriptor.
type OpenFlags uint64

// ReadOnly is the zero access mode, tested by flags&AccessMode == ReadOnly.
const ReadOnly OpenFlags = unix.O_RDONLY

// WriteOnly is the write-only access mode.
const WriteOnly OpenFlags = unix.O_WRONLY

// ReadWrite is the read/write access mode.
const ReadWrite OpenFlags = unix.O_RDWR

// AccessMode selects the access-mode bits, rather than an independent flag.
const AccessMode OpenFlags = unix.O_ACCMODE

// Append reports append mode on the open file description.
const Append OpenFlags = unix.O_APPEND

// Nonblock reports nonblocking mode on the open file description.
const Nonblock OpenFlags = unix.O_NONBLOCK

// CloseOnExec reports the observed descriptor's close-on-exec flag.
const CloseOnExec OpenFlags = unix.O_CLOEXEC

// DescriptorInfo combines selected fdinfo fields with an fd symlink target.
// The reads are separate: concurrent close/reuse can mix observations. Numbers
// and targets grant no access, are not owned resources, and must not be closed.
type DescriptorInfo struct {
	// Target is the exact symlink text, including socket/anon-inode/deleted labels.
	Target string
	// Number identifies the entry in the inspected process's descriptor table.
	Number int
	// Position is the reported offset in bytes; not every facility uses it.
	Position int64
	// Flags retains the complete Linux octal flags value as a typed mask.
	Flags OpenFlags
	// MountID is the reported mount identifier; it can be zero for some facilities.
	MountID uint64
	// Inode is the reported inode number, including synthetic filesystem inodes.
	Inode uint64
}

// Descriptors lists descriptor numbers from [proc_pid_fd(5)] in numeric order.
// The listing may include its own temporary directory descriptor when inspecting
// the current process. Entries can close or be reused before Descriptor reads
// them; callers decide how to handle individual errors. No entries are opened
// or duplicated for the caller.
//
// [proc_pid_fd(5)]: https://man7.org/linux/man-pages/man5/proc_pid_fd.5.html
func (p *Process) Descriptors() ([]int, error) {
	path, err := p.path("fd")
	if err != nil {
		return nil, err
	}

	native, err := p.process.FileDescriptors()
	if err != nil {
		return nil, readError(path, err)
	}

	numbers := make([]int, 0, len(native))
	for _, number := range native {
		if number > math.MaxInt32 {
			return nil, parseError(path)
		}

		numbers = append(numbers, int(number))
	}
	slices.Sort(numbers)
	return numbers, nil
}

// Descriptor reads one [proc_pid_fdinfo(5)] entry and its fd symlink. number
// must be nonnegative and fit Linux's signed 32-bit descriptor type; invalid
// inputs match os.ErrInvalid. Missing or inaccessible entries retain errno.
// All four common numeric fields are required; facility-specific lines are
// omitted. A failed target read returns no partial record.
//
// [proc_pid_fdinfo(5)]: https://man7.org/linux/man-pages/man5/proc_pid_fdinfo.5.html
func (p *Process) Descriptor(number int) (DescriptorInfo, error) {
	path, err := p.path("fdinfo")
	if err != nil {
		return DescriptorInfo{}, err
	}

	if number < 0 || number > math.MaxInt32 {
		return DescriptorInfo{}, os.ErrInvalid
	}

	name := strconv.Itoa(number)
	path = filepath.Join(path, name)
	native, err := p.process.FDInfo(name)
	if err != nil {
		return DescriptorInfo{}, readError(path, err)
	}

	position, err := strconv.ParseInt(native.Pos, 10, 64)
	if err != nil {
		return DescriptorInfo{}, parseError(path)
	}

	flags, err := parseUint(path, native.Flags, 8, 64)
	if err != nil {
		return DescriptorInfo{}, err
	}

	mountID, err := parseUint(path, native.MntID, 10, 64)
	if err != nil {
		return DescriptorInfo{}, err
	}

	inode, err := parseUint(path, native.Ino, 10, 64)
	if err != nil {
		return DescriptorInfo{}, err
	}

	target, err := os.Readlink(filepath.Join(p.fs.root, strconv.Itoa(p.PID()), "fd", name))
	if err != nil {
		return DescriptorInfo{}, err
	}
	return DescriptorInfo{
		Number: number, Target: target, Position: position, Flags: OpenFlags(flags), MountID: mountID, Inode: inode,
	}, nil
}

func parseUint(path, text string, base, bits int) (uint64, error) {
	value, err := strconv.ParseUint(text, base, bits)
	if err != nil {
		return 0, parseError(path)
	}
	return value, nil
}
