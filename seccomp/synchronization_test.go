//go:build linux

package seccomp_test

import (
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/seccomp"
	"github.com/stretchr/testify/require"
)

func TestCallingThreadLeavesExistingThreadUnrestricted(t *testing.T) {
	if !test.SeccompProcess(t) {
		return
	}
	test.LockThread()
	_, queryExistingThread := test.ProcessGroupThread(t, nil)
	filter := test.SeccompErrnoFilter(t, test.ProcessGroupSyscall, test.ErrPermissionDenied)

	err := filter.Load(seccomp.Options{
		Synchronization: seccomp.CallingThread,
		NoNewPrivileges: true,
	})
	require.NoError(t, err)

	_, err = test.ProcessGroup(0)
	require.ErrorIs(t, err, test.ErrPermissionDenied, "loading thread must be restricted")
	require.NoError(t, queryExistingThread(), "existing thread must keep its earlier policy")
}

func TestAllThreadsRestrictsExistingThread(t *testing.T) {
	if !test.SeccompProcess(t) {
		return
	}
	test.LockThread()
	_, queryExistingThread := test.ProcessGroupThread(t, nil)
	filter := test.SeccompErrnoFilter(t, test.ProcessGroupSyscall, test.ErrPermissionDenied)

	err := filter.Load(seccomp.Options{
		Synchronization: seccomp.AllThreads,
		NoNewPrivileges: true,
	})
	require.NoError(t, err)

	_, err = test.ProcessGroup(0)
	require.ErrorIs(t, err, test.ErrPermissionDenied, "loading thread must be restricted")
	require.ErrorIs(t, queryExistingThread(), test.ErrPermissionDenied, "existing thread must receive TSYNC policy")
}

func TestSynchronizationConflictDoesNotAttachFilter(t *testing.T) {
	if !test.SeccompProcess(t) {
		return
	}
	test.LockThread()
	independent := test.SeccompFilter(t, []seccomp.Instruction{{Code: seccomp.Return, K: uint32(seccomp.Allow)}})
	refusingThread, _ := test.ProcessGroupThread(t, independent)
	filtersBefore := test.SeccompFilterCount(t)
	filter := test.SeccompErrnoFilter(t, test.ProcessGroupSyscall, test.ErrPermissionDenied)

	err := filter.Load(seccomp.Options{
		Synchronization: seccomp.AllThreads,
		NoNewPrivileges: true,
	})

	var conflict *seccomp.SynchronizationError
	require.ErrorAs(t, err, &conflict)
	require.Equal(t, refusingThread, conflict.ThreadID)
	require.Contains(t, conflict.Error(), "incompatible filter tree")
	require.Equal(t, filtersBefore, test.SeccompFilterCount(t), "failed TSYNC must not attach on the caller")
	require.True(t, test.NoNewPrivileges(t), "earlier no-new-privileges persists on failure")

	_, err = test.ProcessGroup(0)
	require.NoError(t, err, "failed TSYNC must not fall back to a local restriction")
}
