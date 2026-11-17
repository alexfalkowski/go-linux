//go:build linux

package cgroup

import (
	"errors"
	"io"
	"iter"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/alexfalkowski/go-linux/inotify"
	"golang.org/x/sys/unix"
)

const removalCheckInterval = 100 * time.Millisecond

// Event is a cgroup.events snapshot. It is a current state, not a count or a
// lossless history: notifications can coalesce and state can change again.
type Event struct {
	// Other retains unknown unsigned fields, excluding populated and frozen.
	// Each successful snapshot owns its map; callers may modify it.
	Other map[string]uint64
	// Populated is true if the group or any descendant has live processes.
	Populated bool
	// Frozen reports completion of freezing by this group or an ancestor.
	Frozen bool
}

// Events reads cgroup.events. It is present on non-root groups; the hierarchy
// root normally fails with ENOENT. The result is one snapshot with no watch or
// background goroutine. Errors return a zero Event.
func (g *Group) Events() (Event, error) {
	data, err := g.read("cgroup.events")
	if err != nil {
		return Event{}, err
	}
	return ParseEvents(data)
}

// WatchEvents creates an independent owned watcher of cgroup.events using
// inotify. Procfs must be available at /proc; registration uses a pinned file
// identity rather than resolving the group's old pathname. Failure closes all
// acquired resources. The watcher survives Group.Close. Install the watch before
// Events when reconciling an initial snapshot with subsequent changes.
func (g *Group) WatchEvents() (*EventWatcher, error) {
	file, err := g.openFile("cgroup.events", unix.O_RDONLY)
	if err != nil {
		return nil, err
	}

	watcher, err := inotify.New()
	if err != nil {
		file.Close()
		return nil, err
	}

	conn, err := file.SyscallConn()
	if err != nil {
		watcher.Close()
		file.Close()
		return nil, err
	} else {
		var callErr error
		err = conn.Control(func(fd uintptr) {
			_, callErr = watcher.AddWatch("/proc/self/fd/"+strconv.FormatUint(uint64(fd), 10), inotify.Modify|inotify.DeleteSelf)
		})
		if err := errors.Join(err, callErr); err != nil {
			watcher.Close()
			file.Close()
			return nil, err
		}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		watcher.Close()
		file.Close()
		return nil, eventFileError(err)
	}
	return &EventWatcher{watcher: watcher, file: file}, nil
}

// EventWatcher owns a notification queue and event file and implements io.Closer.
// Do not copy it after first use. Use at most one reader/iterator at a time;
// Close and SetReadDeadline may run concurrently. The zero value owns nothing
// and matches os.ErrInvalid; nil receivers are unsupported.
type EventWatcher struct {
	deadline time.Time
	watcher  *inotify.Watcher
	file     *os.File
	mu       sync.Mutex
}

// Read waits for a modification and returns a fresh snapshot. It does not return
// an initial event. Changes can coalesce; Read may report the same state twice.
// Queue overflow matches ENOBUFS: reconcile with Group.Events before resuming.
// Removal/unmount matches os.ErrNotExist. Deadlines match os.ErrDeadlineExceeded;
// Close interrupts a waiting read with os.ErrClosed. Errors return a zero Event.
// Removal is checked every 100 milliseconds while waiting, subject to scheduling.
func (w *EventWatcher) Read() (Event, error) {
	if w.watcher == nil {
		return Event{}, os.ErrInvalid
	}

	var buffer [4096]byte
	events, err := w.waitEvents(buffer[:])
	if err != nil {
		return Event{}, err
	}

	for _, event := range events {
		if event.Mask&inotify.QueueOverflow != 0 {
			return Event{}, &os.PathError{Op: "read", Path: "cgroup.events", Err: unix.ENOBUFS}
		}
		if event.Mask&(inotify.Ignored|inotify.DeleteSelf|inotify.Unmount) != 0 {
			return Event{}, &os.PathError{Op: "read", Path: "cgroup.events", Err: os.ErrNotExist}
		}
	}

	data, err := io.ReadAll(w.file)
	if err != nil {
		return Event{}, eventFileError(err)
	}
	return ParseEvents(data)
}

// Events returns a synchronous single-use sequence of snapshots using Read.
// A terminal error is yielded once with a zero Event. Breaking performs no
// further I/O; the same sequence resumes on reuse unless it has ended in error.
// It has no private read-ahead or goroutine. The caller still closes the watcher.
func (w *EventWatcher) Events() iter.Seq2[Event, error] {
	var done bool
	return func(yield func(Event, error) bool) {
		for !done {
			event, err := w.Read()
			done = err != nil
			if !yield(event, err) || done {
				return
			}
		}
	}
}

// SetReadDeadline bounds pending and future reads. Zero clears the deadline;
// snapshot reads after receiving a notification are ordinary finite file reads.
func (w *EventWatcher) SetReadDeadline(deadline time.Time) error {
	if w.watcher == nil {
		return os.ErrInvalid
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	// Wake a pending read to apply the new caller deadline without postponing
	// its removal check. Read installs the next bounded wait under the same lock.
	w.deadline = deadline
	return w.watcher.SetReadDeadline(time.Now())
}

// Close interrupts a waiting Read and closes both owned resources. Repeated
// Close matches os.ErrClosed. Group and its kernel state remain unchanged.
func (w *EventWatcher) Close() error {
	if w.watcher == nil {
		return os.ErrInvalid
	}
	return errors.Join(w.watcher.Close(), w.file.Close())
}

func (w *EventWatcher) waitEvents(buffer []byte) ([]inotify.Event, error) {
	for {
		// Kernfs removal need not notify a pinned control inode. Seeking checks
		// its active reference even when no inotify event can wake the reader.
		// https://github.com/torvalds/linux/blob/v7.0/fs/kernfs/file.c
		if _, err := w.file.Seek(0, io.SeekStart); err != nil {
			return nil, eventFileError(err)
		}

		if err := w.setWaitDeadline(); err != nil {
			return nil, err
		}

		events, err := w.watcher.ReadEvents(buffer)
		if err != nil {
			if !errors.Is(err, os.ErrDeadlineExceeded) {
				return nil, err
			}
			if w.readDeadlineExpired() {
				return nil, err
			}
			continue
		}

		return events, nil
	}
}

func (w *EventWatcher) setWaitDeadline() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	nextCheck := time.Now().Add(removalCheckInterval)
	var deadline time.Time
	if !w.deadline.IsZero() && w.deadline.Before(nextCheck) {
		deadline = w.deadline
	} else {
		deadline = nextCheck
	}

	return w.watcher.SetReadDeadline(deadline)
}

func (w *EventWatcher) readDeadlineExpired() bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	return !w.deadline.IsZero() && !time.Now().Before(w.deadline)
}

func eventFileError(err error) error {
	if errors.Is(err, unix.ENODEV) {
		return &os.PathError{Op: "read", Path: "cgroup.events", Err: os.ErrNotExist}
	}
	return err
}
