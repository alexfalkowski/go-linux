//go:build linux

package file_test

import (
	"io"
	"math"
	"os"
	"testing"

	"github.com/alexfalkowski/go-linux/file"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

func TestFadvisePreservesTheBorrowedFileOffset(t *testing.T) {
	f := test.TempFile(t, 0)
	_, err := f.WriteString("abcd")
	require.NoError(t, err)
	reader := test.OpenFile(t, f.Name())
	_, err = reader.Seek(2, io.SeekStart)
	require.NoError(t, err)

	for _, scenario := range []struct {
		name   string
		length int64
		advice file.Advice
	}{
		{name: "normal", advice: file.Normal, length: 0},
		{name: "random", advice: file.Random, length: 0},
		{name: "sequential", advice: file.Sequential, length: 0},
		{name: "will need", advice: file.WillNeed, length: 2},
		{name: "do not need", advice: file.DontNeed, length: 0},
		{name: "no reuse", advice: file.NoReuse, length: 0},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			require.NoError(t, file.Fadvise(reader, scenario.advice, 1, scenario.length))

			offset, err := reader.Seek(0, io.SeekCurrent)
			require.NoError(t, err)
			require.EqualValues(t, 2, offset)
		})
	}
}

func TestFadviseValidatesInputsBeforeBorrowing(t *testing.T) {
	f := test.TempFile(t, 0)
	require.NoError(t, f.Close())

	require.ErrorIs(t, file.Fadvise(f, file.Advice(256), 0, 0), os.ErrInvalid, "unknown advice")
	require.ErrorIs(t, file.Fadvise(f, file.Normal, -1, 0), os.ErrInvalid, "negative offset")
	require.ErrorIs(t, file.Fadvise(f, file.Normal, 0, -1), os.ErrInvalid, "negative length")
	require.ErrorIs(t, file.Fadvise(f, file.Normal, math.MaxInt64, 1), test.ErrFileTooLarge, "overflow")
	require.ErrorIs(t, file.Fadvise(f, file.Normal, math.MaxInt64-1, 1), os.ErrClosed, "largest valid range end")
}

func TestFadviseRejectsUnavailableFiles(t *testing.T) {
	require.ErrorIs(t, file.Fadvise(nil, file.Normal, 0, 0), os.ErrInvalid)
	require.ErrorIs(t, file.Fadvise(new(os.File), file.Normal, 0, 0), os.ErrInvalid)
}

func TestFadvisePreservesThePipeError(t *testing.T) {
	reader, writer := test.Pipe(t)

	err := file.Fadvise(reader, file.Sequential, 0, 0)
	require.ErrorIs(t, err, test.ErrIllegalSeek)
	var pathError *os.PathError
	require.ErrorAs(t, err, &pathError)
	require.Equal(t, "fadvise", pathError.Op)
	require.Equal(t, reader.Name(), pathError.Path)

	_, err = writer.WriteString("x")
	require.NoError(t, err)
	var data [1]byte
	_, err = io.ReadFull(reader, data[:])
	require.NoError(t, err)
	require.Equal(t, byte('x'), data[0])
}

func TestReadaheadPreservesTheBorrowedFileOffset(t *testing.T) {
	f := test.TempFile(t, 0)
	_, err := f.WriteString("abcd")
	require.NoError(t, err)

	for _, length := range []int64{2, 0} {
		require.NoError(t, file.Readahead(f, 1, length), "length %d", length)

		offset, err := f.Seek(0, io.SeekCurrent)
		require.NoError(t, err)
		require.EqualValues(t, 4, offset, "length %d", length)
	}
}

func TestReadaheadValidatesRangesBeforeBorrowing(t *testing.T) {
	f := test.TempFile(t, 0)
	require.NoError(t, f.Close())

	require.ErrorIs(t, file.Readahead(f, -1, 0), os.ErrInvalid, "negative offset")
	require.ErrorIs(t, file.Readahead(f, 0, -1), os.ErrInvalid, "negative length")
	require.ErrorIs(t, file.Readahead(f, math.MaxInt64, 1), test.ErrFileTooLarge, "overflow")
	require.ErrorIs(t, file.Readahead(f, math.MaxInt64-1, 1), os.ErrClosed, "largest valid range end")
}

func TestReadaheadRejectsUnavailableFiles(t *testing.T) {
	require.ErrorIs(t, file.Readahead(nil, 0, 1), os.ErrInvalid)
	require.ErrorIs(t, file.Readahead(new(os.File), 0, 1), os.ErrInvalid)
}

func TestReadaheadOnWriteOnlyFilePreservesTheKernelError(t *testing.T) {
	f := test.TempFile(t, 0)
	writer, err := os.OpenFile(f.Name(), os.O_WRONLY, 0)
	require.NoError(t, err)
	test.Cleanup(t, writer)

	err = file.Readahead(writer, 0, 0)
	require.ErrorIs(t, err, test.ErrBadDescriptor)
	var pathError *os.PathError
	require.ErrorAs(t, err, &pathError)
	require.Equal(t, "readahead", pathError.Op)
	require.Equal(t, writer.Name(), pathError.Path)

	_, err = writer.WriteString("x")
	require.NoError(t, err)
}
