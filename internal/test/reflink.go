//go:build linux

package test

import (
	"os"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// CloneFile creates shared extents through FICLONE on a test-owned destination.
func CloneFile(t *testing.T, destination, source *os.File) {
	t.Helper()
	FileControl(t, source, func(src uintptr) {
		FileControl(t, destination, func(dst uintptr) {
			require.NoError(t, unix.IoctlFileClone(int(dst), int(src)), "fixture must support file cloning")
		})
	})
}

// FileExtent describes the physical mapping and sharing of an allocated block.
type FileExtent struct {
	// Physical is the byte offset of the queried location on the device.
	Physical uint64

	// Shared reports that the mapping is referenced by more than one file range.
	Shared bool

	unwritten bool
}

// FileExtentAt observes the written extent containing offset through FIEMAP.
func FileExtentAt(t *testing.T, file *os.File, offset uint64) FileExtent {
	t.Helper()

	extent := fileExtentAt(t, file, offset)
	require.NotNil(t, extent, "expected a mapped extent at %d", offset)
	require.False(t, extent.unwritten, "expected a written extent at %d", offset)
	return *extent
}

// FileBlockAllocatedAt reports whether offset belongs to a physical mapping,
// including an unwritten extent reserved beyond the logical end of the file.
func FileBlockAllocatedAt(t *testing.T, file *os.File, offset uint64) bool {
	t.Helper()

	return fileExtentAt(t, file, offset) != nil
}

// FileBlockUnwrittenAt reports whether an allocated block is unwritten.
func FileBlockUnwrittenAt(t *testing.T, file *os.File, offset uint64) bool {
	t.Helper()

	extent := fileExtentAt(t, file, offset)
	require.NotNil(t, extent, "expected a mapped extent at %d", offset)
	return extent.unwritten
}

func fileExtentAt(t *testing.T, file *os.File, offset uint64) *FileExtent {
	t.Helper()

	// FS_IOC_FIEMAP is _IOWR('f', 11, struct fiemap) on amd64 and arm64.
	// The 32-byte header is followed by room for one 56-byte extent.
	// https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/fiemap.h
	const ioctlFiemap = 0xc020660b
	const syncExtents = 0x1
	const sharedExtent = 0x2000
	const unknownOrDelayed = 0x2 | 0x4
	const unwrittenExtent = 0x800

	request := struct {
		start, length                uint64
		flags, mapped, count, unused uint32
		logical, physical, extentLen uint64
		reserved64                   [2]uint64
		extentFlags                  uint32
		reserved                     [3]uint32
	}{start: offset, length: 1, flags: syncExtents, count: 1}

	FileControl(t, file, func(fd uintptr) {
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, ioctlFiemap, uintptr(unsafe.Pointer(&request)))
		require.Zero(t, errno, "FIEMAP: %v", errno)
	})
	if request.mapped == 0 {
		return nil
	}
	require.EqualValues(t, 1, request.mapped, "expected a mapped extent at %d", offset)
	require.Zero(t, request.extentFlags&unknownOrDelayed, "expected a physical extent with a known location")
	require.LessOrEqual(t, request.logical, offset)
	require.Less(t, offset-request.logical, request.extentLen)
	return &FileExtent{
		Physical:  request.physical + offset - request.logical,
		Shared:    request.extentFlags&sharedExtent != 0,
		unwritten: request.extentFlags&unwrittenExtent != 0,
	}
}
