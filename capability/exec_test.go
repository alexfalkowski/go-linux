//go:build linux

package capability_test

import (
	"os"
	"testing"

	"github.com/alexfalkowski/go-linux/capability"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

func TestAmbientSurvivesOrdinaryExec(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	test.LockThread()
	test.CapabilityExecRules(t)
	sets := capability.Sets{Permitted: capability.Chown | capability.BPF, Inheritable: capability.Chown | capability.BPF}
	require.NoError(t, capability.SetCurrent(sets))
	require.NoError(t, capability.RaiseAmbient(capability.BPF))

	ctx, command := test.Command(t, "TestCapabilityExecChild", "-test.v", "--", "capability-exec-ambient")
	output, err := command.CombinedOutput()

	require.NoError(t, ctx.Err())
	require.NoError(t, err, "exec must retain ambient and recompute permitted/effective: %s", output)
	require.Contains(t, string(output), "verified capability exec state")
	current, err := capability.Current()
	require.NoError(t, err)
	require.Equal(t, sets, current, "child exec must not change parent credentials")
}

func TestPermittedIsRecomputedOnExec(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	test.LockThread()
	test.CapabilityExecRules(t)
	sets := capability.Sets{
		Permitted: capability.Chown | capability.BPF, Effective: capability.Chown, Inheritable: capability.Chown | capability.BPF,
	}
	require.NoError(t, capability.SetCurrent(sets))
	require.NoError(t, capability.ClearAmbient())

	ctx, command := test.Command(t, "TestCapabilityExecChild", "-test.v", "--", "capability-exec-empty")
	output, err := command.CombinedOutput()

	require.NoError(t, ctx.Err())
	require.NoError(t, err, "ordinary exec without ambient must clear permitted/effective under SECBIT_NOROOT: %s", output)
	require.Contains(t, string(output), "verified capability exec state")
}

func TestBoundingDropSurvivesExec(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	test.LockThread()
	bounded, err := capability.Bounding(capability.BPF)
	require.NoError(t, err)
	require.True(t, bounded, "fixture must start with the bounding capability")
	require.NoError(t, capability.DropBounding(capability.BPF))

	ctx, command := test.Command(t, "TestCapabilityExecChild", "-test.v", "--", "capability-exec-bounding")
	output, err := command.CombinedOutput()

	require.NoError(t, ctx.Err())
	require.NoError(t, err, "bounding restriction must be inherited by exec: %s", output)
	require.Contains(t, string(output), "verified capability exec state")
}

func TestCapabilityExecChild(t *testing.T) {
	marker := os.Args[len(os.Args)-1]
	if marker != "capability-exec-ambient" && marker != "capability-exec-empty" && marker != "capability-exec-bounding" {
		return
	}
	test.LockThread()
	sets, err := capability.Current()
	require.NoError(t, err)
	status := test.CapabilityStatus(t)

	switch marker {
	case "capability-exec-ambient", "capability-exec-empty":
		var active capability.Set
		if marker == "capability-exec-ambient" {
			active = capability.BPF
		} else {
			active = 0
		}
		require.Equal(t, capability.Sets{Permitted: active, Effective: active, Inheritable: capability.Chown | capability.BPF}, sets)
		require.Equal(t, active, status["CapAmb"])
		require.Equal(t, active, status["CapPrm"])
		require.Equal(t, active, status["CapEff"])
	case "capability-exec-bounding":
		bounded, err := capability.Bounding(capability.BPF)
		require.NoError(t, err)
		require.False(t, bounded)
		require.Zero(t, status["CapBnd"]&capability.BPF)
		require.Zero(t, sets.Permitted&capability.BPF)
	}
	t.Log("verified capability exec state")
}
