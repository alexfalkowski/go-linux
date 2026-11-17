//go:build linux

package test

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/alexfalkowski/go-linux/seccomp"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// ProcessGroupSyscall is the native getpgid syscall number for filter fixtures.
const ProcessGroupSyscall = unix.SYS_GETPGID

// PrctlSyscall is the native prctl syscall number for policy-denial fixtures.
const PrctlSyscall = unix.SYS_PRCTL

// SeccompSyscall is the native seccomp syscall number for policy-denial fixtures.
const SeccompSyscall = unix.SYS_SECCOMP

// ErrNotImplemented preserves ENOSYS for untraced SECCOMP_RET_TRACE assertions.
const ErrNotImplemented = unix.ENOSYS

// SeccompProcess confines irreversible filters to a bounded fixture child.
// The caller locks its goroutine and never unlocks, including on failure.
func SeccompProcess(t *testing.T) bool {
	t.Helper()
	const marker = "seccomp-isolated"
	if os.Args[len(os.Args)-1] == marker {
		return true
	}
	ctx, command := Command(t, t.Name(), "-test.v", "--", marker)
	output, err := command.CombinedOutput()
	require.NoError(t, ctx.Err(), "seccomp scenario exceeded its time bound")
	require.NoError(t, err, "isolated seccomp scenario: %s", output)
	t.Log(string(output))
	return false
}

// ProcessGroup independently invokes getpgid for a public filter's observable
// effect. Go's runtime does not depend on this operation for its own progress.
func ProcessGroup(id int) (int, error) {
	return unix.Getpgid(id)
}

// NoNewPrivileges independently observes the calling thread's irreversible bit.
func NoNewPrivileges(t *testing.T) bool {
	t.Helper()
	value, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
	require.NoError(t, err)
	return value != 0
}

// SeccompFilterCount independently observes the calling thread's filter tree.
func SeccompFilterCount(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile("/proc/thread-self/status")
	require.NoError(t, err)
	for line := range strings.SplitSeq(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, "Seccomp_filters:\t"); ok {
			count, err := strconv.Atoi(strings.TrimSpace(value))
			require.NoError(t, err)
			return count
		}
	}
	require.FailNow(t, "kernel status must expose Seccomp_filters")
	return 0
}

// RequireSeccompTermination checks a kernel SIGSYS exit, distinct from a Go
// assertion failure or a timeout in a deliberately fatal fixture.
func RequireSeccompTermination(t *testing.T, err error) {
	t.Helper()
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	status, ok := exit.Sys().(syscall.WaitStatus)
	require.True(t, ok)
	require.True(t, status.Signaled(), "fixture must terminate by signal")
	require.Equal(t, syscall.SIGSYS, status.Signal())
}

// InvokeX32 attempts the x32 form of getpid on amd64. A filter must reject the
// ABI before Linux can return either a PID or ENOSYS. Other architectures
// cannot issue x32 calls and must not invoke this helper.
func InvokeX32(t *testing.T) {
	t.Helper()
	require.Equal(t, "amd64", runtime.GOARCH)
	_, _, errno := unix.RawSyscall(unix.SYS_GETPID|0x40000000, 0, 0, 0)
	require.FailNowf(t, "x32 call returned instead of terminating", "errno=%v", errno)
}

// SeccompFilter constructs an immutable native program through the public API.
func SeccompFilter(t *testing.T, instructions []seccomp.Instruction) *seccomp.Filter {
	t.Helper()
	architecture, err := seccomp.NativeArchitecture()
	require.NoError(t, err)
	filter, err := seccomp.New(architecture, instructions)
	require.NoError(t, err)
	return filter
}

// SeccompSyscallFilter returns result only for number, allowing other syscalls
// so the Go runtime can continue and the fixture can report its assertions.
func SeccompSyscallFilter(t *testing.T, number uint32, result uint32) *seccomp.Filter {
	t.Helper()
	return SeccompFilter(t, []seccomp.Instruction{
		{Code: seccomp.LoadWord, K: seccomp.SyscallOffset},
		{Code: seccomp.JumpEqual, K: number, Jf: 1},
		{Code: seccomp.Return, K: result},
		{Code: seccomp.Return, K: uint32(seccomp.Allow)},
	})
}

// SeccompErrnoFilter constructs a syscall-specific denial, keeping native
// return-action encoding in the fixture rather than its behavioral scenarios.
func SeccompErrnoFilter(t *testing.T, number uint32, errno syscall.Errno) *seccomp.Filter {
	t.Helper()
	require.LessOrEqual(t, errno, syscall.Errno(0xffff), "errno must fit seccomp return data")
	return SeccompSyscallFilter(t, number, uint32(seccomp.Errno)|uint32(errno)) //nolint:gosec // Asserted 16-bit data above.
}

// ProcessGroupThread starts a locked thread with an optional preinstalled
// thread-local filter. It returns that thread's ID and a one-shot getpgid probe.
// The thread stays alive until the probe or test cleanup; cleanup waits for its
// destruction. Use only inside a bounded SeccompProcess child. A nil filter
// leaves the thread's inherited policy unchanged.
func ProcessGroupThread(t *testing.T, filter *seccomp.Filter) (int, func() error) {
	t.Helper()
	require.Equal(t, "seccomp-isolated", os.Args[len(os.Args)-1], "worker requires a bounded seccomp child")
	ready := make(chan ReadResult[int], 1)
	probe := make(chan struct{})
	release := make(chan struct{})
	result := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		id := LockThread()
		defer close(done)
		if filter != nil {
			if err := filter.Load(seccomp.Options{Synchronization: seccomp.CallingThread, NoNewPrivileges: true}); err != nil {
				ready <- ReadResult[int]{Err: err}
				return
			}
		}
		ready <- ReadResult[int]{Value: id}
		select {
		case <-probe:
			_, err := ProcessGroup(0)
			result <- err
		case <-release:
		}
	}()
	worker := Receive(t, ready)
	t.Cleanup(func() {
		close(release)
		Receive(t, done)
		WaitForThreadExit(t, worker.Value)
	})
	require.NoError(t, worker.Err)
	return worker.Value, func() error {
		close(probe)
		return Receive(t, result)
	}
}
