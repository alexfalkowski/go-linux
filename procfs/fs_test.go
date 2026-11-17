//go:build linux

package procfs_test

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/procfs"
	"github.com/stretchr/testify/require"
)

func TestOpenRejectsEmptyAndNondirectoryRoots(t *testing.T) {
	file := test.TempFile(t, 0)
	for _, root := range []string{"", file.Name()} {
		t.Run(fmt.Sprintf("root %q", root), func(t *testing.T) {
			fs, err := procfs.Open(root)
			test.NoResource(t, fs, err, os.ErrInvalid)
		})
	}
}

func TestOpenPreservesMissingRootErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent")

	fs, err := procfs.Open(path)
	test.NoResource(t, fs, err, test.ErrNotExist)
	require.Equal(t, test.ErrNotExist, test.ErrorNumber(t, err))
	test.PathError(t, err, "stat", path)
}

func TestRelativeRootsKeepTheirOriginalDirectory(t *testing.T) {
	parent := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(parent, "proc"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(parent, "proc/meminfo"), []byte(test.ProcMemory), 0o600))
	t.Chdir(parent)
	fs, err := procfs.Open("proc")
	require.NoError(t, err)
	t.Chdir(t.TempDir())

	memory, err := fs.Memory()
	require.NoError(t, err)
	require.Equal(t, uint64(1000*1024), memory.Total)
}

func TestSelfUsesTheSelectedRoot(t *testing.T) {
	root := test.ProcRoot(t, nil)
	fs, err := procfs.Open(root)
	require.NoError(t, err)

	process, err := fs.Self()
	require.NoError(t, err)
	require.Equal(t, 42, process.PID())
}

func TestSelfRejectsInvalidPIDLinks(t *testing.T) {
	for _, target := range []string{"text", "0", "-1", "2147483648", "../42"} {
		t.Run(target, func(t *testing.T) {
			root := test.ProcRoot(t, nil)
			require.NoError(t, os.Remove(filepath.Join(root, "self")))
			require.NoError(t, os.Symlink(target, filepath.Join(root, "self")))
			fs, err := procfs.Open(root)
			require.NoError(t, err)

			process, err := fs.Self()
			test.NoResource(t, process, err, procfs.ErrParse)
		})
	}
}

func TestProcessesAreSortedAndIgnoreNonPIDEntries(t *testing.T) {
	root := test.ProcRoot(t, nil)
	for _, name := range []string{"7", "0", "-1", "2147483648", "other"} {
		require.NoError(t, os.Mkdir(filepath.Join(root, name), 0o700))
	}
	fs, err := procfs.Open(root)
	require.NoError(t, err)

	processes, err := fs.Processes()
	require.NoError(t, err)
	require.Equal(t, []int{7, 42}, processes)
}

func TestProcessRejectsInvalidPIDs(t *testing.T) {
	fs, err := procfs.Open(test.ProcRoot(t, nil))
	require.NoError(t, err)
	for _, pid := range []int{-1, 0, math.MaxInt32 + 1} {
		t.Run(fmt.Sprintf("pid %d", pid), func(t *testing.T) {
			process, err := fs.Process(pid)
			test.NoResource(t, process, err, os.ErrInvalid)
		})
	}
}

func TestProcessRejectsNondirectoryPIDEntries(t *testing.T) {
	root := test.ProcRoot(t, map[string]string{"7": "not a directory"})
	fs, err := procfs.Open(root)
	require.NoError(t, err)

	process, err := fs.Process(7)
	test.NoResource(t, process, err, os.ErrInvalid)
}

func TestProcessReportsMissingPIDs(t *testing.T) {
	fs, err := procfs.Open(test.ProcRoot(t, nil))
	require.NoError(t, err)

	process, err := fs.Process(99)
	test.NoResource(t, process, err, test.ErrNotExist)
}

func TestUninitializedFilesystemViewsRejectOperations(t *testing.T) {
	for _, fs := range []*procfs.FS{nil, {}} {
		process, err := fs.Process(42)
		test.NoResource(t, process, err, os.ErrInvalid)

		process, err = fs.Self()
		test.NoResource(t, process, err, os.ErrInvalid)

		processes, err := fs.Processes()
		require.ErrorIs(t, err, os.ErrInvalid)
		require.Nil(t, processes)

		stat, err := fs.Stat()
		require.ErrorIs(t, err, os.ErrInvalid)
		require.Zero(t, stat)

		memory, err := fs.Memory()
		require.ErrorIs(t, err, os.ErrInvalid)
		require.Zero(t, memory)
	}
}

func TestUninitializedProcessViewsRejectOperations(t *testing.T) {
	for _, process := range []*procfs.Process{nil, {}} {
		require.Zero(t, process.PID())

		stat, err := process.Stat()
		require.ErrorIs(t, err, os.ErrInvalid)
		require.Zero(t, stat)

		numbers, err := process.Descriptors()
		require.ErrorIs(t, err, os.ErrInvalid)
		require.Nil(t, numbers)

		descriptor, err := process.Descriptor(3)
		require.ErrorIs(t, err, os.ErrInvalid)
		require.Zero(t, descriptor)

		mappings, err := process.Maps()
		require.ErrorIs(t, err, os.ErrInvalid)
		require.Nil(t, mappings)

		mounts, err := process.Mounts()
		require.ErrorIs(t, err, os.ErrInvalid)
		require.Nil(t, mounts)
	}
}

func TestMissingFilesRemainFilesystemErrors(t *testing.T) {
	root := test.ProcRoot(t, nil)
	process := test.ProcProcess(t, root)
	fs, err := procfs.Open(root)
	require.NoError(t, err)

	stat, err := process.Stat()
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Zero(t, stat)

	numbers, err := process.Descriptors()
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Nil(t, numbers)

	descriptor, err := process.Descriptor(3)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Zero(t, descriptor)

	mappings, err := process.Maps()
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Nil(t, mappings)

	mounts, err := process.Mounts()
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Nil(t, mounts)

	system, err := fs.Stat()
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Zero(t, system)

	memory, err := fs.Memory()
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Zero(t, memory)
}
