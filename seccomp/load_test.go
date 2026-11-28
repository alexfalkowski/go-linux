//go:build linux

package seccomp_test

import (
	"os"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/seccomp"
	"github.com/stretchr/testify/require"
)

func TestErrnoDeniesOnlySelectedSyscall(t *testing.T) {
	if !test.SeccompProcess(t) {
		return
	}
	test.LockThread()
	group, err := test.ProcessGroup(0)
	require.NoError(t, err)
	require.Positive(t, group)
	filter := test.SeccompErrnoFilter(t, test.ProcessGroupSyscall, test.ErrPermissionDenied)
	filtersBefore := test.SeccompFilterCount(t)

	require.NoError(t, filter.Load(seccomp.Options{
		Synchronization: seccomp.CallingThread,
		Privileges:      seccomp.SetNoNewPrivileges,
	}))

	group, err = test.ProcessGroup(0)
	require.ErrorIs(t, err, test.ErrPermissionDenied)
	require.Equal(t, -1, group)
	require.Equal(t, filtersBefore+1, test.SeccompFilterCount(t))
	require.True(t, test.NoNewPrivileges(t))
}

func TestArgumentFilterDistinguishesEquivalentProcessGroupQueries(t *testing.T) {
	if !test.SeccompProcess(t) {
		return
	}
	test.LockThread()
	filter := test.SeccompFilter(t, []seccomp.Instruction{
		{Code: seccomp.LoadWord, K: seccomp.SyscallOffset},
		{Code: seccomp.JumpEqual, K: test.ProcessGroupSyscall, Jf: 3},
		{Code: seccomp.LoadWord, K: seccomp.ArgumentsOffset},
		{Code: seccomp.JumpEqual, K: 0, Jf: 1},
		{Code: seccomp.Return, K: uint32(seccomp.Errno) | uint32(test.ErrPermissionDenied)},
		{Code: seccomp.Return, K: uint32(seccomp.Allow)},
	})

	require.NoError(t, filter.Load(seccomp.Options{
		Synchronization: seccomp.CallingThread,
		Privileges:      seccomp.SetNoNewPrivileges,
	}))

	_, err := test.ProcessGroup(0)
	require.ErrorIs(t, err, test.ErrPermissionDenied, "zero PID selects the denial branch")
	group, err := test.ProcessGroup(os.Getpid())
	require.NoError(t, err, "explicit current PID must remain allowed")
	require.Positive(t, group)
}

func TestStackedAllowCannotRelaxDenial(t *testing.T) {
	if !test.SeccompProcess(t) {
		return
	}
	test.LockThread()
	deny := test.SeccompErrnoFilter(t, test.ProcessGroupSyscall, test.ErrPermissionDenied)
	allow := test.SeccompFilter(t, []seccomp.Instruction{{Code: seccomp.Return, K: uint32(seccomp.Allow)}})
	options := seccomp.Options{
		Synchronization: seccomp.CallingThread,
		Privileges:      seccomp.SetNoNewPrivileges,
	}
	require.NoError(t, deny.Load(options))

	require.NoError(t, allow.Load(options))

	_, err := test.ProcessGroup(0)
	require.ErrorIs(t, err, test.ErrPermissionDenied)
}

func TestLoadPreservesKernelDenialsAndPriorNoNewPrivileges(t *testing.T) {
	if !test.SeccompProcess(t) {
		return
	}
	test.LockThread()
	blocker := test.SeccompErrnoFilter(t, test.SeccompSyscall, test.ErrPermissionDenied)
	require.NoError(t, blocker.Load(seccomp.Options{
		Synchronization: seccomp.CallingThread,
		Privileges:      seccomp.SetNoNewPrivileges,
	}))
	filtersBefore := test.SeccompFilterCount(t)
	filter := test.SeccompFilter(t, []seccomp.Instruction{{Code: seccomp.Return, K: uint32(seccomp.Allow)}})

	err := filter.Load(seccomp.Options{Synchronization: seccomp.CallingThread})

	require.ErrorIs(t, err, test.ErrPermissionDenied)
	var operation *os.SyscallError
	require.ErrorAs(t, err, &operation)
	require.Equal(t, "seccomp(SECCOMP_SET_MODE_FILTER)", operation.Syscall)
	require.Equal(t, filtersBefore, test.SeccompFilterCount(t))
	require.True(t, test.NoNewPrivileges(t), "false options must not clear inherited no-new-privileges")
}

func TestActionAvailabilityPreservesPolicyDenial(t *testing.T) {
	if !test.SeccompProcess(t) {
		return
	}
	test.LockThread()
	blocker := test.SeccompErrnoFilter(t, test.SeccompSyscall, test.ErrPermissionDenied)
	require.NoError(t, blocker.Load(seccomp.Options{
		Synchronization: seccomp.CallingThread,
		Privileges:      seccomp.SetNoNewPrivileges,
	}))

	available, err := seccomp.ActionAvailable(seccomp.Allow)

	require.ErrorIs(t, err, test.ErrPermissionDenied)
	require.False(t, available)
}

func TestLoadUsesExistingNoNewPrivilegesWhenPrctlDenied(t *testing.T) {
	if !test.SeccompProcess(t) {
		return
	}
	test.LockThread()
	blocker := test.SeccompErrnoFilter(t, test.PrctlSyscall, test.ErrPermissionDenied)
	require.NoError(t, blocker.Load(seccomp.Options{
		Synchronization: seccomp.CallingThread,
		Privileges:      seccomp.SetNoNewPrivileges,
	}))
	filter := test.SeccompErrnoFilter(t, test.ProcessGroupSyscall, test.ErrOperationNotPermitted)

	require.NoError(t, filter.Load(seccomp.Options{Synchronization: seccomp.CallingThread}))

	_, err := test.ProcessGroup(0)
	require.ErrorIs(t, err, test.ErrOperationNotPermitted, "loading with existing no-new-privileges must not require another prctl")
}

func TestNoNewPrivilegesFailurePreventsLoading(t *testing.T) {
	if !test.SeccompProcess(t) {
		return
	}
	test.LockThread()
	blocker := test.SeccompErrnoFilter(t, test.PrctlSyscall, test.ErrPermissionDenied)
	require.NoError(t, blocker.Load(seccomp.Options{
		Synchronization: seccomp.CallingThread,
		Privileges:      seccomp.SetNoNewPrivileges,
	}))
	filtersBefore := test.SeccompFilterCount(t)
	filter := test.SeccompFilter(t, []seccomp.Instruction{{Code: seccomp.Return, K: uint32(seccomp.Allow)}})

	err := filter.Load(seccomp.Options{
		Synchronization: seccomp.CallingThread,
		Privileges:      seccomp.SetNoNewPrivileges,
	})

	require.ErrorIs(t, err, test.ErrPermissionDenied)
	var operation *os.SyscallError
	require.ErrorAs(t, err, &operation)
	require.Equal(t, "prctl(PR_SET_NO_NEW_PRIVS)", operation.Syscall)
	require.Equal(t, filtersBefore, test.SeccompFilterCount(t))
}
