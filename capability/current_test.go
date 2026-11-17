//go:build linux

package capability_test

import (
	"os"
	"testing"

	"github.com/alexfalkowski/go-linux/capability"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

func TestCurrentMatchesKernelThreadStatus(t *testing.T) {
	test.LockThread()

	sets, err := capability.Current()
	require.NoError(t, err)

	status := test.CapabilityStatus(t)
	require.Equal(t, capability.Sets{
		Permitted: status["CapPrm"], Effective: status["CapEff"], Inheritable: status["CapInh"],
	}, sets)
}

func TestSetCurrentRejectsUnknownBits(t *testing.T) {
	if !test.CapabilityProcess(t) {
		return
	}
	test.LockThread()
	before := test.CapabilityStatus(t)

	for _, sets := range []capability.Sets{
		{Permitted: 1 << 41},
		{Effective: 1 << 41},
		{Inheritable: 1 << 41},
		{Permitted: 1 << 63},
		{Effective: 1 << 63},
		{Inheritable: 1 << 63},
		{Permitted: capability.Chown | 1<<41},
	} {
		err := capability.SetCurrent(sets)
		require.ErrorIs(t, err, os.ErrInvalid, "sets=%+v", sets)
		require.Equal(t, before, test.CapabilityStatus(t), "invalid input must not change credentials")
	}
}

func TestSetCurrentClearsAllThreeSets(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	test.LockThread()
	initial := capability.Sets{
		Permitted: capability.Chown | capability.BPF, Effective: capability.Chown | capability.BPF, Inheritable: capability.Chown | capability.BPF,
	}
	require.NoError(t, capability.SetCurrent(initial))
	before := test.CapabilityStatus(t)
	for _, name := range []string{"CapPrm", "CapEff", "CapInh"} {
		require.NotZero(t, before[name], "clearing %s requires a nonempty initial mask", name)
	}

	require.NoError(t, capability.SetCurrent(capability.Sets{}))

	sets, err := capability.Current()
	require.NoError(t, err)
	require.Empty(t, sets)
	status := test.CapabilityStatus(t)
	require.Zero(t, status["CapPrm"])
	require.Zero(t, status["CapEff"])
	require.Zero(t, status["CapInh"])
}

func TestEffectiveMustBePermitted(t *testing.T) {
	if !test.CapabilityProcess(t) {
		return
	}
	test.LockThread()
	require.NoError(t, capability.SetCurrent(capability.Sets{}))

	err := capability.SetCurrent(capability.Sets{Effective: capability.Chown})

	require.ErrorIs(t, err, test.ErrOperationNotPermitted)
	var operation *os.SyscallError
	require.ErrorAs(t, err, &operation)
	require.Equal(t, "capset", operation.Syscall)
	sets, err := capability.Current()
	require.NoError(t, err)
	require.Empty(t, sets, "refused replacement must leave all three sets unchanged")
}

func TestInheritableAdditionRequiresAuthority(t *testing.T) {
	if !test.CapabilityProcess(t) {
		return
	}
	test.LockThread()
	require.NoError(t, capability.SetCurrent(capability.Sets{}))

	err := capability.SetCurrent(capability.Sets{Inheritable: capability.Chown})

	require.ErrorIs(t, err, test.ErrOperationNotPermitted)
	sets, err := capability.Current()
	require.NoError(t, err)
	require.Empty(t, sets)
}

func TestSetCurrentRoundTripsBothABIWords(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	test.LockThread()
	want := capability.Sets{
		Permitted:   capability.Chown | capability.MACOverride | capability.BPF,
		Effective:   capability.Chown | capability.MACOverride,
		Inheritable: capability.BPF,
	}

	require.NoError(t, capability.SetCurrent(want))

	sets, err := capability.Current()
	require.NoError(t, err)
	require.Equal(t, want, sets)
	status := test.CapabilityStatus(t)
	require.Equal(t, want.Permitted, status["CapPrm"])
	require.Equal(t, want.Effective, status["CapEff"])
	require.Equal(t, want.Inheritable, status["CapInh"])
}

func TestEffectiveCanBeReenabledWithinPermitted(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	test.LockThread()
	sets := capability.Sets{Permitted: capability.Chown | capability.BPF}
	require.NoError(t, capability.SetCurrent(sets))
	cleared, err := capability.Current()
	require.NoError(t, err)
	require.Zero(t, cleared.Effective)

	sets.Effective = capability.BPF
	require.NoError(t, capability.SetCurrent(sets))

	current, err := capability.Current()
	require.NoError(t, err)
	require.Equal(t, sets, current)
}

func TestDroppedPermittedCannotBeRegained(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	test.LockThread()
	sets, err := capability.Current()
	require.NoError(t, err)
	require.NotZero(t, sets.Permitted&capability.BPF, "fixture must begin with the capability being dropped")
	require.NoError(t, capability.SetCurrent(capability.Sets{}))

	err = capability.SetCurrent(capability.Sets{Permitted: capability.BPF})

	require.ErrorIs(t, err, test.ErrOperationNotPermitted)
	current, err := capability.Current()
	require.NoError(t, err)
	require.Empty(t, current)
}

func TestThreadChangesLeaveOtherThreadsUntouched(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	test.LockThread()
	before := test.CapabilityStatus(t)
	require.NotZero(t, before["CapEff"], "isolation evidence requires nonempty initial credentials")
	require.NotZero(t, before["CapBnd"]&capability.BPF)

	test.LockedThread(t, func() {
		require.NoError(t, capability.DropBounding(capability.BPF))
		require.NoError(t, capability.ClearAmbient())
		require.NoError(t, capability.SetCurrent(capability.Sets{}))

		sets, err := capability.Current()
		require.NoError(t, err)
		require.Empty(t, sets)
		bounded, err := capability.Bounding(capability.BPF)
		require.NoError(t, err)
		require.False(t, bounded)
	})

	require.Equal(t, before, test.CapabilityStatus(t), "a locked worker's changes do not restrict the calling thread")
}
