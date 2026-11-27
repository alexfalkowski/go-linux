//go:build linux

package file_test

import (
	"io"
	"os"
	"testing"

	"github.com/alexfalkowski/go-linux/file"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

func TestFdatasyncPreservesTheBorrowedFileOffset(t *testing.T) {
	f := test.TempFile(t, 0)
	_, err := f.WriteString("log record")
	require.NoError(t, err)

	require.NoError(t, file.Fdatasync(f))

	offset, err := f.Seek(0, io.SeekCurrent)
	require.NoError(t, err)
	require.EqualValues(t, len("log record"), offset)
}

func TestFdatasyncPreservesThePipeError(t *testing.T) {
	_, writer := test.Pipe(t)

	err := file.Fdatasync(writer)
	require.ErrorIs(t, err, test.ErrInvalidArgument)
	var pathError *os.PathError
	require.ErrorAs(t, err, &pathError)
	require.Equal(t, "fdatasync", pathError.Op)
	require.Equal(t, writer.Name(), pathError.Path)

	_, err = writer.WriteString("x")
	require.NoError(t, err)
}

func TestFdatasyncRejectsUnavailableFiles(t *testing.T) {
	require.ErrorIs(t, file.Fdatasync(nil), os.ErrInvalid)
	require.ErrorIs(t, file.Fdatasync(new(os.File)), os.ErrInvalid)

	f := test.TempFile(t, 0)
	require.NoError(t, f.Close())

	require.ErrorIs(t, file.Fdatasync(f), os.ErrClosed)
}

func TestFdatasyncCanRaceWithClose(t *testing.T) {
	for range 16 {
		f := test.TempFile(t, 0)
		result := make(chan error, 1)
		go func() {
			result <- file.Fdatasync(f)
		}()

		require.NoError(t, f.Close())
		if err := test.Receive(t, result); err != nil {
			require.ErrorIs(t, err, os.ErrClosed)
		}
	}
}

func TestSyncFSRejectsUnavailableFiles(t *testing.T) {
	require.ErrorIs(t, file.SyncFS(nil), os.ErrInvalid)
	require.ErrorIs(t, file.SyncFS(new(os.File)), os.ErrInvalid)

	f := test.TempFile(t, 0)
	require.NoError(t, f.Close())

	require.ErrorIs(t, file.SyncFS(f), os.ErrClosed)
}
