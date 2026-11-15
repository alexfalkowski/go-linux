//go:build linux

package inotify_test

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/inotify"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

var _ io.ReadCloser = (*inotify.Watcher)(nil)

func TestDirectoryEventsPreserveNamesAndRenamePairing(t *testing.T) {
	watcher := test.Watcher(t)
	directory := t.TempDir()
	id, err := watcher.AddWatch(directory, inotify.Create|inotify.MoveEvents|inotify.Delete|inotify.OnlyDir)
	require.NoError(t, err)
	oldPath, newPath := filepath.Join(directory, "old"), filepath.Join(directory, "new")
	require.NoError(t, os.WriteFile(oldPath, nil, 0o600))
	require.NoError(t, os.Rename(oldPath, newPath))
	require.NoError(t, os.Remove(newPath))

	var events []inotify.Event
	for len(events) < 4 {
		batch, err := watcher.ReadEvents(make([]byte, 4096))
		require.NoError(t, err)
		events = append(events, batch...)
	}
	require.Len(t, events, 4)
	cookie := events[1].Cookie
	require.NotZero(t, cookie, "rename partners must carry a nonzero Linux cookie")
	require.Equal(t, []inotify.Event{
		{Watch: id, Mask: inotify.Create, Name: "old"},
		{Watch: id, Mask: inotify.MovedFrom, Cookie: cookie, Name: "old"},
		{Watch: id, Mask: inotify.MovedTo, Cookie: cookie, Name: "new"},
		{Watch: id, Mask: inotify.Delete, Name: "new"},
	}, events)
}

func TestWatchRemovalPreservesQueuedEventsAndReportsIgnored(t *testing.T) {
	watcher := test.Watcher(t)
	directory := t.TempDir()
	id, err := watcher.AddWatch(directory, inotify.Create)
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(filepath.Join(directory, "child"), 0o700))
	require.NoError(t, watcher.RemoveWatch(id))
	require.Equal(t, []inotify.Event{
		{Watch: id, Mask: inotify.Create | inotify.IsDir, Name: "child"},
		{Watch: id, Mask: inotify.Ignored},
	}, test.Value(t, func() ([]inotify.Event, error) {
		return watcher.ReadEvents(make([]byte, 4096))
	}))
	require.ErrorIs(t, watcher.RemoveWatch(id), unix.EINVAL)

	require.NoError(t, os.Mkdir(filepath.Join(directory, "later"), 0o700))
	require.NoError(t, watcher.SetReadDeadline(time.Now().Add(20*time.Millisecond)))
	_, err = watcher.ReadEvents(make([]byte, 4096))
	require.ErrorIs(t, err, os.ErrDeadlineExceeded, "removed watch must produce no new events")
}

func TestOneShotWatchesReportAutomaticRemoval(t *testing.T) {
	watcher := test.Watcher(t)
	directory := t.TempDir()
	id, err := watcher.AddWatch(directory, inotify.Create|inotify.OneShot)
	require.NoError(t, err)
	extended, err := watcher.AddWatch(directory, inotify.Delete|inotify.MaskAdd)
	require.NoError(t, err)
	require.Equal(t, id, extended, "extending conditions must retain the existing one-shot option")
	require.NoError(t, os.WriteFile(filepath.Join(directory, "first"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "second"), nil, 0o600))
	events, err := watcher.ReadEvents(make([]byte, 4096))
	require.NoError(t, err)
	require.Equal(t, []inotify.Event{
		{Watch: id, Mask: inotify.Create, Name: "first"},
		{Watch: id, Mask: inotify.Ignored},
	}, events)
	require.ErrorIs(t, watcher.RemoveWatch(id), unix.EINVAL)
}

func TestFileWatchesReportSelfDeletionWithoutAName(t *testing.T) {
	watcher := test.Watcher(t)
	path := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	id, err := watcher.AddWatch(path, inotify.DeleteSelf)
	require.NoError(t, err)
	require.NoError(t, os.Remove(path))
	events, err := watcher.ReadEvents(make([]byte, 4096))
	require.NoError(t, err)
	require.Equal(t, []inotify.Event{{Watch: id, Mask: inotify.DeleteSelf}, {Watch: id, Mask: inotify.Ignored}}, events)
}

func TestDirectoryWatchesAreNotRecursive(t *testing.T) {
	watcher := test.Watcher(t)
	directory := t.TempDir()
	nested := filepath.Join(directory, "nested")
	require.NoError(t, os.Mkdir(nested, 0o700))
	_, err := watcher.AddWatch(directory, inotify.Create)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(nested, "child"), nil, 0o600))
	require.NoError(t, watcher.SetReadDeadline(time.Now().Add(20*time.Millisecond)))
	_, err = watcher.ReadEvents(make([]byte, 4096))
	require.ErrorIs(t, err, os.ErrDeadlineExceeded)
}

func TestExistingInodeWatchesCanReplaceExtendOrRejectAMask(t *testing.T) {
	watcher := test.Watcher(t)
	directory := t.TempDir()
	id, err := watcher.AddWatch(directory, inotify.Create|inotify.MaskCreate)
	require.NoError(t, err)
	_, err = watcher.AddWatch(directory, inotify.Delete|inotify.MaskCreate)
	require.ErrorIs(t, err, unix.EEXIST)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "original"), nil, 0o600))
	require.Equal(t, []inotify.Event{{Watch: id, Mask: inotify.Create, Name: "original"}},
		test.Value(t, func() ([]inotify.Event, error) {
			return watcher.ReadEvents(make([]byte, 4096))
		}))

	replaced, err := watcher.AddWatch(directory, inotify.Delete)
	require.NoError(t, err)
	require.Equal(t, id, replaced, "same inode must retain its ID")
	require.NoError(t, os.WriteFile(filepath.Join(directory, "unreported"), nil, 0o600))
	require.NoError(t, os.Remove(filepath.Join(directory, "unreported")))
	events, err := watcher.ReadEvents(make([]byte, 4096))
	require.NoError(t, err)
	require.Equal(t, []inotify.Event{{Watch: id, Mask: inotify.Delete, Name: "unreported"}}, events)

	extended, err := watcher.AddWatch(directory, inotify.Create|inotify.MaskAdd)
	require.NoError(t, err)
	require.Equal(t, id, extended, "extending a mask must retain its ID")
	require.NoError(t, os.WriteFile(filepath.Join(directory, "both"), nil, 0o600))
	require.NoError(t, os.Remove(filepath.Join(directory, "both")))
	events, err = watcher.ReadEvents(make([]byte, 4096))
	require.NoError(t, err)
	require.Equal(t, []inotify.Event{
		{Watch: id, Mask: inotify.Create, Name: "both"}, {Watch: id, Mask: inotify.Delete, Name: "both"},
	}, events)
}

func TestSymlinkWatchOptionsKeepLinuxPathSemantics(t *testing.T) {
	directory := t.TempDir()
	path, link := filepath.Join(directory, "file"), filepath.Join(directory, "link")
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	require.NoError(t, os.Symlink(path, link))
	watcher := test.Watcher(t)
	id, err := watcher.AddWatch(path, inotify.DeleteSelf)
	require.NoError(t, err)
	followed, err := watcher.AddWatch(link, inotify.DeleteSelf)
	require.NoError(t, err)
	require.Equal(t, id, followed, "default resolution watches the target inode")
	symlink, err := watcher.AddWatch(link, inotify.DeleteSelf|inotify.DontFollow)
	require.NoError(t, err)
	require.NotEqual(t, id, symlink, "DontFollow watches the symlink inode")
	require.NoError(t, os.Remove(link))
	events, err := watcher.ReadEvents(make([]byte, 4096))
	require.NoError(t, err)
	require.Equal(t, []inotify.Event{{Watch: symlink, Mask: inotify.DeleteSelf}, {Watch: symlink, Mask: inotify.Ignored}}, events)
	_, err = watcher.AddWatch(path, inotify.Create|inotify.OnlyDir)
	require.ErrorIs(t, err, unix.ENOTDIR)
}

func TestExclUnlinkControlsEventsFromUnlinkedChildren(t *testing.T) {
	for _, scenario := range []struct {
		name string
		mask inotify.Mask
	}{
		{name: "default includes unlinked children", mask: inotify.Modify},
		{name: "explicit exclusion", mask: inotify.Modify | inotify.ExclUnlink},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			opened := test.TempFile(t, 0)
			watcher := test.Watcher(t)
			id, err := watcher.AddWatch(filepath.Dir(opened.Name()), scenario.mask)
			require.NoError(t, err)
			require.NoError(t, os.Remove(opened.Name()))
			test.WriteAt(t, opened, 0, "after unlink")
			require.NoError(t, watcher.SetReadDeadline(time.Now().Add(20*time.Millisecond)))
			events, err := watcher.ReadEvents(make([]byte, 4096))
			if scenario.mask&inotify.ExclUnlink != 0 {
				require.ErrorIs(t, err, os.ErrDeadlineExceeded)
				require.Nil(t, events)
				return
			}
			require.NoError(t, err)
			require.Equal(t, []inotify.Event{{Watch: id, Mask: inotify.Modify, Name: filepath.Base(opened.Name())}}, events)
		})
	}
}

func TestRawReadsPreserveRecordBoundariesAndDoNotConsumeOnShortBuffers(t *testing.T) {
	watcher := test.Watcher(t)
	directory := t.TempDir()
	id, err := watcher.AddWatch(directory, inotify.Create)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "a"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "b"), nil, 0o600))
	for _, size := range []int{0, 1, 15, 16, 31} {
		n, err := watcher.Read(make([]byte, size))
		require.Zero(t, n)
		require.ErrorIs(t, err, unix.EINVAL, "buffer size %d must not consume the first named event", size)
	}
	for _, name := range []string{"a", "b"} {
		buffer := make([]byte, 33)
		buffer[32] = 0xff
		n, err := watcher.Read(buffer)
		require.NoError(t, err)
		require.Equal(t, 32, n, "only one whole record fits")
		require.Equal(t, byte(0xff), buffer[32], "unused buffer tail must be preserved")
		events, err := inotify.ParseEvents(buffer[:n])
		require.NoError(t, err)
		require.Equal(t, []inotify.Event{{Watch: id, Mask: inotify.Create, Name: name}}, events)
	}
}

func TestInvalidWatchArgumentsLeaveExistingWatchesUnchanged(t *testing.T) {
	watcher := test.Watcher(t)
	directory := t.TempDir()
	id, err := watcher.AddWatch(directory, inotify.Create)
	require.NoError(t, err)
	for _, mask := range []inotify.Mask{
		0, inotify.OnlyDir, inotify.Ignored | inotify.Create, 0x80000 | inotify.Create,
		inotify.Create | inotify.MaskAdd | inotify.MaskCreate,
	} {
		_, err := watcher.AddWatch(directory, mask)
		require.ErrorIs(t, err, os.ErrInvalid, "invalid mask %#x", mask)
	}
	_, err = watcher.AddWatch("bad\x00path", inotify.Create)
	require.ErrorIs(t, err, os.ErrInvalid)
	_, err = watcher.AddWatch("", inotify.Create)
	require.ErrorIs(t, err, unix.ENOENT)
	_, err = watcher.AddWatch(filepath.Join(directory, "missing"), inotify.Create)
	require.ErrorIs(t, err, unix.ENOENT)
	test.PathError(t, err, "inotify_add_watch", filepath.Join(directory, "missing"))
	require.ErrorIs(t, watcher.RemoveWatch(-1), os.ErrInvalid)
	require.ErrorIs(t, watcher.RemoveWatch(0), unix.EINVAL)

	require.NoError(t, os.WriteFile(filepath.Join(directory, "child"), nil, 0o600))
	events, err := watcher.ReadEvents(make([]byte, 4096))
	require.NoError(t, err)
	require.Equal(t, []inotify.Event{{Watch: id, Mask: inotify.Create, Name: "child"}}, events)
}

func TestReadDeadlinesInterruptPendingReadsAndCanBeCleared(t *testing.T) {
	watcher := test.Watcher(t)
	directory := t.TempDir()
	id, err := watcher.AddWatch(directory, inotify.Create)
	require.NoError(t, err)
	done := test.StartRead(t, func() ([]inotify.Event, error) {
		return watcher.ReadEvents(make([]byte, 4096))
	})
	require.NoError(t, watcher.SetReadDeadline(time.Now().Add(-time.Second)))
	result := test.Receive(t, done)
	require.Nil(t, result.Value)
	require.ErrorIs(t, result.Err, os.ErrDeadlineExceeded)
	require.True(t, os.IsTimeout(result.Err), "read deadlines must expose timeout semantics")

	require.NoError(t, os.WriteFile(filepath.Join(directory, "pending"), nil, 0o600))
	_, err = watcher.ReadEvents(make([]byte, 4096))
	require.ErrorIs(t, err, os.ErrDeadlineExceeded, "deadline persists without consuming queued records")
	require.NoError(t, watcher.SetReadDeadline(time.Time{}))
	events, err := watcher.ReadEvents(make([]byte, 4096))
	require.NoError(t, err)
	require.Equal(t, []inotify.Event{{Watch: id, Mask: inotify.Create, Name: "pending"}}, events)
}

func TestReadsWaitForEvents(t *testing.T) {
	watcher := test.Watcher(t)
	directory := t.TempDir()
	id, err := watcher.AddWatch(directory, inotify.Create)
	require.NoError(t, err)
	done := test.StartRead(t, func() ([]inotify.Event, error) {
		return watcher.ReadEvents(make([]byte, 4096))
	})
	require.NoError(t, os.WriteFile(filepath.Join(directory, "wake"), nil, 0o600))
	require.Equal(t, test.ReadResult[[]inotify.Event]{Value: []inotify.Event{{Watch: id, Mask: inotify.Create, Name: "wake"}}},
		test.Receive(t, done))
}

func TestCloseInterruptsEveryPendingReader(t *testing.T) {
	watcher := test.Watcher(t)
	results := make([]<-chan test.ReadResult[[]inotify.Event], 0, 3)
	for range 3 {
		results = append(results, test.StartRead(t, func() ([]inotify.Event, error) {
			return watcher.ReadEvents(make([]byte, 4096))
		}))
	}
	require.NoError(t, watcher.Close())
	for _, done := range results {
		result := test.Receive(t, done)
		require.Nil(t, result.Value)
		require.ErrorIs(t, result.Err, os.ErrClosed)
	}
}

func TestConcurrentWatchOperationsAndCloseRemainSafe(t *testing.T) {
	watcher := test.Watcher(t)
	directory := t.TempDir()
	var workers sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		workers.Go(func() {
			for range 50 {
				id, err := watcher.AddWatch(directory, inotify.Create)
				if err != nil {
					results <- err
					return
				}
				if err := watcher.RemoveWatch(id); err != nil {
					results <- err
					return
				}
			}
			results <- nil
		})
	}
	require.NoError(t, watcher.Close())
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			require.True(t, errors.Is(err, os.ErrClosed) || errors.Is(err, unix.EINVAL),
				"only closed state or a concurrently removed ID is allowed: %v", err)
		}
	}
}

func TestQueueOverflowIsReturnedAsAnEvent(t *testing.T) {
	limit := test.InotifyQueueLimit(t)
	watcher := test.Watcher(t)
	directory := t.TempDir()
	_, err := watcher.AddWatch(directory, inotify.Create|inotify.Delete)
	require.NoError(t, err)
	path := filepath.Join(directory, "cycle")
	// Alternating masks avoid Linux coalescing identical unread events. Only
	// this instance's queue overflows; no host sysctl or shared watch is changed.
	for range limit/2 + 2 {
		require.NoError(t, os.WriteFile(path, nil, 0o600))
		require.NoError(t, os.Remove(path))
	}
	require.NoError(t, watcher.SetReadDeadline(time.Now().Add(5*time.Second)))
	for {
		events, err := watcher.ReadEvents(make([]byte, 65536))
		require.NoError(t, err)
		for _, event := range events {
			if event.Mask&inotify.QueueOverflow != 0 {
				require.Equal(t, inotify.Event{Watch: -1, Mask: inotify.QueueOverflow}, event)
				return
			}
		}
	}
}

func TestZeroAndClosedWatchersRejectOperations(t *testing.T) {
	closed := test.Watcher(t)
	require.NoError(t, closed.Close())
	for _, scenario := range []struct {
		name     string
		watcher  *inotify.Watcher
		expected error
	}{
		{name: "zero", watcher: &inotify.Watcher{}, expected: os.ErrInvalid},
		{name: "closed", watcher: closed, expected: os.ErrClosed},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			_, err := scenario.watcher.AddWatch(t.TempDir(), inotify.Create)
			require.ErrorIs(t, err, scenario.expected)
			require.ErrorIs(t, scenario.watcher.RemoveWatch(1), scenario.expected)
			_, err = scenario.watcher.Read(make([]byte, 4096))
			require.ErrorIs(t, err, scenario.expected)
			_, err = scenario.watcher.ReadEvents(make([]byte, 4096))
			require.ErrorIs(t, err, scenario.expected)
			require.ErrorIs(t, scenario.watcher.SetReadDeadline(time.Time{}), scenario.expected)
			require.ErrorIs(t, scenario.watcher.Close(), scenario.expected)
		})
	}
}

func TestCreatedWatchersUseNonblockingCloseOnExecDescriptors(t *testing.T) {
	before := test.Descriptors(t)
	watcher := test.Watcher(t)
	fd := test.NewDescriptor(t, before)
	test.CloseOnExec(t, fd)
	require.NotZero(t, test.Fcntl(t, fd, unix.F_GETFL)&unix.O_NONBLOCK, "Go polling requires a nonblocking descriptor")
	require.NoError(t, watcher.Close())
	require.Equal(t, before, test.Descriptors(t), "Close must release the sole acquired descriptor")
}

func TestClosingWatchersWithActiveWatchesReleasesDescriptors(t *testing.T) {
	watcher := test.Watcher(t)
	require.NoError(t, watcher.Close())
	before := test.DescriptorCount(t)
	directory := t.TempDir()
	for range 32 {
		watcher, err := inotify.New()
		require.NoError(t, err)
		_, err = watcher.AddWatch(directory, inotify.AllEvents)
		require.NoError(t, err)
		require.NoError(t, watcher.Close())
	}
	require.LessOrEqual(t, test.DescriptorCount(t), before, "Close must release descriptors and their watches")
}

func TestCreationFailureReturnsNoResource(t *testing.T) {
	if len(os.Args) > 1 && os.Args[len(os.Args)-1] == "inotify-exhaustion" {
		test.ExhaustDescriptors(t)
		watcher, err := inotify.New()
		test.NoResource(t, watcher, err, unix.EMFILE)
		return
	}
	ctx, command := test.Command(t, t.Name(), "inotify-exhaustion")
	output, err := command.CombinedOutput()
	require.NoError(t, ctx.Err())
	require.NoError(t, err, "descriptor exhaustion subprocess: %s", output)
}

func TestInvalidArgumentsPrecedeClosedState(t *testing.T) {
	watcher := test.Watcher(t)
	require.NoError(t, watcher.Close())
	_, err := watcher.AddWatch("any", 0)
	require.ErrorIs(t, err, os.ErrInvalid)
	require.ErrorIs(t, watcher.RemoveWatch(-1), os.ErrInvalid)
	for size := range 16 {
		t.Run(fmt.Sprintf("%d byte buffer", size), func(t *testing.T) {
			_, err := watcher.Read(make([]byte, size))
			require.ErrorIs(t, err, unix.EINVAL)
		})
	}
}
