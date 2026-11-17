//go:build linux

package landlock_test

import (
	"os"
	"testing"

	"github.com/alexfalkowski/go-linux/capability"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/landlock"
	"github.com/stretchr/testify/require"
)

func TestRestrictSetsNoNewPrivilegesFromZero(t *testing.T) {
	test.SecurityPrivileges(t)
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	_, denied := test.LandlockFiles(t)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadFile})
	require.NoError(t, capability.SetCurrent(capability.Sets{}))
	require.False(t, test.NoNewPrivileges(t), "the transition must start with the bit unset")

	require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread, NoNewPrivileges: true}))

	require.True(t, test.NoNewPrivileges(t), "Restrict must set the requested bit")
	_, err := os.ReadFile(denied)
	require.ErrorIs(t, err, test.ErrPermissionDenied, "the unprivileged caller's ruleset must enforce its policy")
}

func TestRestrictWithoutEitherAuthorityPreservesUnrestrictedState(t *testing.T) {
	test.SecurityPrivileges(t)
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	_, denied := test.LandlockFiles(t)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadFile})
	require.NoError(t, capability.SetCurrent(capability.Sets{}))
	require.False(t, test.NoNewPrivileges(t))

	err := ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread})

	require.ErrorIs(t, err, test.ErrOperationNotPermitted)
	require.False(t, test.NoNewPrivileges(t), "failed restriction must not set an unrequested privilege bit")
	_, err = os.ReadFile(denied)
	require.NoError(t, err, "failed restriction must leave the file readable")
}

func TestCapabilityAuthorizedRestrictLeavesNoNewPrivilegesUnset(t *testing.T) {
	test.SecurityPrivileges(t)
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	_, denied := test.LandlockFiles(t)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadFile})
	require.False(t, test.NoNewPrivileges(t))

	require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread}))

	require.False(t, test.NoNewPrivileges(t), "capability-authorized restriction must preserve the unset bit")
	_, err := os.ReadFile(denied)
	require.ErrorIs(t, err, test.ErrPermissionDenied, "capability-authorized restriction must enforce the ruleset")
}

func TestAllThreadsPropagatesNoNewPrivilegesFromZero(t *testing.T) {
	test.SecurityPrivileges(t)
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	_, denied := test.LandlockFiles(t)
	probePrivileges := test.NoNewPrivilegesThread(t)
	probePolicy := test.LandlockReadThread(t, denied, nil)
	require.False(t, probePrivileges(), "the existing sibling must start with the bit unset")
	require.NoError(t, probePolicy(), "the existing sibling must initially read the file")
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadFile})
	require.NoError(t, capability.SetCurrent(capability.Sets{}))
	require.False(t, test.NoNewPrivileges(t))

	require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.AllThreads, NoNewPrivileges: true}))

	require.True(t, test.NoNewPrivileges(t), "the restricting thread must receive the bit")
	require.True(t, probePrivileges(), "TSYNC must set the bit on the existing sibling")
	require.ErrorIs(t, probePolicy(), test.ErrPermissionDenied, "TSYNC must enforce the ruleset on the existing sibling")
}
