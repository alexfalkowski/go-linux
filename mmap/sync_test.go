//go:build linux

package mmap_test

import (
	"os"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/mmap"
	"github.com/stretchr/testify/require"
)

func TestSyncWritesSharedFileAfterSourceClose(t *testing.T) {
	pageSize := os.Getpagesize()
	offset := int64(pageSize)
	file := test.TempFile(t, 3*offset)
	region := test.Mapping(t, file, offset, pageSize+1, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read | mmap.Write})
	require.NoError(t, file.Close())

	view := region.Bytes()
	view[0], view[pageSize] = 'a', 'z'
	require.NoError(t, region.Sync(mmap.SyncOptions{}))

	reader := test.OpenFile(t, file.Name())
	require.Equal(t, []byte("a"), test.ReadAt(t, reader, offset, 1), "first mapped page")
	require.Equal(t, []byte("z"), test.ReadAt(t, reader, 2*offset, 1), "partial last page")
}

func TestDefaultSyncRequestsSynchronousWritebackForEntireRegion(t *testing.T) {
	if !test.SeccompProcess(t) {
		return
	}
	test.LockThread()
	length := os.Getpagesize() + 1
	file := test.TempFile(t, int64(length))
	region := test.Mapping(t, file, 0, length, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read})
	test.RejectSynchronousMappingWriteback(t, region.Bytes())

	err := region.Sync(mmap.SyncOptions{})
	require.ErrorIs(t, err, test.ErrCanceled, "default Sync must request MS_SYNC over both pages")

	require.NoError(t, region.Sync(mmap.SyncOptions{Mode: mmap.SyncAsync}))
}

func TestSyncWithInvalidationKeepsPrivateFileWritesInTheView(t *testing.T) {
	file := test.TempFile(t, int64(os.Getpagesize()))
	test.WriteAt(t, file, 0, "file")
	region := test.Mapping(t, file, 0, 4, mmap.Options{Protection: mmap.Read | mmap.Write})
	copy(region.Bytes(), "copy")

	require.NoError(t, region.Sync(mmap.SyncOptions{Mode: mmap.SyncWait, Flags: mmap.SyncInvalidate}))

	require.Equal(t, "copy", string(region.Bytes()))
	require.Equal(t, []byte("file"), test.ReadAt(t, file, 0, 4))
}

func TestAsyncSyncPreservesAnonymousContents(t *testing.T) {
	for _, scenario := range []struct {
		name string
		mode mmap.Mode
	}{
		{name: "private", mode: mmap.Private},
		{name: "shared", mode: mmap.Shared},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			region := test.Anonymous(t, 4, mmap.Options{Mode: scenario.mode, Protection: mmap.Read | mmap.Write})
			copy(region.Bytes(), "data")

			require.NoError(t, region.Sync(mmap.SyncOptions{Mode: mmap.SyncAsync}))

			require.Equal(t, "data", string(region.Bytes()))
		})
	}
}

func TestSyncRejectsInvalidModes(t *testing.T) {
	region := test.Anonymous(t, 1, mmap.Options{})
	for _, scenario := range []struct {
		name string
		mode mmap.SyncMode
	}{
		{name: "unknown mode", mode: 255},
		{name: "combined modes", mode: mmap.SyncWait | mmap.SyncAsync},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			err := region.Sync(mmap.SyncOptions{Mode: scenario.mode})

			require.ErrorIs(t, err, os.ErrInvalid)
			test.PathError(t, err, "msync", "mmap")
		})
	}
}

func TestSyncRejectsUnknownFlags(t *testing.T) {
	region := test.Anonymous(t, 1, mmap.Options{})

	err := region.Sync(mmap.SyncOptions{Flags: 255})
	require.ErrorIs(t, err, os.ErrInvalid)
	test.PathError(t, err, "msync", "mmap")
}

func TestSyncInvalidationRejectsALockedLastPage(t *testing.T) {
	pageSize := os.Getpagesize()
	region := test.Anonymous(t, pageSize+1, mmap.Options{Protection: mmap.Read | mmap.Write})
	test.LockMappingPage(t, region.Bytes()[pageSize:])

	err := region.Sync(mmap.SyncOptions{Mode: mmap.SyncAsync, Flags: mmap.SyncInvalidate})
	require.ErrorIs(t, err, test.ErrBusy)
	test.PathError(t, err, "msync", "mmap")

	require.NoError(t, region.Sync(mmap.SyncOptions{Mode: mmap.SyncAsync}), "synchronization without invalidation accepts the same locked region")
}

func TestSyncSafelyRacesWithClose(t *testing.T) {
	region := test.Anonymous(t, 1, mmap.Options{})
	first := make(chan error, 1)
	result := make(chan error, 1)
	ctx := t.Context()
	go func() {
		first <- region.Sync(mmap.SyncOptions{})
		for ctx.Err() == nil {
			if err := region.Sync(mmap.SyncOptions{}); err != nil {
				result <- err
				return
			}
		}
	}()

	require.NoError(t, test.Receive(t, first), "synchronization must begin before Close")
	require.NoError(t, region.Close())
	require.ErrorIs(t, test.Receive(t, result), os.ErrClosed)
}
