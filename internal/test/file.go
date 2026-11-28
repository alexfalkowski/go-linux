//go:build linux

package test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unsafe"

	"github.com/alexfalkowski/go-linux/internal/testenv"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// ReservedAllocationMode is the reserved fallocate(2) codepoint.
const ReservedAllocationMode = unix.FALLOC_FL_NO_HIDE_STALE

// ExtendedStatxMask requests all statx direct-I/O, subvolume, and atomic-write fields.
const ExtendedStatxMask = unix.STATX_DIOALIGN | unix.STATX_DIO_READ_ALIGN | unix.STATX_SUBVOL | unix.STATX_WRITE_ATOMIC

// DataSyncFlag is the native data-integrity synchronization status flag.
const DataSyncFlag = unix.O_DSYNC

// SyncFlag is the native file-integrity synchronization status flag.
const SyncFlag = unix.O_SYNC

// FileCreationMask observes the process umask without modifying it.
func FileCreationMask(t *testing.T) os.FileMode {
	t.Helper()
	status, err := os.ReadFile("/proc/self/status")
	require.NoError(t, err)
	_, field, found := strings.Cut(string(status), "\nUmask:\t")
	require.True(t, found, "procfs reports the process umask")
	value, _, _ := strings.Cut(field, "\n")
	mask, err := strconv.ParseUint(value, 8, 32)
	require.NoError(t, err)
	return os.FileMode(mask)
}

// AlignedBuffer allocates size bytes with the given address alignment.
func AlignedBuffer(t *testing.T, size, alignment uint32) []byte {
	t.Helper()
	require.Positive(t, alignment, "buffer alignment in bytes")
	buffer := make([]byte, int(size)+int(alignment)-1)
	remainder := uintptr(unsafe.Pointer(&buffer[0])) % uintptr(alignment)
	start := int((uintptr(alignment) - remainder) % uintptr(alignment))
	return buffer[start : start+int(size)]
}

// PathDeviceNumbers independently observes the represented device of an inode.
func PathDeviceNumbers(t *testing.T, path string) (uint32, uint32) {
	t.Helper()
	var stat unix.Stat_t
	require.NoError(t, unix.Lstat(path, &stat))
	return unix.Major(stat.Rdev), unix.Minor(stat.Rdev)
}

// ShmemFile creates an ordinary writable memfd for testing shmem behavior.
func ShmemFile(t *testing.T) *os.File {
	t.Helper()
	fd, err := unix.MemfdCreate(t.Name(), unix.MFD_CLOEXEC)
	require.NoError(t, err)
	return OwnDescriptor(t, fd, t.Name())
}

// FileStatx independently observes Linux metadata for focused ABI comparisons.
// The file remains open and no offset or flags are changed.
func FileStatx(t *testing.T, file *os.File, mask uint32) unix.Statx_t {
	t.Helper()
	var stat unix.Statx_t
	var err error
	FileControl(t, file, func(fd uintptr) {
		err = unix.Statx(int(fd), "", unix.AT_EMPTY_PATH, int(mask), &stat)
	})
	require.NoError(t, err)
	return stat
}

// FileAllocatedBlocks independently counts the file's allocated 512-byte blocks.
func FileAllocatedBlocks(t *testing.T, file *os.File) int64 {
	t.Helper()
	var stat unix.Stat_t
	FileControl(t, file, func(fd uintptr) {
		require.NoError(t, unix.Fstat(int(fd), &stat))
	})
	return stat.Blocks
}

// AllocationRequirements selects the filesystem behavior a range test needs.
type AllocationRequirements uint8

// BasicAllocation accepts ordinary file allocation.
const BasicAllocation AllocationRequirements = 0

// ZeroOrShiftAllocation requires zero-range or range shifting support.
const ZeroOrShiftAllocation AllocationRequirements = 1

// FileAllocationBlockSize provides alignment for range tests. The advanced
// operations require a filesystem such as ext4 or XFS. In optional runs,
// classify tmpfs explicitly since it cannot zero, collapse, or insert ranges.
func FileAllocationBlockSize(t *testing.T, file *os.File, requirements AllocationRequirements) int {
	t.Helper()
	var stat unix.Statfs_t
	FileControl(t, file, func(fd uintptr) {
		require.NoError(t, unix.Fstatfs(int(fd), &stat))
	})
	if requirements == ZeroOrShiftAllocation && stat.Type == unix.TMPFS_MAGIC {
		options, err := testenv.FromEnvironment()
		require.NoError(t, err)
		const reason = "tmpfs lacks zero-range, collapse, and insert; set TMPDIR to a directory on a supporting filesystem"
		require.False(t, options.Required(), reason)
		t.Skip(reason)
	}
	return int(stat.Bsize)
}

// DumpPolicy selects whether a temporary file is excluded from dumps.
type DumpPolicy uint8

// IncludeInDump clears the kernel's dump-exclusion flag.
const IncludeInDump DumpPolicy = 0

// ExcludeFromDump sets the kernel's dump-exclusion flag.
const ExcludeFromDump DumpPolicy = 1

// SetDumpPolicy changes only the temporary file's dump-exclusion flag. The fixture
// filesystem must support FS_IOC_GETFLAGS and FS_IOC_SETFLAGS for its owner.
func SetDumpPolicy(t *testing.T, file *os.File, policy DumpPolicy) {
	t.Helper()
	// x/sys omits FS_NODUMP_FL from Linux's include/uapi/linux/fs.h.
	// https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/fs.h
	const noDump = 0x40
	FileControl(t, file, func(fd uintptr) {
		flags, err := unix.IoctlGetInt(int(fd), unix.FS_IOC_GETFLAGS)
		require.NoError(t, err)
		if policy == ExcludeFromDump {
			flags |= noDump
		} else {
			flags &^= noDump
		}
		require.NoError(t, unix.IoctlSetPointerInt(int(fd), unix.FS_IOC_SETFLAGS, flags))
	})
}

// BlockDevice selects an existing /dev block inode for read-only metadata
// observations. The namespace lane exposes host device inodes in its privileged
// container; rootless lanes leave this fixture unselected. No device is opened.
func BlockDevice(t *testing.T) string {
	t.Helper()
	options, err := testenv.FromEnvironment()
	require.NoError(t, err)
	if options.Profile != "namespaces" {
		t.Skip("live block-device metadata requires profile=namespaces with a visible /dev block inode")
	}
	entries, err := os.ReadDir("/dev")
	require.NoError(t, err)
	for _, entry := range entries {
		info, err := entry.Info()
		require.NoError(t, err)
		if info.Mode().Type() == os.ModeDevice {
			return filepath.Join("/dev", entry.Name())
		}
	}

	require.False(t, options.Required(), "block-device metadata requires a visible /dev block inode; use the privileged namespace container")
	t.Skip("no /dev block inode is visible for metadata-only observation")
	return ""
}
