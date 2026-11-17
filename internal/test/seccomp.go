//go:build linux

package test

import (
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// RefusePollRegistration rejects EPOLL_CTL_ADD on the current thread of a
// bounded fixture child. Initialize Go's poller first so the restriction tests
// resource registration rather than process-wide poller startup.
func RefusePollRegistration(t *testing.T) {
	t.Helper()
	argument := os.Args[len(os.Args)-1]
	isolated := argument == "poll-registration-denied" || strings.HasPrefix(argument, "fanotify-isolated=")
	require.True(t, isolated, "poll restriction requires a bounded poll-registration or fanotify namespace child")
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	require.NoError(t, reader.SetReadDeadline(time.Time{}))
	require.NoError(t, reader.Close())
	require.NoError(t, writer.Close())

	restrictThread(t, []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0}, // seccomp_data.nr
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.SYS_EPOLL_CTL, Jf: 3},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 24}, // epoll_ctl's operation argument
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.EPOLL_CTL_ADD, Jf: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
	})
}

// RefuseCapabilityCalls returns EACCES for capget, capset, and prctl on a
// bounded capability fixture's thread, establishing real policy-denial errors
// without production hooks. The caller must leave the thread locked on exit.
func RefuseCapabilityCalls(t *testing.T) {
	t.Helper()
	require.Equal(t, "capability-isolated", os.Args[len(os.Args)-1], "capability restriction requires a dedicated child")
	restrictThread(t, []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.SYS_CAPGET, Jt: 3},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.SYS_CAPSET, Jt: 2},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.SYS_PRCTL, Jt: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EACCES)},
	})
}

// restrictThread is shared by the concrete child-only fault fixtures. Never
// unlock the thread: Go retires it when the test goroutine exits, so restrictions
// cannot leak into unrelated goroutines. The kernel copies the filter on load.
func restrictThread(t *testing.T, filters []unix.SockFilter) {
	t.Helper()
	var arch uint32
	switch runtime.GOARCH {
	case "amd64":
		arch = unix.AUDIT_ARCH_X86_64
	case "arm64":
		arch = unix.AUDIT_ARCH_AARCH64
	}
	require.NotZero(t, arch, "syscall restrictions support the required amd64/arm64 runners only")
	filters = append([]unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 4}, // seccomp_data.arch
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: arch, Jt: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_KILL_PROCESS},
	}, filters...)
	require.Less(t, len(filters), 1<<16, "filter length must fit sock_fprog.len")
	program := unix.SockFprog{Len: uint16(len(filters)), Filter: &filters[0]} //nolint:gosec // Length checked above.
	// Prctl accepts uintptr, so pin both objects across its wrapper call.
	var pins runtime.Pinner
	pins.Pin(&program)
	pins.Pin(&filters[0])
	defer pins.Unpin()

	LockThread()
	require.NoError(t, unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0))
	require.NoError(t, unix.Prctl(unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER, uintptr(unsafe.Pointer(&program)), 0, 0))
	runtime.KeepAlive(filters)
}
