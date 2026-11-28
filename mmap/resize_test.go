//go:build linux

package mmap_test

import (
	"os"
	"sync"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/mmap"
	"github.com/stretchr/testify/require"
)

func TestResizeMovesBlockedSharedMappingAndCloseUnmapsIt(t *testing.T) {
	const marker = "mmap-resize-child"
	if os.Args[len(os.Args)-1] != marker {
		ctx, command := test.Command(t, t.Name(), "-test.v", "--", marker)
		output, err := command.CombinedOutput()
		require.NoError(t, ctx.Err(), "resize subprocess exceeded its time bound")
		require.NoError(t, err, "resize subprocess: %s", output)
		return
	}

	pageSize := os.Getpagesize()
	file := test.TempFile(t, int64(3*pageSize))
	region := test.Mapping(t, file, int64(pageSize), pageSize, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read | mmap.Write})
	region.Bytes()[pageSize-1] = 43
	oldAddress := test.BlockMappingGrowth(t, region.Bytes())

	require.NoError(t, region.Resize(pageSize+7))

	grown := region.Bytes()
	require.Len(t, grown, pageSize+7)
	require.Equal(t, len(grown), cap(grown))
	grownAddress := test.MappingAddress(grown)
	require.NotEqual(t, oldAddress, grownAddress, "the occupied next page forces relocation")
	test.RequireUnmappedPage(t, oldAddress)
	require.Equal(t, byte(43), grown[pageSize-1])
	require.Equal(t, "rw-s", test.MappingPermissions(t, grown))

	grown[pageSize+6] = 17
	var contents [1]byte
	_, err := file.ReadAt(contents[:], int64(2*pageSize+6))
	require.NoError(t, err)
	require.Equal(t, byte(17), contents[0], "the new page keeps the original file offset and shared backing")

	require.NoError(t, region.Resize(pageSize-1))

	shrunk := region.Bytes()
	require.Len(t, shrunk, pageSize-1)
	require.Equal(t, len(shrunk), cap(shrunk))
	test.RequireUnmappedPage(t, grownAddress+uintptr(pageSize))
	shrunkAddress := test.MappingAddress(shrunk)

	require.NoError(t, region.Close())

	require.Nil(t, region.Bytes())
	test.RequireUnmappedPage(t, shrunkAddress)
}

func TestResizeWithinOnePageKeepsTheRetainedPrefix(t *testing.T) {
	region := test.Anonymous(t, 3, mmap.Options{Protection: mmap.Read | mmap.Write})
	copy(region.Bytes(), "abc")

	require.NoError(t, region.Resize(2))
	require.Equal(t, "ab", string(region.Bytes()))
	require.Equal(t, 2, cap(region.Bytes()))

	require.NoError(t, region.Resize(3))
	require.Equal(t, "abc", string(region.Bytes()), "shrinking within a page never unmapped its last byte")
}

func TestResizeRejectsGrowthBeyondSharedAnonymousBacking(t *testing.T) {
	pageSize := os.Getpagesize()
	region := test.Anonymous(t, pageSize, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read | mmap.Write})
	oldView := region.Bytes()
	oldView[pageSize-1] = 43

	err := region.Resize(pageSize + 1)
	require.ErrorIs(t, err, os.ErrInvalid)
	test.PathError(t, err, "mremap", "mmap")

	require.Len(t, region.Bytes(), pageSize)
	require.Equal(t, byte(43), oldView[pageSize-1])
}

func TestResizeRegrowsWithinSharedAnonymousBacking(t *testing.T) {
	pageSize := os.Getpagesize()
	region := test.Anonymous(t, pageSize+1, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read | mmap.Write})
	region.Bytes()[pageSize] = 43

	require.NoError(t, region.Resize(pageSize))
	require.NoError(t, region.Resize(2*pageSize))

	view := region.Bytes()
	require.Len(t, view, 2*pageSize)
	require.Equal(t, byte(43), view[pageSize])
}

func TestResizeFailureRetainsTheLockedMappingAndItsViews(t *testing.T) {
	pageSize := os.Getpagesize()
	if !test.MemoryLockLimitProcess(t, pageSize) {
		return
	}
	region := test.Anonymous(t, pageSize, mmap.Options{Protection: mmap.Read | mmap.Write})
	oldView := region.Bytes()
	oldView[pageSize-1] = 43
	before := test.LockedMemory(t)
	require.NoError(t, region.Lock(mmap.LockImmediate))

	err := region.Resize(pageSize + 1)
	require.ErrorIs(t, err, test.ErrTryAgain)
	test.PathError(t, err, "mremap", "mmap")

	require.Len(t, region.Bytes(), pageSize)
	require.Equal(t, byte(43), oldView[pageSize-1])
	require.Equal(t, before+pageSize, test.LockedMemory(t))

	require.NoError(t, region.Close())
	require.Equal(t, before, test.LockedMemory(t))
}

func TestResizeRejectsNonpositiveLength(t *testing.T) {
	region := test.Anonymous(t, 1, mmap.Options{Protection: mmap.Read | mmap.Write})
	region.Bytes()[0] = 43

	for _, length := range []int{0, -1} {
		err := region.Resize(length)
		require.ErrorIs(t, err, os.ErrInvalid)
		test.PathError(t, err, "mremap", "mmap")
	}
	require.Equal(t, []byte{43}, region.Bytes())
}

func TestResizeSafelyRacesWithClose(t *testing.T) {
	region := test.Anonymous(t, 1, mmap.Options{})
	var group sync.WaitGroup
	var resizeErr error

	group.Go(func() {
		resizeErr = region.Resize(os.Getpagesize() + 1)
	})

	require.NoError(t, region.Close())
	group.Wait()

	if resizeErr != nil {
		require.ErrorIs(t, resizeErr, os.ErrClosed)
	}
	require.Nil(t, region.Bytes())
}
