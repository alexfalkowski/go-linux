//go:build linux

package epoll_test

import (
	"io"
	"math"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/epoll"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

var (
	_ io.Closer = (*epoll.Poller)(nil)
	_ io.Closer = (*epoll.Registration)(nil)
)

func TestLevelReadinessRepeatsUntilTheInputIsConsumed(t *testing.T) {
	poller := test.Poller(t)
	reader, writer := test.Pipe(t)
	registration := test.Registration(t, poller, reader, epoll.Readable|epoll.LevelTriggered)
	sentinel := epoll.Event{Events: epoll.Priority}
	events := []epoll.Event{sentinel, sentinel}

	n, err := poller.Wait(events, 0)
	require.NoError(t, err)
	require.Zero(t, n, "empty pipe must not be readable")
	require.Equal(t, []epoll.Event{sentinel, sentinel}, events)

	_, err = writer.WriteString("ab")
	require.NoError(t, err)
	n, err = poller.Wait(events, time.Second)
	require.NoError(t, err)
	require.Equal(t, []epoll.Event{{Registration: registration, Events: epoll.Readable}}, events[:n])
	require.Equal(t, sentinel, events[1], "Wait must preserve the unused tail")

	var value [1]byte
	_, err = io.ReadFull(reader, value[:])
	require.NoError(t, err)
	require.Equal(t, byte('a'), value[0])
	n, err = poller.Wait(events, 0)
	require.NoError(t, err)
	require.Equal(t, []epoll.Event{{Registration: registration, Events: epoll.Readable}}, events[:n])

	_, err = io.ReadFull(reader, value[:])
	require.NoError(t, err)
	require.Equal(t, byte('b'), value[0])
	n, err = poller.Wait(events, 0)
	require.NoError(t, err)
	require.Zero(t, n, "consumed input must clear readiness")
}

func TestEdgeReadinessRequiresANewInputTransition(t *testing.T) {
	poller := test.Poller(t)
	reader, writer := test.Pipe(t)
	registration := test.Registration(t, poller, reader, epoll.Readable|epoll.EdgeTriggered)
	events := make([]epoll.Event, 1)

	_, err := writer.WriteString("ab")
	require.NoError(t, err)
	n, err := poller.Wait(events, time.Second)
	require.NoError(t, err)
	require.Equal(t, []epoll.Event{{Registration: registration, Events: epoll.Readable}}, events[:n])

	var value [1]byte
	_, err = io.ReadFull(reader, value[:])
	require.NoError(t, err)
	n, err = poller.Wait(events, 0)
	require.NoError(t, err)
	require.Zero(t, n, "partially consumed input must not create an edge")

	_, err = io.ReadFull(reader, value[:])
	require.NoError(t, err)
	_, err = writer.WriteString("c")
	require.NoError(t, err)
	n, err = poller.Wait(events, time.Second)
	require.NoError(t, err)
	require.Equal(t, []epoll.Event{{Registration: registration, Events: epoll.Readable}}, events[:n])
}

func TestOneShotDeliveryIsRearmedByModifyingTheRegistration(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		trigger epoll.Events
	}{
		{name: "level triggered", trigger: epoll.LevelTriggered},
		{name: "edge triggered", trigger: epoll.EdgeTriggered},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			poller := test.Poller(t)
			reader, writer := test.Pipe(t)
			interests := epoll.Readable | epoll.OneShot | scenario.trigger
			registration := test.Registration(t, poller, reader, interests)
			events := make([]epoll.Event, 1)

			_, err := writer.WriteString("ready")
			require.NoError(t, err)
			n, err := poller.Wait(events, time.Second)
			require.NoError(t, err)
			require.Equal(t, []epoll.Event{{Registration: registration, Events: epoll.Readable}}, events[:n])

			_, err = writer.WriteString("more")
			require.NoError(t, err)
			n, err = poller.Wait(events, 0)
			require.NoError(t, err)
			require.Zero(t, n, "disabled one-shot must suppress further delivery")

			require.NoError(t, registration.Modify(interests))
			n, err = poller.Wait(events, time.Second)
			require.NoError(t, err)
			require.Equal(t, []epoll.Event{{Registration: registration, Events: epoll.Readable}}, events[:n])
		})
	}
}

func TestModificationReplacesTheCompleteInterestMask(t *testing.T) {
	poller := test.Poller(t)
	reader, writer := test.Pipe(t)
	registration := test.Registration(t, poller, reader, epoll.Readable)
	events := make([]epoll.Event, 1)

	_, err := writer.WriteString("ready")
	require.NoError(t, err)
	for _, interests := range []epoll.Events{0, epoll.Writable} {
		require.NoError(t, registration.Modify(interests))
		n, err := poller.Wait(events, 0)
		require.NoError(t, err)
		require.Zero(t, n, "mask %v must replace Readable", interests)
	}

	require.NoError(t, registration.Modify(epoll.Readable))
	n, err := poller.Wait(events, time.Second)
	require.NoError(t, err)
	require.Equal(t, []epoll.Event{{Registration: registration, Events: epoll.Readable}}, events[:n])
}

func TestBatchEventsIdentifyIndependentRegistrations(t *testing.T) {
	poller := test.Poller(t)
	reader, writer := test.Pipe(t)
	first := test.Registration(t, poller, reader, epoll.Readable)
	second := test.Registration(t, poller, reader, epoll.Readable)
	output := test.Registration(t, poller, writer, epoll.Writable)

	_, err := writer.WriteString("ready")
	require.NoError(t, err)
	events := make([]epoll.Event, 4)
	n, err := poller.Wait(events, time.Second)
	require.NoError(t, err)
	require.ElementsMatch(t, []epoll.Event{
		{Registration: first, Events: epoll.Readable},
		{Registration: second, Events: epoll.Readable},
		{Registration: output, Events: epoll.Writable},
	}, events[:n])

	require.NoError(t, first.Close())
	n, err = poller.Wait(events, 0)
	require.NoError(t, err)
	require.ElementsMatch(t, []epoll.Event{
		{Registration: second, Events: epoll.Readable},
		{Registration: output, Events: epoll.Writable},
	}, events[:n])
}

func TestHangupAndErrorAreReportedWithoutBeingRequested(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		events epoll.Events
		writer bool
	}{
		{name: "reader observes peer closure", events: epoll.Hangup},
		{name: "writer observes missing readers", events: epoll.Error, writer: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			poller := test.Poller(t)
			source, peer := test.Pipe(t)
			if scenario.writer {
				source, peer = peer, source
			}
			registration := test.Registration(t, poller, source, 0)
			require.NoError(t, peer.Close())

			events := make([]epoll.Event, 1)
			n, err := poller.Wait(events, time.Second)
			require.NoError(t, err)
			require.Equal(t, []epoll.Event{{Registration: registration, Events: scenario.events}}, events[:n])
		})
	}
}

func TestRemovingARegistrationLeavesTheSourceUsable(t *testing.T) {
	poller := test.Poller(t)
	reader, writer := test.Pipe(t)
	registration := test.Registration(t, poller, reader, epoll.Readable)

	_, err := writer.WriteString("x")
	require.NoError(t, err)
	require.NoError(t, registration.Close())
	require.ErrorIs(t, registration.Close(), os.ErrClosed)
	require.ErrorIs(t, registration.Modify(epoll.Readable), os.ErrClosed)

	n, err := poller.Wait(make([]epoll.Event, 1), 0)
	require.NoError(t, err)
	require.Zero(t, n, "removed ready entry must not be delivered")
	var value [1]byte
	_, err = io.ReadFull(reader, value[:])
	require.NoError(t, err)
	require.Equal(t, byte('x'), value[0])
}

func TestAddingAReadySourceWakesAnEmptyWait(t *testing.T) {
	poller := test.Poller(t)
	reader, writer := test.Pipe(t)
	events := make([]epoll.Event, 1)
	pending := test.StartRead(t, func() (int, error) {
		return poller.Wait(events, -1)
	})

	_, err := writer.WriteString("ready")
	require.NoError(t, err)
	registration := test.Registration(t, poller, reader, epoll.Readable)

	result := test.Receive(t, pending)
	require.NoError(t, result.Err)
	require.Equal(t, []epoll.Event{{Registration: registration, Events: epoll.Readable}}, events[:result.Value])
}

func TestNewInputWakesARegisteredWait(t *testing.T) {
	poller := test.Poller(t)
	reader, writer := test.Pipe(t)
	registration := test.Registration(t, poller, reader, epoll.Readable)
	events := make([]epoll.Event, 1)
	pending := test.StartRead(t, func() (int, error) {
		return poller.Wait(events, -1)
	})

	_, err := writer.WriteString("ready")
	require.NoError(t, err)
	result := test.Receive(t, pending)
	require.NoError(t, result.Err)
	require.Equal(t, []epoll.Event{{Registration: registration, Events: epoll.Readable}}, events[:result.Value])
}

func TestClosingThePollerInterruptsEveryWaitAndReleasesRegistrations(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		timeout time.Duration
	}{
		{name: "indefinite waits", timeout: -1},
		{name: "waits beyond the syscall timeout range", timeout: math.MaxInt64},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			poller := test.Poller(t)
			reader, writer := test.Pipe(t)
			registration := test.Registration(t, poller, reader, epoll.Readable)
			results := make(chan error, 4)
			for range 4 {
				go func() {
					_, err := poller.Wait(make([]epoll.Event, 1), scenario.timeout)
					results <- err
				}()
			}
			test.Pending(t, results)

			require.NoError(t, poller.Close())
			for range 4 {
				require.ErrorIs(t, test.Receive(t, results), os.ErrClosed)
			}
			require.ErrorIs(t, registration.Close(), os.ErrClosed)
			require.ErrorIs(t, registration.Modify(epoll.Readable), os.ErrClosed)
			require.ErrorIs(t, poller.Close(), os.ErrClosed)
			_, err := poller.Wait(make([]epoll.Event, 1), 0)
			require.ErrorIs(t, err, os.ErrClosed)
			added, err := poller.Add(reader, epoll.Readable)
			test.NoResource(t, added, err, os.ErrClosed)

			_, err = writer.WriteString("x")
			require.NoError(t, err)
			var value [1]byte
			_, err = io.ReadFull(reader, value[:])
			require.NoError(t, err, "poller closure must leave borrowed files usable")
		})
	}
}

func TestTimeoutsReturnAnEmptyResultWithoutChangingTheBuffer(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		timeout time.Duration
	}{
		{name: "immediate poll"},
		{name: "submillisecond timeout", timeout: time.Nanosecond},
		{name: "bounded wait", timeout: 20 * time.Millisecond},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			poller := test.Poller(t)
			sentinel := epoll.Event{Events: epoll.Priority}
			events := []epoll.Event{sentinel}

			started := time.Now()
			n, err := poller.Wait(events, scenario.timeout)
			elapsed := time.Since(started)
			require.NoError(t, err)
			require.Zero(t, n)
			require.GreaterOrEqual(t, elapsed, scenario.timeout, "timeout must not expire early")
			require.Equal(t, []epoll.Event{sentinel}, events)
		})
	}
}

func TestInvalidArgumentsAndUnsupportedSourcesPreserveOwnership(t *testing.T) {
	poller := test.Poller(t)
	file := test.TempFile(t, 0)
	reader, _ := test.Pipe(t)
	closed, _ := test.Pipe(t)
	require.NoError(t, closed.Close())
	before := test.DescriptorCount(t)

	for _, scenario := range []struct {
		name      string
		file      *os.File
		interests epoll.Events
		err       error
	}{
		{name: "nil file", interests: epoll.Readable, err: os.ErrInvalid},
		{name: "unknown flag", file: reader, interests: 1 << 26, err: os.ErrInvalid},
		{name: "closed file", file: closed, interests: epoll.Readable, err: os.ErrClosed},
		{name: "ordinary file", file: file, interests: epoll.Readable, err: unix.EPERM},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			registration, err := poller.Add(scenario.file, scenario.interests)
			test.NoResource(t, registration, err, scenario.err)
		})
	}

	require.Equal(t, before, test.DescriptorCount(t), "failed additions must release every duplicate")
	_, err := file.Stat()
	require.NoError(t, err, "failed registration must not close a borrowed file")
	registration := test.Registration(t, poller, reader, epoll.Readable)
	require.ErrorIs(t, registration.Modify(1<<26), os.ErrInvalid)
	_, err = poller.Wait(nil, 0)
	require.ErrorIs(t, err, os.ErrInvalid)

	require.NoError(t, poller.Close())
	require.ErrorIs(t, registration.Modify(1<<26), os.ErrInvalid, "mask validation precedes closed state")
	_, err = poller.Wait(nil, 0)
	require.ErrorIs(t, err, os.ErrInvalid, "buffer validation precedes closed state")
}

func TestZeroOwnersRejectOperations(t *testing.T) {
	var poller epoll.Poller
	var registration epoll.Registration
	reader, _ := test.Pipe(t)

	added, err := poller.Add(reader, epoll.Readable)
	test.NoResource(t, added, err, os.ErrInvalid)
	_, err = poller.Wait(make([]epoll.Event, 1), 0)
	require.ErrorIs(t, err, os.ErrInvalid)
	require.ErrorIs(t, poller.Close(), os.ErrInvalid)
	require.ErrorIs(t, registration.Modify(epoll.Readable), os.ErrInvalid)
	require.ErrorIs(t, registration.Close(), os.ErrInvalid)
}

func TestConcurrentRegistrationChangesAndWaitsPreserveLifetime(t *testing.T) {
	poller := test.Poller(t)
	reader, writer := test.Pipe(t)
	_, err := writer.WriteString("ready")
	require.NoError(t, err)
	results := make(chan error, 4)
	pending := test.Start(t, func() error {
		for {
			_, err := poller.Wait(make([]epoll.Event, 1), -1)
			if err != nil {
				return err
			}
		}
	})

	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			for range 25 {
				registration, err := poller.Add(reader, epoll.Readable|epoll.OneShot)
				if err == nil {
					err = registration.Modify(epoll.Readable | epoll.EdgeTriggered)
				}
				if err == nil {
					err = registration.Close()
				}
				if err != nil {
					results <- err
					return
				}
			}
		})
	}
	workers.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	require.NoError(t, poller.Close())
	require.ErrorIs(t, test.Receive(t, pending), os.ErrClosed)
}
