//go:build linux

package eventfd_test

import (
	"math"
	"os"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/epoll"
	"github.com/alexfalkowski/go-linux/eventfd"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

func TestRegistrationsObserveCountsWithoutConsumingThem(t *testing.T) {
	counter := test.Counter(t, 0, eventfd.Default)
	poller := test.Poller(t)
	registration, err := counter.Register(poller, epoll.Readable|epoll.OneShot)
	require.NoError(t, err)
	test.Cleanup(t, registration)
	events := make([]epoll.Event, 1)

	n, err := poller.Wait(events, 0)
	require.NoError(t, err)
	require.Zero(t, n)

	require.NoError(t, counter.Add(7))
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
	require.Equal(t, uint64(7), test.Value(t, counter.ReadValue), "readiness must not consume the count")

	n, err = poller.Wait(events, 0)
	require.NoError(t, err)
	require.Zero(t, n, "draining the counter must clear readability")
}

func TestRegistrationsReportCounterWriteCapacity(t *testing.T) {
	counter := test.Counter(t, 0, eventfd.Default)
	require.NoError(t, counter.Add(math.MaxUint64-1))
	poller := test.Poller(t)
	registration, err := counter.Register(poller, epoll.Writable)
	require.NoError(t, err)
	test.Cleanup(t, registration)
	events := make([]epoll.Event, 1)

	n, err := poller.Wait(events, 0)
	require.NoError(t, err)
	require.Zero(t, n, "a full counter must not report write capacity")

	require.Equal(t, uint64(math.MaxUint64-1), test.Value(t, counter.ReadValue))
	n, err = poller.Wait(events, time.Second)
	require.NoError(t, err)
	require.Equal(t, []epoll.Event{{Registration: registration, Events: epoll.Writable}}, events[:n])
}

func TestRegistrationClosureLeavesTheCounterUsable(t *testing.T) {
	counter := test.Counter(t, 0, eventfd.Default)
	poller := test.Poller(t)
	registration, err := counter.Register(poller, epoll.Readable)
	require.NoError(t, err)
	test.Cleanup(t, registration)

	require.NoError(t, registration.Close())
	require.NoError(t, counter.Add(3))
	require.Equal(t, uint64(3), test.Value(t, counter.ReadValue))
}

func TestPollerClosureLeavesTheCounterUsable(t *testing.T) {
	counter := test.Counter(t, 0, eventfd.Default)
	poller := test.Poller(t)
	registration, err := counter.Register(poller, epoll.Readable)
	require.NoError(t, err)
	test.Cleanup(t, registration)

	require.NoError(t, poller.Close())
	require.NoError(t, counter.Add(3))
	require.Equal(t, uint64(3), test.Value(t, counter.ReadValue))
	require.ErrorIs(t, registration.Modify(epoll.Readable), os.ErrClosed)
}

func TestCounterClosureLeavesItsRegistrationAlive(t *testing.T) {
	poller := test.Poller(t)
	before := test.Descriptors(t)
	counter := test.Counter(t, 2, eventfd.Default)
	registration, err := counter.Register(poller, epoll.Readable)
	require.NoError(t, err)
	test.Cleanup(t, registration)

	require.NoError(t, counter.Close())
	events := make([]epoll.Event, 1)
	n, err := poller.Wait(events, time.Second)
	require.NoError(t, err)
	require.Equal(t, []epoll.Event{{Registration: registration, Events: epoll.Readable}}, events[:n])

	require.NoError(t, registration.Close())
	require.Equal(t, before, test.Descriptors(t), "both counter references must be released")
}

func TestRejectedRegistrationsLeaveTheCounterUsable(t *testing.T) {
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
			counter := test.Counter(t, 5, eventfd.Default)
			before := test.Descriptors(t)

			registration, err := counter.Register(scenario.poller, scenario.interests)
			test.NoResource(t, registration, err, scenario.err)

			require.Equal(t, before, test.Descriptors(t), "rejected registration must not retain a duplicate")
			require.Equal(t, uint64(5), test.Value(t, counter.ReadValue))
		})
	}
}

func TestUnavailableCountersRejectRegistration(t *testing.T) {
	poller := test.Poller(t)
	closed := test.Counter(t, 0, eventfd.Default)
	require.NoError(t, closed.Close())

	for _, scenario := range []struct {
		name      string
		counter   *eventfd.Counter
		interests epoll.Events
		err       error
	}{
		{name: "zero counter", counter: new(eventfd.Counter), interests: epoll.Readable, err: os.ErrInvalid},
		{name: "closed counter", counter: closed, interests: epoll.Readable, err: os.ErrClosed},
		{name: "unknown interests before closed counter", counter: closed, interests: 1 << 26, err: os.ErrInvalid},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			registration, err := scenario.counter.Register(poller, scenario.interests)
			test.NoResource(t, registration, err, scenario.err)
		})
	}
}

func TestCounterClosureCanRaceWithRegistrationWithoutLeaking(t *testing.T) {
	poller := test.Poller(t)
	before := test.Descriptors(t)
	for range 25 {
		counter := test.Counter(t, 0, eventfd.Default)
		closed := make(chan error, 1)
		go func() {
			closed <- counter.Close()
		}()

		registration, err := counter.Register(poller, epoll.Readable)
		if err != nil {
			test.NoResource(t, registration, err, os.ErrClosed)
		} else {
			require.NoError(t, registration.Close())
		}
		require.NoError(t, test.Receive(t, closed))
	}

	require.Equal(t, before, test.Descriptors(t))
}
