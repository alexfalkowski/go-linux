//go:build linux

package fanotify_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/fanotify"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestPermissionResponsesAndAbandonedEvents(t *testing.T) {
	if !test.FanotifyNamespace(t) {
		return
	}

	for _, scenario := range []struct {
		name     string
		decision fanotify.Decision
	}{{name: "allow", decision: fanotify.Allow}, {name: "deny", decision: fanotify.Deny}, {name: "abandoned"}} {
		t.Run(scenario.name, func(t *testing.T) {
			group := test.Value(t, func() (*fanotify.Group, error) {
				return fanotify.New(fanotify.Content)
			})
			test.Cleanup(t, group)
			require.NoError(t, group.SetReadDeadline(time.Now().Add(5*time.Second)))

			path := filepath.Join(t.TempDir(), "file")
			require.NoError(t, os.WriteFile(path, []byte("scan me"), 0o600))

			require.NoError(t, group.AddMark(path, fanotify.OpenPermission, 0))

			opened := test.StartRead(t, func() (*os.File, error) {
				return os.Open(path)
			})
			events, err := group.ReadEvents(make([]byte, 4096))
			require.NoError(t, err)
			require.Len(t, events, 1)

			event := events[0]
			test.Cleanup(t, event)
			require.Equal(t, fanotify.OpenPermission, event.Mask)
			require.Equal(t, []byte("scan me"), test.ReadAt(t, event, 0, 7))

			require.ErrorIs(t, event.Respond(0), os.ErrInvalid)

			test.Pending(t, opened)
			if scenario.decision != 0 {
				require.NoError(t, event.Respond(scenario.decision))
				require.ErrorIs(t, event.Respond(fanotify.Allow), os.ErrInvalid)
			}

			require.NoError(t, event.Close())

			result := test.Receive(t, opened)
			if scenario.decision == fanotify.Allow {
				require.NoError(t, result.Err)

				require.NoError(t, result.Value.Close())
			} else {
				require.Nil(t, result.Value)
				require.ErrorIs(t, result.Err, unix.EPERM)
			}
		})
	}
}

func TestAccessPermissionAndGroupClosure(t *testing.T) {
	if !test.FanotifyNamespace(t) {
		return
	}

	group := test.Value(t, func() (*fanotify.Group, error) {
		return fanotify.New(fanotify.Content)
	})
	test.Cleanup(t, group)
	require.NoError(t, group.SetReadDeadline(time.Now().Add(5*time.Second)))

	path := test.TempFile(t, 1).Name()
	require.NoError(t, group.AddMark(path, fanotify.AccessPermission, 0))

	// Open after marking: Linux selects read-permission notifications at open time.
	file := test.OpenFile(t, path)
	read := test.StartRead(t, func() (int, error) {
		return file.Read(make([]byte, 1))
	})
	events, err := group.ReadEvents(make([]byte, 4096))
	require.NoError(t, err)
	require.Len(t, events, 1)

	event := events[0]
	test.Cleanup(t, event)
	require.Equal(t, fanotify.AccessPermission, event.Mask)
	require.Equal(t, []byte{0}, test.ReadAt(t, event, 0, 1), "event-file inspection must not recursively request permission")

	require.NoError(t, group.Close())

	result := test.Receive(t, read)
	require.NoError(t, result.Err, "Linux permits outstanding requests when the group closes")
	require.Equal(t, 1, result.Value)

	_, err = event.Stat()
	require.NoError(t, err, "transferred event files outlive their group")

	require.ErrorIs(t, event.Close(), os.ErrClosed, "failed deny must still consume the event")
	require.ErrorIs(t, event.Close(), os.ErrClosed)
}

func TestExecOpenPermissionAndNotification(t *testing.T) {
	if !test.FanotifyNamespace(t) {
		return
	}

	path := test.ExecutableCopy(t)
	group := test.Value(t, func() (*fanotify.Group, error) {
		return fanotify.New(fanotify.Content)
	})
	test.Cleanup(t, group)
	require.NoError(t, group.SetReadDeadline(time.Now().Add(5*time.Second)))

	require.NoError(t, group.AddMark(path, fanotify.OpenExecPermission|fanotify.OpenExec, 0))

	ctx, command := test.Command(t, "TestZeroOwnersRejectOperations")
	command.Path, command.Args[0] = path, path
	completed := test.StartRead(t, command.CombinedOutput)
	events, err := group.ReadEvents(make([]byte, 4096))
	require.NoError(t, err)
	require.Len(t, events, 1)
	test.Cleanup(t, events[0])
	require.Equal(t, fanotify.OpenExecPermission, events[0].Mask)

	require.NoError(t, events[0].Respond(fanotify.Allow))

	require.NoError(t, events[0].Close())

	events, err = group.ReadEvents(make([]byte, 4096))
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, fanotify.OpenExec, events[0].Mask)

	require.NoError(t, events[0].Close())

	result := test.Receive(t, completed)
	require.NoError(t, result.Err, "exec-open fixture: %s", result.Value)
	require.NoError(t, ctx.Err())
}

func TestIterationClosesCurrentAndUnreadFilesOnBreakAndPanic(t *testing.T) {
	if !test.FanotifyNamespace(t) {
		return
	}

	for _, scenario := range []string{"break", "panic"} {
		t.Run(scenario, func(t *testing.T) {
			group := test.Value(t, func() (*fanotify.Group, error) {
				return fanotify.New(fanotify.Notification)
			})
			test.Cleanup(t, group)
			require.NoError(t, group.SetReadDeadline(time.Now().Add(5*time.Second)))

			root := t.TempDir()
			require.NoError(t, group.AddMark(root, fanotify.CloseWrite|fanotify.EventOnChild, fanotify.OnlyDir))

			for _, name := range []string{"first", "second", "third"} {
				require.NoError(t, os.WriteFile(filepath.Join(root, name), nil, 0o600))
			}

			before := test.Descriptors(t)
			var borrowed *fanotify.Event
			iterate := func() {
				for event, err := range group.Events() {
					require.NoError(t, err)

					borrowed = event
					if scenario == "panic" {
						panic("loop body")
					}
					break
				}
			}
			if scenario == "panic" {
				require.PanicsWithValue(t, "loop body", iterate)
			} else {
				iterate()
			}

			require.NotNil(t, borrowed)

			_, err := borrowed.Stat()
			require.ErrorIs(t, err, os.ErrClosed)
			require.Equal(t, before, test.Descriptors(t), "iteration must release the whole read-ahead batch")

			require.NoError(t, group.SetReadDeadline(time.Now().Add(20*time.Millisecond)))

			var terminal int
			for event, err := range group.Events() {
				require.Nil(t, event)
				require.ErrorIs(t, err, os.ErrDeadlineExceeded)

				terminal++
			}

			require.Equal(t, 1, terminal, "iteration must yield exactly one deadline error")
		})
	}
}

func TestIterationDeniesPermissionOnEarlyExit(t *testing.T) {
	if !test.FanotifyNamespace(t) {
		return
	}

	group := test.Value(t, func() (*fanotify.Group, error) {
		return fanotify.New(fanotify.Content)
	})
	test.Cleanup(t, group)
	require.NoError(t, group.SetReadDeadline(time.Now().Add(5*time.Second)))

	path := test.TempFile(t, 0).Name()
	require.NoError(t, group.AddMark(path, fanotify.OpenPermission, 0))

	opened := test.StartRead(t, func() (*os.File, error) {
		return os.Open(path)
	})
	for event, err := range group.Events() {
		require.NoError(t, err)
		require.Equal(t, fanotify.OpenPermission, event.Mask)
		break
	}

	result := test.Receive(t, opened)
	require.Nil(t, result.Value)
	require.ErrorIs(t, result.Err, unix.EPERM)
}
