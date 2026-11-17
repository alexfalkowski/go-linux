//go:build linux

package procfs_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/mmap"
	"github.com/alexfalkowski/go-linux/procfs"
	"github.com/stretchr/testify/require"
)

func TestLiveProcessStatisticsDescribeTheCurrentProcess(t *testing.T) {
	fs, err := procfs.Open("/proc")
	require.NoError(t, err)
	process, err := fs.Self()
	require.NoError(t, err)

	stat, err := process.Stat()
	require.NoError(t, err)

	require.Equal(t, os.Getpid(), process.PID())
	require.Equal(t, os.Getppid(), stat.ParentPID)
	require.NotEmpty(t, stat.Name)
	require.Positive(t, stat.Threads)
	require.Positive(t, stat.VirtualMemory)
}

func TestLiveProcessListingContainsTheCurrentProcess(t *testing.T) {
	fs, err := procfs.Open("/proc")
	require.NoError(t, err)

	pids, err := fs.Processes()
	require.NoError(t, err)

	require.Contains(t, pids, os.Getpid())
	require.IsIncreasing(t, pids)
}

func TestLiveDescriptorInformationMatchesATestOwnedFile(t *testing.T) {
	file := test.TempFile(t, 4096)
	_, err := file.Seek(17, io.SeekStart)
	require.NoError(t, err)
	fs, err := procfs.Open("/proc")
	require.NoError(t, err)
	process, err := fs.Self()
	require.NoError(t, err)
	number := test.FileDescriptor(t, file)

	info, err := process.Descriptor(number)
	require.NoError(t, err)

	require.Equal(t, number, info.Number)
	require.Equal(t, file.Name(), info.Target)
	require.Equal(t, int64(17), info.Position)
	require.EqualValues(t, test.FileFlags(t, file), info.Flags&^procfs.CloseOnExec)
	require.Equal(t, procfs.CloseOnExec, info.Flags&procfs.CloseOnExec)
	require.NotZero(t, info.Inode)
}

func TestLiveDescriptorListingIncludesATestOwnedFile(t *testing.T) {
	file := test.TempFile(t, 0)
	fs, err := procfs.Open("/proc")
	require.NoError(t, err)
	process, err := fs.Self()
	require.NoError(t, err)

	numbers, err := process.Descriptors()
	require.NoError(t, err)

	require.Contains(t, numbers, test.FileDescriptor(t, file))
	require.IsIncreasing(t, numbers)
}

func TestLiveMapsPreserveATestOwnedMappingName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mapped  file\tname \r")
	length := os.Getpagesize()
	require.NoError(t, os.WriteFile(path, make([]byte, length), 0o600))
	file := test.OpenFile(t, path)
	test.Mapping(t, file, 0, length, mmap.Options{Protection: mmap.Read})
	fs, err := procfs.Open("/proc")
	require.NoError(t, err)
	process, err := fs.Self()
	require.NoError(t, err)

	mappings, err := process.Maps()
	require.NoError(t, err)

	var matching []procfs.Mapping
	for _, mapping := range mappings {
		if mapping.Name == path {
			matching = append(matching, mapping)
		}
	}
	require.Len(t, matching, 1, "the test-owned file must retain its complete kernel pathname")
	require.EqualValues(t, length, matching[0].End-matching[0].Start)
	require.Equal(t, procfs.MapRead|procfs.MapPrivate, matching[0].Permissions&(procfs.MapRead|procfs.MapPrivate))
	require.Zero(t, matching[0].Permissions&procfs.MapShared, "the test mapping is private")
}

func TestLiveMountsDescribeTheCurrentMountNamespace(t *testing.T) {
	fs, err := procfs.Open("/proc")
	require.NoError(t, err)
	process, err := fs.Self()
	require.NoError(t, err)

	mounts, err := process.Mounts()
	require.NoError(t, err)

	require.NotEmpty(t, mounts)
	for _, mount := range mounts {
		require.Positive(t, mount.ID, "mount ID")
		require.True(t, filepath.IsAbs(mount.MountPoint), "mount point must be absolute")
		require.NotEmpty(t, mount.Filesystem)
	}
}

func TestLiveSystemStatisticsIncludeCPUAndBootInformation(t *testing.T) {
	fs, err := procfs.Open("/proc")
	require.NoError(t, err)

	stat, err := fs.Stat()
	require.NoError(t, err)

	require.NotEmpty(t, stat.CPUs)
	require.False(t, stat.BootTime.IsZero(), "the live kernel must supply boot time")
	require.Positive(t, stat.Total.Idle)
}

func TestLiveMemoryStatisticsReportUsableRAM(t *testing.T) {
	fs, err := procfs.Open("/proc")
	require.NoError(t, err)

	memory, err := fs.Memory()
	require.NoError(t, err)

	require.Positive(t, memory.Total)
	require.LessOrEqual(t, memory.Free, memory.Total, "free RAM cannot exceed total RAM")
	require.LessOrEqual(t, memory.Available, memory.Total, "available RAM cannot exceed total RAM")
	require.LessOrEqual(t, memory.SwapFree, memory.SwapTotal, "free swap cannot exceed total swap")
}
