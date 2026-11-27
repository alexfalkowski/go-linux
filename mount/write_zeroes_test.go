//go:build linux

package mount_test

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/alexfalkowski/go-linux/file"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

func TestFallocateWriteZeroesPreparesOverwritesOnExt4(t *testing.T) {
	path := test.WriteZeroesFilesystem(t)
	if path == "" {
		return
	}

	t.Run("written extents for subsequent overwrites", func(t *testing.T) {
		segment, err := os.CreateTemp(path, "write-zeroes-*")
		require.NoError(t, err)
		test.Cleanup(t, segment)
		block := test.FileAllocationBlockSize(t, segment, false)
		first := bytes.Repeat([]byte("a"), block)
		second := bytes.Repeat([]byte("b"), block)
		_, err = segment.Write(bytes.Join([][]byte{first, second}, nil))
		require.NoError(t, err)
		require.NoError(t, segment.Sync())

		require.NoError(t, file.Fallocate(segment, file.WriteZeroes, int64(block), int64(2*block)))

		offset, err := segment.Seek(0, io.SeekCurrent)
		require.NoError(t, err)
		require.EqualValues(t, 2*block, offset, "the borrowed file offset must stay at the old EOF")
		contents, err := os.ReadFile(segment.Name())
		require.NoError(t, err)
		require.Equal(t, append(first, make([]byte, 2*block)...), contents)
		//nolint:gosec // statfs supplies the positive block size of the fixture.
		secondOffset, thirdOffset := uint64(block), uint64(2*block)
		test.FileExtentAt(t, segment, secondOffset)
		before := test.FileExtentAt(t, segment, thirdOffset)

		_, err = segment.WriteAt([]byte("record"), int64(2*block))
		require.NoError(t, err)
		require.NoError(t, segment.Sync())

		require.Equal(t, before, test.FileExtentAt(t, segment, thirdOffset), "the overwrite must reuse the prepared mapping")
	})

	t.Run("zero range leaves the new extent unwritten", func(t *testing.T) {
		control, err := os.CreateTemp(path, "zero-range-*")
		require.NoError(t, err)
		test.Cleanup(t, control)
		block := test.FileAllocationBlockSize(t, control, false)

		require.NoError(t, file.Fallocate(control, file.ZeroRange, 0, int64(block)))

		require.True(t, test.FileBlockUnwrittenAt(t, control, 0))
	})

	t.Run("unaligned edges preserve bytes outside the range", func(t *testing.T) {
		segment, err := os.CreateTemp(path, "unaligned-*")
		require.NoError(t, err)
		test.Cleanup(t, segment)
		block := test.FileAllocationBlockSize(t, segment, false)
		original := bytes.Repeat([]byte("a"), 2*block)
		_, err = segment.Write(original)
		require.NoError(t, err)
		require.NoError(t, segment.Sync())

		require.NoError(t, file.Fallocate(segment, file.WriteZeroes, 1, int64(len(original)-2)))

		contents, err := os.ReadFile(segment.Name())
		require.NoError(t, err)
		expected := make([]byte, len(original))
		expected[0], expected[len(expected)-1] = 'a', 'a'
		require.Equal(t, expected, contents)
	})
}
