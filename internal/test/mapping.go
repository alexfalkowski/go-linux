//go:build linux

package test

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unsafe"

	"github.com/alexfalkowski/go-linux/capability"
	"github.com/alexfalkowski/go-linux/internal/testenv"
	"github.com/alexfalkowski/go-linux/memfd"
	"github.com/alexfalkowski/go-linux/mmap"
	"github.com/alexfalkowski/go-linux/seccomp"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// MappingPermissions finds the live mapping containing view without reading its
// contents. The caller must keep the view mapped throughout the inspection.
func MappingPermissions(t *testing.T, view []byte) string {
	t.Helper()

	mappings, err := os.ReadFile("/proc/self/maps")
	require.NoError(t, err)

	address := uint64(reflect.ValueOf(view).Pointer())
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

// WritableMapping creates a shared writable page with independent cleanup.
// Kernel errors are returned for behavior assertions at the call site.
func WritableMapping(t *testing.T, file *memfd.File) (*mmap.Region, error) {
	t.Helper()

	region, err := file.Map(0, os.Getpagesize(), mmap.Options{Mode: mmap.Shared, Protection: mmap.Read | mmap.Write})
	if err != nil {
		return region, err
	}
	Cleanup(t, region)
	return region, nil
}

// RejectSynchronousMappingWriteback makes Linux return ECANCELED for MS_SYNC
// on exactly view. Call on a locked thread in a bounded SeccompProcess child.
func RejectSynchronousMappingWriteback(t *testing.T, view []byte) {
	t.Helper()

	const (
		addressOffset = seccomp.ArgumentsOffset
		lengthOffset  = seccomp.ArgumentsOffset + 8
		flagsOffset   = seccomp.ArgumentsOffset + 16
		wordSize      = 4
	)
	address := uint64(reflect.ValueOf(view).Pointer())
	length := uint64(len(view))
	addressLow := uint32(address & 0xffffffff)
	addressHigh := uint32(address >> 32)
	lengthLow := uint32(length & 0xffffffff)
	lengthHigh := uint32(length >> 32)

	// Each mismatch jumps to Allow. The kernel takes flags as a 32-bit int.
	filter := SeccompFilter(t, []seccomp.Instruction{
		{Code: seccomp.LoadWord, K: seccomp.SyscallOffset},
		{Code: seccomp.JumpEqual, K: unix.SYS_MSYNC, Jf: 11},
		{Code: seccomp.LoadWord, K: addressOffset},
		{Code: seccomp.JumpEqual, K: addressLow, Jf: 9},
		{Code: seccomp.LoadWord, K: addressOffset + wordSize},
		{Code: seccomp.JumpEqual, K: addressHigh, Jf: 7},
		{Code: seccomp.LoadWord, K: lengthOffset},
		{Code: seccomp.JumpEqual, K: lengthLow, Jf: 5},
		{Code: seccomp.LoadWord, K: lengthOffset + wordSize},
		{Code: seccomp.JumpEqual, K: lengthHigh, Jf: 3},
		{Code: seccomp.LoadWord, K: flagsOffset},
		{Code: seccomp.JumpEqual, K: unix.MS_SYNC, Jf: 1},
		{Code: seccomp.Return, K: uint32(seccomp.Errno) | uint32(ErrCanceled)},
		{Code: seccomp.Return, K: uint32(seccomp.Allow)},
	})
	require.NoError(t, filter.Load(seccomp.Options{
		Synchronization: seccomp.CallingThread,
		Privileges:      seccomp.SetNoNewPrivileges,
	}))
}

// LockMappingPage locks one page for a native observation. View must start on
// a page boundary and remain mapped until cleanup unlocks it.
func LockMappingPage(t *testing.T, view []byte) {
	t.Helper()
	RequireMemoryLock(t, os.Getpagesize())

	require.NoError(t, unix.Mlock(view))
	t.Cleanup(func() {
		require.NoError(t, unix.Munlock(view))
	})
}

// RequireMemoryLock checks the soft limit in bytes for a bounded lock fixture.
func RequireMemoryLock(t *testing.T, size int) {
	t.Helper()
	if size < 0 {
		require.FailNow(t, "memory lock fixture size must be nonnegative")
		return
	}

	var limit unix.Rlimit
	require.NoError(t, unix.Getrlimit(unix.RLIMIT_MEMLOCK, &limit))
	if limit.Cur < uint64(size) {
		unavailableMemoryFixture(t, fmt.Sprintf("fixture requires RLIMIT_MEMLOCK of at least %d bytes, got %d", size, limit.Cur))
	}
}

// MemoryLockLimitProcess lowers RLIMIT_MEMLOCK only in a bounded child. The
// caller runs its limit assertions when this returns true in the child.
func MemoryLockLimitProcess(t *testing.T, size int) bool {
	t.Helper()
	if size < 0 {
		require.FailNow(t, "memory lock fixture size must be nonnegative")
		return false
	}

	const marker = "memlock-limited"
	if os.Args[len(os.Args)-1] == marker {
		var limit unix.Rlimit
		require.NoError(t, unix.Getrlimit(unix.RLIMIT_MEMLOCK, &limit))
		require.NoError(t, unix.Setrlimit(unix.RLIMIT_MEMLOCK, &unix.Rlimit{Cur: uint64(size), Max: limit.Max}))
		return true
	}

	RequireMemoryLock(t, size)
	sets, err := capability.Current()
	require.NoError(t, err)
	if sets.Effective&capability.IPCLock != 0 {
		unavailableMemoryFixture(t, "effective CAP_IPC_LOCK can bypass RLIMIT_MEMLOCK")
	}

	ctx, command := Command(t, t.Name(), "-test.v", "--", marker)
	output, err := command.CombinedOutput()
	require.NoError(t, ctx.Err(), "memory lock scenario exceeded its time bound")
	require.NoError(t, err, "isolated memory lock scenario: %s", output)
	t.Log(string(output))
	return false
}

// LockedMemory observes the process's kernel-accounted locked bytes.
func LockedMemory(t *testing.T) int {
	t.Helper()

	status, err := os.ReadFile("/proc/self/status")
	require.NoError(t, err)

	for line := range strings.SplitSeq(string(status), "\n") {
		value, found := strings.CutPrefix(line, "VmLck:")
		if !found {
			continue
		}

		fields := strings.Fields(value)
		require.Len(t, fields, 2)
		require.Equal(t, "kB", fields[1])
		size, err := strconv.Atoi(fields[0])
		require.NoError(t, err)
		return size * 1024
	}

	require.FailNow(t, "procfs status did not report VmLck")
	return 0
}

func unavailableMemoryFixture(t *testing.T, reason string) {
	t.Helper()
	options, err := testenv.FromEnvironment()
	require.NoError(t, err)
	require.False(t, options.Required(), reason)
	t.Skip(reason)
}

// MappingAddress obtains the base address of a live mapping for ABI checks.
func MappingAddress(view []byte) uintptr {
	return reflect.ValueOf(view).Pointer()
}

// BlockMappingGrowth keeps the next virtual page occupied without replacing
// an existing mapping. View must begin at a page boundary and fill one page.
func BlockMappingGrowth(t *testing.T, view []byte) uintptr {
	t.Helper()

	page := unsafe.Add(unsafe.Pointer(unsafe.SliceData(view)), len(view))
	flags := unix.MAP_PRIVATE | unix.MAP_ANONYMOUS | unix.MAP_FIXED_NOREPLACE
	length := uintptr(len(view))
	blocker, err := unix.MmapPtr(-1, 0, page, length, unix.PROT_NONE, flags)
	if err != nil {
		require.ErrorIs(t, err, unix.EEXIST, "reserve the page immediately after the mapping")
		return MappingAddress(view)
	}
	t.Cleanup(func() {
		require.NoError(t, unix.MunmapPtr(blocker, length))
	})
	require.Equal(t, page, blocker)
	return MappingAddress(view)
}

// RequireUnmappedPage independently checks the former address of a page with
// mincore. Capture the address while the view is live and call in a bounded child.
func RequireUnmappedPage(t *testing.T, address uintptr) {
	t.Helper()

	var residency byte
	_, _, errno := unix.Syscall(unix.SYS_MINCORE, address, uintptr(os.Getpagesize()), uintptr(unsafe.Pointer(&residency)))
	require.Equal(t, unix.ENOMEM, errno, "the former page must be unmapped")
}

// MappingStatus reads one /proc/self/smaps field for the live region holding
// view. The mapping must stay live throughout the inspection.
func MappingStatus(t *testing.T, view []byte, field string) string {
	t.Helper()

	data := memoryFixtureFile(t, "/proc/self/smaps", "per-mapping procfs statistics")

	address := uint64(MappingAddress(view))
	inMapping := false
	for line := range strings.SplitSeq(data, "\n") {
		label, _, _ := strings.Cut(line, " ")
		if start, end, ok := strings.Cut(label, "-"); ok {
			low, err := strconv.ParseUint(start, 16, 64)
			require.NoError(t, err)

			high, err := strconv.ParseUint(end, 16, 64)
			require.NoError(t, err)

			inMapping = address >= low && address < high
		} else if inMapping {
			if value, ok := strings.CutPrefix(line, field+":"); ok {
				return strings.TrimSpace(value)
			}
		}
	}

	require.FailNow(t, "live mapping field absent from smaps", "%s", field)
	return ""
}

// RequireKSM checks that Linux exposes kernel same-page merging.
func RequireKSM(t *testing.T) {
	t.Helper()
	memoryFixtureFile(t, "/sys/kernel/mm/ksm/run", "kernel same-page merging")
}

// TransparentHugePageSize returns the kernel's PMD-sized huge page in bytes.
func TransparentHugePageSize(t *testing.T) int {
	t.Helper()
	setting := memoryFixtureFile(t, "/sys/kernel/mm/transparent_hugepage/hpage_pmd_size", "transparent huge pages")
	size, err := strconv.Atoi(strings.TrimSpace(setting))
	require.NoError(t, err)
	require.Greater(t, size, os.Getpagesize())
	return size
}

// RequireBoundedHugePage limits the transparent huge-page fixture to 64 MiB.
func RequireBoundedHugePage(t *testing.T, size int) {
	t.Helper()
	if size > 32<<20 {
		unavailableMemoryFixture(t, "collapse fixture requires a PMD page size of at most 32 MiB")
	}
}

func memoryFixtureFile(t *testing.T, path, facility string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		unavailableMemoryFixture(t, facility+" is unavailable")
	}
	require.NoError(t, err)
	return string(data)
}
