//go:build linux

package epoll_test

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/epoll"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestClosingTheSourceRetainsTheRegistrationUntilRemoval(t *testing.T) {
	poller := test.Poller(t)
	reader, writer := test.Pipe(t)
	registration := test.Registration(t, poller, writer, epoll.Writable)

	require.NoError(t, writer.Close())
	events := make([]epoll.Event, 1)
	n, err := poller.Wait(events, time.Second)
	require.NoError(t, err)
	require.Equal(t, []epoll.Event{{Registration: registration, Events: epoll.Writable}}, events[:n])

	pending := test.StartRead(t, func() (int, error) {
		var value [1]byte
		return reader.Read(value[:])
	})

	require.NoError(t, registration.Close())
	result := test.Receive(t, pending)
	require.ErrorIs(t, result.Err, io.EOF, "removal must close the last writer reference")
	require.Zero(t, result.Value)
}

func TestDescriptorReuseCannotRetargetAnExistingRegistration(t *testing.T) {
	// This focused kernel check establishes that the caller's original numeric
	// descriptor really is reused. Behavioral tests otherwise use only owners.
	poller := test.Poller(t)
	original, oldWriter := test.Pipe(t)
	oldRegistration := test.Registration(t, poller, original, epoll.Readable)
	originalFD := test.FileDescriptor(t, original)
	require.NoError(t, original.Close())

	replacement, newWriter := test.Pipe(t)
	require.Equal(t, originalFD, test.FileDescriptor(t, replacement), "fixture must reuse the original descriptor number")
	newRegistration := test.Registration(t, poller, replacement, epoll.Readable)
	events := make([]epoll.Event, 2)

	_, err := oldWriter.WriteString("old")
	require.NoError(t, err)
	n, err := poller.Wait(events, time.Second)
	require.NoError(t, err)
	require.Equal(t, []epoll.Event{{Registration: oldRegistration, Events: epoll.Readable}}, events[:n])
	oldEvent := events[0]

	require.NoError(t, oldRegistration.Modify(epoll.Readable|epoll.OneShot))
	require.NoError(t, oldRegistration.Close())
	_, err = newWriter.WriteString("new")
	require.NoError(t, err)
	n, err = poller.Wait(events, time.Second)
	require.NoError(t, err)
	require.Equal(t, []epoll.Event{{Registration: newRegistration, Events: epoll.Readable}}, events[:n])
	require.Same(t, oldRegistration, oldEvent.Registration, "already returned identity must remain stable")
	require.ErrorIs(t, oldEvent.Registration.Modify(epoll.Readable), os.ErrClosed)
}

func TestRegistrationPreservesSourceFlagsAndDeadlines(t *testing.T) {
	poller := test.Poller(t)
	reader, writer := test.Pipe(t)
	beforeFlags := test.FileFlags(t, reader)
	require.NoError(t, reader.SetReadDeadline(time.Now().Add(-time.Second)))

	before := test.Descriptors(t)
	registration := test.Registration(t, poller, reader, epoll.Readable)
	duplicate := test.NewDescriptor(t, before)
	test.CloseOnExec(t, duplicate)
	require.Equal(t, beforeFlags, test.FileFlags(t, reader), "registration must not change shared status flags")

	_, err := writer.WriteString("ready")
	require.NoError(t, err)
	events := make([]epoll.Event, 1)
	n, err := poller.Wait(events, time.Second)
	require.NoError(t, err, "source deadlines must not affect explicit readiness")
	require.Equal(t, []epoll.Event{{Registration: registration, Events: epoll.Readable}}, events[:n])
	var value [1]byte
	_, err = reader.Read(value[:])
	require.ErrorIs(t, err, os.ErrDeadlineExceeded, "registration must preserve the source deadline")
}

func TestSocketReadinessPreservesLinuxConditionBits(t *testing.T) {
	for _, scenario := range []struct {
		trigger   func(*testing.T, *os.File)
		name      string
		interests epoll.Events
	}{
		{name: "urgent input", interests: epoll.Priority, trigger: test.SendUrgent},
		{name: "peer write-side shutdown", interests: epoll.ReadHangup, trigger: test.ShutdownWrite},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			poller := test.Poller(t)
			source, peer := test.SocketPair(t)
			registration := test.Registration(t, poller, source, scenario.interests)

			scenario.trigger(t, peer)
			events := make([]epoll.Event, 1)
			n, err := poller.Wait(events, time.Second)
			require.NoError(t, err)
			require.Equal(t, []epoll.Event{{Registration: registration, Events: scenario.interests}}, events[:n])
		})
	}
}

func TestSourceClosureCanRaceWithRegistrationWithoutLeaking(t *testing.T) {
	poller := test.Poller(t)
	before := test.Descriptors(t)
	for range 25 {
		reader, writer := test.Pipe(t)
		closed := make(chan error, 1)
		go func() {
			closed <- reader.Close()
		}()

		registration, err := poller.Add(reader, epoll.Readable)
		if err != nil {
			test.NoResource(t, registration, err, os.ErrClosed)
		} else {
			require.NoError(t, registration.Close())
		}
		require.NoError(t, test.Receive(t, closed))
		require.NoError(t, writer.Close())
	}
	require.Equal(t, before, test.Descriptors(t))
}

func TestPollerDescriptorsHaveAtomicCloseOnExecAndAreReleased(t *testing.T) {
	reader, _ := test.Pipe(t)
	before := test.Descriptors(t)
	poller := test.Poller(t)
	test.CloseOnExec(t, test.NewEpollDescriptor(t, before))
	wakeFD := test.NewEventfdDescriptor(t, before)
	test.CloseOnExec(t, wakeFD)
	require.NotZero(t, test.Fcntl(t, wakeFD, unix.F_GETFL)&unix.O_NONBLOCK, "close wakeups must not block")

	test.Registration(t, poller, reader, epoll.Readable)
	require.NoError(t, poller.Close())
	require.Equal(t, before, test.Descriptors(t), "Close must release the set, wake file, and source duplicate")
}

func TestRepeatedRegistrationLifecyclesReleaseEveryDescriptor(t *testing.T) {
	reader, _ := test.Pipe(t)
	before := test.Descriptors(t)
	for range 25 {
		poller, err := epoll.New()
		require.NoError(t, err)
		registration, err := poller.Add(reader, epoll.Readable)
		require.NoError(t, err)
		require.NoError(t, registration.Modify(epoll.Readable|epoll.EdgeTriggered))
		require.NoError(t, registration.Close())
		require.NoError(t, poller.Close())
	}
	require.Equal(t, before, test.Descriptors(t))
}

func TestAcquisitionFailuresReleaseDescriptorsAndRetainBorrowedFiles(t *testing.T) {
	scenarios := map[string]struct {
		addSource   bool
		releaseSlot bool
	}{
		"epoll allocation":   {},
		"wake allocation":    {releaseSlot: true},
		"source duplication": {addSource: true},
	}
	if argument := os.Args[len(os.Args)-1]; strings.HasPrefix(argument, "epoll-limit=") {
		scenario, ok := scenarios[strings.TrimPrefix(argument, "epoll-limit=")]
		require.True(t, ok, "resource-limit subprocess must select a known scenario")
		checkAcquisitionFailure(t, scenario.addSource, scenario.releaseSlot)
		return
	}
	for name := range scenarios {
		t.Run(name, func(t *testing.T) {
			ctx, command := test.Command(t, "TestAcquisitionFailuresReleaseDescriptorsAndRetainBorrowedFiles", "-test.v", "--", "epoll-limit="+name)
			output, err := command.CombinedOutput()
			require.NoError(t, ctx.Err())
			require.NoError(t, err, "resource-limit subprocess: %s", output)
			require.Contains(t, string(output), "verified acquisition failure cleanup")
		})
	}
}

func checkAcquisitionFailure(t *testing.T, addSource, releaseSlot bool) {
	t.Helper()

	reader, writer := test.Pipe(t)
	var poller *epoll.Poller
	if addSource {
		poller = test.Poller(t)
	}
	freeSlot := test.ExhaustDescriptors(t)

	if poller != nil {
		registration, err := poller.Add(reader, epoll.Readable)
		test.NoResource(t, registration, err, unix.EMFILE)
		_, err = writer.WriteString("x")
		require.NoError(t, err)
		var value [1]byte
		_, err = io.ReadFull(reader, value[:])
		require.NoError(t, err, "failed registration must leave borrowed input usable")
	} else {
		slot := -1
		if releaseSlot {
			slot = freeSlot()
		}
		created, err := epoll.New()
		test.NoResource(t, created, err, unix.EMFILE)
		if slot >= 0 {
			require.Equal(t, slot, test.DescriptorSlot(t), "wake allocation failure must release the epoll descriptor")
		}
	}
	t.Log("verified acquisition failure cleanup")
}
