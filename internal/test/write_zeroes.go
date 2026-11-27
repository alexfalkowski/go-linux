//go:build linux

package test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/internal/testenv"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// WriteZeroesFilesystem formats the runner's disposable SCSI disk and mounts
// it in a bounded private mount-namespace child. The parent returns "".
func WriteZeroesFilesystem(t *testing.T) string {
	t.Helper()

	options, err := testenv.FromEnvironment()
	require.NoError(t, err)
	if options.Profile != "namespaces" || os.Getenv("GO_LINUX_TEST_WRITE_ZEROES") != "true" {
		t.Skip("write-zeroes verification requires the selected disposable SCSI disk")
	}

	const prefix = "write-zeroes-isolated="
	if parent, child := strings.CutPrefix(os.Args[len(os.Args)-1], prefix); child {
		current, err := os.Readlink("/proc/self/ns/mnt")
		require.NoError(t, err)
		require.NotEqual(t, parent, current, "the ext4 fixture requires a private mount namespace")
		require.NoError(t, unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""))
		return mountWriteZeroesDisk(t)
	}

	parent, err := os.Readlink("/proc/self/ns/mnt")
	require.NoError(t, err)
	ctx, command := Command(t, t.Name(), "-test.v", "--", prefix+parent)
	command.Dir = t.TempDir()
	command.SysProcAttr = &syscall.SysProcAttr{Cloneflags: syscall.CLONE_NEWNS}
	output, err := command.CombinedOutput()
	require.NoError(t, ctx.Err(), "write-zeroes scenario exceeded its time bound")
	require.NoError(t, err, "write-zeroes requires a private ext4 mount: %s", output)
	require.Contains(t, string(output), "verified isolated write-zeroes operations")
	t.Log(string(output))
	return ""
}

func writeZeroesQueueLimit(t *testing.T) string {
	t.Helper()

	// Only consider disks below the RAM-backed driver. Wait for the queue
	// attribute because SCSI probing can finish after modprobe returns.
	const pattern = "/sys/bus/pseudo/drivers/scsi_debug/adapter*/host*/target*/*/block/sd*/queue/write_zeroes_unmap_max_bytes"
	for range 50 {
		limits, err := filepath.Glob(pattern)
		require.NoError(t, err)
		if len(limits) > 0 {
			require.Len(t, limits, 1, "expected one scsi_debug disk")
			return limits[0]
		}
		time.Sleep(100 * time.Millisecond)
	}
	require.FailNow(t, "the disposable scsi_debug disk's queue did not appear")
	return ""
}

func mountWriteZeroesDisk(t *testing.T) string {
	t.Helper()

	limitPath := writeZeroesQueueLimit(t)
	queue := filepath.Dir(limitPath)
	disk := filepath.Dir(queue)
	model, err := os.ReadFile(filepath.Join(disk, "device", "model"))
	require.NoError(t, err)
	require.Equal(t, "go-linux-ext4", strings.TrimSpace(string(model)), "only the disposable SCSI disk can be formatted")
	limit, err := os.ReadFile(limitPath)
	require.NoError(t, err)
	maxBytes, err := strconv.ParseUint(strings.TrimSpace(string(limit)), 10, 64)
	require.NoError(t, err)
	require.Positive(t, maxBytes, "the backing disk must advertise write zeroes with unmap")
	identity, err := os.ReadFile(filepath.Join(disk, "dev"))
	require.NoError(t, err)
	majorText, minorText, ok := strings.Cut(strings.TrimSpace(string(identity)), ":")
	require.True(t, ok, "expected a block major:minor")
	major, err := strconv.ParseUint(majorText, 10, 32)
	require.NoError(t, err)
	minor, err := strconv.ParseUint(minorText, 10, 32)
	require.NoError(t, err)

	root, err := os.Getwd()
	require.NoError(t, err)
	target := filepath.Join(root, "ext4")
	require.NoError(t, os.Mkdir(target, 0o700))

	// The temporary directory can be on a nodev mount. Give the device
	// node its own mount inside the private namespace.
	deviceDir := filepath.Join(root, "device")
	require.NoError(t, os.Mkdir(deviceDir, 0o700))
	require.NoError(t, unix.Mount("tmpfs", deviceDir, "tmpfs", unix.MS_NOSUID|unix.MS_NOEXEC, "mode=0700"))
	t.Cleanup(func() {
		require.NoError(t, unix.Unmount(deviceDir, 0))
		if !t.Failed() && !t.Skipped() {
			t.Logf("verified isolated write-zeroes operations on ext4 (write_zeroes_unmap_max_bytes=%d)", maxBytes)
		}
	})

	node := filepath.Join(deviceDir, "write-zeroes-disk")
	require.NoError(t, unix.Mknod(node, unix.S_IFBLK|0o600, int(unix.Mkdev(uint32(major), uint32(minor))))) //nolint:gosec // Mkdev encodes the verified Linux device identity.
	output, err := runFilesystemTool(t, "mkfs.ext4", "-q", "-b", "4096", "-E", "nodiscard", node)
	require.NoError(t, err, "mkfs.ext4: %s", output)
	require.NoError(t, unix.Mount(node, target, "ext4", unix.MS_NODEV|unix.MS_NOSUID|unix.MS_NOEXEC, ""))
	t.Cleanup(func() {
		require.NoError(t, unix.Unmount(target, 0))
	})
	return target
}
