//go:build linux

package file_test

import (
	"bytes"
	"io"
	"math"
	"os"
	"testing"

	"github.com/alexfalkowski/go-linux/file"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

func TestFallocateReservesBlocksWithoutOverwritingData(t *testing.T) {
	for _, scenario := range []struct {
		name string
		mode file.AllocationMode
		grow bool
	}{
		{name: "growing allocation", mode: file.Allocate, grow: true},
		{name: "keep size", mode: file.KeepSize},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f := test.TempFile(t, 0)
			_, err := f.WriteAt([]byte("a"), 0)
			require.NoError(t, err)
			require.NoError(t, f.Sync())
			blocksBefore := test.FileAllocatedBlocks(t, f)
			length := int64(4 * test.FileAllocationBlockSize(t, f, false))

			require.NoError(t, file.Fallocate(f, scenario.mode, 0, length))

			contents, err := io.ReadAll(f)
			require.NoError(t, err)
			if scenario.grow {
				require.Len(t, contents, int(length))
				require.Equal(t, make([]byte, length-1), contents[1:])
			} else {
				require.Len(t, contents, 1)
			}
			require.Equal(t, byte('a'), contents[0])
			require.Greater(t, test.FileAllocatedBlocks(t, f), blocksBefore)
		})
	}
}

func TestFallocatePunchesAHoleWithoutChangingSize(t *testing.T) {
	f := test.TempFile(t, 0)
	block := test.FileAllocationBlockSize(t, f, false)
	original := bytes.Repeat([]byte("a"), 3*block)
	_, err := f.Write(original)
	require.NoError(t, err)
	require.NoError(t, f.Sync())
	blocksBefore := test.FileAllocatedBlocks(t, f)

	require.NoError(t, file.Fallocate(f, file.PunchHole|file.KeepSize, int64(block-1), int64(block+2)))

	contents, err := os.ReadFile(f.Name())
	require.NoError(t, err)
	require.Len(t, contents, 3*block)
	require.Equal(t, original[:block-1], contents[:block-1])
	require.Equal(t, make([]byte, block+2), contents[block-1:2*block+1])
	require.Equal(t, original[2*block+1:], contents[2*block+1:])
	require.Less(t, test.FileAllocatedBlocks(t, f), blocksBefore)
}

func TestFallocateZerosAndAllocatesTheRange(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		mode       file.AllocationMode
		sizeBlocks int
	}{
		{name: "grow through end of range", mode: file.ZeroRange, sizeBlocks: 4},
		{name: "keep size", mode: file.ZeroRange | file.KeepSize, sizeBlocks: 2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f := test.TempFile(t, 0)
			block := test.FileAllocationBlockSize(t, f, true)
			original := bytes.Repeat([]byte("a"), 2*block)
			_, err := f.Write(original)
			require.NoError(t, err)
			require.NoError(t, f.Sync())
			blocksBefore := test.FileAllocatedBlocks(t, f)

			require.NoError(t, file.Fallocate(f, scenario.mode, int64(block), int64(3*block)))

			contents, err := os.ReadFile(f.Name())
			require.NoError(t, err)
			require.Len(t, contents, scenario.sizeBlocks*block)
			require.Equal(t, original[:block], contents[:block])
			require.Equal(t, make([]byte, (scenario.sizeBlocks-1)*block), contents[block:])
			require.Greater(t, test.FileAllocatedBlocks(t, f), blocksBefore)
		})
	}
}

func TestFallocateZeroRangePreservesAllocatedBlocks(t *testing.T) {
	f := test.TempFile(t, 0)
	block := test.FileAllocationBlockSize(t, f, true)
	_, err := f.Write(bytes.Repeat([]byte("a"), block))
	require.NoError(t, err)
	require.NoError(t, f.Sync())
	blocksBefore := test.FileAllocatedBlocks(t, f)
	require.Positive(t, blocksBefore)

	require.NoError(t, file.Fallocate(f, file.ZeroRange, 0, int64(block)))

	contents, err := os.ReadFile(f.Name())
	require.NoError(t, err)
	require.Equal(t, make([]byte, block), contents)
	require.GreaterOrEqual(t, test.FileAllocatedBlocks(t, f), blocksBefore)
}

func TestFallocateCollapsesARangeAndShiftsLaterData(t *testing.T) {
	f := test.TempFile(t, 0)
	block := test.FileAllocationBlockSize(t, f, true)
	first := bytes.Repeat([]byte("a"), block)
	middle := bytes.Repeat([]byte("b"), block)
	last := bytes.Repeat([]byte("c"), block)
	_, err := f.Write(bytes.Join([][]byte{first, middle, last}, nil))
	require.NoError(t, err)

	require.NoError(t, file.Fallocate(f, file.CollapseRange, int64(block), int64(block)))

	contents, err := os.ReadFile(f.Name())
	require.NoError(t, err)
	require.Equal(t, bytes.Join([][]byte{first, last}, nil), contents)
}

func TestFallocateInsertsAHoleAndShiftsLaterData(t *testing.T) {
	f := test.TempFile(t, 0)
	block := test.FileAllocationBlockSize(t, f, true)
	first := bytes.Repeat([]byte("a"), block)
	last := bytes.Repeat([]byte("b"), block)
	_, err := f.Write(bytes.Join([][]byte{first, last}, nil))
	require.NoError(t, err)
	require.NoError(t, f.Sync())
	blocksBefore := test.FileAllocatedBlocks(t, f)

	require.NoError(t, file.Fallocate(f, file.InsertRange, int64(block), int64(block)))

	contents, err := os.ReadFile(f.Name())
	require.NoError(t, err)
	require.Equal(t, bytes.Join([][]byte{first, make([]byte, block), last}, nil), contents)
	require.Equal(t, blocksBefore, test.FileAllocatedBlocks(t, f))
}

func TestFallocatePreservesTheFilesystemAlignmentError(t *testing.T) {
	f := test.TempFile(t, 0)
	block := test.FileAllocationBlockSize(t, f, true)
	original := bytes.Repeat([]byte("a"), 3*block)
	_, err := f.Write(original)
	require.NoError(t, err)

	err = file.Fallocate(f, file.CollapseRange, 1, int64(block))
	require.ErrorIs(t, err, test.ErrInvalidArgument)

	contents, err := os.ReadFile(f.Name())
	require.NoError(t, err)
	require.Equal(t, original, contents)
}

func TestFallocateRejectsInvalidInputsBeforeBorrowing(t *testing.T) {
	f := test.TempFile(t, 0)
	require.NoError(t, f.Close())

	for _, scenario := range []struct {
		name   string
		want   error
		offset int64
		length int64
		mode   file.AllocationMode
	}{
		{name: "negative offset", offset: -1, length: 1, want: os.ErrInvalid},
		{name: "zero length", want: os.ErrInvalid},
		{name: "negative length", length: -1, want: os.ErrInvalid},
		{name: "overflow", offset: math.MaxInt64, length: 1, want: test.ErrFileTooLarge},
		{name: "punch without keep size", mode: file.PunchHole, length: 1, want: os.ErrInvalid},
		{name: "collapse with keep size", mode: file.CollapseRange | file.KeepSize, length: 1, want: os.ErrInvalid},
		{name: "insert with keep size", mode: file.InsertRange | file.KeepSize, length: 1, want: os.ErrInvalid},
		{name: "multiple operations", mode: file.PunchHole | file.KeepSize | file.ZeroRange, length: 1, want: os.ErrInvalid},
		{name: "unshare with zero range", mode: file.UnshareRange | file.ZeroRange, length: 1, want: os.ErrInvalid},
		{name: "unshare range overflow", mode: file.UnshareRange, offset: math.MaxInt64, length: 1, want: test.ErrFileTooLarge},
		{name: "write zeroes with keep size", mode: file.WriteZeroes | file.KeepSize, length: 1, want: os.ErrInvalid},
		{name: "write zeroes with zero range", mode: file.WriteZeroes | file.ZeroRange, length: 1, want: os.ErrInvalid},
		{name: "write zeroes range overflow", mode: file.WriteZeroes, offset: math.MaxInt64, length: 1, want: test.ErrFileTooLarge},
		{name: "reserved bit", mode: test.ReservedAllocationMode, length: 1, want: os.ErrInvalid},
		{name: "unknown bit", mode: 1 << 31, length: 1, want: os.ErrInvalid},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			err := file.Fallocate(f, scenario.mode, scenario.offset, scenario.length)
			require.ErrorIs(t, err, scenario.want)
		})
	}
	require.ErrorIs(t, file.Fallocate(f, file.Allocate, math.MaxInt64-1, 1), os.ErrClosed)
}

func TestFallocateUnshareRangePreservesUnsupportedFilesystemError(t *testing.T) {
	f := test.ShmemFile(t)
	_, err := f.WriteAt([]byte("segment"), 0)
	require.NoError(t, err)

	err = file.Fallocate(f, file.UnshareRange, 0, 4096)
	require.ErrorIs(t, err, test.ErrNotSupported)

	contents, err := io.ReadAll(f)
	require.NoError(t, err)
	require.Equal(t, "segment", string(contents))
}

func TestFallocateWriteZeroesPreservesUnsupportedFilesystemError(t *testing.T) {
	f := test.ShmemFile(t)
	_, err := f.WriteAt([]byte("segment"), 0)
	require.NoError(t, err)

	err = file.Fallocate(f, file.WriteZeroes, 0, 4096)
	require.ErrorIs(t, err, test.ErrNotSupported)

	contents, err := io.ReadAll(f)
	require.NoError(t, err)
	require.Equal(t, "segment", string(contents))
}

func TestFallocateRejectsUnavailableHandles(t *testing.T) {
	require.ErrorIs(t, file.Fallocate(nil, file.Allocate, 0, 1), os.ErrInvalid)
	require.ErrorIs(t, file.Fallocate(new(os.File), file.Allocate, 0, 1), os.ErrInvalid)

	f := test.TempFile(t, 0)
	require.NoError(t, f.Close())

	require.ErrorIs(t, file.Fallocate(f, file.Allocate, 0, 1), os.ErrClosed)
}

func TestFallocateLeavesAReadOnlyFileOpenOnFailure(t *testing.T) {
	f := test.TempFile(t, 0)
	_, err := f.WriteString("data")
	require.NoError(t, err)
	reader := test.OpenFile(t, f.Name())

	err = file.Fallocate(reader, file.Allocate, 0, 4096)
	require.ErrorIs(t, err, test.ErrBadDescriptor)
	var pathError *os.PathError
	require.ErrorAs(t, err, &pathError)
	require.Equal(t, "fallocate", pathError.Op)
	require.Equal(t, reader.Name(), pathError.Path)

	contents, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, "data", string(contents))
}

func TestFallocatePreservesTheBorrowedFileOffset(t *testing.T) {
	f := test.TempFile(t, 0)
	_, err := f.Seek(3, io.SeekStart)
	require.NoError(t, err)

	require.NoError(t, file.Fallocate(f, file.Allocate, 0, 4096))

	offset, err := f.Seek(0, io.SeekCurrent)
	require.NoError(t, err)
	require.EqualValues(t, 3, offset)
}

func TestFallocateCanRaceWithClose(t *testing.T) {
	for range 16 {
		f := test.TempFile(t, 0)
		result := make(chan error, 1)
		go func() {
			result <- file.Fallocate(f, file.Allocate, 0, 4096)
		}()

		require.NoError(t, f.Close())
		err := <-result

		info, statErr := os.Stat(f.Name())
		require.NoError(t, statErr)
		if err != nil {
			require.ErrorIs(t, err, os.ErrClosed)
			require.Zero(t, info.Size())
		} else {
			require.EqualValues(t, 4096, info.Size())
		}
	}
}
