//go:build linux

package test

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

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
		skipMemoryLock(t, fmt.Sprintf("fixture requires RLIMIT_MEMLOCK of at least %d bytes, got %d", size, limit.Cur))
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
		skipMemoryLock(t, "effective CAP_IPC_LOCK can bypass RLIMIT_MEMLOCK")
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

func skipMemoryLock(t *testing.T, reason string) {
	t.Helper()
	options, err := testenv.FromEnvironment()
	require.NoError(t, err)
	require.False(t, options.Required(), reason)
	t.Skip(reason)
}
