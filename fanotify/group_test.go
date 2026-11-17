//go:build linux

package fanotify_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/fanotify"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

var _ io.ReaderAt = (*fanotify.Event)(nil)

var _ io.Closer = (*fanotify.Group)(nil)

func TestInvalidClassesReturnNoResource(t *testing.T) {
	before := test.Descriptors(t)
	for _, class := range []fanotify.Class{1, 8, 12, 0x200} {
		group, err := fanotify.New(class)
		require.Nil(t, group)
		require.ErrorIs(t, err, os.ErrInvalid)
	}

	require.Equal(t, before, test.Descriptors(t))
}

func TestZeroGroupRejectsOperations(t *testing.T) {
	group := new(fanotify.Group)
	require.ErrorIs(t, group.Close(), os.ErrInvalid)

	require.ErrorIs(t, group.AddMark("", fanotify.Open, 0), os.ErrInvalid)

	require.ErrorIs(t, group.RemoveMark("", fanotify.Open, 0), os.ErrInvalid)

	require.ErrorIs(t, group.Flush(0), os.ErrInvalid)

	require.ErrorIs(t, group.SetReadDeadline(time.Time{}), os.ErrInvalid)

	events, err := group.ReadEvents(make([]byte, 24))
	require.Nil(t, events)
	require.ErrorIs(t, err, os.ErrInvalid)
}

func TestZeroGroupIterationYieldsOneTerminalError(t *testing.T) {
	var group fanotify.Group

	var terminal int
	for event, err := range group.Events() {
		require.Nil(t, event)
		require.ErrorIs(t, err, os.ErrInvalid)

		terminal++
	}

	require.Equal(t, 1, terminal, "iteration must yield exactly one terminal error")
}

func TestZeroEventRejectsOperations(t *testing.T) {
	event := new(fanotify.Event)
	require.ErrorIs(t, event.Close(), os.ErrInvalid)

	require.ErrorIs(t, event.Respond(fanotify.Allow), os.ErrInvalid)

	_, err := event.Stat()
	require.ErrorIs(t, err, os.ErrInvalid)

	_, err = event.ReadAt(make([]byte, 1), 0)
	require.ErrorIs(t, err, os.ErrInvalid)
}

func TestRemovingMarksPreservesQueuedNotifications(t *testing.T) {
	if !test.FanotifyNamespace(t) {
		return
	}

	before := test.Descriptors(t)
	group, err := fanotify.New(fanotify.Notification)
	require.NoError(t, err)
	test.Cleanup(t, group)
	require.NoError(t, group.SetReadDeadline(time.Now().Add(5*time.Second)))

	path := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(path, []byte("contents"), 0o600))

	require.NoError(t, group.AddMark(path, fanotify.CloseWrite, 0))
	require.NoError(t, group.AddMark(path, fanotify.Open, 0))

	require.NoError(t, os.WriteFile(path, []byte("updated!"), 0o600))

	require.NoError(t, group.RemoveMark(path, fanotify.Open|fanotify.CloseWrite, 0))

	var observed fanotify.Mask
	for observed != fanotify.Open|fanotify.CloseWrite {
		events, err := group.ReadEvents(make([]byte, 24))
		require.NoError(t, err)
		require.Len(t, events, 1)

		event := events[0]
		test.Cleanup(t, event)
		observed |= event.Mask
		require.NoError(t, event.Close())
	}

	require.ErrorIs(t, group.RemoveMark(path, fanotify.Open, 0), test.ErrNotExist)

	require.NoError(t, group.SetReadDeadline(time.Now().Add(-time.Second)))

	_, err = group.ReadEvents(make([]byte, 4096))
	require.ErrorIs(t, err, os.ErrDeadlineExceeded)

	require.NoError(t, group.Close())
	require.ErrorIs(t, group.Close(), os.ErrClosed)
	require.Equal(t, before, test.Descriptors(t), "group and delivered event files must be released")
}

func TestShortNotificationBuffersPreserveQueuedEvent(t *testing.T) {
	if !test.FanotifyNamespace(t) {
		return
	}
	group, err := fanotify.New(fanotify.Notification)
	require.NoError(t, err)
	test.Cleanup(t, group)
	require.NoError(t, group.SetReadDeadline(time.Now().Add(5*time.Second)))
	path := test.TempFile(t, 0).Name()
	require.NoError(t, group.AddMark(path, fanotify.CloseWrite, 0))
	require.NoError(t, os.WriteFile(path, []byte("contents"), 0o600))

	for _, size := range []int{0, 1, 23} {
		events, err := group.ReadEvents(make([]byte, size))
		require.Nil(t, events)
		require.ErrorIs(t, err, test.ErrInvalidArgument)
	}

	events, err := group.ReadEvents(make([]byte, 24))
	require.NoError(t, err)
	require.Len(t, events, 1)
	test.Cleanup(t, events[0])
	require.Equal(t, fanotify.CloseWrite, events[0].Mask)
}

func TestNotificationEventProvidesFileAccessWithoutPermissionResponse(t *testing.T) {
	if !test.FanotifyNamespace(t) {
		return
	}
	group, err := fanotify.New(fanotify.Notification)
	require.NoError(t, err)
	test.Cleanup(t, group)
	require.NoError(t, group.SetReadDeadline(time.Now().Add(5*time.Second)))
	path := test.TempFile(t, 0).Name()
	require.NoError(t, group.AddMark(path, fanotify.CloseWrite, 0))
	require.NoError(t, os.WriteFile(path, []byte("updated!"), 0o600))

	events, err := group.ReadEvents(make([]byte, 24))
	require.NoError(t, err)
	require.Len(t, events, 1)
	event := events[0]
	test.Cleanup(t, event)
	require.Equal(t, os.Getpid(), event.PID)
	require.Empty(t, event.Info)
	require.Equal(t, []byte("updated!"), test.ReadAt(t, event, 0, 8))
	require.ErrorIs(t, event.Respond(fanotify.Allow), os.ErrInvalid)

	require.NoError(t, event.Close())
	require.ErrorIs(t, event.Close(), os.ErrClosed)
	_, err = event.Stat()
	require.ErrorIs(t, err, os.ErrClosed)
}

func TestGroupAndEventFilesAreCloseOnExec(t *testing.T) {
	if !test.FanotifyNamespace(t) {
		return
	}

	before := test.Descriptors(t)
	group, err := fanotify.New(fanotify.Notification)
	require.NoError(t, err)
	test.Cleanup(t, group)
	require.NoError(t, group.SetReadDeadline(time.Now().Add(5*time.Second)))

	fd := test.NewDescriptor(t, before)
	test.CloseOnExec(t, fd)
	require.True(t, test.DescriptorNonblocking(t, fd))

	path := test.TempFile(t, 4).Name()
	require.NoError(t, group.AddMark(path, fanotify.CloseWrite, 0))

	require.NoError(t, os.WriteFile(path, []byte("data"), 0o600))

	before = test.Descriptors(t)
	events, err := group.ReadEvents(make([]byte, 4096))
	require.NoError(t, err)
	require.Len(t, events, 1)
	test.Cleanup(t, events[0])

	fd = test.NewDescriptor(t, before)
	test.CloseOnExec(t, fd)
	require.True(t, test.DescriptorReadOnly(t, fd), "event files must be read-only")
}

func TestEventFileSurvivesUnlinkAndGroupClosure(t *testing.T) {
	if !test.FanotifyNamespace(t) {
		return
	}
	group, err := fanotify.New(fanotify.Notification)
	require.NoError(t, err)
	test.Cleanup(t, group)
	require.NoError(t, group.SetReadDeadline(time.Now().Add(5*time.Second)))
	path := test.TempFile(t, 4).Name()
	require.NoError(t, group.AddMark(path, fanotify.CloseWrite, 0))
	require.NoError(t, os.WriteFile(path, []byte("data"), 0o600))
	events, err := group.ReadEvents(make([]byte, 4096))
	require.NoError(t, err)
	require.Len(t, events, 1)
	test.Cleanup(t, events[0])

	info, err := events[0].Stat()
	require.NoError(t, err)
	require.Equal(t, int64(4), info.Size())

	require.NoError(t, os.Remove(path))

	require.NoError(t, group.Close())
	require.Equal(t, []byte("data"), test.ReadAt(t, events[0], 0, 4), "event files must survive unlink and group closure")
}

func TestKernelCreationFailureReturnsNoGroup(t *testing.T) {
	if !test.FanotifyNamespace(t) {
		return
	}

	test.Descriptors(t) // Initialize Go polling before descriptor exhaustion.

	freeSlot := test.ExhaustDescriptors(t)
	group, err := fanotify.New(fanotify.Notification)
	require.Nil(t, group)
	require.ErrorIs(t, err, test.ErrTooManyOpenFiles)

	freeSlot()
	group, err = fanotify.New(fanotify.Notification)
	require.NoError(t, err)

	require.NoError(t, group.Close())

	test.DescriptorSlot(t)
}

func TestPollRegistrationFailureReturnsNoGroupAndClosesDescriptor(t *testing.T) {
	if !test.FanotifyNamespace(t) {
		return
	}

	for _, scenario := range []struct {
		name  string
		class fanotify.Class
	}{
		{name: "notification", class: fanotify.Notification},
		{name: "content", class: fanotify.Content},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			group, err := fanotify.New(scenario.class)
			require.NoError(t, err)
			require.NoError(t, group.Close())

			test.RefusePollRegistration(t)
			before := test.DescriptorCount(t)

			group, err = fanotify.New(scenario.class)
			test.NoResource(t, group, err, os.ErrNoDeadline)
			require.Equal(t, before, test.DescriptorCount(t), "failed poll registration must close the acquired fanotify group")
		})
	}
}

func TestInvalidMarksAreRejected(t *testing.T) {
	if !test.FanotifyNamespace(t) {
		return
	}

	group, err := fanotify.New(fanotify.Notification)
	require.NoError(t, err)
	test.Cleanup(t, group)
	require.NoError(t, group.SetReadDeadline(time.Now().Add(5*time.Second)))
	for _, mask := range []fanotify.Mask{0, fanotify.OnDir, fanotify.QueueOverflow, fanotify.OpenPermission, 1 << 63} {
		require.ErrorIs(t, group.AddMark("", mask, 0), os.ErrInvalid, "mask %#x", mask)
	}

	for _, flags := range []fanotify.MarkFlags{1, fanotify.Mount | fanotify.Filesystem, 1 << 31} {
		require.ErrorIs(t, group.AddMark("", fanotify.Open, flags), os.ErrInvalid, "flags %#x", flags)
	}

	require.ErrorIs(t, group.AddMark("bad\x00path", fanotify.Open, 0), os.ErrInvalid)
	require.ErrorIs(t, group.AddMark("", fanotify.Open, 0), test.ErrNotExist)

	path := test.TempFile(t, 0).Name()
	require.ErrorIs(t, group.AddMark(path, fanotify.Open, fanotify.OnlyDir), test.ErrNotDirectory)

	require.ErrorIs(t, group.Flush(fanotify.OnlyDir), os.ErrInvalid)
}

func TestClosedGroupRejectsOperations(t *testing.T) {
	if !test.FanotifyNamespace(t) {
		return
	}
	group, err := fanotify.New(fanotify.Notification)
	require.NoError(t, err)
	test.Cleanup(t, group)
	require.NoError(t, group.SetReadDeadline(time.Now().Add(5*time.Second)))
	path := test.TempFile(t, 0).Name()

	require.NoError(t, group.Close())

	require.ErrorIs(t, group.AddMark(path, fanotify.Open, 0), os.ErrClosed)

	require.ErrorIs(t, group.RemoveMark(path, fanotify.Open, 0), os.ErrClosed)

	require.ErrorIs(t, group.Flush(0), os.ErrClosed)

	require.ErrorIs(t, group.SetReadDeadline(time.Time{}), os.ErrClosed)

	require.ErrorIs(t, group.AddMark(path, 0, 0), os.ErrInvalid)

	_, err = group.ReadEvents(make([]byte, 24))
	require.ErrorIs(t, err, os.ErrClosed)
}

func TestReadDeadlineInterruptsPendingRead(t *testing.T) {
	if !test.FanotifyNamespace(t) {
		return
	}

	group, err := fanotify.New(fanotify.Notification)
	require.NoError(t, err)
	test.Cleanup(t, group)
	require.NoError(t, group.SetReadDeadline(time.Now().Add(5*time.Second)))

	read := test.StartRead(t, func() ([]*fanotify.Event, error) {
		return group.ReadEvents(make([]byte, 4096))
	})
	require.NoError(t, group.SetReadDeadline(time.Now().Add(-time.Second)))
	require.ErrorIs(t, test.Receive(t, read).Err, os.ErrDeadlineExceeded)
}

func TestCloseInterruptsReadersWhileMarksAreFlushed(t *testing.T) {
	if !test.FanotifyNamespace(t) {
		return
	}
	group, err := fanotify.New(fanotify.Notification)
	require.NoError(t, err)
	test.Cleanup(t, group)
	require.NoError(t, group.SetReadDeadline(time.Now().Add(5*time.Second)))
	require.NoError(t, group.SetReadDeadline(time.Time{}))

	var readers []<-chan test.ReadResult[[]*fanotify.Event]
	for range 3 {
		readers = append(readers, test.StartRead(t, func() ([]*fanotify.Event, error) {
			return group.ReadEvents(make([]byte, 4096))
		}))
	}

	var workers sync.WaitGroup
	results := make(chan error, 120)
	for range 4 {
		workers.Go(func() {
			for range 30 {
				results <- group.Flush(0)
			}
		})
	}

	require.NoError(t, group.Close())

	workers.Wait()
	close(results)
	for err := range results {
		require.True(t, err == nil || errors.Is(err, os.ErrClosed), "flush racing Close: %v", err)
	}

	for _, result := range readers {
		require.ErrorIs(t, test.Receive(t, result).Err, os.ErrClosed)
	}
}

func TestMountAndFilesystemScopes(t *testing.T) {
	if !test.FanotifyNamespace(t) {
		return
	}

	for _, scenario := range []struct {
		name  string
		scope fanotify.MarkFlags
	}{{name: "mount", scope: fanotify.Mount}, {name: "filesystem", scope: fanotify.Filesystem}} {
		t.Run(scenario.name, func(t *testing.T) {
			scope := scenario.scope
			root := t.TempDir()
			test.AttachMount(t, test.Mount(t), nil, root, root)

			nested := filepath.Join(root, "nested")
			require.NoError(t, os.Mkdir(nested, 0o700))

			group, err := fanotify.New(fanotify.Notification)
			require.NoError(t, err)
			test.Cleanup(t, group)
			require.NoError(t, group.SetReadDeadline(time.Now().Add(5*time.Second)))

			require.NoError(t, group.AddMark(root, fanotify.CloseWrite, scope))

			require.NoError(t, os.WriteFile(filepath.Join(nested, "file"), nil, 0o600))

			events, err := group.ReadEvents(make([]byte, 4096))
			require.NoError(t, err)
			require.Len(t, events, 1)
			require.Equal(t, fanotify.CloseWrite, events[0].Mask)

			require.NoError(t, events[0].Close())

			require.NoError(t, group.Flush(scope))

			require.NoError(t, os.WriteFile(filepath.Join(nested, "later"), nil, 0o600))

			require.NoError(t, group.SetReadDeadline(time.Now().Add(20*time.Millisecond)))

			_, err = group.ReadEvents(make([]byte, 4096))
			require.ErrorIs(t, err, os.ErrDeadlineExceeded)
		})
	}
}
