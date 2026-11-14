//go:build linux

package test

import (
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/timerfd"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// ArmedTimer checks a running schedule's interval and positive remaining time,
// allowing time to pass since arming or an earlier snapshot.
func ArmedTimer(t *testing.T, state timerfd.State, remaining, interval time.Duration) {
	t.Helper()
	require.Positive(t, state.Remaining)
	require.LessOrEqual(t, state.Remaining, remaining)
	require.Equal(t, interval, state.Interval)
}

// Clocks supplies named cases for every supported timer clock and its Linux ID.
// Each call returns independent table storage.
func Clocks() []ClockCase {
	return []ClockCase{
		{Name: "monotonic", Clock: timerfd.Monotonic, ID: unix.CLOCK_MONOTONIC},
		{Name: "realtime", Clock: timerfd.Realtime, ID: unix.CLOCK_REALTIME},
		{Name: "boottime", Clock: timerfd.Boottime, ID: unix.CLOCK_BOOTTIME},
	}
}

// ClockCase connects a readable test label, the public clock, and the kernel ID.
type ClockCase struct {
	Name  string
	Clock timerfd.Clock
	ID    int32
}
