//go:build linux

package test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/alexfalkowski/go-linux/internal/testenv"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

const reflinkPrerequisite = "reflink prerequisite unavailable: "

// ReflinkFilesystem runs the current test on disposable XFS in a bounded,
// private mount-namespace child. Only the child receives a writable path.
func ReflinkFilesystem(t *testing.T) string {
	t.Helper()

	const prefix = "reflink-isolated="
	if parent, child := strings.CutPrefix(os.Args[len(os.Args)-1], prefix); child {
		enterReflinkMountNamespace(t, parent)
		return mountReflinkImage(t)
	}

	options, err := testenv.FromEnvironment()
	require.NoError(t, err)
	if options.Profile != "namespaces" {
		t.Skip("reflink verification requires profile=namespaces with XFS, loop devices, and initial-user-namespace CAP_SYS_ADMIN")
	}
	for _, tool := range []string{"mkfs.xfs", "mount"} {
		if _, err := exec.LookPath(tool); err != nil {
			require.ErrorIs(t, err, exec.ErrNotFound)
			require.False(t, options.Required, "%s%s: %v", reflinkPrerequisite, tool, err)
			t.Skipf("%s%s: %v", reflinkPrerequisite, tool, err)
		}
	}

	before, err := os.Readlink("/proc/self/ns/mnt")
	require.NoError(t, err)
	t.Cleanup(func() {
		after, err := os.Readlink("/proc/self/ns/mnt")
		require.NoError(t, err)
		require.Equal(t, before, after, "parent mount namespace must remain unchanged")
	})
	ctx, command := Command(t, t.Name(), "-test.v", "--", prefix+before)
	command.Dir = t.TempDir()
	command.SysProcAttr = &syscall.SysProcAttr{Cloneflags: syscall.CLONE_NEWNS}
	output, err := command.CombinedOutput()
	require.NoError(t, ctx.Err(), "reflink scenario exceeded its time bound")
	unavailable := errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.ENOSPC)
	if unavailable {
		require.False(t, options.Required, "XFS isolation requires permitted clone(mount) with initial-user-namespace CAP_SYS_ADMIN: %v", err)
		t.Skipf("XFS mount namespace unavailable: %v", err)
	}
	require.NoError(t, err, "reflink scenario requires a private XFS loop mount: %s", output)
	if strings.Contains(string(output), reflinkPrerequisite) {
		require.False(t, options.Required, "%s", output)
		t.Skip(string(output))
	}
	require.Contains(t, string(output), "verified isolated reflink operations")
	t.Log(string(output))
	return ""
}

func enterReflinkMountNamespace(t *testing.T, parent string) {
	t.Helper()

	current, err := os.Readlink("/proc/self/ns/mnt")
	require.NoError(t, err)
	require.NotEqual(t, parent, current, "the XFS fixture requires a private mount namespace")
	err = unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, "")
	if err != nil {
		unavailable := errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES) || errors.Is(err, unix.ENOSYS)
		if unavailable {
			t.Skipf("%sprivate mount propagation: %v", reflinkPrerequisite, err)
		}
		require.NoError(t, err)
	}
	t.Cleanup(func() {
		if !t.Failed() && !t.Skipped() {
			t.Log("verified isolated reflink operations")
		}
	})
}

func mountReflinkImage(t *testing.T) string {
	t.Helper()

	root, err := os.Getwd()
	require.NoError(t, err)
	image, err := os.CreateTemp(root, "reflink-*.img")
	require.NoError(t, err)
	Cleanup(t, image)
	require.NoError(t, image.Truncate(512<<20))
	require.NoError(t, image.Close())
	target := filepath.Join(root, "xfs")
	require.NoError(t, os.Mkdir(target, 0o700))

	output, err := runReflinkTool(t, "mkfs.xfs", "-m", "reflink=1", image.Name())
	require.NoError(t, err, "mkfs.xfs: %s", output)
	// util-linux binds the loop device to this image with autoclear, so
	// namespace teardown also releases it if the child is killed.
	output, err = runReflinkTool(t, "mount", "-i", "-n", "-t", "xfs", "-o", "loop,nodev,nosuid,noexec", "--", image.Name(), target)
	if err != nil {
		if _, exited := errors.AsType[*exec.ExitError](err); exited {
			checkReflinkMountPrerequisites(t, output)
		}
		require.NoError(t, err, "mount: %s", output)
	}
	t.Cleanup(func() {
		require.NoError(t, unix.Unmount(target, 0))
	})

	var stat unix.Statfs_t
	require.NoError(t, unix.Statfs(target, &stat))
	require.EqualValues(t, unix.XFS_SUPER_MAGIC, stat.Type)
	return target
}

func checkReflinkMountPrerequisites(t *testing.T, output []byte) {
	t.Helper()

	fd, err := unix.Fsopen("xfs", unix.FSOPEN_CLOEXEC)
	if err != nil {
		if errors.Is(err, unix.ENODEV) {
			t.Skipf("%sXFS kernel support: %s", reflinkPrerequisite, output)
		}
	} else {
		require.NoError(t, unix.Close(fd))
	}

	// Inspect device nodes without allocating a loop device or opening a
	// device that might belong to another process.
	if reflinkLoopNodesAbsent() {
		t.Skipf("%sno visible loop block devices: %s", reflinkPrerequisite, output)
	}
}

func reflinkLoopNodesAbsent() bool {
	for _, location := range []struct {
		dir    string
		prefix string
	}{
		{dir: "/dev", prefix: "loop"},
		{dir: "/dev/loop"},
	} {
		entries, err := os.ReadDir(location.dir)
		if err != nil {
			if location.dir == "/dev/loop" && errors.Is(err, os.ErrNotExist) {
				continue
			}
			return false
		}
		for _, entry := range entries {
			suffix, found := strings.CutPrefix(entry.Name(), location.prefix)
			if !found {
				continue
			}
			if _, err := strconv.ParseUint(suffix, 10, 32); err != nil {
				continue
			}
			var device unix.Stat_t
			if err := unix.Stat(filepath.Join(location.dir, entry.Name()), &device); err != nil {
				// Preserve the mount failure if node availability is ambiguous.
				return false
			}
			if device.Mode&unix.S_IFMT == unix.S_IFBLK {
				return false
			}
		}
	}
	return true
}

func runReflinkTool(t *testing.T, name string, arguments ...string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, name, arguments...).CombinedOutput()
	require.NoError(t, ctx.Err(), "%s exceeded its time bound", name)
	return output, err
}

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
