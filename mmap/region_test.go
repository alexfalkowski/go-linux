//go:build linux

package mmap_test

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/mmap"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

var _ io.Closer = (*mmap.Region)(nil)

func TestAnonymousMappingsAreZeroFilledAndBounded(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		permissions string
		mode        mmap.Mode
	}{
		{name: "private mapping", permissions: "rw-p", mode: mmap.Private},
		{name: "shared mapping", permissions: "rw-s", mode: mmap.Shared},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			length := os.Getpagesize() + 13
			region := test.Anonymous(t, length, mmap.Options{Mode: scenario.mode, Protection: mmap.Read | mmap.Write})
			data := region.Bytes()
			require.Equal(t, scenario.permissions, test.MappingPermissions(t, region.Bytes()))
			require.Len(t, data, length)
			require.Equal(t, length, cap(data), "page rounding must not expose extra capacity")
			require.Equal(t, make([]byte, length), data)

			data[0], data[length-1] = 'a', 'z'
			expected := make([]byte, length)
			expected[0], expected[length-1] = 'a', 'z'
			require.Equal(t, expected, region.Bytes(), "Bytes borrows the same writable mapping through its partial last page")
			require.Panics(t, func() {
				_ = data[length]
			})

			require.Panics(t, func() {
				_ = data[:length+1]
			})
		})
	}
}

func TestAnonymousAllocationsHaveIndependentBacking(t *testing.T) {
	for _, scenario := range []struct {
		name string
		mode mmap.Mode
	}{
		{name: "private mappings", mode: mmap.Private},
		{name: "shared mappings", mode: mmap.Shared},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			first := test.Anonymous(t, 1, mmap.Options{Mode: scenario.mode, Protection: mmap.Read | mmap.Write})
			second := test.Anonymous(t, 1, mmap.Options{Mode: scenario.mode, Protection: mmap.Read})
			first.Bytes()[0] = 'a'
			require.Zero(t, second.Bytes()[0])
		})
	}
}

func TestMappingWritesAreSharedOnlyWhenRequested(t *testing.T) {
	for _, scenario := range []struct {
		name                string
		mode                mmap.Mode
		peersAfterViewWrite string
		viewAfterPeerWrite  string
		viewAfterFileWrite  string
	}{
		{name: "private copies", mode: mmap.Private, peersAfterViewWrite: "source", viewAfterPeerWrite: "change", viewAfterFileWrite: "change"},
		{name: "shared views", mode: mmap.Shared, peersAfterViewWrite: "change", viewAfterPeerWrite: "update", viewAfterFileWrite: "fileIO"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			offset := int64(os.Getpagesize())
			file := test.TempFile(t, 2*offset)

			test.WriteAt(t, file, offset, "source")
			view := test.Mapping(t, file, offset, 6, mmap.Options{Mode: scenario.mode, Protection: mmap.Read | mmap.Write})
			peer := test.Mapping(t, file, offset, 6, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read | mmap.Write})
			type contents struct{ view, peer, file string }
			snapshot := func() contents {
				return contents{string(view.Bytes()), string(peer.Bytes()), string(test.ReadAt(t, file, offset, 6))}
			}

			require.Equal(t, contents{"source", "source", "source"}, snapshot(), "offset selects the second page")

			copy(view.Bytes(), "change")
			require.Equal(t, contents{"change", scenario.peersAfterViewWrite, scenario.peersAfterViewWrite}, snapshot(), "writes through the tested view")

			copy(peer.Bytes(), "update")
			require.Equal(t, contents{scenario.viewAfterPeerWrite, "update", "update"}, snapshot(), "writes through the shared peer")

			test.WriteAt(t, file, offset, "fileIO")
			require.Equal(t, contents{scenario.viewAfterFileWrite, "fileIO", "fileIO"}, snapshot(), "ordinary file writes")
		})
	}
}

func TestSourceFileAndMappingHaveIndependentLifetimes(t *testing.T) {
	file := test.TempFile(t, int64(os.Getpagesize()))
	region := test.Mapping(t, file, 0, 7, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read | mmap.Write})
	require.NoError(t, region.Close())
	_, err := file.WriteAt([]byte("message"), 0)
	require.NoError(t, err, "closing the region must leave the file open")

	region = test.Mapping(t, file, 0, 7, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read | mmap.Write})
	require.NoError(t, file.Close())
	require.Equal(t, "message", string(region.Bytes()), "closing the file must leave the mapping valid")
	copy(region.Bytes(), "changed")
	require.Equal(t, "changed", string(region.Bytes()))
}

func TestClosingARegionUnmapsItsEntireOriginalAllocation(t *testing.T) {
	file := test.TempFile(t, int64(3*os.Getpagesize()))
	region := test.Mapping(t, file, 0, 2*os.Getpagesize()+13, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read | mmap.Write})
	view := region.Bytes()[1:2]
	view[0] = 42
	require.NoError(t, region.Protect(mmap.Read))

	mappings, err := os.ReadFile("/proc/self/maps")
	require.NoError(t, err)
	require.Contains(t, string(mappings), file.Name(), "live file mapping must be present")
	require.NoError(t, region.Close())
	mappings, err = os.ReadFile("/proc/self/maps")
	require.NoError(t, err)
	require.NotContains(t, string(mappings), file.Name(), "Close must remove the entire original mapping despite escaped subslices")
}

func TestFailedMappingsLeaveNoRegionOrDescriptor(t *testing.T) {
	file := test.TempFile(t, int64(os.Getpagesize()))
	reader := test.OpenFile(t, file.Name())
	before := test.DescriptorCount(t)
	for range 20 {
		failed, err := mmap.Map(reader, 0, 1, mmap.Options{Mode: mmap.Shared, Protection: mmap.Write})
		test.NoResource(t, failed, err, unix.EACCES)
	}

	after := test.DescriptorCount(t)
	require.Equal(t, before, after, "failed mapping must not retain descriptors")

	mappings, err := os.ReadFile("/proc/self/maps")
	require.NoError(t, err)
	require.NotContains(t, string(mappings), file.Name(), "failed construction must not leave a mapping")
}

func TestReadOnlyFilesAllowPrivateWritesAndRejectSharedWrites(t *testing.T) {
	file := test.TempFile(t, int64(os.Getpagesize()))

	_, err := file.WriteAt([]byte("data"), 0)
	require.NoError(t, err)

	reader := test.OpenFile(t, file.Name())
	shared := test.Mapping(t, reader, 0, 4, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read})
	private := test.Mapping(t, reader, 0, 4, mmap.Options{Protection: mmap.Read | mmap.Write})

	copy(private.Bytes(), "copy")
	require.Equal(t, "data", string(shared.Bytes()), "a read-only source still permits private writable copies")
	require.ErrorIs(t, shared.Protect(mmap.Read|mmap.Write), unix.EACCES)
	require.Equal(t, "data", string(shared.Bytes()), "failed permission upgrade must retain the readable view")

	region, err := mmap.Map(reader, 0, 4, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read | mmap.Write})
	test.NoResource(t, region, err, unix.EACCES)
	test.PathError(t, err, "mmap", reader.Name())
}

func TestProtectionChangesCoverTheLastPageAndPreserveContents(t *testing.T) {
	region := test.Anonymous(t, os.Getpagesize()+1, mmap.Options{})
	require.Len(t, region.Bytes(), os.Getpagesize()+1, "NoAccess must still return the sized borrowed view")
	require.NoError(t, region.Protect(mmap.Read|mmap.Write))

	data := region.Bytes()
	data[0], data[len(data)-1] = 17, 23

	require.NoError(t, region.Protect(mmap.Read))
	require.Equal(t, [2]byte{17, 23}, [2]byte{data[0], data[len(data)-1]}, "protection preserves both endpoints")
	require.NoError(t, region.Protect(mmap.NoAccess))
	require.NoError(t, region.Protect(mmap.Write))

	data[len(data)-1] = 42

	require.NoError(t, region.Protect(mmap.Read|mmap.Write))
	require.Equal(t, byte(42), data[len(data)-1])
}

func TestUnknownProtectionLeavesTheMappingWritable(t *testing.T) {
	region := test.Anonymous(t, 1, mmap.Options{Protection: mmap.Read | mmap.Write})
	require.ErrorIs(t, region.Protect(mmap.Protection(4)), os.ErrInvalid)
	region.Bytes()[0] = 99
	require.Equal(t, byte(99), region.Bytes()[0])
}

func TestAccessPatternAdvicePreservesContents(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		advice mmap.Advice
	}{
		{name: "normal", advice: mmap.Normal},
		{name: "random", advice: mmap.Random},
		{name: "sequential", advice: mmap.Sequential},
		{name: "will need", advice: mmap.WillNeed},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			region := test.Anonymous(t, os.Getpagesize()+1, mmap.Options{Protection: mmap.Read | mmap.Write})

			copy(region.Bytes(), "keep")

			require.NoError(t, region.Advise(scenario.advice))
			require.Equal(t, "keep", string(region.Bytes()[:4]))
		})
	}
}

func TestUnknownAdvicePreservesContents(t *testing.T) {
	region := test.Anonymous(t, os.Getpagesize()+1, mmap.Options{Protection: mmap.Read | mmap.Write})
	copy(region.Bytes(), "keep")
	require.ErrorIs(t, region.Advise(mmap.Advice(255)), os.ErrInvalid)
	require.Equal(t, "keep", string(region.Bytes()[:4]))
}

func TestDontNeedDiscardsEveryPrivateAnonymousPage(t *testing.T) {
	region := test.Anonymous(t, os.Getpagesize()+1, mmap.Options{Protection: mmap.Read | mmap.Write})
	data := region.Bytes()
	data[0], data[len(data)-1] = 1, 1
	require.NoError(t, region.Advise(mmap.DontNeed))
	require.Equal(t, make([]byte, len(data)), data)
}

func TestDontNeedDiscardsPrivateChangesAndRetainsSharedContents(t *testing.T) {
	file := test.TempFile(t, int64(os.Getpagesize()))
	_, err := file.WriteAt([]byte("source"), 0)
	require.NoError(t, err)
	private := test.Mapping(t, file, 0, 6, mmap.Options{Protection: mmap.Read | mmap.Write})
	copy(private.Bytes(), "secret")
	require.NoError(t, private.Advise(mmap.DontNeed))
	require.Equal(t, "source", string(private.Bytes()), "DontNeed must discard private file changes")

	shared := test.Mapping(t, file, 0, 6, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read | mmap.Write})
	copy(shared.Bytes(), "public")
	require.NoError(t, shared.Advise(mmap.DontNeed))
	require.Equal(t, "public", string(shared.Bytes()), "DontNeed must retain shared backing contents")
}

func TestInvalidMappingArgumentsReturnNoRegion(t *testing.T) {
	file := test.TempFile(t, int64(os.Getpagesize()))
	page := int64(os.Getpagesize())
	for _, scenario := range []struct {
		name    string
		err     error
		offset  int64
		length  int
		options mmap.Options
	}{
		{name: "zero length", err: os.ErrInvalid},
		{name: "negative length", length: -1, err: os.ErrInvalid},
		{name: "negative offset", offset: -page, length: 1, err: os.ErrInvalid},
		{name: "unaligned offset", offset: 1, length: 1, err: os.ErrInvalid},
		{name: "offset overflow", offset: math.MaxInt64 - page + 1, length: int(page), err: unix.EOVERFLOW},
		{name: "unknown mode", length: 1, options: mmap.Options{Mode: 255}, err: os.ErrInvalid},
		{name: "unknown protection", length: 1, options: mmap.Options{Protection: 128}, err: os.ErrInvalid},
		{name: "execute protection", length: 1, options: mmap.Options{Protection: 4}, err: os.ErrInvalid},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			region, err := mmap.Map(file, scenario.offset, scenario.length, scenario.options)
			test.NoResource(t, region, err, scenario.err)

			if scenario.offset == 0 {
				region, err = mmap.Anonymous(scenario.length, scenario.options)
				test.NoResource(t, region, err, scenario.err)
			}
		})
	}
}

func TestInvalidMappingSourcesReturnNoRegion(t *testing.T) {
	closed := test.TempFile(t, int64(os.Getpagesize()))

	require.NoError(t, closed.Close())

	directory := test.OpenFile(t, t.TempDir())
	for _, scenario := range []struct {
		name   string
		file   *os.File
		offset int64
		err    error
	}{
		{name: "nil file", err: os.ErrInvalid},
		{name: "closed file", file: closed, err: os.ErrClosed},
		{name: "invalid offset precedes closed state", file: closed, offset: 1, err: os.ErrInvalid},
		{name: "directory is not mappable", file: directory, err: unix.ENODEV},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			region, err := mmap.Map(scenario.file, scenario.offset, 1, mmap.Options{Protection: mmap.Read})
			test.NoResource(t, region, err, scenario.err)
		})
	}
}

func TestImpossibleAnonymousAllocationsReturnENOMEMWithoutAnOwner(t *testing.T) {
	region, err := mmap.Anonymous(math.MaxInt, mmap.Options{Protection: mmap.Read | mmap.Write})
	require.Nil(t, region, "an impossible address-space request must not return an owner")
	require.ErrorIs(t, err, unix.ENOMEM)
	test.PathError(t, err, "mmap", "anonymous")
}

func TestUnavailableRegionsRejectAccess(t *testing.T) {
	closed := test.Anonymous(t, 1, mmap.Options{})

	require.NoError(t, closed.Close())

	for _, scenario := range []struct {
		name   string
		region *mmap.Region
		err    error
	}{
		{name: "zero region", region: &mmap.Region{}, err: os.ErrInvalid},
		{name: "closed region", region: closed, err: os.ErrClosed},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			region := scenario.region
			require.Nil(t, region.Bytes())

			for _, operation := range []struct {
				name string
				run  func() error
			}{
				{name: "close", run: region.Close},
				{name: "change protection", run: func() error {
					return region.Protect(mmap.Read)
				}},

				{name: "reject unknown protection", run: func() error {
					return region.Protect(255)
				}},

				{name: "advise access pattern", run: func() error {
					return region.Advise(mmap.Normal)
				}},

				{name: "reject unknown advice", run: func() error {
					return region.Advise(255)
				}},
			} {
				require.ErrorIs(t, operation.run(), scenario.err, operation.name)
			}
		})
	}
}

func TestClosingARegionSafelyRacesWithMetadataOperations(t *testing.T) {
	region := test.Anonymous(t, 1, mmap.Options{Protection: mmap.Read})
	var group sync.WaitGroup
	failures := make(chan error, 8)
	for range 8 {
		group.Go(func() {
			for range 20 {
				// Inspect only the slice header: Bytes does not pin memory.
				if length := len(region.Bytes()); length != 0 && length != 1 {
					failures <- fmt.Errorf("unexpected view length during Close: %d", length)
					return
				}

				if err := region.Protect(mmap.Read); err != nil && !errors.Is(err, os.ErrClosed) {
					failures <- err
					return
				}

				if err := region.Advise(mmap.Normal); err != nil && !errors.Is(err, os.ErrClosed) {
					failures <- err
					return
				}
			}
		})
	}

	require.NoError(t, region.Close())

	group.Wait()
	close(failures)

	for err := range failures {
		require.NoError(t, err)
	}
}

func TestMappingSafelyRacesWithClosingTheSourceFile(t *testing.T) {
	for range 20 {
		file := test.TempFile(t, int64(os.Getpagesize()))
		closed := make(chan error, 1)

		go func() {
			closed <- file.Close()
		}()

		region, err := mmap.Map(file, 0, 1, mmap.Options{Protection: mmap.Read})
		require.NoError(t, <-closed)

		if err == nil {
			require.Zero(t, region.Bytes()[0], "successful mapping must survive concurrent source closure")
			require.NoError(t, region.Close())
		} else {
			require.ErrorIs(t, err, os.ErrClosed)
		}
	}
}

func TestInvalidMemoryAccessCausesAnAddressFault(t *testing.T) {
	if argument := os.Args[len(os.Args)-1]; strings.HasPrefix(argument, "mmap-fault=") {
		checkMemoryFault(t, strings.TrimPrefix(argument, "mmap-fault="))
		return
	}

	for _, scenario := range []string{"default no access", "protected no access", "read only write", "closed view", "truncated file"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, command := test.Command(t, "TestInvalidMemoryAccessCausesAnAddressFault", "-test.v", "--", "mmap-fault="+scenario)

			output, err := command.CombinedOutput()
			require.NoError(t, ctx.Err(), "fault subprocess must finish within its bound")
			require.NoError(t, err, "fault subprocess: %s", output)
			require.Contains(t, string(output), "observed memory fault", "child must reach and verify the fault")
		})
	}
}

func checkMemoryFault(t *testing.T, scenario string) {
	t.Helper()

	// Non-Go memory faults cannot safely run in the main test process. The
	// runtime's documented recovery mode lets the child verify an address fault
	// instead of accepting any subprocess failure as evidence of enforcement.
	previous := debug.SetPanicOnFault(true)
	defer debug.SetPanicOnFault(previous)
	region := test.Anonymous(t, os.Getpagesize()+1, mmap.Options{})
	view := region.Bytes()

	switch scenario {
	case "default no access":
	case "protected no access":
		require.NoError(t, region.Protect(mmap.Read|mmap.Write))

		view[0] = 1

		require.NoError(t, region.Protect(mmap.NoAccess))
	case "read only write":
		require.NoError(t, region.Protect(mmap.Read))
	case "closed view":
		require.NoError(t, region.Protect(mmap.Read|mmap.Write))

		view[0] = 1

		require.NoError(t, region.Close())
	case "truncated file":
		file := test.TempFile(t, int64(os.Getpagesize()))
		region = test.Mapping(t, file, 0, os.Getpagesize(), mmap.Options{Protection: mmap.Read})
		view = region.Bytes()

		require.NoError(t, file.Truncate(0))
	default:
		require.FailNow(t, "unknown fault scenario", "%q", scenario)
	}

	defer func() {
		fault, ok := recover().(interface {
			error
			Addr() uintptr
		})
		require.True(t, ok, "access must cause a memory-address fault")
		require.NotZero(t, fault.Addr(), "fault must identify the mapped address")

		t.Log("observed memory fault")
	}()

	if scenario == "read only write" {
		view[len(view)-1] = 1
	} else {
		t.Logf("unexpectedly read %d", view[0])
	}

	runtime.KeepAlive(view)
}
