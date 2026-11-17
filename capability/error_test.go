//go:build linux

package capability_test

import (
	"testing"

	"github.com/alexfalkowski/go-linux/capability"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

func TestPolicyDenialsPreserveErrorsWithoutChangingCredentials(t *testing.T) {
	if !test.CapabilityProcess(t) {
		return
	}
	test.LockThread()
	before := test.CapabilityStatus(t)
	test.RefuseCapabilityCalls(t)

	sets, err := capability.Current()
	require.ErrorIs(t, err, test.ErrPermissionDenied)
	require.Empty(t, sets)

	bounded, err := capability.Bounding(capability.BPF)
	require.ErrorIs(t, err, test.ErrPermissionDenied)
	require.False(t, bounded)

	ambient, err := capability.Ambient(capability.BPF)
	require.ErrorIs(t, err, test.ErrPermissionDenied)
	require.False(t, ambient)

	require.ErrorIs(t, capability.SetCurrent(capability.Sets{}), test.ErrPermissionDenied)
	require.ErrorIs(t, capability.DropBounding(capability.Chown), test.ErrPermissionDenied)
	require.ErrorIs(t, capability.RaiseAmbient(capability.Chown), test.ErrPermissionDenied)
	require.ErrorIs(t, capability.LowerAmbient(capability.BPF), test.ErrPermissionDenied)
	require.ErrorIs(t, capability.ClearAmbient(), test.ErrPermissionDenied)

	require.Equal(t, before, test.CapabilityStatus(t), "policy-denied operations must leave credentials intact")
}
