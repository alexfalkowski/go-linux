//go:build linux

package mmap_test

import (
	"os"
	"strings"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/memfd"
	"github.com/alexfalkowski/go-linux/mmap"
	"github.com/stretchr/testify/require"
)

func TestFreeDiscardsLazilyOnlyOnPrivateAnonymousMappings(t *testing.T) {
	pageSize := os.Getpagesize()
	region := test.Anonymous(t, pageSize, mmap.Options{Protection: mmap.Read | mmap.Write})
	region.Bytes()[0] = 43
	shared := test.Anonymous(t, pageSize, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read | mmap.Write})

	require.ErrorIs(t, shared.Advise(mmap.Free), test.ErrInvalidArgument)
	require.NoError(t, region.Advise(mmap.Free))

	// The page may be reclaimed before the first read. A new write must work.
	require.Contains(t, []byte{0, 43}, region.Bytes()[0])
	region.Bytes()[0] = 17
	require.Equal(t, byte(17), region.Bytes()[0])
}

func TestRemovePunchesAHoleThroughAReadOnlySharedMapping(t *testing.T) {
	pageSize := os.Getpagesize()
	file := test.Memfd(t, memfd.Options{})
	require.NoError(t, file.Truncate(int64(2*pageSize)))

	_, err := file.WriteAt([]byte{43}, 0)
	require.NoError(t, err)
	_, err = file.WriteAt([]byte{17}, int64(pageSize))
	require.NoError(t, err)

	region, err := file.Map(0, pageSize, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read})
	require.NoError(t, err)
	test.Cleanup(t, region)
	require.Equal(t, byte(43), region.Bytes()[0])

	require.NoError(t, region.Advise(mmap.Remove))

	var contents [2]byte
	_, err = file.ReadAt(contents[:1], 0)
	require.NoError(t, err)
	_, err = file.ReadAt(contents[1:], int64(pageSize))
	require.NoError(t, err)
	require.Equal(t, [2]byte{0, 17}, contents)
	require.Zero(t, region.Bytes()[0])

	info, err := file.Stat()
	require.NoError(t, err)
	require.EqualValues(t, 2*pageSize, info.Size())
}

func TestForkAndDumpAdviceChangesKernelPolicy(t *testing.T) {
	for _, scenario := range []struct {
		name string
		flag string
		set  mmap.Advice
		undo mmap.Advice
	}{
		{name: "exclude from fork", flag: "dc", set: mmap.DontFork, undo: mmap.DoFork},
		{name: "wipe on fork", flag: "wf", set: mmap.WipeOnFork, undo: mmap.KeepOnFork},
		{name: "exclude from dumps", flag: "dd", set: mmap.DontDump, undo: mmap.DoDump},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			region := test.Anonymous(t, 1, mmap.Options{Protection: mmap.Read | mmap.Write})
			region.Bytes()[0] = 43

			require.NoError(t, region.Advise(scenario.set))
			require.Contains(t, strings.Fields(test.MappingStatus(t, region.Bytes(), "VmFlags")), scenario.flag)
			require.Equal(t, byte(43), region.Bytes()[0])

			require.NoError(t, region.Advise(scenario.undo))
			require.NotContains(t, strings.Fields(test.MappingStatus(t, region.Bytes(), "VmFlags")), scenario.flag)
		})
	}
}

func TestMergeAdviceChangesKernelPolicy(t *testing.T) {
	test.RequireKSM(t)
	region := test.Anonymous(t, 1, mmap.Options{Protection: mmap.Read | mmap.Write})
	region.Bytes()[0] = 43

	require.NoError(t, region.Advise(mmap.Mergeable))
	require.Contains(t, strings.Fields(test.MappingStatus(t, region.Bytes(), "VmFlags")), "mg")

	require.NoError(t, region.Advise(mmap.Unmergeable))
	require.NotContains(t, strings.Fields(test.MappingStatus(t, region.Bytes(), "VmFlags")), "mg")
	require.Equal(t, byte(43), region.Bytes()[0])
}

func TestHugePageAdviceChangesKernelPolicy(t *testing.T) {
	test.TransparentHugePageSize(t)
	region := test.Anonymous(t, 1, mmap.Options{Protection: mmap.Read | mmap.Write})

	require.NoError(t, region.Advise(mmap.HugePage))
	require.Contains(t, strings.Fields(test.MappingStatus(t, region.Bytes(), "VmFlags")), "hg")

	require.NoError(t, region.Advise(mmap.NoHugePage))
	flags := strings.Fields(test.MappingStatus(t, region.Bytes(), "VmFlags"))
	require.Contains(t, flags, "nh")
	require.NotContains(t, flags, "hg")
}

func TestCollapseCreatesAnAnonymousHugePage(t *testing.T) {
	hugeSize := test.TransparentHugePageSize(t)
	test.RequireBoundedHugePage(t, hugeSize)
	// Two PMD-sized intervals include a complete, naturally aligned interval.
	region := test.Anonymous(t, 2*hugeSize, mmap.Options{Protection: mmap.Read | mmap.Write})
	require.NoError(t, region.Advise(mmap.NoHugePage))
	view := region.Bytes()
	for offset := 0; offset < len(view); offset += os.Getpagesize() {
		view[offset] = 43
	}
	require.Equal(t, []string{"0", "kB"}, strings.Fields(test.MappingStatus(t, view, "AnonHugePages")))
	require.NoError(t, region.Advise(mmap.HugePage))

	require.NoError(t, region.Advise(mmap.Collapse))

	require.NotEqual(t, []string{"0", "kB"}, strings.Fields(test.MappingStatus(t, view, "AnonHugePages")))
	require.Equal(t, byte(43), view[hugeSize])
}

func TestReclaimAdvicePreservesContents(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		advice mmap.Advice
	}{
		{name: "cold", advice: mmap.Cold},
		{name: "page out", advice: mmap.PageOut},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			region := test.Anonymous(t, 1, mmap.Options{Protection: mmap.Read | mmap.Write})
			region.Bytes()[0] = 43

			require.NoError(t, region.Advise(scenario.advice))

			require.Equal(t, byte(43), region.Bytes()[0])
		})
	}
}

func TestPopulatePrefaultsEveryPage(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		protection mmap.Protection
		advice     mmap.Advice
	}{
		{name: "readable", protection: mmap.Read, advice: mmap.PopulateRead},
		{name: "writable", protection: mmap.Write, advice: mmap.PopulateWrite},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			pageSize := os.Getpagesize()
			region := test.Anonymous(t, pageSize+1, mmap.Options{Protection: scenario.protection})

			before, err := region.Residency()
			require.NoError(t, err)
			require.Equal(t, []bool{false, false}, before.Resident)

			require.NoError(t, region.Advise(scenario.advice))

			after, err := region.Residency()
			require.NoError(t, err)
			require.Equal(t, []bool{true, true}, after.Resident)
		})
	}
}

func TestPopulateWriteRejectsReadOnlyMapping(t *testing.T) {
	region := test.Anonymous(t, 1, mmap.Options{Protection: mmap.Read})

	err := region.Advise(mmap.PopulateWrite)
	require.ErrorIs(t, err, test.ErrInvalidArgument)
	test.PathError(t, err, "madvise", "mmap")
}

func TestDontNeedLockedDiscardsContentsWithoutUnlocking(t *testing.T) {
	pageSize := os.Getpagesize()
	test.RequireMemoryLock(t, pageSize)
	region := test.Anonymous(t, pageSize, mmap.Options{Protection: mmap.Read | mmap.Write})
	region.Bytes()[0] = 43
	before := test.LockedMemory(t)
	require.NoError(t, region.Lock(mmap.LockImmediate))
	require.ErrorIs(t, region.Advise(mmap.DontNeed), test.ErrInvalidArgument)

	require.NoError(t, region.Advise(mmap.DontNeedLocked))

	require.Zero(t, region.Bytes()[0])
	require.Equal(t, before+pageSize, test.LockedMemory(t))
}

func TestGuardRemoveRestoresAccessWithDiscardedContents(t *testing.T) {
	pageSize := os.Getpagesize()
	region := test.Anonymous(t, pageSize+1, mmap.Options{Protection: mmap.Read | mmap.Write})
	region.Bytes()[0] = 43
	region.Bytes()[pageSize] = 17
	require.NoError(t, region.Advise(mmap.GuardInstall))

	require.NoError(t, region.Advise(mmap.GuardRemove))

	require.Zero(t, region.Bytes()[0])
	require.Zero(t, region.Bytes()[pageSize])
}

func TestGuardRemoveReloadsFileContents(t *testing.T) {
	file := test.TempFile(t, int64(os.Getpagesize()))
	_, err := file.WriteAt([]byte{43}, 0)
	require.NoError(t, err)
	region := test.Mapping(t, file, 0, 1, mmap.Options{Protection: mmap.Read | mmap.Write})
	region.Bytes()[0] = 17
	require.NoError(t, region.Advise(mmap.GuardInstall))

	require.NoError(t, region.Advise(mmap.GuardRemove))

	require.Equal(t, byte(43), region.Bytes()[0])
}
