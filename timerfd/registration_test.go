//go:build linux

package timerfd_test

import (
	"os"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/epoll"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/timerfd"
	"github.com/stretchr/testify/require"
)

func TestRegistrationsObserveExpirationsWithoutConsumingThem(t *testing.T) {
	timer := test.Timer(t, timerfd.Monotonic)
	poller := test.Poller(t)
	registration, err := timer.Register(poller, epoll.Readable|epoll.OneShot)
	require.NoError(t, err)
	test.Cleanup(t, registration)
	events := make([]epoll.Event, 1)

	n, err := poller.Wait(events, 0)
	require.NoError(t, err)
	require.Zero(t, n, "a disarmed timer must not be readable")

	_, err = timer.SetAfter(time.Millisecond, 0)
	require.NoError(t, err)
	n, err = poller.Wait(events, time.Second)
	require.NoError(t, err)
	require.Equal(t, []epoll.Event{{Registration: registration, Events: epoll.Readable}}, events[:n])

	n, err = poller.Wait(events, 0)
	require.NoError(t, err)
	require.Zero(t, n, "one-shot delivery must disable the registration")

	require.NoError(t, registration.Modify(epoll.Readable))
	n, err = poller.Wait(events, time.Second)
	require.NoError(t, err)
	require.Equal(t, []epoll.Event{{Registration: registration, Events: epoll.Readable}}, events[:n])
	require.Equal(t, uint64(1), test.Value(t, timer.ReadExpirations), "readiness must not consume expirations")

	n, err = poller.Wait(events, 0)
	require.NoError(t, err)
	require.Zero(t, n, "consuming expirations must clear readability")
}

func TestRegistrationClosureLeavesTheTimerUsable(t *testing.T) {
	timer := test.Timer(t, timerfd.Monotonic)
	poller := test.Poller(t)
	registration, err := timer.Register(poller, epoll.Readable)
	require.NoError(t, err)
	test.Cleanup(t, registration)

	require.NoError(t, registration.Close())
	_, err = timer.SetAfter(time.Nanosecond, 0)
	require.NoError(t, err)
	require.Equal(t, uint64(1), test.Value(t, timer.ReadExpirations))
}

func TestPollerClosureLeavesTheTimerUsable(t *testing.T) {
	timer := test.Timer(t, timerfd.Monotonic)
	poller := test.Poller(t)
	registration, err := timer.Register(poller, epoll.Readable)
	require.NoError(t, err)
	test.Cleanup(t, registration)

	require.NoError(t, poller.Close())
	_, err = timer.SetAfter(time.Nanosecond, 0)
	require.NoError(t, err)
	require.Equal(t, uint64(1), test.Value(t, timer.ReadExpirations))
	require.ErrorIs(t, registration.Modify(epoll.Readable), os.ErrClosed)
}

func TestTimerClosureLeavesItsRegistrationAndScheduleAlive(t *testing.T) {
	poller := test.Poller(t)
	before := test.Descriptors(t)
	timer := test.Timer(t, timerfd.Monotonic)
	registration, err := timer.Register(poller, epoll.Readable)
	require.NoError(t, err)
	test.Cleanup(t, registration)

	_, err = timer.SetAfter(time.Millisecond, 0)
	require.NoError(t, err)
	require.NoError(t, timer.Close())
	events := make([]epoll.Event, 1)
	n, err := poller.Wait(events, time.Second)
	require.NoError(t, err)
	require.Equal(t, []epoll.Event{{Registration: registration, Events: epoll.Readable}}, events[:n])

	require.NoError(t, registration.Close())
	require.Equal(t, before, test.Descriptors(t), "both timer references must be released")
}

func TestRejectedRegistrationsLeaveTheTimerUsable(t *testing.T) {
	poller := test.Poller(t)
	closed := test.Poller(t)
	require.NoError(t, closed.Close())

	for _, scenario := range []struct {
		name      string
		poller    *epoll.Poller
		interests epoll.Events
		err       error
	}{
		{name: "nil poller", interests: epoll.Readable, err: os.ErrInvalid},
		{name: "zero poller", poller: new(epoll.Poller), interests: epoll.Readable, err: os.ErrInvalid},
		{name: "closed poller", poller: closed, interests: epoll.Readable, err: os.ErrClosed},
		{name: "unknown interests", poller: poller, interests: 1 << 26, err: os.ErrInvalid},
		{name: "unknown interests before closed poller", poller: closed, interests: 1 << 26, err: os.ErrInvalid},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			timer := test.Timer(t, timerfd.Monotonic)
			_, err := timer.SetAfter(time.Nanosecond, 0)
			require.NoError(t, err)
			before := test.Descriptors(t)

			registration, err := timer.Register(scenario.poller, scenario.interests)
			test.NoResource(t, registration, err, scenario.err)

			require.Equal(t, before, test.Descriptors(t), "rejected registration must not retain a duplicate")
			require.Equal(t, uint64(1), test.Value(t, timer.ReadExpirations))
		})
	}
}

func TestUnavailableTimersRejectRegistration(t *testing.T) {
	poller := test.Poller(t)
	closed := test.Timer(t, timerfd.Monotonic)
	require.NoError(t, closed.Close())

	for _, scenario := range []struct {
		name      string
		timer     *timerfd.Timer
		interests epoll.Events
		err       error
	}{
		{name: "zero timer", timer: new(timerfd.Timer), interests: epoll.Readable, err: os.ErrInvalid},
		{name: "closed timer", timer: closed, interests: epoll.Readable, err: os.ErrClosed},
		{name: "unknown interests before closed timer", timer: closed, interests: 1 << 26, err: os.ErrInvalid},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			registration, err := scenario.timer.Register(poller, scenario.interests)
			test.NoResource(t, registration, err, scenario.err)
		})
	}
}

func TestTimerClosureCanRaceWithRegistrationWithoutLeaking(t *testing.T) {
	poller := test.Poller(t)
	before := test.Descriptors(t)
	for range 25 {
		timer := test.Timer(t, timerfd.Monotonic)
		closed := make(chan error, 1)
		go func() {
			closed <- timer.Close()
		}()

		registration, err := timer.Register(poller, epoll.Readable)
		if err != nil {
			test.NoResource(t, registration, err, os.ErrClosed)
		} else {
			require.NoError(t, registration.Close())
		}
		require.NoError(t, test.Receive(t, closed))
	}

	require.Equal(t, before, test.Descriptors(t))
}
