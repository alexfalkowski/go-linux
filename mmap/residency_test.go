//go:build linux

package mmap_test

import (
	"os"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/mmap"
	"github.com/stretchr/testify/require"
)

func TestImmediateLockPopulatesAndAccountsForTheWholeRegion(t *testing.T) {
	pageSize := os.Getpagesize()
	test.RequireMemoryLock(t, 2*pageSize)
	region := test.Anonymous(t, pageSize+1, mmap.Options{Protection: mmap.Read | mmap.Write})
	before := test.LockedMemory(t)

	var mode mmap.LockMode
	require.NoError(t, region.Lock(mode))
	require.Equal(t, before+2*pageSize, test.LockedMemory(t))

	snapshot, err := region.Residency()
	require.NoError(t, err)
	require.Equal(t, mmap.Residency{PageSize: pageSize, Resident: []bool{true, true}}, snapshot)
}

func TestOnFaultLockReservesTheWholeRangeWithoutPrefaulting(t *testing.T) {
	pageSize := os.Getpagesize()
	test.RequireMemoryLock(t, 2*pageSize)
	region := test.Anonymous(t, pageSize+1, mmap.Options{Protection: mmap.Read | mmap.Write})
	before := test.LockedMemory(t)

	require.NoError(t, region.Lock(mmap.LockOnFault))
	require.Equal(t, before+2*pageSize, test.LockedMemory(t))

	snapshot, err := region.Residency()
	require.NoError(t, err)
	require.Equal(t, mmap.Residency{PageSize: pageSize, Resident: []bool{false, false}}, snapshot)

	region.Bytes()[pageSize] = 1

	snapshot, err = region.Residency()
	require.NoError(t, err)
	require.Len(t, snapshot.Resident, 2)
	require.True(t, snapshot.Resident[1], "the faulted final page is resident")
}

func TestUnlockReleasesRepeatedLocksAndKeepsTheMapping(t *testing.T) {
	pageSize := os.Getpagesize()
	test.RequireMemoryLock(t, pageSize)
	region := test.Anonymous(t, pageSize, mmap.Options{Protection: mmap.Read | mmap.Write})
	region.Bytes()[0] = 42
	before := test.LockedMemory(t)
	require.NoError(t, region.Lock(mmap.LockImmediate))
	require.NoError(t, region.Lock(mmap.LockImmediate))
	require.Equal(t, before+pageSize, test.LockedMemory(t))

	require.NoError(t, region.Unlock())

	require.Equal(t, before, test.LockedMemory(t))
	require.Equal(t, byte(42), region.Bytes()[0])
}

func TestCloseReleasesMemoryLock(t *testing.T) {
	pageSize := os.Getpagesize()
	test.RequireMemoryLock(t, pageSize)
	region := test.Anonymous(t, pageSize, mmap.Options{Protection: mmap.Read | mmap.Write})
	before := test.LockedMemory(t)
	require.NoError(t, region.Lock(mmap.LockImmediate))
	require.Equal(t, before+pageSize, test.LockedMemory(t))

	require.NoError(t, region.Close())

	require.Equal(t, before, test.LockedMemory(t))
}

func TestLockCountsThePartialPageAgainstTheChildLimit(t *testing.T) {
	pageSize := os.Getpagesize()
	if !test.MemoryLockLimitProcess(t, pageSize) {
		return
	}
	region := test.Anonymous(t, pageSize+1, mmap.Options{Protection: mmap.Read | mmap.Write})
	before := test.LockedMemory(t)

	err := region.Lock(mmap.LockImmediate)
	require.ErrorIs(t, err, test.ErrNoMemory)
	test.PathError(t, err, "mlock", "mmap")

	require.Equal(t, before, test.LockedMemory(t))
	require.Len(t, region.Bytes(), pageSize+1, "the rejected lock retains the mapping")
}

func TestLockRejectsUnknownModes(t *testing.T) {
	region := test.Anonymous(t, 1, mmap.Options{})

	err := region.Lock(255)
	require.ErrorIs(t, err, os.ErrInvalid)
	test.PathError(t, err, "mlock", "mmap")
}

func TestResidencySnapshotsOutliveTheRegion(t *testing.T) {
	pageSize := os.Getpagesize()
	region := test.Anonymous(t, pageSize+1, mmap.Options{Protection: mmap.Read | mmap.Write})
	before, err := region.Residency()
	require.NoError(t, err)

	region.Bytes()[pageSize] = 1

	after, err := region.Residency()
	require.NoError(t, err)
	require.NoError(t, region.Close())

	require.Equal(t, mmap.Residency{PageSize: pageSize, Resident: []bool{false, false}}, before)
	require.Equal(t, pageSize, after.PageSize)
	require.Len(t, after.Resident, 2)
	require.True(t, after.Resident[1])
}
