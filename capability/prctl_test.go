//go:build linux

package capability_test

import (
	"os"
	"testing"

	"github.com/alexfalkowski/go-linux/capability"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

func TestMembershipMatchesKernelThreadStatus(t *testing.T) {
	test.LockThread()
	status := test.CapabilityStatus(t)

	for mask := capability.Chown; mask <= capability.CheckpointRestore; mask <<= 1 {
		bounded, err := capability.Bounding(mask)
		require.NoError(t, err)
		require.Equal(t, status["CapBnd"]&mask != 0, bounded, "bounding mask=%#x", mask)

		ambient, err := capability.Ambient(mask)
		require.NoError(t, err)
		require.Equal(t, status["CapAmb"]&mask != 0, ambient, "ambient mask=%#x", mask)
	}
}

func TestSingleCapabilityOperationsRejectInvalidMasks(t *testing.T) {
	if !test.CapabilityProcess(t) {
		return
	}
	test.LockThread()
	before := test.CapabilityStatus(t)

	for _, mask := range []capability.Set{0, capability.Chown | capability.BPF, 1 << 41, 1 << 63, capability.BPF | 1<<41} {
		bounded, err := capability.Bounding(mask)
		require.ErrorIs(t, err, os.ErrInvalid, "bounding mask=%#x", mask)
		require.False(t, bounded)

		ambient, err := capability.Ambient(mask)
		require.ErrorIs(t, err, os.ErrInvalid, "ambient mask=%#x", mask)
		require.False(t, ambient)

		require.ErrorIs(t, capability.DropBounding(mask), os.ErrInvalid, "drop mask=%#x", mask)
		require.ErrorIs(t, capability.RaiseAmbient(mask), os.ErrInvalid, "raise mask=%#x", mask)
		require.ErrorIs(t, capability.LowerAmbient(mask), os.ErrInvalid, "lower mask=%#x", mask)
		require.Equal(t, before, test.CapabilityStatus(t), "invalid input must not change credentials")
	}
}

func TestDropBoundingRequiresSetpcap(t *testing.T) {
	if !test.CapabilityProcess(t) {
		return
	}
	test.LockThread()
	require.NoError(t, capability.SetCurrent(capability.Sets{}))
	before := test.CapabilityStatus(t)

	err := capability.DropBounding(capability.Chown)

	require.ErrorIs(t, err, test.ErrOperationNotPermitted)
	var operation *os.SyscallError
	require.ErrorAs(t, err, &operation)
	require.Equal(t, "prctl(PR_CAPBSET_DROP)", operation.Syscall)
	require.Equal(t, before, test.CapabilityStatus(t))
}

func TestDropBoundingLeavesCurrentAuthority(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	test.LockThread()
	before := test.CapabilityStatus(t)
	require.NotZero(t, before["CapBnd"]&capability.BPF)
	require.NotZero(t, before["CapEff"]&capability.BPF)

	require.NoError(t, capability.DropBounding(capability.BPF))

	bounded, err := capability.Bounding(capability.BPF)
	require.NoError(t, err)
	require.False(t, bounded)
	before["CapBnd"] &^= capability.BPF
	require.Equal(t, before, test.CapabilityStatus(t), "bounding drop must leave the other four sets unchanged")
	require.NoError(t, capability.DropBounding(capability.BPF), "dropping an absent bit is permitted with CAP_SETPCAP")
}

func TestBoundingDropPreventsNewInheritableCapability(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	test.LockThread()
	sets, err := capability.Current()
	require.NoError(t, err)
	sets.Inheritable &^= capability.Chown
	require.NoError(t, capability.SetCurrent(sets))
	require.NoError(t, capability.DropBounding(capability.Chown))

	sets.Inheritable |= capability.Chown
	err = capability.SetCurrent(sets)

	require.ErrorIs(t, err, test.ErrOperationNotPermitted)
	require.Zero(t, test.CapabilityStatus(t)["CapInh"]&capability.Chown)
}

func TestAbsentAmbientCanBeLoweredAndCleared(t *testing.T) {
	if !test.CapabilityProcess(t) {
		return
	}
	test.LockThread()
	require.NoError(t, capability.SetCurrent(capability.Sets{}))

	require.NoError(t, capability.LowerAmbient(capability.BPF))
	require.NoError(t, capability.ClearAmbient())

	ambient, err := capability.Ambient(capability.BPF)
	require.NoError(t, err)
	require.False(t, ambient)
	require.Zero(t, test.CapabilityStatus(t)["CapAmb"])
}

func TestAmbientRequiresPermittedAndInheritable(t *testing.T) {
	for name, sets := range map[string]capability.Sets{
		"missing permitted":   {Inheritable: capability.Chown},
		"missing inheritable": {Permitted: capability.Chown},
	} {
		t.Run(name, func(t *testing.T) {
			if !test.NamespaceProcess(t) {
				return
			}
			test.LockThread()
			require.NoError(t, capability.SetCurrent(sets))

			err := capability.RaiseAmbient(capability.Chown)

			require.ErrorIs(t, err, test.ErrOperationNotPermitted)
			ambient, err := capability.Ambient(capability.Chown)
			require.NoError(t, err)
			require.False(t, ambient)
		})
	}
}

func TestAmbientRaiseAndLowerPreserveOtherSets(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	test.LockThread()
	sets := capability.Sets{Permitted: capability.Chown | capability.BPF, Inheritable: capability.Chown | capability.BPF}
	require.NoError(t, capability.SetCurrent(sets))
	require.NoError(t, capability.RaiseAmbient(capability.Chown))
	require.NoError(t, capability.RaiseAmbient(capability.BPF))
	require.Equal(t, capability.Chown|capability.BPF, test.CapabilityStatus(t)["CapAmb"])

	require.NoError(t, capability.LowerAmbient(capability.Chown))

	ambient, err := capability.Ambient(capability.Chown)
	require.NoError(t, err)
	require.False(t, ambient)
	ambient, err = capability.Ambient(capability.BPF)
	require.NoError(t, err)
	require.True(t, ambient)
	require.Equal(t, capability.BPF, test.CapabilityStatus(t)["CapAmb"])
	current, err := capability.Current()
	require.NoError(t, err)
	require.Equal(t, sets, current, "ambient changes do not immediately grant effective capabilities")
}

func TestClearAmbientRemovesAllCapabilities(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	test.LockThread()
	sets := capability.Sets{Permitted: capability.Chown | capability.BPF, Inheritable: capability.Chown | capability.BPF}
	require.NoError(t, capability.SetCurrent(sets))
	require.NoError(t, capability.RaiseAmbient(capability.Chown))
	require.NoError(t, capability.RaiseAmbient(capability.BPF))
	require.NotZero(t, test.CapabilityStatus(t)["CapAmb"])

	require.NoError(t, capability.ClearAmbient())

	require.Zero(t, test.CapabilityStatus(t)["CapAmb"])
	current, err := capability.Current()
	require.NoError(t, err)
	require.Equal(t, sets, current)
}

func TestLoweringPermittedOrInheritableLowersAmbient(t *testing.T) {
	for name, sets := range map[string]capability.Sets{
		"permitted":   {Permitted: capability.BPF, Inheritable: capability.Chown | capability.BPF},
		"inheritable": {Permitted: capability.Chown | capability.BPF, Inheritable: capability.BPF},
	} {
		t.Run(name, func(t *testing.T) {
			if !test.NamespaceProcess(t) {
				return
			}
			test.LockThread()
			initial := capability.Sets{Permitted: capability.Chown | capability.BPF, Inheritable: capability.Chown | capability.BPF}
			require.NoError(t, capability.SetCurrent(initial))
			require.NoError(t, capability.RaiseAmbient(capability.Chown))
			require.NoError(t, capability.RaiseAmbient(capability.BPF))

			require.NoError(t, capability.SetCurrent(sets))

			require.Equal(t, capability.BPF, test.CapabilityStatus(t)["CapAmb"])
		})
	}
}

func TestSecurebitsCanRefuseAmbientRaise(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	test.LockThread()
	test.BlockAmbientRaise(t)
	require.NoError(t, capability.SetCurrent(capability.Sets{Permitted: capability.Chown, Inheritable: capability.Chown}))

	err := capability.RaiseAmbient(capability.Chown)

	require.ErrorIs(t, err, test.ErrOperationNotPermitted)
	require.Zero(t, test.CapabilityStatus(t)["CapAmb"])
}
