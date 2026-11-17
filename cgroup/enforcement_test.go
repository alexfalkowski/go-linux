//go:build linux

package cgroup_test

import (
	"context"
	"io"
	"os/exec"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/cgroup"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCPUMaxThrottlesBusyWorkload(t *testing.T) {
	leaf := test.CgroupWithController(t, cgroup.CPU)
	require.NoError(t, leaf.SetCPUMax(cgroup.CPUBandwidth{Quota: time.Millisecond, Period: 100 * time.Millisecond}))
	before := test.Value(t, leaf.CPUStat)
	ctx, command := test.Command(t, "TestCgroupWorkloadChild", "--", "cgroup-cpu")
	input := test.StartCgroupWorkload(t, command)
	require.NoError(t, leaf.AddProcess(command.Process.Pid))

	_, err := io.WriteString(input, "\n")
	require.NoError(t, err)

	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		stats, err := leaf.CPUStat()
		require.NoError(collect, err)
		require.Greater(collect, stats["nr_throttled"], before["nr_throttled"], "the child's own quota must throttle its CPU consumption")
		require.Greater(collect, stats["throttled_usec"], before["throttled_usec"], "the kernel must account throttled time")
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, command.Wait())
	require.NoError(t, ctx.Err())
}

func TestMemoryHighRecordsPressureWithoutKillingWorkload(t *testing.T) {
	leaf := test.CgroupWithController(t, cgroup.Memory)
	require.NoError(t, leaf.SetMemoryHigh(8<<20))
	before := test.Value(t, leaf.MemoryEvents)
	ctx, command := test.Command(t, "TestCgroupWorkloadChild", "--", "cgroup-memory")
	input := test.StartCgroupWorkload(t, command)
	require.NoError(t, leaf.AddProcess(command.Process.Pid))

	_, err := io.WriteString(input, "\n")
	require.NoError(t, err)

	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		events, err := leaf.MemoryEvents()
		require.NoError(collect, err)
		require.Greater(collect, events["high"], before["high"], "new charges must cross the configured high boundary")
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, leaf.SetMemoryHigh(cgroup.Max))
	require.NoError(t, input.Close())
	require.NoError(t, command.Wait())
	require.NoError(t, ctx.Err())

	events := test.Value(t, leaf.MemoryEvents)
	require.Equal(t, before["oom_kill"], events["oom_kill"], "memory.high must not require killing the workload")
}

func TestMemoryMaxRecordsEnforcementUnderPressure(t *testing.T) {
	leaf := test.CgroupWithController(t, cgroup.Memory)
	require.NoError(t, leaf.SetMemoryMax(16<<20))
	before := test.Value(t, leaf.MemoryEvents)
	ctx, command := test.Command(t, "TestCgroupWorkloadChild", "--", "cgroup-memory")
	input := test.StartCgroupWorkload(t, command)
	require.NoError(t, leaf.AddProcess(command.Process.Pid))

	_, err := io.WriteString(input, "\n")
	require.NoError(t, err)

	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		events, err := leaf.MemoryEvents()
		require.NoError(collect, err)
		require.Greater(collect, events["max"], before["max"], "the kernel must enforce the child's memory boundary")
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, leaf.SetMemoryMax(cgroup.Max))
	require.NoError(t, input.Close())
	err = command.Wait()
	require.NoError(t, ctx.Err())
	if err != nil {
		var exit *exec.ExitError
		require.ErrorAs(t, err, &exit)
		require.Equal(t, test.KillSignal, test.ExitSignal(t, exit.ProcessState), "only a kernel OOM kill may terminate the memory workload")
		require.Greater(t, test.Value(t, leaf.MemoryEvents)["oom_kill"], before["oom_kill"], "a killed child must have cgroup OOM evidence")
	}
}

func TestPIDsMaxRejectsForkAndAllowsItAfterReset(t *testing.T) {
	leaf := test.CgroupWithController(t, cgroup.PIDs)
	for _, limit := range []cgroup.Limit{0, cgroup.Max} {
		require.NoError(t, leaf.SetPIDsMax(limit))
		before := test.Value(t, leaf.PIDsEvents)
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		t.Cleanup(cancel)
		command := exec.CommandContext(ctx, "/bin/sh", "-c", `printf '\001'; read -r start; /bin/true & wait "$!"`)
		input := test.StartCgroupWorkload(t, command)
		require.NoError(t, leaf.AddProcess(command.Process.Pid))

		_, err := io.WriteString(input, "\n")
		require.NoError(t, err)
		err = command.Wait()
		require.NoError(t, ctx.Err())

		if limit == 0 {
			var exit *exec.ExitError
			require.ErrorAs(t, err, &exit)
			require.True(t, exit.Exited(), "PID enforcement must reject fork without killing the existing shell")
			require.Greater(t, test.Value(t, leaf.PIDsEvents)["max"], before["max"], "the rejected fork must increment the limit-hit counter")
		} else {
			require.NoError(t, err, "resetting pids.max must restore child creation")
			require.Equal(t, before["max"], test.Value(t, leaf.PIDsEvents)["max"], "the reset must permit fork without another limit hit")
		}
	}
}

func TestCgroupWorkloadChild(t *testing.T) {
	test.CgroupWorkloadChild(t)
}
