//go:build linux

package inotify_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/inotify"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

func TestEventSequenceResumesAfterBreakAndPreservesNames(t *testing.T) {
	watcher := test.Watcher(t)
	directory := t.TempDir()
	id, err := watcher.AddWatch(directory, inotify.Create)
	require.NoError(t, err)
	sequence := watcher.Events()
	for _, name := range []string{"first", "second"} {
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), nil, 0o600))
	}

	var actual []inotify.Event
	for event, err := range sequence {
		require.NoError(t, err)
		actual = append(actual, event)
		break
	}
	require.NoError(t, os.WriteFile(filepath.Join(directory, "third"), nil, 0o600))
	for event, err := range sequence {
		require.NoError(t, err)
		actual = append(actual, event)
		if len(actual) == 3 {
			break
		}
	}
	require.Equal(t, []inotify.Event{
		{Watch: id, Mask: inotify.Create, Name: "first"},
		{Watch: id, Mask: inotify.Create, Name: "second"},
		{Watch: id, Mask: inotify.Create, Name: "third"},
	}, actual, "resuming must retain the unread batch and names must survive another read")

	require.NoError(t, os.WriteFile(filepath.Join(directory, "later"), nil, 0o600))
	events, err := watcher.ReadEvents(make([]byte, 4096))
	require.NoError(t, err)
	require.Equal(t, []inotify.Event{{Watch: id, Mask: inotify.Create, Name: "later"}}, events,
		"breaking iteration must leave the watcher caller-owned and usable")
}

func TestEventSequencePreservesBufferedEventsAfterClose(t *testing.T) {
	watcher := test.Watcher(t)
	directory := t.TempDir()
	id, err := watcher.AddWatch(directory, inotify.Create)
	require.NoError(t, err)
	for _, name := range []string{"first", "second"} {
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), nil, 0o600))
	}
	sequence := watcher.Events()
	for event, err := range sequence {
		require.NoError(t, err)
		require.Equal(t, inotify.Event{Watch: id, Mask: inotify.Create, Name: "first"}, event)
		break
	}
	require.NoError(t, watcher.Close())

	var events []inotify.Event
	var failures []error
	for event, err := range sequence {
		if err != nil {
			require.Equal(t, inotify.Event{}, event)
			failures = append(failures, err)
			continue
		}
		events = append(events, event)
	}
	require.Equal(t, []inotify.Event{{Watch: id, Mask: inotify.Create, Name: "second"}}, events)
	require.Len(t, failures, 1, "a terminal error must be yielded exactly once")
	require.ErrorIs(t, failures[0], os.ErrClosed)
	for event, err := range sequence {
		require.FailNow(t, "an exhausted sequence must not yield again", "event=%+v error=%v", event, err)
	}
}

func TestEventSequenceReportsErrorsAndRequiresANewSequenceAfterTimeout(t *testing.T) {
	watcher := test.Watcher(t)
	require.NoError(t, watcher.SetReadDeadline(time.Now().Add(-time.Second)))
	sequence := watcher.Events()
	var failures []error
	for event, err := range sequence {
		require.Equal(t, inotify.Event{}, event)
		failures = append(failures, err)
	}
	require.Len(t, failures, 1)
	require.ErrorIs(t, failures[0], os.ErrDeadlineExceeded)

	require.NoError(t, watcher.SetReadDeadline(time.Now().Add(5*time.Second)))
	directory := t.TempDir()
	id, err := watcher.AddWatch(directory, inotify.Create)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "ready"), nil, 0o600))
	for event, err := range sequence {
		require.FailNow(t, "renewing a deadline must not restart an exhausted sequence", "event=%+v error=%v", event, err)
	}
	var events []inotify.Event
	for event, err := range watcher.Events() {
		require.NoError(t, err)
		events = append(events, event)
		break
	}
	require.Equal(t, []inotify.Event{{Watch: id, Mask: inotify.Create, Name: "ready"}}, events)
}

func TestEventSequenceReturnsRemovalAsAnEvent(t *testing.T) {
	watcher := test.Watcher(t)
	directory := t.TempDir()
	id, err := watcher.AddWatch(directory, inotify.Create|inotify.OneShot)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "child"), nil, 0o600))
	var events []inotify.Event
	for event, err := range watcher.Events() {
		require.NoError(t, err)
		events = append(events, event)
		if event.Mask&inotify.Ignored != 0 {
			break
		}
	}
	require.Equal(t, []inotify.Event{
		{Watch: id, Mask: inotify.Create, Name: "child"},
		{Watch: id, Mask: inotify.Ignored},
	}, events)
}

func TestCloseInterruptsBlockedIteration(t *testing.T) {
	watcher := test.Watcher(t)
	pending := test.StartRead(t, func() (inotify.Event, error) {
		for event, err := range watcher.Events() {
			return event, err
		}
		return inotify.Event{}, nil
	})
	require.NoError(t, watcher.Close())
	result := test.Receive(t, pending)
	require.ErrorIs(t, result.Err, os.ErrClosed)
	require.Equal(t, inotify.Event{}, result.Value)
}

func TestZeroWatcherIterationReturnsInvalid(t *testing.T) {
	var watcher inotify.Watcher
	var failures []error
	for event, err := range watcher.Events() {
		require.Equal(t, inotify.Event{}, event)
		failures = append(failures, err)
	}
	require.Len(t, failures, 1)
	require.ErrorIs(t, failures[0], os.ErrInvalid)
}
