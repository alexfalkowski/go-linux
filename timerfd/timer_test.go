//go:build linux

package timerfd_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/timerfd"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

var _ io.ReadCloser = (*timerfd.Timer)(nil)

func TestTimersStartDisarmedUsingTheSelectedClock(t *testing.T) {
	for _, scenario := range test.Clocks() {
		t.Run(scenario.Name, func(t *testing.T) {
			timer, fd := test.TimerDescriptor(t, scenario.Clock)

			state := test.Value(t, timer.State)
			require.Equal(t, timerfd.State{}, state)
			require.Contains(t, test.FDInfo(t, fd), fmt.Sprintf("clockid: %d\n", scenario.ID))
		})
	}
}

func TestNowReturnsANormalizedReadingOfTheSelectedClock(t *testing.T) {
	for _, scenario := range test.Clocks() {
		t.Run(scenario.Name, func(t *testing.T) {
			var before, after unix.Timespec
			require.NoError(t, unix.ClockGettime(scenario.ID, &before))

			now, err := timerfd.Now(scenario.Clock)
			require.NoError(t, err)
			require.NoError(t, unix.ClockGettime(scenario.ID, &after))
			require.Equal(t, scenario.Clock, now.Clock)

			reading := now.Seconds*1e9 + now.Nanoseconds
			require.GreaterOrEqual(t, reading, before.Nano())
			require.LessOrEqual(t, reading, after.Nano())
			require.GreaterOrEqual(t, now.Nanoseconds, int64(0))
			require.Less(t, now.Nanoseconds, int64(1e9))
		})
	}
}

func TestRelativeOneShotTimersExpireOnceAndRemainDisarmed(t *testing.T) {
	for _, scenario := range test.Clocks() {
		t.Run(scenario.Name, func(t *testing.T) {
			clock := scenario.Clock
			timer, fd := test.TimerDescriptor(t, clock)
			started := time.Now()

			old, err := timer.SetAfter(40*time.Millisecond, 0)
			require.NoError(t, err)
			require.Equal(t, timerfd.State{}, old)

			count := test.Value(t, timer.ReadExpirations)
			require.Equal(t, uint64(1), count, "a one-shot expires exactly once")
			require.GreaterOrEqual(t, time.Since(started), 20*time.Millisecond, "relative arming must not expire immediately")
			test.NoPendingRecord(t, fd)

			state := test.Value(t, timer.State)
			require.Equal(t, timerfd.State{}, state, "consumed one-shots must remain disarmed")
		})
	}
}

func TestAbsoluteOneShotTimersUseTheSelectedClockOrigin(t *testing.T) {
	for _, scenario := range test.Clocks() {
		t.Run(scenario.Name, func(t *testing.T) {
			clock := scenario.Clock
			timer, fd := test.TimerDescriptor(t, clock)

			_, err := timer.SetAfter(5*time.Second, 7*time.Second)
			require.NoError(t, err)

			started := time.Now()
			now, err := timerfd.Now(clock)
			require.NoError(t, err)

			// Add whole seconds so timestamp arithmetic cannot denormalize nanos.
			deadline := now
			deadline.Seconds++

			old, err := timer.SetAt(deadline, 0, 0)
			require.NoError(t, err)
			test.ArmedTimer(t, old, 5*time.Second, 7*time.Second)

			state := test.Value(t, timer.State)
			test.ArmedTimer(t, state, time.Second, 0)

			count := test.Value(t, timer.ReadExpirations)
			require.Equal(t, uint64(1), count)
			require.GreaterOrEqual(t, time.Since(started), 500*time.Millisecond, "future absolute deadlines must not expire immediately")
			test.NoPendingRecord(t, fd)
		})
	}
}

func TestPeriodicReadsIncludeMissedExpirationsAndKeepRunning(t *testing.T) {
	timer := test.Timer(t, timerfd.Monotonic)
	const period = 10 * time.Millisecond
	started := time.Now()
	_, err := timer.SetAfter(period, period)
	require.NoError(t, err)
	time.Sleep(65 * time.Millisecond)

	// Inspection must preserve accumulated expirations, including overruns.
	state := test.Value(t, timer.State)
	require.Equal(t, period, state.Interval)

	count := test.Value(t, timer.ReadExpirations)
	require.GreaterOrEqual(t, count, uint64(5), "periodic reads must include missed expirations")
	require.LessOrEqual(t, float64(count), float64(time.Since(started))/float64(period)+1, "decoded counts must agree with elapsed periods")

	count = test.Value(t, timer.ReadExpirations)
	require.Positive(t, count, "periodic timers must continue after a read")
}

func TestAbsolutePeriodicTimersRetainTheirArmingFlags(t *testing.T) {
	for _, scenario := range test.Clocks() {
		t.Run(scenario.Name, func(t *testing.T) {
			clock := scenario.Clock
			timer, fd := test.TimerDescriptor(t, clock)
			flags := timerfd.SetFlags(0)
			if clock == timerfd.Realtime {
				flags = timerfd.CancelOnSet
			}

			// A nonzero past absolute deadline must expire, rather than disarm.
			past := timerfd.Instant{Clock: clock, Nanoseconds: 1}

			_, err := timer.SetAt(past, 10*time.Millisecond, flags)
			require.NoError(t, err)

			options := unix.TFD_TIMER_ABSTIME
			if flags != 0 {
				options |= unix.TFD_TIMER_CANCEL_ON_SET
			}

			require.Contains(t, test.FDInfo(t, fd), fmt.Sprintf("settime flags: 0%o\n", options), "the kernel must retain the requested absolute/cancellation flags")

			for range 2 {
				count := test.Value(t, timer.ReadExpirations)
				require.Positive(t, count, "absolute periodic timers must deliver counts repeatedly")
			}

			state := test.Value(t, timer.Disarm)
			require.Equal(t, 10*time.Millisecond, state.Interval)
			test.NoPendingRecord(t, fd)
		})
	}
}

func TestRearmingReturnsThePreviousSchedule(t *testing.T) {
	timer := test.Timer(t, timerfd.Monotonic)
	old, err := timer.SetAfter(5*time.Second, 7*time.Second)
	require.NoError(t, err)
	require.Equal(t, timerfd.State{}, old)
	before := test.Value(t, timer.State)
	test.ArmedTimer(t, before, 5*time.Second, 7*time.Second)

	old, err = timer.SetAfter(6*time.Second, 8*time.Second)
	require.NoError(t, err)
	test.ArmedTimer(t, old, before.Remaining, 7*time.Second)
	test.ArmedTimer(t, test.Value(t, timer.State), 6*time.Second, 8*time.Second)
}

func TestDisarmingReturnsThePreviousScheduleAndClearsIt(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		initial time.Duration
	}{
		{name: "running timer", initial: 6 * time.Second},
		{name: "inactive timer"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			timer, fd := test.TimerDescriptor(t, timerfd.Monotonic)
			if scenario.initial > 0 {
				_, err := timer.SetAfter(scenario.initial, 8*time.Second)
				require.NoError(t, err)
			}

			old := test.Value(t, timer.Disarm)
			if scenario.initial > 0 {
				test.ArmedTimer(t, old, scenario.initial, 8*time.Second)
			} else {
				require.Equal(t, timerfd.State{}, old)
			}

			require.Equal(t, timerfd.State{}, test.Value(t, timer.State))
			require.Equal(t, timerfd.State{}, test.Value(t, timer.Disarm), "repeated disarming is valid")
			test.NoPendingRecord(t, fd)
		})
	}
}

func TestZeroInitialTimeDisarmsWhileRetainingTheInterval(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		absolute bool
	}{
		{name: "zero relative delay", absolute: false},
		{name: "zero absolute instant", absolute: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			timer, fd := test.TimerDescriptor(t, timerfd.Monotonic)

			_, err := timer.SetAfter(time.Nanosecond, 0)
			require.NoError(t, err)
			test.Readable(t, fd)

			if scenario.absolute {
				_, err = timer.SetAt(timerfd.Instant{}, time.Second, 0)
			} else {
				_, err = timer.SetAfter(0, time.Second)
			}
			require.NoError(t, err)

			state := test.Value(t, timer.State)
			require.Equal(t, timerfd.State{Interval: time.Second}, state, "zero initial time disarms while retaining the supplied interval")
			test.NoPendingRecord(t, fd)
		})
	}
}

func TestRearmingOrDisarmingClearsUnreadExpirations(t *testing.T) {
	timer, fd := test.TimerDescriptor(t, timerfd.Monotonic)

	_, err := timer.SetAfter(time.Nanosecond, 0)
	require.NoError(t, err)
	test.Readable(t, fd)

	state := test.Value(t, timer.State)
	require.Equal(t, timerfd.State{}, state, "expired one-shots can have unread expirations despite zero remaining time")

	_, err = timer.SetAfter(5*time.Second, 0)
	require.NoError(t, err)
	test.NoPendingRecord(t, fd)

	_, err = timer.SetAfter(time.Nanosecond, 0)
	require.NoError(t, err)
	test.Readable(t, fd)

	_ = test.Value(t, timer.Disarm)

	test.NoPendingRecord(t, fd)
}

func TestByteReadsReturnOneNativeEndianExpirationRecord(t *testing.T) {
	timer, fd := test.TimerDescriptor(t, timerfd.Monotonic)

	_, err := timer.SetAfter(time.Nanosecond, 0)
	require.NoError(t, err)

	buffer := bytes.Repeat([]byte{0xff}, 16)

	n, err := timer.Read(buffer)
	require.NoError(t, err)
	require.Equal(t, 8, n, "reads must return one complete record")

	expected := bytes.Repeat([]byte{0xff}, len(buffer))
	binary.NativeEndian.PutUint64(expected, 1)
	require.Equal(t, expected, buffer, "one native-endian record followed by the untouched tail")
	test.NoPendingRecord(t, fd)
}

func TestShortReadsFailWithoutConsumingExpirations(t *testing.T) {
	for size := range 8 {
		t.Run(fmt.Sprintf("%d byte buffer", size), func(t *testing.T) {
			timer, fd := test.TimerDescriptor(t, timerfd.Monotonic)

			_, err := timer.SetAfter(time.Nanosecond, 0)
			require.NoError(t, err)
			test.Readable(t, fd)

			n, err := timer.Read(make([]byte, size))
			require.Zero(t, n)
			require.ErrorIs(t, err, unix.EINVAL)

			var errno syscall.Errno
			require.ErrorAs(t, err, &errno)
			require.Equal(t, unix.EINVAL, errno)

			var pathErr *os.PathError
			require.ErrorAs(t, err, &pathErr)
			require.Equal(t, "read", pathErr.Op)

			count := test.Value(t, timer.ReadExpirations)
			require.Equal(t, uint64(1), count, "invalid reads must not consume expirations")
		})
	}
}

func TestInvalidRelativeArmingLeavesTheTimerUnchanged(t *testing.T) {
	for _, values := range []struct {
		name     string
		initial  time.Duration
		interval time.Duration
	}{
		{name: "negative initial", initial: -1},
		{name: "negative interval", initial: 1, interval: -1},
	} {
		t.Run(values.name, func(t *testing.T) {
			timer := test.Timer(t, timerfd.Monotonic)

			_, err := timer.SetAfter(5*time.Second, 7*time.Second)
			require.NoError(t, err)

			old, err := timer.SetAfter(values.initial, values.interval)
			require.ErrorIs(t, err, os.ErrInvalid)
			require.Equal(t, timerfd.State{}, old)

			checkUnchangedTimer(t, timer)
		})
	}
}

func TestInvalidAbsoluteArmingLeavesTheTimerUnchanged(t *testing.T) {
	for _, values := range []struct {
		name     string
		deadline timerfd.Instant
		interval time.Duration
		flags    timerfd.SetFlags
		err      error
	}{
		{name: "mismatched clock", deadline: timerfd.Instant{Clock: timerfd.Realtime}, err: os.ErrInvalid},
		{name: "unknown clock", deadline: timerfd.Instant{Clock: 255}, err: os.ErrInvalid},
		{name: "negative seconds", deadline: timerfd.Instant{Seconds: -1}, err: os.ErrInvalid},
		{name: "negative nanoseconds", deadline: timerfd.Instant{Nanoseconds: -1}, err: os.ErrInvalid},
		{name: "unnormalized nanoseconds", deadline: timerfd.Instant{Nanoseconds: 1e9}, err: os.ErrInvalid},
		{name: "negative interval", interval: -1, err: os.ErrInvalid},
		{name: "unknown flags", flags: 2, err: os.ErrInvalid},
		{name: "mixed flags", flags: 3, err: os.ErrInvalid},
		{name: "cancel monotonic", flags: timerfd.CancelOnSet, err: os.ErrInvalid},
		{name: "overflow seconds", deadline: timerfd.Instant{Seconds: math.MaxInt64}, err: unix.EOVERFLOW},
		{
			name:     "overflow nanoseconds",
			deadline: timerfd.Instant{Seconds: math.MaxInt64 / int64(time.Second), Nanoseconds: math.MaxInt64%int64(time.Second) + 1},
			err:      unix.EOVERFLOW,
		},
	} {
		t.Run(values.name, func(t *testing.T) {
			timer := test.Timer(t, timerfd.Monotonic)

			_, err := timer.SetAfter(5*time.Second, 7*time.Second)
			require.NoError(t, err)

			old, err := timer.SetAt(values.deadline, values.interval, values.flags)
			require.ErrorIs(t, err, values.err)
			require.Equal(t, timerfd.State{}, old, "failed setters must return a zero state")

			checkUnchangedTimer(t, timer)
		})
	}
}

func TestPastNormalizedInstantsExpireImmediately(t *testing.T) {
	timer := test.Timer(t, timerfd.Monotonic)
	_, err := timer.SetAt(timerfd.Instant{Seconds: 1, Nanoseconds: 999999999}, 0, 0)
	require.NoError(t, err)
	count := test.Value(t, timer.ReadExpirations)
	require.Equal(t, uint64(1), count)
}

func TestMaximumRelativeDurationsDoNotWrap(t *testing.T) {
	timer := test.Timer(t, timerfd.Monotonic)
	_, err := timer.SetAfter(time.Duration(math.MaxInt64), time.Duration(math.MaxInt64))
	require.NoError(t, err)
	state := test.Value(t, timer.State)
	require.Positive(t, state.Remaining)
	require.Equal(t, time.Duration(math.MaxInt64), state.Interval)
}

func TestMaximumAbsoluteInstantsDoNotWrap(t *testing.T) {
	timer := test.Timer(t, timerfd.Monotonic)
	deadline := timerfd.Instant{Seconds: math.MaxInt64 / int64(time.Second), Nanoseconds: math.MaxInt64 % int64(time.Second)}

	_, err := timer.SetAt(deadline, 0, 0)
	require.NoError(t, err)

	state := test.Value(t, timer.State)
	require.Positive(t, state.Remaining)
	require.Zero(t, state.Interval)
}

func TestBoottimeRejectsClockChangeCancellation(t *testing.T) {
	timer := test.Timer(t, timerfd.Boottime)
	_, err := timer.SetAfter(5*time.Second, 7*time.Second)
	require.NoError(t, err)
	_, err = timer.SetAt(timerfd.Instant{Clock: timerfd.Boottime}, 0, timerfd.CancelOnSet)
	require.ErrorIs(t, err, os.ErrInvalid)
	checkUnchangedTimer(t, timer)
}

func TestCreatedTimersAreNonblockingAndClosedOnExec(t *testing.T) {
	_, fd := test.TimerDescriptor(t, timerfd.Monotonic)
	test.CloseOnExec(t, fd)
	require.NotZero(t, test.Fcntl(t, fd, unix.F_GETFL)&unix.O_NONBLOCK)
	test.NoPendingRecord(t, fd)
}

func TestClearingAReadDeadlineRestoresUnreadExpirations(t *testing.T) {
	timer, fd := test.TimerDescriptor(t, timerfd.Monotonic)
	require.NoError(t, timer.SetReadDeadline(time.Now().Add(30*time.Millisecond)))
	count, err := timer.ReadExpirations()
	require.Zero(t, count)
	require.ErrorIs(t, err, os.ErrDeadlineExceeded)

	_, err = timer.SetAfter(time.Nanosecond, 0)
	require.NoError(t, err, "read deadlines must not prevent arming")
	test.Readable(t, fd)

	_, err = timer.State()
	require.NoError(t, err, "read deadlines must not prevent inspection")

	count, err = timer.ReadExpirations()
	require.Zero(t, count)
	require.ErrorIs(t, err, os.ErrDeadlineExceeded, "expired deadlines persist even when expirations are available")
	require.NoError(t, timer.SetReadDeadline(time.Time{}))

	count = test.Value(t, timer.ReadExpirations)
	require.Equal(t, uint64(1), count, "timed-out reads must not consume expirations")
	test.NoPendingRecord(t, fd)
}

func TestDisarmedReadsWaitUntilTheTimerIsArmed(t *testing.T) {
	timer := test.Timer(t, timerfd.Monotonic)
	done := test.StartRead(t, timer.ReadExpirations)

	_ = test.Value(t, timer.Disarm)

	test.Pending(t, done)

	_, err := timer.SetAfter(time.Nanosecond, 0)
	require.NoError(t, err)
	require.Equal(t, test.ReadResult[uint64]{Value: 1}, test.Receive(t, done))
}

func TestExpiredDeadlinesInterruptPendingExpirationReads(t *testing.T) {
	timer := test.Timer(t, timerfd.Monotonic)
	done := test.StartRead(t, timer.ReadExpirations)
	require.NoError(t, timer.SetReadDeadline(time.Now().Add(-time.Second)))
	result := test.Receive(t, done)
	require.Zero(t, result.Value)
	require.ErrorIs(t, result.Err, os.ErrDeadlineExceeded)
}

func TestClosingATimerInterruptsPendingExpirationReads(t *testing.T) {
	timer := test.Timer(t, timerfd.Monotonic)
	done := test.StartRead(t, timer.ReadExpirations)

	require.NoError(t, timer.Close())

	result := test.Receive(t, done)
	require.Zero(t, result.Value)
	require.ErrorIs(t, result.Err, os.ErrClosed)
	require.ErrorIs(t, timer.Close(), os.ErrClosed)
}

func TestArmingAndStateInspectionCanRunConcurrently(t *testing.T) {
	timer := test.Timer(t, timerfd.Monotonic)
	var workers sync.WaitGroup
	failures := make(chan error, 8)
	for range 8 {
		workers.Go(func() {
			for range 32 {
				if _, err := timer.SetAfter(time.Second, 2*time.Second); err != nil {
					failures <- err
					return
				}

				if _, err := timer.State(); err != nil {
					failures <- err
					return
				}
			}
		})
	}

	workers.Wait()
	close(failures)

	for err := range failures {
		require.NoError(t, err)
	}

	state := test.Value(t, timer.Disarm)
	test.ArmedTimer(t, state, time.Second, 2*time.Second)
}

func TestZeroTimersRejectOperations(t *testing.T) {
	var timer timerfd.Timer
	checkUnavailableTimer(t, &timer, os.ErrInvalid)
	_, err := timer.Read(nil)
	require.ErrorIs(t, err, os.ErrInvalid, "the zero timer has no descriptor to validate")
}

func TestClosedTimersRejectOperations(t *testing.T) {
	timer := test.Timer(t, timerfd.Monotonic)

	require.NoError(t, timer.Close())

	checkUnavailableTimer(t, timer, os.ErrClosed)

	_, err := timer.Read(nil)
	require.ErrorIs(t, err, unix.EINVAL, "invalid buffers take precedence after close")

	_, err = timer.SetAfter(-1, 0)
	require.ErrorIs(t, err, os.ErrInvalid, "invalid arming takes precedence after close")

	_, err = timer.SetAt(timerfd.Instant{Seconds: -1}, 0, 0)
	require.ErrorIs(t, err, os.ErrInvalid)
}

func TestInvalidClocksAndClosedTimersLeaveNoDescriptorsBehind(t *testing.T) {
	// Initialize the runtime poller before comparing descriptor counts.
	timer := test.Timer(t, timerfd.Monotonic)

	require.NoError(t, timer.Close())

	before := test.DescriptorCount(t)
	for _, scenario := range []struct {
		name  string
		clock timerfd.Clock
	}{
		{name: "first unsupported clock", clock: 3},
		{name: "next unsupported clock", clock: 4},
		{name: "maximum clock value", clock: 255},
	} {
		for range 16 {
			timer, err := timerfd.New(scenario.clock)
			require.Nil(t, timer, scenario.name)
			require.ErrorIs(t, err, os.ErrInvalid, scenario.name)

			now, err := timerfd.Now(scenario.clock)
			require.Equal(t, timerfd.Instant{}, now, scenario.name)
			require.ErrorIs(t, err, os.ErrInvalid, scenario.name)
		}
	}

	for range 32 {
		timer := test.Timer(t, timerfd.Monotonic)

		_, err := timer.SetAfter(time.Second, time.Second)
		require.NoError(t, err)
		require.NoError(t, timer.Close())
	}

	after := test.DescriptorCount(t)
	require.LessOrEqual(t, after, before, "rejected clocks and closed timers must not retain descriptors")
}

func checkUnchangedTimer(t *testing.T, timer *timerfd.Timer) {
	t.Helper()
	state := test.Value(t, timer.State)
	require.Positive(t, state.Remaining, "validation must not disarm the existing timer")
	require.LessOrEqual(t, state.Remaining, 5*time.Second)
	require.Equal(t, 7*time.Second, state.Interval, "validation must not modify the existing timer")
}

func checkUnavailableTimer(t *testing.T, timer *timerfd.Timer, expected error) {
	t.Helper()

	n, err := timer.Read(make([]byte, 8))
	require.Zero(t, n, "byte read")
	require.ErrorIs(t, err, expected, "byte read")

	count, err := timer.ReadExpirations()
	require.Zero(t, count, "expiration read")
	require.ErrorIs(t, err, expected, "expiration read")

	for _, scenario := range []struct {
		name      string
		operation func() (timerfd.State, error)
	}{
		{name: "state inspection", operation: timer.State},
		{name: "relative arming", operation: func() (timerfd.State, error) {
			return timer.SetAfter(time.Second, 0)
		}},

		{name: "absolute arming", operation: func() (timerfd.State, error) {
			return timer.SetAt(timerfd.Instant{}, 0, 0)
		}},

		{name: "disarming", operation: timer.Disarm},
	} {
		state, err := scenario.operation()
		require.Equal(t, timerfd.State{}, state, scenario.name)
		require.ErrorIs(t, err, expected, scenario.name)
	}

	require.ErrorIs(t, timer.SetReadDeadline(time.Time{}), expected, "read deadline")
	require.ErrorIs(t, timer.Close(), expected, "close")
}
