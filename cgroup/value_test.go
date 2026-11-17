//go:build linux

package cgroup_test

import (
	"math"
	"os"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/cgroup"
	"github.com/stretchr/testify/require"
)

func TestLimitText(t *testing.T) {
	for _, scenario := range []struct {
		text  string
		value cgroup.Limit
	}{
		{text: "max", value: cgroup.Max},
		{text: "0", value: 0},
		{text: "4096", value: 4096},
		{text: "9223372036854775807", value: math.MaxInt64},
	} {
		t.Run(scenario.text, func(t *testing.T) {
			data, err := scenario.value.MarshalText()
			require.NoError(t, err)
			require.Equal(t, scenario.text, string(data))

			var value cgroup.Limit
			require.NoError(t, value.UnmarshalText([]byte(" \t"+scenario.text+"\n")))
			require.Equal(t, scenario.value, value)
		})
	}
}

func TestLimitTextRejectsInvalidInputWithoutChangingValue(t *testing.T) {
	for _, text := range []string{"", "-1", "-2", "+1", "1 2", "MAX", "9223372036854775808", "18446744073709551616", "1\x00"} {
		t.Run("invalid "+text, func(t *testing.T) {
			value := cgroup.Limit(4096)

			require.ErrorIs(t, value.UnmarshalText([]byte(text)), os.ErrInvalid)
			require.Equal(t, cgroup.Limit(4096), value, "failed decoding must leave the receiver unchanged")
		})
	}
}

func TestLimitTextRejectsInvalidNegativeValue(t *testing.T) {
	data, err := cgroup.Limit(-2).MarshalText()
	require.ErrorIs(t, err, os.ErrInvalid)
	require.Nil(t, data)
}

func TestCPUBandwidthText(t *testing.T) {
	for _, scenario := range []struct {
		text  string
		value cgroup.CPUBandwidth
	}{
		{text: "max 100000", value: cgroup.CPUBandwidth{Quota: cgroup.UnlimitedQuota, Period: 100 * time.Millisecond}},
		{text: "1000 1000", value: cgroup.CPUBandwidth{Quota: time.Millisecond, Period: time.Millisecond}},
		{text: "2000000 1000000", value: cgroup.CPUBandwidth{Quota: 2 * time.Second, Period: time.Second}},
		{text: "1001 1001", value: cgroup.CPUBandwidth{Quota: 1001 * time.Microsecond, Period: 1001 * time.Microsecond}},
	} {
		t.Run(scenario.text, func(t *testing.T) {
			data, err := scenario.value.MarshalText()
			require.NoError(t, err)
			require.Equal(t, scenario.text, string(data))

			var value cgroup.CPUBandwidth
			require.NoError(t, value.UnmarshalText([]byte(scenario.text+"\n")))
			require.Equal(t, scenario.value, value)
		})
	}
}

func TestCPUBandwidthTextRejectsMalformedRecords(t *testing.T) {
	for _, text := range []string{
		"", "max", "max max", "max 1000 extra", "-1 1000", "+1000 1000", "999 1000", "0 1000", "max 999", "max 1000001", //nolint:dupword // Invalid repeated token fixture.
		"9223372036854775807 1000", "18446744073709551616 1000", "1000 18446744073709551616",
	} {
		t.Run("invalid "+text, func(t *testing.T) {
			value := cgroup.CPUBandwidth{Quota: time.Second, Period: time.Second}
			original := value

			require.ErrorIs(t, value.UnmarshalText([]byte(text)), os.ErrInvalid)
			require.Equal(t, original, value, "failed decoding must leave the receiver unchanged")
		})
	}
}

func TestCPUBandwidthTextRejectsInvalidDurations(t *testing.T) {
	for _, value := range []cgroup.CPUBandwidth{
		{},
		{Quota: time.Second, Period: time.Second + 1},
		{Quota: time.Millisecond + 1, Period: time.Second},
		{Quota: -2, Period: time.Second},
		{Quota: time.Millisecond, Period: -time.Second},
	} {
		data, err := value.MarshalText()
		require.ErrorIs(t, err, os.ErrInvalid, "%+v", value)
		require.Nil(t, data)
	}
}

func TestCounterSnapshots(t *testing.T) {
	values, err := cgroup.ParseCounters([]byte("low 0\nhigh 12\nmax 3\nfuture 18446744073709551615\n"))
	require.NoError(t, err)
	require.Equal(t, map[string]uint64{"low": 0, "high": 12, "max": 3, "future": math.MaxUint64}, values)
}

func TestCounterSnapshotsRejectMalformedRecords(t *testing.T) {
	for _, text := range []string{"", " \n", "max", "max 1 2", "max -1", "max +1", "max 18446744073709551616", "max 1\nmax 2", "max 1\nbroken"} {
		t.Run(text, func(t *testing.T) {
			values, err := cgroup.ParseCounters([]byte(text))
			require.ErrorIs(t, err, os.ErrInvalid)
			require.Nil(t, values, "malformed snapshots must return no partial counters")
		})
	}
}

func TestEventSnapshots(t *testing.T) {
	event, err := cgroup.ParseEvents([]byte("frozen 0\nfuture 17\npopulated 1\n"))
	require.NoError(t, err)
	require.Equal(t, cgroup.Event{Populated: true, Other: map[string]uint64{"future": 17}}, event)

	event, err = cgroup.ParseEvents([]byte("populated 0\nfrozen 1\n"))
	require.NoError(t, err)
	require.Equal(t, cgroup.Event{Frozen: true, Other: map[string]uint64{}}, event)
}

func TestEventSnapshotsRejectMalformedState(t *testing.T) {
	for _, text := range []string{"", "populated 1", "frozen 1", "populated 2\nfrozen 0", "populated 0\nfrozen 2", "populated 0\nfrozen 0\nfrozen 1"} {
		t.Run(text, func(t *testing.T) {
			event, err := cgroup.ParseEvents([]byte(text))
			require.ErrorIs(t, err, os.ErrInvalid)
			require.Zero(t, event)
		})
	}
}
