//go:build linux && clockchange

package timerfd_test

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/timerfd"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestRealtimeClockChangesCancelPendingReads(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		shift time.Duration
	}{
		{name: "forward", shift: time.Second},
		{name: "backward", shift: -time.Second},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			timer := test.Timer(t, timerfd.Realtime)
			deadline, err := timerfd.Now(timerfd.Realtime)
			require.NoError(t, err)
			deadline.Seconds += 60
			_, err = timer.SetAt(deadline, 0, timerfd.CancelOnSet)
			require.NoError(t, err)

			done := test.StartRead(t, timer.ReadExpirations)
			test.ShiftRealtime(t, scenario.shift)

			got := test.Receive(t, done)
			require.Zero(t, got.Value, "cancellation must not manufacture an expiration")
			require.ErrorIs(t, got.Err, unix.ECANCELED)

			// Consuming cancellation permits ordinary rearming and count reads.
			_, err = timer.SetAfter(20*time.Millisecond, 0)
			require.NoError(t, err)
			require.Equal(t, uint64(1), test.Value(t, timer.ReadExpirations))
		})
	}
}

func TestRearmingAnUnreadCanceledTimerAppliesTheNewSchedule(t *testing.T) {
	timer, fd := test.TimerDescriptor(t, timerfd.Realtime)
	deadline, err := timerfd.Now(timerfd.Realtime)
	require.NoError(t, err)
	deadline.Seconds += 60
	_, err = timer.SetAt(deadline, 0, timerfd.CancelOnSet)
	require.NoError(t, err)

	test.ShiftRealtime(t, time.Second)
	// The independent readiness observation leaves cancellation unconsumed.
	test.Readable(t, fd)
	deadline, err = timerfd.Now(timerfd.Realtime)
	require.NoError(t, err)
	deadline.Seconds++
	old, err := timer.SetAt(deadline, 0, timerfd.CancelOnSet)
	require.ErrorIs(t, err, unix.ECANCELED)
	require.Equal(t, timerfd.State{}, old, "an error must return no previous state")
	test.ArmedTimer(t, test.Value(t, timer.State), time.Second, 0)

	require.Equal(t, uint64(1), test.Value(t, timer.ReadExpirations), "ECANCELED must not undo the newly armed schedule")
}

func TestBackwardClockChangesDoNotInventExpirationsOrEOF(t *testing.T) {
	for _, scenario := range []struct {
		name string
		read func(*timerfd.Timer) (uint64, error)
	}{
		{
			name: "byte records",
			read: func(timer *timerfd.Timer) (uint64, error) {
				var record [8]byte
				n, err := timer.Read(record[:])
				if n != len(record) {
					return 0, err
				}
				return binary.NativeEndian.Uint64(record[:]), err
			},
		},
		{name: "typed counts", read: (*timerfd.Timer).ReadExpirations},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			timer, fd := test.TimerDescriptor(t, timerfd.Realtime)
			deadline, err := timerfd.Now(timerfd.Realtime)
			require.NoError(t, err)
			deadline.Seconds++
			_, err = timer.SetAt(deadline, time.Minute, 0)
			require.NoError(t, err)
			test.Readable(t, fd)

			// After expiry, rewind before the original deadline. Linux's
			// periodic forward calculation can then yield zero ticks. With
			// O_NONBLOCK it returns EAGAIN; Go must wait for a real record.
			test.ShiftRealtime(t, -2*time.Second)
			done := test.StartRead(t, func() (uint64, error) {
				return scenario.read(timer)
			})
			test.ShiftRealtime(t, 2*time.Second)

			require.Equal(t, test.ReadResult[uint64]{Value: 1}, test.Receive(t, done), "deliver one real expiration after the clock advances again")
		})
	}
}
