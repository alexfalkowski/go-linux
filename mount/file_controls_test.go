//go:build linux

package mount_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alexfalkowski/go-linux/file"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

func TestDirectIOOnXFS(t *testing.T) {
	path := test.XFSFilesystem(t)
	if path == "" {
		return
	}
	dir := test.OpenFile(t, path)
	segment, err := file.Open(dir, "segment", file.OpenOptions{
		Flags: file.Direct | file.ReadWrite | file.Create,
		Perm:  0o600,
	})
	require.NoError(t, err)
	test.Cleanup(t, segment)
	alignment := test.FileStatx(t, segment, test.ExtendedStatxMask)
	require.Greater(t, alignment.Dio_mem_align, uint32(1), "XFS buffer alignment")
	require.Greater(t, alignment.Dio_offset_align, uint32(1), "XFS offset and length alignment")
	size := alignment.Dio_offset_align

	t.Run("aligned round trip", func(t *testing.T) {
		record := test.AlignedBuffer(t, size, alignment.Dio_mem_align)
		copy(record, "record")
		readback := test.AlignedBuffer(t, size, alignment.Dio_mem_align)

		written, err := segment.WriteAt(record, 0)
		require.NoError(t, err)
		require.Equal(t, len(record), written)
		read, err := segment.ReadAt(readback, 0)
		require.NoError(t, err)

		require.Equal(t, len(record), read)
		require.Equal(t, record, readback)
	})

	t.Run("misaligned writes", func(t *testing.T) {
		record := test.AlignedBuffer(t, size+1, alignment.Dio_mem_align)
		for _, scenario := range []struct {
			name   string
			record []byte
			offset int64
		}{
			{name: "buffer", record: record[1:]},
			{name: "length", record: record[:size-1]},
			{name: "offset", record: record[:size], offset: 1},
		} {
			t.Run(scenario.name, func(t *testing.T) {
				written, err := segment.WriteAt(scenario.record, scenario.offset)
				require.ErrorIs(t, err, test.ErrInvalidArgument)
				require.Zero(t, written)
			})
		}
	})
}

func TestRenameCreatesXFSWhiteout(t *testing.T) {
	path := test.XFSFilesystem(t)
	if path == "" {
		return
	}
	for _, scenario := range []struct {
		name         string
		mode         file.RenameMode
		targetExists bool
	}{
		{name: "replace", mode: file.Whiteout, targetExists: true},
		{name: "no replace", mode: file.WhiteoutNoReplace},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := filepath.Join(path, scenario.name)
			require.NoError(t, os.Mkdir(root, 0o700))
			dir := test.OpenFile(t, root)
			oldPath, newPath := filepath.Join(root, "source"), filepath.Join(root, "target")
			require.NoError(t, os.WriteFile(oldPath, []byte("moved contents"), 0o600))
			source, err := os.Stat(oldPath)
			require.NoError(t, err)
			if scenario.targetExists {
				require.NoError(t, os.WriteFile(newPath, []byte("old target"), 0o600))
			}

			require.NoError(t, file.Rename(dir, "source", dir, "target", scenario.mode))

			moved, err := os.Stat(newPath)
			require.NoError(t, err)
			require.True(t, os.SameFile(source, moved), "the destination identifies the original inode")
			whiteout, err := os.Lstat(oldPath)
			require.NoError(t, err)
			require.Equal(t, os.ModeDevice|os.ModeCharDevice, whiteout.Mode().Type())
			major, minor := test.PathDeviceNumbers(t, oldPath)
			require.Zero(t, major, "whiteout device major")
			require.Zero(t, minor, "whiteout device minor")
		})
	}
}

func TestRenameXFSWhiteoutNoReplacePreservesExistingNames(t *testing.T) {
	path := test.XFSFilesystem(t)
	if path == "" {
		return
	}
	dir := test.OpenFile(t, path)
	oldPath, newPath := filepath.Join(path, "source"), filepath.Join(path, "target")
	require.NoError(t, os.WriteFile(oldPath, []byte("source contents"), 0o600))
	require.NoError(t, os.WriteFile(newPath, []byte("target contents"), 0o600))

	err := file.Rename(dir, "source", dir, "target", file.WhiteoutNoReplace)
	require.ErrorIs(t, err, test.ErrExists)

	source, err := os.ReadFile(oldPath)
	require.NoError(t, err)
	require.Equal(t, "source contents", string(source))
	target, err := os.ReadFile(newPath)
	require.NoError(t, err)
	require.Equal(t, "target contents", string(target))
}
