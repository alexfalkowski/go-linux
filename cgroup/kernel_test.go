//go:build linux

package cgroup_test

import (
	"os"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/cgroup"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

func TestOpenedGroupOwnsIndependentReference(t *testing.T) {
	group := test.DelegatedCgroup(t)
	child := test.CgroupChild(t, group, "child")
	duplicate, err := group.Open("child")
	require.NoError(t, err)
	test.Cleanup(t, duplicate)

	require.NoError(t, child.Close())

	require.Empty(t, test.Value(t, duplicate.Processes))
}

func TestCreatingExistingGroupReturnsNoResource(t *testing.T) {
	group := test.DelegatedCgroup(t)
	test.CgroupChild(t, group, "child")

	conflict, err := group.New("child")

	test.NoResource(t, conflict, err, test.ErrExists)
}

func TestRemovedGroupIsUnavailable(t *testing.T) {
	group := test.DelegatedCgroup(t)
	test.CgroupChild(t, group, "child")

	require.NoError(t, group.Remove("child"))

	require.ErrorIs(t, group.Remove("child"), os.ErrNotExist)
	missing, err := group.Open("child")
	test.NoResource(t, missing, err, os.ErrNotExist)
}

func TestUnavailableControllerRejectsConfiguration(t *testing.T) {
	group := test.DelegatedCgroup(t)
	leaf := test.CgroupChild(t, group, "leaf")

	_, err := leaf.PIDsMax()
	require.ErrorIs(t, err, os.ErrNotExist)
	require.ErrorIs(t, leaf.SetPIDsMax(16), os.ErrNotExist)
	require.ErrorIs(t, leaf.SetControllers([]cgroup.Controller{cgroup.PIDs}, nil), os.ErrNotExist)
	require.Empty(t, test.Value(t, leaf.Controllers))
}

func TestEnabledControllersAreListed(t *testing.T) {
	group := test.DelegatedCgroup(t)

	require.NoError(t, group.SetControllers([]cgroup.Controller{cgroup.CPU, cgroup.Memory, cgroup.PIDs}, nil))

	require.ElementsMatch(t, []cgroup.Controller{cgroup.CPU, cgroup.Memory, cgroup.PIDs}, test.Value(t, group.SubtreeControl))
}

func TestParentControllerEnablementControlsChildLimits(t *testing.T) {
	group := test.DelegatedCgroup(t)
	leaf := test.CgroupChild(t, group, "leaf")

	require.NoError(t, group.SetControllers([]cgroup.Controller{cgroup.PIDs}, nil))

	require.Equal(t, cgroup.Max, test.Value(t, leaf.PIDsMax))

	require.NoError(t, group.SetControllers(nil, []cgroup.Controller{cgroup.PIDs}))

	require.NotContains(t, test.Value(t, group.SubtreeControl), cgroup.PIDs)
	require.ErrorIs(t, leaf.SetPIDsMax(16), os.ErrNotExist)
}

func TestControllerRequiredByChildCannotBeDisabled(t *testing.T) {
	group := test.DelegatedCgroup(t)
	require.NoError(t, group.SetControllers([]cgroup.Controller{cgroup.PIDs}, nil))
	leaf := test.CgroupChild(t, group, "leaf")
	require.NoError(t, leaf.SetControllers([]cgroup.Controller{cgroup.PIDs}, nil))

	require.ErrorIs(t, group.SetControllers(nil, []cgroup.Controller{cgroup.PIDs}), test.ErrBusy)

	require.NoError(t, leaf.SetControllers(nil, []cgroup.Controller{cgroup.PIDs}))
	require.NoError(t, group.SetControllers(nil, []cgroup.Controller{cgroup.PIDs}))
}

func TestCPUWeightRoundTrips(t *testing.T) {
	leaf := test.CgroupWithController(t, cgroup.CPU)

	for _, weight := range []uint64{1, 10000, 100} {
		require.NoError(t, leaf.SetCPUWeight(weight))
		require.Equal(t, weight, test.Value(t, leaf.CPUWeight))
	}
}

func TestCPUBandwidthCanBeLimitedAndReset(t *testing.T) {
	leaf := test.CgroupWithController(t, cgroup.CPU)

	for _, bandwidth := range []cgroup.CPUBandwidth{
		{Quota: 25 * time.Millisecond, Period: 100 * time.Millisecond},
		{Quota: cgroup.UnlimitedQuota, Period: 100 * time.Millisecond},
	} {
		require.NoError(t, leaf.SetCPUMax(bandwidth))
		require.Equal(t, bandwidth, test.Value(t, leaf.CPUMax))
	}
}

func TestMemoryHighCanBeLimitedAndReset(t *testing.T) {
	leaf := test.CgroupWithController(t, cgroup.Memory)

	for _, limit := range []cgroup.Limit{64 << 20, cgroup.Max} {
		require.NoError(t, leaf.SetMemoryHigh(limit))
		require.Equal(t, limit, test.Value(t, leaf.MemoryHigh))
	}
}

func TestMemoryMaxCanBeLimitedAndReset(t *testing.T) {
	leaf := test.CgroupWithController(t, cgroup.Memory)

	for _, limit := range []cgroup.Limit{64 << 20, cgroup.Max} {
		require.NoError(t, leaf.SetMemoryMax(limit))
		require.Equal(t, limit, test.Value(t, leaf.MemoryMax))
	}
}

func TestPIDsMaxCanBeLimitedAndReset(t *testing.T) {
	leaf := test.CgroupWithController(t, cgroup.PIDs)

	for _, limit := range []cgroup.Limit{0, 16, cgroup.Max} {
		require.NoError(t, leaf.SetPIDsMax(limit))
		require.Equal(t, limit, test.Value(t, leaf.PIDsMax))
	}
}

func TestEmptyGroupHasNoTasks(t *testing.T) {
	leaf := test.CgroupWithController(t, cgroup.PIDs)

	require.Zero(t, test.Value(t, leaf.PIDsCurrent))
}

func TestMemoryEventsIncludeOOMCounter(t *testing.T) {
	leaf := test.CgroupWithController(t, cgroup.Memory)

	require.Contains(t, test.Value(t, leaf.MemoryEvents), "oom")
}

func TestPIDsEventsIncludeMaxCounter(t *testing.T) {
	leaf := test.CgroupWithController(t, cgroup.PIDs)

	require.Contains(t, test.Value(t, leaf.PIDsEvents), "max")
}

func TestMovingProcessChangesMembership(t *testing.T) {
	group := test.DelegatedCgroup(t)
	leaf := test.CgroupChild(t, group, "member")
	process, _ := test.Child(t)

	require.NoError(t, leaf.AddProcess(process.Pid))
	require.Contains(t, test.Value(t, leaf.Processes), process.Pid)

	require.NoError(t, group.AddProcess(process.Pid))

	require.Contains(t, test.Value(t, group.Processes), process.Pid)
	require.Empty(t, test.Value(t, leaf.Processes))
}

func TestPopulatedEventsFollowProcessMigration(t *testing.T) {
	group := test.DelegatedCgroup(t)
	leaf := test.CgroupChild(t, group, "member")
	watcher := test.CgroupWatcher(t, leaf)
	process, _ := test.Child(t)

	require.False(t, test.Value(t, leaf.Events).Populated)
	require.NoError(t, leaf.AddProcess(process.Pid))

	require.True(t, test.Value(t, watcher.Read).Populated)

	require.NoError(t, group.AddProcess(process.Pid))

	require.False(t, test.Value(t, watcher.Read).Populated)
}

func TestPopulatedGroupCannotBeRemoved(t *testing.T) {
	group := test.DelegatedCgroup(t)
	leaf := test.CgroupChild(t, group, "member")
	process, _ := test.Child(t)
	require.NoError(t, leaf.AddProcess(process.Pid))

	require.ErrorIs(t, group.Remove("member"), test.ErrBusy)
}

func TestRemovingWatchedGroupInterruptsPendingRead(t *testing.T) {
	group := test.DelegatedCgroup(t)
	leaf := test.CgroupChild(t, group, "watched")
	watcher := test.CgroupWatcher(t, leaf)
	results := test.StartRead(t, watcher.Read)

	require.NoError(t, group.Remove("watched"))

	result := test.Receive(t, results)
	require.ErrorIs(t, result.Err, os.ErrNotExist)
	require.Zero(t, result.Value)
}

func TestPopulatedGroupCannotEnableDomainController(t *testing.T) {
	leaf := test.CgroupWithController(t, cgroup.Memory)
	process, _ := test.Child(t)
	require.NoError(t, leaf.AddProcess(process.Pid))

	require.ErrorIs(t, leaf.SetControllers([]cgroup.Controller{cgroup.Memory}, nil), test.ErrBusy,
		"domain controllers require an empty internal group")
}

func TestDelegatedCreationFailureCleansUp(t *testing.T) {
	if os.Args[len(os.Args)-1] != "cgroup-create-exhaustion" {
		_ = test.DelegatedCgroup(t)
		ctx, command := test.Command(t, t.Name(), "--", "cgroup-create-exhaustion")
		output, err := command.CombinedOutput()
		require.NoError(t, ctx.Err())
		require.NoError(t, err, "%s", output)
		return
	}
	group := test.DelegatedCgroup(t)
	release := test.ExhaustDescriptors(t)

	child, err := group.New("failure")
	test.NoResource(t, child, err, test.ErrTooManyOpenFiles)

	slot := release()
	child, err = group.Open("failure")
	test.NoResource(t, child, err, os.ErrNotExist)

	child, err = group.New("failure")
	require.NoError(t, err, "creation works once a descriptor is available")

	require.NoError(t, child.Close())
	require.NoError(t, group.Remove("failure"))
	require.Equal(t, slot, test.DescriptorSlot(t), "successful retry must release its descriptor")
}

func TestProcessChild(t *testing.T) {
	test.ProcessChild(t)
}
