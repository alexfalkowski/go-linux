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

	"github.com/alexfalkowski/go-linux/internal/testenv"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

const loopFilesystemPrerequisite = "loop filesystem prerequisite unavailable: "

type loopFilesystem struct {
	name          string
	formatOptions []string
	tools         []string
	magic         int64
}

// XFSFilesystem runs the current test on disposable reflink-enabled XFS in a
// bounded private mount namespace. Only the child receives a writable path.
func XFSFilesystem(t *testing.T) string {
	t.Helper()
	fixture := loopFilesystem{name: "xfs", magic: unix.XFS_SUPER_MAGIC, formatOptions: []string{"-m", "reflink=1"}}
	return fixture.setup(t)
}

// BtrfsFilesystem runs the current test on disposable Btrfs in a bounded
// private mount namespace. Only the child receives a writable path.
func BtrfsFilesystem(t *testing.T) string {
	t.Helper()
	fixture := loopFilesystem{name: "btrfs", magic: unix.BTRFS_SUPER_MAGIC, tools: []string{"btrfs"}}
	return fixture.setup(t)
}

func (fixture loopFilesystem) setup(t *testing.T) string {
	t.Helper()

	prefix := fixture.name + "-isolated="
	if parent, child := strings.CutPrefix(os.Args[len(os.Args)-1], prefix); child {
		fixture.enterMountNamespace(t, parent)
		return fixture.mountImage(t)
	}

	options, err := testenv.FromEnvironment()
	require.NoError(t, err)
	if options.Profile != "namespaces" {
		t.Skipf("%s verification requires profile=namespaces with loop devices and initial-user-namespace CAP_SYS_ADMIN", fixture.name)
	}
	for _, tool := range append([]string{"mkfs." + fixture.name, "mount"}, fixture.tools...) {
		if _, err := exec.LookPath(tool); err != nil {
			require.ErrorIs(t, err, exec.ErrNotFound)
			require.False(t, options.Required, "%s%s: %v", loopFilesystemPrerequisite, tool, err)
			t.Skipf("%s%s: %v", loopFilesystemPrerequisite, tool, err)
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
	require.NoError(t, ctx.Err(), "%s scenario exceeded its time bound", fixture.name)
	unavailable := errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.ENOSPC)
	if unavailable {
		require.False(t, options.Required, "%s isolation requires permitted clone(mount) with initial-user-namespace CAP_SYS_ADMIN: %v", fixture.name, err)
		t.Skipf("%s mount namespace unavailable: %v", fixture.name, err)
	}
	require.NoError(t, err, "scenario requires a private %s loop mount: %s", fixture.name, output)
	if strings.Contains(string(output), loopFilesystemPrerequisite) {
		require.False(t, options.Required, "%s", output)
		t.Skip(string(output))
	}
	require.Contains(t, string(output), "verified isolated "+fixture.name+" operations")
	t.Log(string(output))
	return ""
}

func (fixture loopFilesystem) enterMountNamespace(t *testing.T, parent string) {
	t.Helper()

	current, err := os.Readlink("/proc/self/ns/mnt")
	require.NoError(t, err)
	require.NotEqual(t, parent, current, "the fixture requires a private mount namespace")
	err = unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, "")
	if err != nil {
		unavailable := errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES) || errors.Is(err, unix.ENOSYS)
		if unavailable {
			t.Skipf("%sprivate mount propagation: %v", loopFilesystemPrerequisite, err)
		}
		require.NoError(t, err)
	}
	t.Cleanup(func() {
		if !t.Failed() && !t.Skipped() {
			t.Log("verified isolated " + fixture.name + " operations")
		}
	})
}

func (fixture loopFilesystem) mountImage(t *testing.T) string {
	t.Helper()

	root, err := os.Getwd()
	require.NoError(t, err)
	image, err := os.CreateTemp(root, fixture.name+"-*.img")
	require.NoError(t, err)
	Cleanup(t, image)
	require.NoError(t, image.Truncate(512<<20))
	require.NoError(t, image.Close())
	target := filepath.Join(root, fixture.name)
	require.NoError(t, os.Mkdir(target, 0o700))

	mkfs := "mkfs." + fixture.name
	output, err := runFilesystemTool(t, mkfs, append(fixture.formatOptions, image.Name())...)
	require.NoError(t, err, "%s: %s", mkfs, output)
	// util-linux binds the loop device to this image with autoclear, so
	// namespace teardown also releases it if the child is killed.
	output, err = runFilesystemTool(t, "mount", "-i", "-n", "-t", fixture.name, "-o", "loop,nodev,nosuid,noexec", "--", image.Name(), target)
	if err != nil {
		if _, exited := errors.AsType[*exec.ExitError](err); exited {
			fixture.checkMountPrerequisites(t, output)
		}
		require.NoError(t, err, "mount: %s", output)
	}
	t.Cleanup(func() {
		require.NoError(t, unix.Unmount(target, 0))
	})

	var stat unix.Statfs_t
	require.NoError(t, unix.Statfs(target, &stat))
	require.Equal(t, fixture.magic, stat.Type)
	return target
}

func (fixture loopFilesystem) checkMountPrerequisites(t *testing.T, output []byte) {
	t.Helper()

	fd, err := unix.Fsopen(fixture.name, unix.FSOPEN_CLOEXEC)
	if err != nil {
		if errors.Is(err, unix.ENODEV) {
			t.Skipf("%s%s kernel support: %s", loopFilesystemPrerequisite, fixture.name, output)
		}
	} else {
		require.NoError(t, unix.Close(fd))
	}

	// Inspect device nodes without allocating a loop device or opening a
	// device that might belong to another process.
	if loopNodesAbsent() {
		t.Skipf("%sno visible loop block devices: %s", loopFilesystemPrerequisite, output)
	}
}

func loopNodesAbsent() bool {
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

func runFilesystemTool(t *testing.T, name string, arguments ...string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, name, arguments...).CombinedOutput()
	require.NoError(t, ctx.Err(), "%s exceeded its time bound", name)
	return output, err
}

// CreateBtrfsSubvolume creates a new subvolume at path inside the fixture.
func CreateBtrfsSubvolume(t *testing.T, path string) {
	t.Helper()
	output, err := runFilesystemTool(t, "btrfs", "subvolume", "create", path)
	require.NoError(t, err, "btrfs subvolume create: %s", output)
}

// BtrfsSubvolumeID queries the containing root through btrfs-progs independently of statx.
func BtrfsSubvolumeID(t *testing.T, path string) uint64 {
	t.Helper()
	output, err := runFilesystemTool(t, "btrfs", "inspect-internal", "rootid", path)
	require.NoError(t, err, "btrfs inspect-internal rootid: %s", output)
	id, err := strconv.ParseUint(strings.TrimSpace(string(output)), 10, 64)
	require.NoError(t, err, "Btrfs root ID")
	return id
}
