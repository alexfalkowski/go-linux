//go:build linux

package mmap_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/mmap"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

var _ io.Closer = (*mmap.Region)(nil)

func TestAnonymousModes(t *testing.T) {
	for _, mode := range []mmap.Mode{mmap.Private, mmap.Shared} {
		t.Run(map[mmap.Mode]string{mmap.Private: "private", mmap.Shared: "shared"}[mode], func(t *testing.T) {
			length := os.Getpagesize() + 13
			region := anonymous(t, length, mmap.Options{Mode: mode, Protection: mmap.Read | mmap.Write})
			data := region.Bytes()
			permissions := mappingPermissions(t, region)
			if mode == mmap.Shared {
				require.Equal(t, "rw-s", permissions, "Shared must install a shared anonymous mapping")
			} else {
				require.Equal(t, "rw-p", permissions, "Private must install a private anonymous mapping")
			}
			require.Len(t, data, length)
			require.Equal(t, length, cap(data), "page rounding must not expose extra bytes")
			require.Equal(t, make([]byte, length), data, "anonymous memory must start zeroed")

			data[0], data[length-1] = 'a', 'z'
			require.Equal(t, byte('a'), region.Bytes()[0], "Bytes must borrow the same mapping")
			require.Equal(t, byte('z'), region.Bytes()[length-1], "the partial final page must be writable")
			require.Panics(t, func() { _ = data[length] }, "indexing must respect requested length")
			require.Panics(t, func() { _ = data[:length+1] }, "reslicing must respect requested capacity")

			other := anonymous(t, length, mmap.Options{Mode: mode, Protection: mmap.Read})
			require.Zero(t, other.Bytes()[0], "separate anonymous calls must create separate objects")
		})
	}
}

func TestFileSharingAndPrivateCopies(t *testing.T) {
	page := os.Getpagesize()
	file := sourceFile(t, 2*page)
	_, err := file.WriteAt([]byte("source"), int64(page))
	require.NoError(t, err)
	shared := mapped(t, file, int64(page), 6, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read | mmap.Write})
	peer := mapped(t, file, int64(page), 6, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read})
	private := mapped(t, file, int64(page), 6, mmap.Options{Protection: mmap.Read | mmap.Write})
	require.Equal(t, "source", string(shared.Bytes()), "offset selects the second page")
	require.Equal(t, "source", string(private.Bytes()))

	copy(private.Bytes(), "secret")
	require.Equal(t, "source", string(peer.Bytes()), "private writes must not reach a shared peer")
	contents := make([]byte, 6)
	_, err = file.ReadAt(contents, int64(page))
	require.NoError(t, err)
	require.Equal(t, "source", string(contents), "private writes must not reach the file")

	copy(shared.Bytes(), "public")
	require.Equal(t, "public", string(peer.Bytes()), "shared writes must reach an independent mapping")
	_, err = file.ReadAt(contents, int64(page))
	require.NoError(t, err)
	require.Equal(t, "public", string(contents), "shared writes must reach ordinary file I/O")
	require.Equal(t, "secret", string(private.Bytes()), "an already-copied private page must stay private")

	_, err = file.WriteAt([]byte("update"), int64(page))
	require.NoError(t, err)
	require.Equal(t, "update", string(peer.Bytes()), "ordinary file writes must reach a shared view")
}

func TestSourceFileAndMappingHaveIndependentLifetimes(t *testing.T) {
	file := sourceFile(t, os.Getpagesize())
	region := mapped(t, file, 0, 7, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read | mmap.Write})
	require.NoError(t, region.Close())
	_, err := file.WriteAt([]byte("message"), 0)
	require.NoError(t, err, "closing the region must leave the file open")

	region = mapped(t, file, 0, 7, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read | mmap.Write})
	require.NoError(t, file.Close())
	require.Equal(t, "message", string(region.Bytes()), "closing the file must leave the mapping valid")
	copy(region.Bytes(), "changed")
	require.Equal(t, "changed", string(region.Bytes()))
}

func TestCompleteUnmapAndConstructionCleanup(t *testing.T) {
	file := sourceFile(t, 3*os.Getpagesize())
	region := mapped(t, file, 0, 2*os.Getpagesize()+13, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read | mmap.Write})
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

	reader, err := os.Open(file.Name())
	require.NoError(t, err)
	t.Cleanup(func() { reader.Close() })
	before, err := os.ReadDir("/proc/self/fd")
	require.NoError(t, err)
	for range 20 {
		failed, err := mmap.Map(reader, 0, 1, mmap.Options{Mode: mmap.Shared, Protection: mmap.Write})
		require.Nil(t, failed)
		require.ErrorIs(t, err, unix.EACCES)
	}
	after, err := os.ReadDir("/proc/self/fd")
	require.NoError(t, err)
	require.Len(t, after, len(before), "failed mapping must not retain descriptors")
	mappings, err = os.ReadFile("/proc/self/maps")
	require.NoError(t, err)
	require.NotContains(t, string(mappings), file.Name(), "failed construction must not leave a mapping")
}

func TestReadOnlySource(t *testing.T) {
	file := sourceFile(t, os.Getpagesize())
	_, err := file.WriteAt([]byte("data"), 0)
	require.NoError(t, err)
	reader, err := os.Open(file.Name())
	require.NoError(t, err)
	t.Cleanup(func() { reader.Close() })
	shared := mapped(t, reader, 0, 4, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read})
	private := mapped(t, reader, 0, 4, mmap.Options{Protection: mmap.Read | mmap.Write})
	copy(private.Bytes(), "copy")
	require.Equal(t, "data", string(shared.Bytes()), "a read-only source still permits private writable copies")
	require.ErrorIs(t, shared.Protect(mmap.Read|mmap.Write), unix.EACCES)
	require.Equal(t, "data", string(shared.Bytes()), "failed permission upgrade must retain the readable view")

	region, err := mmap.Map(reader, 0, 4, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read | mmap.Write})
	require.Nil(t, region)
	require.ErrorIs(t, err, unix.EACCES)
	var pathErr *os.PathError
	require.ErrorAs(t, err, &pathErr)
	require.Equal(t, "mmap", pathErr.Op)
	require.Equal(t, reader.Name(), pathErr.Path)
}

func TestProtectionChanges(t *testing.T) {
	region := anonymous(t, os.Getpagesize()+1, mmap.Options{})
	require.Len(t, region.Bytes(), os.Getpagesize()+1, "NoAccess must still return the sized borrowed view")
	require.NoError(t, region.Protect(mmap.Read|mmap.Write))
	data := region.Bytes()
	data[0], data[len(data)-1] = 17, 23
	require.NoError(t, region.Protect(mmap.Read))
	require.Equal(t, byte(17), data[0], "protection changes must preserve contents")
	require.Equal(t, byte(23), data[len(data)-1], "protection applies through the partial final page")
	require.NoError(t, region.Protect(mmap.NoAccess))
	require.NoError(t, region.Protect(mmap.Write))
	data[len(data)-1] = 42
	require.NoError(t, region.Protect(mmap.Read|mmap.Write))
	require.Equal(t, byte(42), data[len(data)-1])

	require.ErrorIs(t, region.Protect(mmap.Protection(4)), os.ErrInvalid, "executable protection is outside the API")
	data[0] = 99
	require.Equal(t, byte(99), data[0], "invalid protection must not alter writable permissions")
}

func TestAdvice(t *testing.T) {
	region := anonymous(t, os.Getpagesize()+1, mmap.Options{Protection: mmap.Read | mmap.Write})
	for _, test := range []struct {
		name   string
		advice mmap.Advice
	}{
		{name: "normal", advice: mmap.Normal},
		{name: "random", advice: mmap.Random},
		{name: "sequential", advice: mmap.Sequential},
		{name: "will need", advice: mmap.WillNeed},
	} {
		t.Run(test.name, func(t *testing.T) {
			copy(region.Bytes(), "keep")
			require.NoError(t, region.Advise(test.advice))
			require.Equal(t, "keep", string(region.Bytes()[:4]), "access-pattern advice must preserve contents")
		})
	}

	region.Bytes()[len(region.Bytes())-1] = 1
	require.ErrorIs(t, region.Advise(mmap.Advice(255)), os.ErrInvalid)
	require.Equal(t, "keep", string(region.Bytes()[:4]), "invalid advice must not discard contents")
	require.NoError(t, region.Advise(mmap.DontNeed))
	require.Equal(t, make([]byte, len(region.Bytes())), region.Bytes(), "DontNeed must discard private anonymous pages, including the last page")
}

func TestDontNeedFileMappings(t *testing.T) {
	file := sourceFile(t, os.Getpagesize())
	_, err := file.WriteAt([]byte("source"), 0)
	require.NoError(t, err)
	private := mapped(t, file, 0, 6, mmap.Options{Protection: mmap.Read | mmap.Write})
	copy(private.Bytes(), "secret")
	require.NoError(t, private.Advise(mmap.DontNeed))
	require.Equal(t, "source", string(private.Bytes()), "DontNeed must discard private file changes")

	shared := mapped(t, file, 0, 6, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read | mmap.Write})
	copy(shared.Bytes(), "public")
	require.NoError(t, shared.Advise(mmap.DontNeed))
	require.Equal(t, "public", string(shared.Bytes()), "DontNeed must retain shared backing contents")
}

func TestInvalidMappingArguments(t *testing.T) {
	file := sourceFile(t, os.Getpagesize())
	page := int64(os.Getpagesize())
	for _, test := range []struct {
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
		t.Run(test.name, func(t *testing.T) {
			region, err := mmap.Map(file, test.offset, test.length, test.options)
			require.Nil(t, region)
			require.ErrorIs(t, err, test.err)
			if test.offset == 0 {
				region, err = mmap.Anonymous(test.length, test.options)
				require.Nil(t, region)
				require.ErrorIs(t, err, test.err)
			}
		})
	}
}

func TestSourceErrors(t *testing.T) {
	region, err := mmap.Map(nil, 0, 1, mmap.Options{})
	require.Nil(t, region)
	require.ErrorIs(t, err, os.ErrInvalid)
	file := sourceFile(t, os.Getpagesize())
	require.NoError(t, file.Close())
	region, err = mmap.Map(file, 0, 1, mmap.Options{})
	require.Nil(t, region)
	require.ErrorIs(t, err, os.ErrClosed)
	region, err = mmap.Map(file, 1, 1, mmap.Options{})
	require.Nil(t, region)
	require.ErrorIs(t, err, os.ErrInvalid, "argument validation precedes source state")

	directory, err := os.Open(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { directory.Close() })
	region, err = mmap.Map(directory, 0, 1, mmap.Options{Protection: mmap.Read})
	require.Nil(t, region)
	require.ErrorIs(t, err, unix.ENODEV)
}

func TestAnonymousAllocationError(t *testing.T) {
	region, err := mmap.Anonymous(math.MaxInt, mmap.Options{Protection: mmap.Read | mmap.Write})
	require.Nil(t, region, "an impossible address-space request must not return an owner")
	require.ErrorIs(t, err, unix.ENOMEM)
	var pathErr *os.PathError
	require.ErrorAs(t, err, &pathErr)
	require.Equal(t, "mmap", pathErr.Op)
	require.Equal(t, "anonymous", pathErr.Path)
}

func TestZeroAndClosedRegion(t *testing.T) {
	var zero mmap.Region
	require.Nil(t, zero.Bytes())
	require.ErrorIs(t, zero.Protect(mmap.Read), os.ErrInvalid)
	require.ErrorIs(t, zero.Advise(mmap.Normal), os.ErrInvalid)
	require.ErrorIs(t, zero.Close(), os.ErrInvalid)

	region := anonymous(t, 1, mmap.Options{})
	require.NoError(t, region.Close())
	require.Nil(t, region.Bytes())
	require.ErrorIs(t, region.Close(), os.ErrClosed)
	require.ErrorIs(t, region.Protect(mmap.Read), os.ErrClosed)
	require.ErrorIs(t, region.Protect(255), os.ErrClosed)
	require.ErrorIs(t, region.Advise(mmap.Normal), os.ErrClosed)
	require.ErrorIs(t, region.Advise(255), os.ErrClosed)
}

func TestConcurrentMethodsAndClose(t *testing.T) {
	region := anonymous(t, 1, mmap.Options{Protection: mmap.Read})
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

func TestConcurrentSourceClose(t *testing.T) {
	for range 20 {
		file := sourceFile(t, os.Getpagesize())
		closed := make(chan error, 1)
		go func() { closed <- file.Close() }()
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

func TestMemoryFaults(t *testing.T) {
	if argument := os.Args[len(os.Args)-1]; strings.HasPrefix(argument, "mmap-fault=") {
		checkMemoryFault(t, strings.TrimPrefix(argument, "mmap-fault="))
		return
	}

	executable, err := os.Executable()
	require.NoError(t, err)
	for _, scenario := range []string{"default no access", "protected no access", "read only write", "closed view", "truncated file"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			//nolint:gosec // Re-exec only this test binary with fixed test-owned scenario arguments; no shell.
			command := exec.CommandContext(ctx, executable, "-test.run=^TestMemoryFaults$", "-test.v", "--", "mmap-fault="+scenario)
			output, err := command.CombinedOutput()
			require.NoError(t, ctx.Err(), "fault subprocess must finish within its bound")
			require.NoError(t, err, "fault subprocess: %s", output)
			require.Contains(t, string(output), "observed memory fault", "child must reach and verify the fault")
		})
	}
}

func anonymous(t *testing.T, length int, options mmap.Options) *mmap.Region {
	t.Helper()
	region, err := mmap.Anonymous(length, options)
	require.NoError(t, err)
	cleanupRegion(t, region)
	return region
}

func mapped(t *testing.T, file *os.File, offset int64, length int, options mmap.Options) *mmap.Region {
	t.Helper()
	region, err := mmap.Map(file, offset, length, options)
	require.NoError(t, err)
	cleanupRegion(t, region)
	return region
}

func cleanupRegion(t *testing.T, region *mmap.Region) {
	t.Helper()
	t.Cleanup(func() {
		if err := region.Close(); !errors.Is(err, os.ErrClosed) {
			require.NoError(t, err)
		}
	})
}

func sourceFile(t *testing.T, length int) *os.File {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "mapping-")
	require.NoError(t, err)
	t.Cleanup(func() { file.Close() })
	require.NoError(t, file.Truncate(int64(length)))
	return file
}

func mappingPermissions(t *testing.T, region *mmap.Region) string {
	t.Helper()
	mappings, err := os.ReadFile("/proc/self/maps")
	require.NoError(t, err)
	address := uint64(reflect.ValueOf(region.Bytes()).Pointer())
	for line := range strings.SplitSeq(string(mappings), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		start, end, found := strings.Cut(fields[0], "-")
		require.True(t, found, "procfs mapping must supply an address range")
		low, err := strconv.ParseUint(start, 16, 64)
		require.NoError(t, err)
		high, err := strconv.ParseUint(end, 16, 64)
		require.NoError(t, err)
		if address >= low && address < high {
			return fields[1]
		}
	}

	require.FailNow(t, "live region absent from procfs mappings")
	return ""
}

func checkMemoryFault(t *testing.T, scenario string) {
	t.Helper()
	// Non-Go memory faults cannot safely run in the main test process. The
	// runtime's documented recovery mode lets the child verify an address fault
	// instead of accepting any subprocess failure as evidence of enforcement.
	previous := debug.SetPanicOnFault(true)
	defer debug.SetPanicOnFault(previous)
	region := anonymous(t, os.Getpagesize()+1, mmap.Options{})
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
		file := sourceFile(t, os.Getpagesize())
		region = mapped(t, file, 0, os.Getpagesize(), mmap.Options{Protection: mmap.Read})
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
