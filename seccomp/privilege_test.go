//go:build linux

package seccomp_test

import (
	"testing"

	"github.com/alexfalkowski/go-linux/capability"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/seccomp"
	"github.com/stretchr/testify/require"
)

func TestLoadSetsNoNewPrivilegesFromZero(t *testing.T) {
	test.SecurityPrivileges(t)
	if !test.SeccompProcess(t) {
		return
	}
	test.LockThread()
	require.NoError(t, capability.SetCurrent(capability.Sets{}))
	require.False(t, test.NoNewPrivileges(t), "the transition must start with the bit unset")
	filter := test.SeccompErrnoFilter(t, test.ProcessGroupSyscall, test.ErrPermissionDenied)

	require.NoError(t, filter.Load(seccomp.Options{Synchronization: seccomp.CallingThread, Privileges: seccomp.SetNoNewPrivileges}))

	require.True(t, test.NoNewPrivileges(t), "Load must set the requested bit")
	_, err := test.ProcessGroup(0)
	require.ErrorIs(t, err, test.ErrPermissionDenied, "the unprivileged caller's filter must enforce its policy")
}

func TestLoadWithoutEitherAuthorityPreservesUnrestrictedState(t *testing.T) {
	test.SecurityPrivileges(t)
	if !test.SeccompProcess(t) {
		return
	}
	test.LockThread()
	require.NoError(t, capability.SetCurrent(capability.Sets{}))
	require.False(t, test.NoNewPrivileges(t))
	filtersBefore := test.SeccompFilterCount(t)
	filter := test.SeccompErrnoFilter(t, test.ProcessGroupSyscall, test.ErrPermissionDenied)

	err := filter.Load(seccomp.Options{Synchronization: seccomp.CallingThread})

	require.ErrorIs(t, err, test.ErrPermissionDenied)
	require.False(t, test.NoNewPrivileges(t), "failed loading must not set an unrequested privilege bit")
	require.Equal(t, filtersBefore, test.SeccompFilterCount(t), "failed loading must not attach the filter")
	_, err = test.ProcessGroup(0)
	require.NoError(t, err, "failed loading must leave the operation allowed")
}

func TestCapabilityAuthorizedLoadLeavesNoNewPrivilegesUnset(t *testing.T) {
	test.SecurityPrivileges(t)
	if !test.SeccompProcess(t) {
		return
	}
	test.LockThread()
	require.False(t, test.NoNewPrivileges(t))
	filter := test.SeccompErrnoFilter(t, test.ProcessGroupSyscall, test.ErrPermissionDenied)

	require.NoError(t, filter.Load(seccomp.Options{Synchronization: seccomp.CallingThread}))

	require.False(t, test.NoNewPrivileges(t), "capability-authorized loading must preserve the unset bit")
	_, err := test.ProcessGroup(0)
	require.ErrorIs(t, err, test.ErrPermissionDenied, "capability-authorized loading must enforce the filter")
}

func TestAllThreadsPropagatesNoNewPrivilegesFromZero(t *testing.T) {
	test.SecurityPrivileges(t)
	if !test.SeccompProcess(t) {
		return
	}
	test.LockThread()
	probePrivileges := test.NoNewPrivilegesThread(t)
	_, probePolicy := test.ProcessGroupThread(t, nil)
	require.False(t, probePrivileges(), "the existing sibling must start with the bit unset")
	require.NoError(t, capability.SetCurrent(capability.Sets{}))
	require.False(t, test.NoNewPrivileges(t))
	filter := test.SeccompErrnoFilter(t, test.ProcessGroupSyscall, test.ErrPermissionDenied)

	require.NoError(t, filter.Load(seccomp.Options{Synchronization: seccomp.AllThreads, Privileges: seccomp.SetNoNewPrivileges}))

	require.True(t, test.NoNewPrivileges(t), "the loading thread must receive the bit")
	require.True(t, probePrivileges(), "TSYNC must set the bit on the existing sibling")
	require.ErrorIs(t, probePolicy(), test.ErrPermissionDenied, "TSYNC must enforce the filter on the existing sibling")
}
