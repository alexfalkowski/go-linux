//go:build linux

package timerfd_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/timerfd"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

var (
	_ io.ReadCloser = (*timerfd.Timer)(nil)
	_ syscall.Conn  = (*timerfd.Timer)(nil)
)

func TestClocks(t *testing.T) {
	for _, clock := range []struct {
		name  string
		clock timerfd.Clock
		id    int32
	}{
		{name: "monotonic", clock: timerfd.Monotonic, id: unix.CLOCK_MONOTONIC},
		{name: "realtime", clock: timerfd.Realtime, id: unix.CLOCK_REALTIME},
		{name: "boottime", clock: timerfd.Boottime, id: unix.CLOCK_BOOTTIME},
	} {
		t.Run(clock.name, func(t *testing.T) {
			timer := newTimer(t, clock.clock)
			state, err := timer.State()
			require.NoError(t, err)
			require.Equal(t, timerfd.State{}, state, "new timers must be disarmed")

			var before, after unix.Timespec
			require.NoError(t, unix.ClockGettime(clock.id, &before))
			now, err := timerfd.Now(clock.clock)
			require.NoError(t, err)
			require.NoError(t, unix.ClockGettime(clock.id, &after))
			require.Equal(t, clock.clock, now.Clock)
			reading := now.Seconds*1e9 + now.Nanoseconds
			require.GreaterOrEqual(t, reading, before.Nano(), "Now must read the selected Linux clock")
			require.LessOrEqual(t, reading, after.Nano(), "Now must read the selected Linux clock")
			require.GreaterOrEqual(t, now.Nanoseconds, int64(0))
			require.Less(t, now.Nanoseconds, int64(1e9))

			require.Contains(t, timerInfo(t, timer), fmt.Sprintf("clockid: %d\n", clock.id), "creation must use the selected clock even when boottime and monotonic readings coincide")
		})
	}
}

func TestRelativeOneShot(t *testing.T) {
	for _, clock := range []timerfd.Clock{timerfd.Monotonic, timerfd.Realtime, timerfd.Boottime} {
		t.Run(strconv.Itoa(int(clock)), func(t *testing.T) {
			timer := newTimer(t, clock)
			started := time.Now()
			old, err := timer.SetAfter(40*time.Millisecond, 0)
			require.NoError(t, err)
			require.Equal(t, timerfd.State{}, old)
			count, err := timer.ReadExpirations()
			require.NoError(t, err)
			require.Equal(t, uint64(1), count, "a one-shot expires exactly once")
			require.GreaterOrEqual(t, time.Since(started), 20*time.Millisecond, "relative arming must not expire immediately")
			checkEmptyTimer(t, timer)
			state, err := timer.State()
			require.NoError(t, err)
			require.Equal(t, timerfd.State{}, state, "consumed one-shots must remain disarmed")
		})
	}
}

func TestAbsoluteOneShot(t *testing.T) {
	for _, clock := range []timerfd.Clock{timerfd.Monotonic, timerfd.Realtime, timerfd.Boottime} {
		t.Run(strconv.Itoa(int(clock)), func(t *testing.T) {
			timer := newTimer(t, clock)
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
			require.Positive(t, old.Remaining)
			require.LessOrEqual(t, old.Remaining, 5*time.Second)
			require.Equal(t, 7*time.Second, old.Interval, "absolute setters must return the previous relative state")
			state, err := timer.State()
			require.NoError(t, err)
			require.Positive(t, state.Remaining)
			require.LessOrEqual(t, state.Remaining, time.Second, "absolute arming must use the clock's origin")
			require.Zero(t, state.Interval)
			count, err := timer.ReadExpirations()
			require.NoError(t, err)
			require.Equal(t, uint64(1), count)
			require.GreaterOrEqual(t, time.Since(started), 500*time.Millisecond, "future absolute deadlines must not expire immediately")
			checkEmptyTimer(t, timer)
		})
	}
}

func TestPeriodicCounts(t *testing.T) {
	timer := newTimer(t, timerfd.Monotonic)
	const period = 10 * time.Millisecond
	started := time.Now()
	_, err := timer.SetAfter(period, period)
	require.NoError(t, err)
	time.Sleep(65 * time.Millisecond)

	// Inspection must preserve accumulated expirations, including overruns.
	state, err := timer.State()
	require.NoError(t, err)
	require.Equal(t, period, state.Interval)
	count, err := timer.ReadExpirations()
	require.NoError(t, err)
	require.GreaterOrEqual(t, count, uint64(5), "periodic reads must include missed expirations")
	require.LessOrEqual(t, float64(count), float64(time.Since(started))/float64(period)+1, "decoded counts must agree with elapsed periods")
	count, err = timer.ReadExpirations()
	require.NoError(t, err)
	require.Positive(t, count, "periodic timers must continue after a read")
}

func TestAbsolutePeriodicAndCancelFlag(t *testing.T) {
	for _, clock := range []timerfd.Clock{timerfd.Monotonic, timerfd.Realtime, timerfd.Boottime} {
		t.Run(strconv.Itoa(int(clock)), func(t *testing.T) {
			timer := newTimer(t, clock)
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
			require.Contains(t, timerInfo(t, timer), fmt.Sprintf("settime flags: 0%o\n", options), "the kernel must retain the requested absolute/cancellation flags")
			for range 2 {
				count, err := timer.ReadExpirations()
				require.NoError(t, err)
				require.Positive(t, count, "absolute periodic timers must deliver counts repeatedly")
			}
			state, err := timer.Disarm()
			require.NoError(t, err)
			require.Equal(t, 10*time.Millisecond, state.Interval)
			checkEmptyTimer(t, timer)
		})
	}
}

func TestStateReplacementAndDisarm(t *testing.T) {
	timer := newTimer(t, timerfd.Monotonic)
	old, err := timer.SetAfter(5*time.Second, 7*time.Second)
	require.NoError(t, err)
	require.Equal(t, timerfd.State{}, old)
	state, err := timer.State()
	require.NoError(t, err)
	require.Positive(t, state.Remaining)
	require.LessOrEqual(t, state.Remaining, 5*time.Second)
	require.Equal(t, 7*time.Second, state.Interval)

	old, err = timer.SetAfter(6*time.Second, 8*time.Second)
	require.NoError(t, err)
	require.Positive(t, old.Remaining)
	require.LessOrEqual(t, old.Remaining, state.Remaining, "setters must return the previous remaining duration")
	require.Equal(t, 7*time.Second, old.Interval, "setters must return the previous interval")
	old, err = timer.Disarm()
	require.NoError(t, err)
	require.Positive(t, old.Remaining)
	require.LessOrEqual(t, old.Remaining, 6*time.Second)
	require.Equal(t, 8*time.Second, old.Interval)
	state, err = timer.State()
	require.NoError(t, err)
	require.Equal(t, timerfd.State{}, state)
	old, err = timer.Disarm()
	require.NoError(t, err)
	require.Equal(t, timerfd.State{}, old, "disarming an inactive timer is valid")
	checkEmptyTimer(t, timer)
}

func TestZeroInitialDisarms(t *testing.T) {
	for _, absolute := range []bool{false, true} {
		t.Run(strconv.FormatBool(absolute), func(t *testing.T) {
			timer := newTimer(t, timerfd.Monotonic)
			_, err := timer.SetAfter(time.Nanosecond, 0)
			require.NoError(t, err)
			waitReadable(t, timer)
			if absolute {
				_, err = timer.SetAt(timerfd.Instant{}, time.Second, 0)
			} else {
				_, err = timer.SetAfter(0, time.Second)
			}
			require.NoError(t, err)
			state, err := timer.State()
			require.NoError(t, err)
			require.Equal(t, timerfd.State{Interval: time.Second}, state, "zero initial time disarms while retaining the supplied interval")
			checkEmptyTimer(t, timer)
		})
	}
}

func TestRearmingClearsUnreadExpirations(t *testing.T) {
	timer := newTimer(t, timerfd.Monotonic)
	_, err := timer.SetAfter(time.Nanosecond, 0)
	require.NoError(t, err)
	waitReadable(t, timer)
	state, err := timer.State()
	require.NoError(t, err)
	require.Equal(t, timerfd.State{}, state, "expired one-shots can have unread expirations despite zero remaining time")
	_, err = timer.SetAfter(5*time.Second, 0)
	require.NoError(t, err)
	checkEmptyTimer(t, timer)
	_, err = timer.SetAfter(time.Nanosecond, 0)
	require.NoError(t, err)
	waitReadable(t, timer)
	_, err = timer.Disarm()
	require.NoError(t, err)
	checkEmptyTimer(t, timer)
}

func TestByteRecords(t *testing.T) {
	timer := newTimer(t, timerfd.Monotonic)
	_, err := timer.SetAfter(time.Nanosecond, 0)
	require.NoError(t, err)
	buffer := bytes.Repeat([]byte{0xff}, 16)
	n, err := timer.Read(buffer)
	require.NoError(t, err)
	require.Equal(t, 8, n, "reads must return one complete record")
	var expected [8]byte
	binary.NativeEndian.PutUint64(expected[:], 1)
	require.Equal(t, expected[:], buffer[:8], "byte counts must use native byte order")
	require.Equal(t, bytes.Repeat([]byte{0xff}, 8), buffer[8:], "reads must leave the buffer tail untouched")
	checkEmptyTimer(t, timer)
}

func TestInvalidReadBuffers(t *testing.T) {
	for size := range 8 {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			timer := newTimer(t, timerfd.Monotonic)
			_, err := timer.SetAfter(time.Nanosecond, 0)
			require.NoError(t, err)
			waitReadable(t, timer)
			n, err := timer.Read(make([]byte, size))
			require.Zero(t, n)
			require.ErrorIs(t, err, unix.EINVAL)
			var errno syscall.Errno
			require.ErrorAs(t, err, &errno)
			require.Equal(t, unix.EINVAL, errno)
			var pathErr *os.PathError
			require.ErrorAs(t, err, &pathErr)
			require.Equal(t, "read", pathErr.Op)
			count, err := timer.ReadExpirations()
			require.NoError(t, err)
			require.Equal(t, uint64(1), count, "invalid reads must not consume expirations")
		})
	}
}

func TestInvalidRelativeArming(t *testing.T) {
	for _, values := range []struct {
		name     string
		initial  time.Duration
		interval time.Duration
	}{
		{name: "negative initial", initial: -1},
		{name: "negative interval", initial: 1, interval: -1},
	} {
		t.Run(values.name, func(t *testing.T) {
			timer := newTimer(t, timerfd.Monotonic)
			_, err := timer.SetAfter(5*time.Second, 7*time.Second)
			require.NoError(t, err)
			old, err := timer.SetAfter(values.initial, values.interval)
			require.ErrorIs(t, err, os.ErrInvalid)
			require.Equal(t, timerfd.State{}, old)
			checkUnchangedTimer(t, timer)
		})
	}
}

func TestInvalidAbsoluteArming(t *testing.T) {
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
			timer := newTimer(t, timerfd.Monotonic)
			_, err := timer.SetAfter(5*time.Second, 7*time.Second)
			require.NoError(t, err)
			old, err := timer.SetAt(values.deadline, values.interval, values.flags)
			require.ErrorIs(t, err, values.err)
			require.Equal(t, timerfd.State{}, old, "failed setters must return a zero state")
			checkUnchangedTimer(t, timer)
		})
	}
}

func TestDurationLimits(t *testing.T) {
	timer := newTimer(t, timerfd.Monotonic)
	_, err := timer.SetAt(timerfd.Instant{Seconds: 1, Nanoseconds: 999999999}, 0, 0)
	require.NoError(t, err, "the largest normalized nanosecond field must be valid")
	count, err := timer.ReadExpirations()
	require.NoError(t, err)
	require.Equal(t, uint64(1), count, "a nonzero past one-shot must expire immediately")
	_, err = timer.SetAfter(time.Duration(math.MaxInt64), time.Duration(math.MaxInt64))
	require.NoError(t, err)
	state, err := timer.State()
	require.NoError(t, err)
	require.Positive(t, state.Remaining, "maximum relative values must not wrap")
	require.Equal(t, time.Duration(math.MaxInt64), state.Interval)
	_, err = timer.SetAt(timerfd.Instant{Seconds: math.MaxInt64 / int64(time.Second), Nanoseconds: math.MaxInt64 % int64(time.Second)}, 0, 0)
	require.NoError(t, err, "the exact absolute nanosecond limit must remain valid")
	state, err = timer.State()
	require.NoError(t, err)
	require.Positive(t, state.Remaining, "maximum absolute values must not wrap")
	require.Zero(t, state.Interval)
}

func TestBoottimeRejectsCancellation(t *testing.T) {
	timer := newTimer(t, timerfd.Boottime)
	_, err := timer.SetAfter(5*time.Second, 7*time.Second)
	require.NoError(t, err)
	_, err = timer.SetAt(timerfd.Instant{Clock: timerfd.Boottime}, 0, timerfd.CancelOnSet)
	require.ErrorIs(t, err, os.ErrInvalid)
	checkUnchangedTimer(t, timer)
}

func TestCreationFlagsAndRawAccess(t *testing.T) {
	timer := newTimer(t, timerfd.Monotonic)
	var connection syscall.Conn = timer
	conn, err := connection.SyscallConn()
	require.NoError(t, err)
	var descriptorFlags, statusFlags int
	var descriptorErr, statusErr error
	err = conn.Control(func(fd uintptr) {
		descriptorFlags, descriptorErr = unix.FcntlInt(fd, unix.F_GETFD, 0)
		statusFlags, statusErr = unix.FcntlInt(fd, unix.F_GETFL, 0)
	})
	require.NoError(t, err)
	require.NoError(t, descriptorErr)
	require.NoError(t, statusErr)
	require.NotZero(t, descriptorFlags&unix.FD_CLOEXEC, "timers must not leak across exec")
	require.NotZero(t, statusFlags&unix.O_NONBLOCK, "borrowed descriptors must stay nonblocking")
	checkEmptyTimer(t, timer)
}

func TestReadDeadlineAndReset(t *testing.T) {
	timer := newTimer(t, timerfd.Monotonic)
	require.NoError(t, timer.SetReadDeadline(time.Now().Add(30*time.Millisecond)))
	count, err := timer.ReadExpirations()
	require.Zero(t, count)
	require.ErrorIs(t, err, os.ErrDeadlineExceeded)

	_, err = timer.SetAfter(time.Nanosecond, 0)
	require.NoError(t, err, "read deadlines must not prevent arming")
	waitReadable(t, timer)
	_, err = timer.State()
	require.NoError(t, err, "read deadlines must not prevent inspection")
	count, err = timer.ReadExpirations()
	require.Zero(t, count)
	require.ErrorIs(t, err, os.ErrDeadlineExceeded, "expired deadlines persist even when expirations are available")
	require.NoError(t, timer.SetReadDeadline(time.Time{}))
	count, err = timer.ReadExpirations()
	require.NoError(t, err)
	require.Equal(t, uint64(1), count, "timed-out reads must not consume expirations")
	checkEmptyTimer(t, timer)
}

func TestReadWaitsForArming(t *testing.T) {
	timer := newTimer(t, timerfd.Monotonic)
	done := startWaiting(t, timer)
	_, err := timer.Disarm()
	require.NoError(t, err)
	select {
	case result := <-done:
		t.Fatalf("disarming must not complete a waiting read: %+v", result)
	case <-time.After(20 * time.Millisecond):
	}
	_, err = timer.SetAfter(time.Nanosecond, 0)
	require.NoError(t, err)
	result := awaitResult(t, done)
	require.NoError(t, result.err)
	require.Equal(t, uint64(1), result.count)
}

func TestDeadlineInterruptsPendingRead(t *testing.T) {
	timer := newTimer(t, timerfd.Monotonic)
	done := startWaiting(t, timer)
	require.NoError(t, timer.SetReadDeadline(time.Now().Add(-time.Second)))
	result := awaitResult(t, done)
	require.Zero(t, result.count)
	require.ErrorIs(t, result.err, os.ErrDeadlineExceeded)
}

func TestCloseInterruptsPendingRead(t *testing.T) {
	timer := newTimer(t, timerfd.Monotonic)
	done := startWaiting(t, timer)
	require.NoError(t, timer.Close())
	result := awaitResult(t, done)
	require.Zero(t, result.count)
	require.ErrorIs(t, result.err, os.ErrClosed)
	require.ErrorIs(t, timer.Close(), os.ErrClosed)
}

func TestConcurrentArmingAndState(t *testing.T) {
	timer := newTimer(t, timerfd.Monotonic)
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
	state, err := timer.Disarm()
	require.NoError(t, err)
	require.Positive(t, state.Remaining)
	require.LessOrEqual(t, state.Remaining, time.Second)
	require.Equal(t, 2*time.Second, state.Interval)
}

func TestZeroTimer(t *testing.T) {
	var timer timerfd.Timer
	checkUnavailableTimer(t, &timer, os.ErrInvalid)
	conn, err := timer.SyscallConn()
	require.Nil(t, conn)
	require.ErrorIs(t, err, os.ErrInvalid)
	_, err = timer.Read(nil)
	require.ErrorIs(t, err, os.ErrInvalid, "the zero timer has no descriptor to validate")
}

func TestClosedTimer(t *testing.T) {
	timer := newTimer(t, timerfd.Monotonic)
	conn, err := timer.SyscallConn()
	require.NoError(t, err)
	require.NoError(t, timer.Close())
	checkUnavailableTimer(t, timer, os.ErrClosed)
	called := false
	err = conn.Control(func(uintptr) { called = true })
	require.Error(t, err)
	require.False(t, called, "raw callbacks must not receive a closed or reused descriptor")
	_, err = timer.Read(nil)
	require.ErrorIs(t, err, unix.EINVAL, "invalid buffers take precedence after close")
	_, err = timer.SetAfter(-1, 0)
	require.ErrorIs(t, err, os.ErrInvalid, "invalid arming takes precedence after close")
	_, err = timer.SetAt(timerfd.Instant{Seconds: -1}, 0, 0)
	require.ErrorIs(t, err, os.ErrInvalid)
}

func TestInvalidClocksAndDescriptorCleanup(t *testing.T) {
	// Initialize the runtime poller before comparing descriptor counts.
	timer := newTimer(t, timerfd.Monotonic)
	require.NoError(t, timer.Close())
	before, err := os.ReadDir("/proc/self/fd")
	require.NoError(t, err)
	for _, clock := range []timerfd.Clock{3, 4, 255} {
		for range 16 {
			timer, err := timerfd.New(clock)
			require.Nil(t, timer)
			require.ErrorIs(t, err, os.ErrInvalid)
			now, err := timerfd.Now(clock)
			require.Equal(t, timerfd.Instant{}, now)
			require.ErrorIs(t, err, os.ErrInvalid)
		}
	}
	for range 32 {
		timer := newTimer(t, timerfd.Monotonic)
		_, err := timer.SetAfter(time.Second, time.Second)
		require.NoError(t, err)
		require.NoError(t, timer.Close())
	}
	after, err := os.ReadDir("/proc/self/fd")
	require.NoError(t, err)
	require.LessOrEqual(t, len(after), len(before), "rejected clocks and closed timers must not retain descriptors")
}

func newTimer(t *testing.T, clock timerfd.Clock) *timerfd.Timer {
	t.Helper()
	timer, err := timerfd.New(clock)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := timer.Close(); !errors.Is(err, os.ErrClosed) {
			require.NoError(t, err)
		}
	})
	require.NoError(t, timer.SetReadDeadline(time.Now().Add(5*time.Second)), "bound unexpected I/O waits")
	return timer
}

func timerInfo(t *testing.T, timer *timerfd.Timer) string {
	t.Helper()
	conn, err := timer.SyscallConn()
	require.NoError(t, err)
	var info []byte
	var infoErr error
	err = conn.Control(func(fd uintptr) {
		info, infoErr = os.ReadFile(fmt.Sprintf("/proc/self/fdinfo/%d", fd))
	})
	require.NoError(t, err)
	require.NoError(t, infoErr)
	return string(info)
}

func checkUnchangedTimer(t *testing.T, timer *timerfd.Timer) {
	t.Helper()
	state, err := timer.State()
	require.NoError(t, err)
	require.Positive(t, state.Remaining, "validation must not disarm the existing timer")
	require.LessOrEqual(t, state.Remaining, 5*time.Second)
	require.Equal(t, 7*time.Second, state.Interval, "validation must not modify the existing timer")
}

func checkEmptyTimer(t *testing.T, timer *timerfd.Timer) {
	t.Helper()
	conn, err := timer.SyscallConn()
	require.NoError(t, err)
	var record [8]byte
	var readErr error
	err = conn.Control(func(fd uintptr) { _, readErr = unix.Read(int(fd), record[:]) })
	require.NoError(t, err)
	require.ErrorIs(t, readErr, unix.EAGAIN, "the kernel must have no pending expirations")
}

func waitReadable(t *testing.T, timer *timerfd.Timer) {
	t.Helper()
	conn, err := timer.SyscallConn()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		var ready int
		var pollErr error
		err := conn.Control(func(fd uintptr) {
			if fd > math.MaxInt32 {
				t.Fatal("file descriptor exceeds the Linux pollfd range")
				return
			}
			ready, pollErr = unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, 0)
		})
		require.NoError(t, err)
		require.NoError(t, pollErr)
		return ready == 1
	}, 5*time.Second, time.Millisecond, "timer must become readable without consuming its record")
}

func startWaiting(t *testing.T, timer *timerfd.Timer) <-chan readResult {
	t.Helper()
	done := make(chan readResult, 1)
	go func() {
		count, err := timer.ReadExpirations()
		done <- readResult{count: count, err: err}
	}()
	select {
	case result := <-done:
		t.Fatalf("read returned before it could be unblocked: %+v", result)
	case <-time.After(20 * time.Millisecond):
	}
	return done
}

func awaitResult(t *testing.T, done <-chan readResult) readResult {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("read did not wake")
		return readResult{}
	}
}

func checkUnavailableTimer(t *testing.T, timer *timerfd.Timer, expected error) {
	t.Helper()
	n, err := timer.Read(make([]byte, 8))
	require.Zero(t, n)
	require.ErrorIs(t, err, expected)
	count, err := timer.ReadExpirations()
	require.Zero(t, count)
	require.ErrorIs(t, err, expected)
	state, err := timer.State()
	require.Equal(t, timerfd.State{}, state)
	require.ErrorIs(t, err, expected)
	state, err = timer.SetAfter(time.Second, 0)
	require.Equal(t, timerfd.State{}, state)
	require.ErrorIs(t, err, expected)
	state, err = timer.SetAt(timerfd.Instant{}, 0, 0)
	require.Equal(t, timerfd.State{}, state)
	require.ErrorIs(t, err, expected)
	state, err = timer.Disarm()
	require.Equal(t, timerfd.State{}, state)
	require.ErrorIs(t, err, expected)
	require.ErrorIs(t, timer.SetReadDeadline(time.Time{}), expected)
	require.ErrorIs(t, timer.Close(), expected)
}

type readResult struct {
	err   error
	count uint64
}
