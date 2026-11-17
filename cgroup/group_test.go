//go:build linux

package cgroup_test

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/cgroup"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

func TestOpenRejectsOtherFilesystemsWithoutLeaks(t *testing.T) {
	before := test.DescriptorCount(t)

	group, err := cgroup.Open(t.TempDir())
	test.NoResource(t, group, err, os.ErrInvalid)
	require.Equal(t, before, test.DescriptorCount(t), "rejected acquisition must close the directory")
}

func TestOpenMissingGroupReturnsNoResource(t *testing.T) {
	group, err := cgroup.Open("/nonexistent/go-linux-cgroup")

	test.NoResource(t, group, err, os.ErrNotExist)
}

func TestOpenRejectsFIFOWithoutBlocking(t *testing.T) {
	group, err := cgroup.Open(test.FIFO(t))

	test.NoResource(t, group, err, test.ErrNotDirectory)
}

func TestReadCurrentGroupControllers(t *testing.T) {
	group := test.CurrentCgroup(t)

	controllers, err := group.Controllers()
	require.NoError(t, err)
	t.Logf("controllers: %v", controllers)
}

func TestReadCurrentGroupSubtreeControl(t *testing.T) {
	group := test.CurrentCgroup(t)

	controllers, err := group.SubtreeControl()
	require.NoError(t, err)
	t.Logf("controllers: %v", controllers)
}

func TestReadCurrentGroupProcesses(t *testing.T) {
	group := test.CurrentCgroup(t)

	processes, err := group.Processes()
	require.NoError(t, err)
	require.Contains(t, processes, os.Getpid())
}

func TestReadGroupEvents(t *testing.T) {
	group := test.CgroupWithEvents(t)

	_, err := group.Events()
	require.NoError(t, err)
}

func TestReadCurrentGroupCPUStat(t *testing.T) {
	group := test.CurrentCgroup(t)

	stat, err := group.CPUStat()
	require.NoError(t, err)
	require.Contains(t, stat, "usage_usec")
}

func TestReadCurrentGroupCPUWeight(t *testing.T) {
	group := test.CurrentCgroup(t)

	_, err := group.CPUWeight()
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("controller file unavailable in the current group")
	}
	require.NoError(t, err)
}

func TestReadCurrentGroupCPUMax(t *testing.T) {
	group := test.CurrentCgroup(t)

	_, err := group.CPUMax()
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("controller file unavailable in the current group")
	}
	require.NoError(t, err)
}

func TestReadCurrentGroupMemoryHigh(t *testing.T) {
	group := test.CurrentCgroup(t)

	_, err := group.MemoryHigh()
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("controller file unavailable in the current group")
	}
	require.NoError(t, err)
}

func TestReadCurrentGroupMemoryMax(t *testing.T) {
	group := test.CurrentCgroup(t)

	_, err := group.MemoryMax()
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("controller file unavailable in the current group")
	}
	require.NoError(t, err)
}

func TestReadCurrentGroupMemoryCurrent(t *testing.T) {
	group := test.CurrentCgroup(t)

	_, err := group.MemoryCurrent()
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("controller file unavailable in the current group")
	}
	require.NoError(t, err)
}

func TestReadCurrentGroupMemoryEvents(t *testing.T) {
	group := test.CurrentCgroup(t)

	_, err := group.MemoryEvents()
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("controller file unavailable in the current group")
	}
	require.NoError(t, err)
}

func TestReadCurrentGroupPIDsMax(t *testing.T) {
	group := test.CurrentCgroup(t)

	_, err := group.PIDsMax()
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("controller file unavailable in the current group")
	}
	require.NoError(t, err)
}

func TestReadCurrentGroupPIDsCurrent(t *testing.T) {
	group := test.CurrentCgroup(t)

	_, err := group.PIDsCurrent()
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("controller file unavailable in the current group")
	}
	require.NoError(t, err)
}

func TestReadCurrentGroupPIDsEvents(t *testing.T) {
	group := test.CurrentCgroup(t)

	_, err := group.PIDsEvents()
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("controller file unavailable in the current group")
	}
	require.NoError(t, err)
}

func TestOpenWithoutAvailableDescriptors(t *testing.T) {
	if os.Args[len(os.Args)-1] != "cgroup-open-exhaustion" {
		ctx, command := test.Command(t, t.Name(), "--", "cgroup-open-exhaustion")
		output, err := command.CombinedOutput()
		require.NoError(t, ctx.Err())
		require.NoError(t, err, "%s", output)
		return
	}
	release := test.ExhaustDescriptors(t)

	group, err := cgroup.Open("/sys/fs/cgroup")
	test.NoResource(t, group, err, test.ErrTooManyOpenFiles)

	release()
	group, err = cgroup.Open("/sys/fs/cgroup")
	require.NoError(t, err)
	require.NoError(t, group.Close())
	test.DescriptorSlot(t)
}

func TestGroupRejectsInvalidChildNames(t *testing.T) {
	group := test.CurrentCgroup(t)

	for _, name := range []string{"", ".", "..", "../other", "/absolute", "child/name", "nul\x00"} {
		t.Run(name, func(t *testing.T) {
			child, err := group.New(name)
			test.NoResource(t, child, err, os.ErrInvalid)

			child, err = group.Open(name)
			test.NoResource(t, child, err, os.ErrInvalid)

			require.ErrorIs(t, group.Remove(name), os.ErrInvalid)
		})
	}
}

func TestGroupRejectsInvalidPIDs(t *testing.T) {
	group := test.CurrentCgroup(t)

	for _, pid := range []int{0, -1} {
		require.ErrorIs(t, group.AddProcess(pid), os.ErrInvalid)
	}
}

func TestGroupRejectsInvalidCPUWeights(t *testing.T) {
	group := test.CurrentCgroup(t)

	for _, weight := range []uint64{0, 10001} {
		require.ErrorIs(t, group.SetCPUWeight(weight), os.ErrInvalid)
	}
}

func TestGroupRejectsInvalidCPUBandwidth(t *testing.T) {
	group := test.CurrentCgroup(t)

	require.ErrorIs(t, group.SetCPUMax(cgroup.CPUBandwidth{}), os.ErrInvalid)
}

func TestGroupRejectsNegativeLimits(t *testing.T) {
	group := test.CurrentCgroup(t)

	require.ErrorIs(t, group.SetMemoryHigh(-2), os.ErrInvalid)
	require.ErrorIs(t, group.SetMemoryMax(-2), os.ErrInvalid)
	require.ErrorIs(t, group.SetPIDsMax(-2), os.ErrInvalid)
}

func TestInvalidControllerCommandsLeaveConfigurationUnchanged(t *testing.T) {
	group := test.CurrentCgroup(t)
	before, err := group.SubtreeControl()
	require.NoError(t, err)

	for _, scenario := range []struct {
		name            string
		enable, disable []cgroup.Controller
	}{
		{name: "empty"},
		{name: "unknown", enable: []cgroup.Controller{"future"}},
		{name: "injected command", enable: []cgroup.Controller{"cpu +memory"}},
		{name: "duplicate", enable: []cgroup.Controller{cgroup.CPU, cgroup.CPU}},
		{name: "overlap", enable: []cgroup.Controller{cgroup.CPU}, disable: []cgroup.Controller{cgroup.CPU}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			require.ErrorIs(t, group.SetControllers(scenario.enable, scenario.disable), os.ErrInvalid)

			after, err := group.SubtreeControl()
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestGroupLifetimeErrors(t *testing.T) {
	for _, closed := range []bool{false, true} {
		var group *cgroup.Group
		var expected error
		if closed {
			group = test.CurrentCgroup(t)
			require.NoError(t, group.Close())
			expected = os.ErrClosed
		} else {
			group = &cgroup.Group{}
			expected = os.ErrInvalid
		}

		child, err := group.New("child")
		test.NoResource(t, child, err, expected)
		child, err = group.Open("child")
		test.NoResource(t, child, err, expected)
		require.ErrorIs(t, group.Remove("child"), expected)
		processes, err := group.Processes()
		require.ErrorIs(t, err, expected)
		require.Nil(t, processes)
		require.ErrorIs(t, group.AddProcess(1), expected)
		require.ErrorIs(t, group.SetControllers([]cgroup.Controller{cgroup.CPU}, nil), expected)
		require.ErrorIs(t, group.SetCPUWeight(100), expected)
		require.ErrorIs(t, group.SetCPUMax(cgroup.CPUBandwidth{Quota: cgroup.UnlimitedQuota, Period: time.Second}), expected)
		require.ErrorIs(t, group.SetMemoryHigh(cgroup.Max), expected)
		require.ErrorIs(t, group.SetMemoryMax(cgroup.Max), expected)
		require.ErrorIs(t, group.SetPIDsMax(cgroup.Max), expected)
		require.ErrorIs(t, group.Close(), expected)
	}
}

func TestGroupReadLifetimeErrors(t *testing.T) {
	for _, closed := range []bool{false, true} {
		var group *cgroup.Group
		var expected error
		if closed {
			group = test.CurrentCgroup(t)
			require.NoError(t, group.Close())
			expected = os.ErrClosed
		} else {
			group = &cgroup.Group{}
			expected = os.ErrInvalid
		}

		_, err := group.Controllers()
		require.ErrorIs(t, err, expected)
		_, err = group.SubtreeControl()
		require.ErrorIs(t, err, expected)
		_, err = group.CPUWeight()
		require.ErrorIs(t, err, expected)
		_, err = group.CPUMax()
		require.ErrorIs(t, err, expected)
		_, err = group.CPUStat()
		require.ErrorIs(t, err, expected)
		_, err = group.MemoryHigh()
		require.ErrorIs(t, err, expected)
		_, err = group.MemoryMax()
		require.ErrorIs(t, err, expected)
		_, err = group.MemoryCurrent()
		require.ErrorIs(t, err, expected)
		_, err = group.MemoryEvents()
		require.ErrorIs(t, err, expected)
		_, err = group.PIDsMax()
		require.ErrorIs(t, err, expected)
		_, err = group.PIDsCurrent()
		require.ErrorIs(t, err, expected)
		_, err = group.PIDsEvents()
		require.ErrorIs(t, err, expected)
		_, err = group.Events()
		require.ErrorIs(t, err, expected)
		watcher, err := group.WatchEvents()
		test.NoResource(t, watcher, err, expected)
	}
}

func TestConcurrentReadAndClose(t *testing.T) {
	group := test.CurrentCgroup(t)
	results := make(chan error, 16)
	for range cap(results) {
		go func() {
			_, err := group.Controllers()
			results <- err
		}()
	}

	require.NoError(t, group.Close())

	for range cap(results) {
		err := test.Receive(t, results)
		if !errors.Is(err, os.ErrClosed) {
			require.NoError(t, err)
		}
	}
}

func TestEventWatcherZeroValue(t *testing.T) {
	var watcher cgroup.EventWatcher

	event, err := watcher.Read()
	require.ErrorIs(t, err, os.ErrInvalid)
	require.Zero(t, event)
	require.ErrorIs(t, watcher.SetReadDeadline(time.Time{}), os.ErrInvalid)
	require.ErrorIs(t, watcher.Close(), os.ErrInvalid)
}

func TestZeroWatcherSequenceYieldsOneTerminalError(t *testing.T) {
	var watcher cgroup.EventWatcher

	sequence := watcher.Events()
	var count int
	for event, err := range sequence {
		require.ErrorIs(t, err, os.ErrInvalid)
		require.Zero(t, event)
		count++
	}
	require.Equal(t, 1, count, "terminal errors are yielded once")
	for _, err := range sequence {
		require.NoError(t, err)
		require.FailNow(t, "exhausted sequence yielded another event")
	}
}

func TestWatcherSurvivesGroupClosure(t *testing.T) {
	group := test.CgroupWithEvents(t)
	watcher := test.CgroupWatcher(t, group)

	require.NoError(t, group.Close())
	require.NoError(t, watcher.SetReadDeadline(time.Now().Add(-time.Second)))
	event, err := watcher.Read()
	require.ErrorIs(t, err, os.ErrDeadlineExceeded, "watcher retains independent ownership after group close")
	require.Zero(t, event)
}

func TestClosingWatcherInterruptsPendingRead(t *testing.T) {
	watcher := test.CgroupWatcher(t, test.CgroupWithEvents(t))
	require.NoError(t, watcher.SetReadDeadline(time.Time{}))
	results := test.StartRead(t, watcher.Read)

	require.NoError(t, watcher.Close())

	require.ErrorIs(t, test.Receive(t, results).Err, os.ErrClosed)
	require.ErrorIs(t, watcher.Close(), os.ErrClosed)
	_, err := watcher.Read()
	require.ErrorIs(t, err, os.ErrClosed)
	require.ErrorIs(t, watcher.SetReadDeadline(time.Time{}), os.ErrClosed)
}

func TestWatcherReadWaitsUntilCallerDeadline(t *testing.T) {
	watcher := test.CgroupWatcher(t, test.CgroupWithEvents(t))
	deadline := time.Now().Add(250 * time.Millisecond)
	require.NoError(t, watcher.SetReadDeadline(deadline))

	event, err := watcher.Read()

	require.ErrorIs(t, err, os.ErrDeadlineExceeded)
	require.Zero(t, event)
	require.False(t, time.Now().Before(deadline), "removal checks must not expire the caller's deadline")
}

func TestWatcherDeadlineChangeInterruptsPendingRead(t *testing.T) {
	watcher := test.CgroupWatcher(t, test.CgroupWithEvents(t))
	results := test.StartRead(t, watcher.Read)

	require.NoError(t, watcher.SetReadDeadline(time.Now().Add(-time.Second)))

	result := test.Receive(t, results)
	require.ErrorIs(t, result.Err, os.ErrDeadlineExceeded)
	require.Zero(t, result.Value)
}

func TestWatcherReadDeadlineCanBeCleared(t *testing.T) {
	watcher := test.CgroupWatcher(t, test.CgroupWithEvents(t))
	require.NoError(t, watcher.SetReadDeadline(time.Now().Add(-time.Second)))
	_, err := watcher.Read()
	require.ErrorIs(t, err, os.ErrDeadlineExceeded)

	require.NoError(t, watcher.SetReadDeadline(time.Time{}))
	results := test.StartRead(t, watcher.Read)

	require.NoError(t, watcher.Close())

	require.ErrorIs(t, test.Receive(t, results).Err, os.ErrClosed)
}
