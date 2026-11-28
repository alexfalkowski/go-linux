//go:build linux

package mount_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexfalkowski/go-linux/file"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

func TestSyncFSOnAnIsolatedFilesystem(t *testing.T) {
	path := test.XFSFilesystem(t)
	if path == "" {
		return
	}

	const first = "segment-1"
	const second = "segment-2"
	require.NoError(t, os.WriteFile(filepath.Join(path, first), []byte("first segment"), 0o600))
	anchor := test.OpenFile(t, filepath.Join(path, first))
	_, err := anchor.Seek(2, io.SeekStart)
	require.NoError(t, err)

	require.NoError(t, file.SyncFS(anchor))

	offset, err := anchor.Seek(0, io.SeekCurrent)
	require.NoError(t, err)
	require.EqualValues(t, 2, offset)

	dir := test.OpenFile(t, path)
	require.NoError(t, os.WriteFile(filepath.Join(path, second), []byte("second segment"), 0o600))

	require.NoError(t, file.SyncFS(dir))

	names, err := dir.Readdirnames(-1)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{first, second}, names)
}

func TestSyncFSRejectsPathHandles(t *testing.T) {
	dir := test.OpenFile(t, t.TempDir())
	anchor, err := file.Open(dir, ".", file.OpenOptions{Flags: file.Path | file.Directory})
	require.NoError(t, err)
	test.Cleanup(t, anchor)

	err = file.SyncFS(anchor)
	require.ErrorIs(t, err, test.ErrBadDescriptor)
	var pathError *os.PathError
	require.ErrorAs(t, err, &pathError)
	require.Equal(t, "syncfs", pathError.Op)
	require.Equal(t, anchor.Name(), pathError.Path)

	_, err = anchor.Stat()
	require.NoError(t, err)
}
