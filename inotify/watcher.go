//go:build linux

package inotify

import (
	"iter"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// New creates an empty, owned watcher using [inotify_init1(2)]. The caller must
// close the result. Creation atomically sets nonblocking and close-on-exec,
// then requires Go polling and read deadlines. Failure closes any acquired
// descriptor and returns no watcher. There is no blocking fallback.
//
// [inotify_init1(2)]: https://man7.org/linux/man-pages/man2/inotify_init.2.html
func New() (*Watcher, error) {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		return nil, &os.PathError{Op: "inotify_init1", Path: "inotify", Err: err}
	}

	file := os.NewFile(uintptr(fd), "inotify")
	if err := file.SetReadDeadline(time.Time{}); err != nil {
		file.Close()
		return nil, err
	}
	return &Watcher{file: file}, nil
}

// Watcher owns an inotify instance and all its watches. Methods are safe for
// concurrent use; readers compete for batches. Sequences require caller
// coordination. Do not copy a Watcher after first use. Its zero value owns
// nothing and error-returning methods match os.ErrInvalid; nil receivers are
// unsupported. Valid operations after Close match os.ErrClosed. For initialized
// watchers, invalid arguments take precedence over closed state. Syscall errors
// preserve errno identity through errors.Is/As, normally in os.PathError.
type Watcher struct {
	file *os.File
}

// AddWatch adds or updates an inode watch using [inotify_add_watch(2)]. Path
// resolution follows symlinks unless DontFollow is set. Read permission is
// required. Watching a directory covers its immediate children, not descendants.
// Watching an already watched inode returns the same ID and replaces its mask,
// unless MaskAdd or MaskCreate changes that behavior. Paths are not retained.
//
// At least one AllEvents bit is required. Unknown/output-only bits, embedded
// NULs, and MaskAdd|MaskCreate match os.ErrInvalid before changing any watch.
// Empty paths reach Linux and fail with ENOENT. Failure returns ID zero and
// leaves the Watcher caller-owned. Limits may fail with ENOSPC or ENOMEM.
// IDs are kernel values, not permanent identities; see WatchID.
//
// [inotify_add_watch(2)]: https://man7.org/linux/man-pages/man2/inotify_add_watch.2.html
func (w *Watcher) AddWatch(path string, mask Mask) (WatchID, error) {
	if w.file == nil {
		return 0, os.ErrInvalid
	}
	const allowed = AllEvents | OnlyDir | DontFollow | ExclUnlink | MaskCreate | MaskAdd | OneShot
	unknownBits := mask&^allowed != 0
	missingEvents := mask&AllEvents == 0
	conflictingOptions := mask&(MaskAdd|MaskCreate) == MaskAdd|MaskCreate
	if unknownBits || missingEvents || conflictingOptions {
		return 0, &os.PathError{Op: "inotify_add_watch", Path: path, Err: os.ErrInvalid}
	}

	if _, err := unix.BytePtrFromString(path); err != nil {
		return 0, &os.PathError{Op: "inotify_add_watch", Path: path, Err: os.ErrInvalid}
	}

	conn, err := w.file.SyscallConn()
	if err != nil {
		return 0, err
	}

	var id int
	var callErr error
	err = conn.Control(func(fd uintptr) {
		id, callErr = unix.InotifyAddWatch(int(fd), path, uint32(mask))
	})
	if err != nil {
		// RawConn exposes the poller's private closing error.
		return 0, &os.PathError{Op: "inotify_add_watch", Path: path, Err: os.ErrClosed}
	}
	if callErr != nil {
		return 0, &os.PathError{Op: "inotify_add_watch", Path: path, Err: callErr}
	}
	return WatchID(id), nil //nolint:gosec // Linux watch descriptors are signed 32-bit values.
}

// RemoveWatch removes id using [inotify_rm_watch(2)], which queues Ignored.
// Earlier queued events remain readable. IDs belong to one Watcher; callers
// must avoid stale/reused or foreign IDs, which can refer to another watch.
// Negative IDs match os.ErrInvalid before the syscall. Already removed or
// nonexistent nonnegative IDs fail with Linux EINVAL; removal is not idempotent.
//
// [inotify_rm_watch(2)]: https://man7.org/linux/man-pages/man2/inotify_rm_watch.2.html
func (w *Watcher) RemoveWatch(id WatchID) error {
	if w.file == nil {
		return os.ErrInvalid
	}
	if id < 0 {
		return &os.PathError{Op: "inotify_rm_watch", Path: "inotify", Err: os.ErrInvalid}
	}

	conn, err := w.file.SyscallConn()
	if err != nil {
		return err
	}

	var callErr error
	err = conn.Control(func(fd uintptr) {
		_, callErr = unix.InotifyRmWatch(int(fd), uint32(id)) //nolint:gosec // Negative IDs are rejected above.
	})
	if err != nil {
		return &os.PathError{Op: "inotify_rm_watch", Path: "inotify", Err: os.ErrClosed}
	}
	if callErr != nil {
		return &os.PathError{Op: "inotify_rm_watch", Path: "inotify", Err: callErr}
	}
	return nil
}

// Read consumes one or more complete kernel records into p using [read(2)].
// Buffers smaller than the 16-byte header (including nil) fail with EINVAL
// without consuming events. Linux also returns EINVAL if the first queued
// record cannot fit; use at least 4096 bytes for ordinary filesystem names.
// A larger batch may return fewer bytes, leaving later whole records queued.
// Read waits for an event, a read deadline, or Close. It implements io.Reader
// with record constraints rather than arbitrary stream/chunk semantics.
//
// [read(2)]: https://man7.org/linux/man-pages/man2/read.2.html
func (w *Watcher) Read(p []byte) (int, error) {
	if w.file == nil {
		return 0, os.ErrInvalid
	}
	if len(p) < unix.SizeofInotifyEvent {
		return 0, &os.PathError{Op: "read", Path: "inotify", Err: unix.EINVAL}
	}
	return w.file.Read(p)
}

// ReadEvents consumes a batch with Read into p and decodes it with ParseEvents.
// Returned names remain valid when p is reused. Read's size, wait, and error
// rules apply. A decoding error returns no events, but the read batch has
// already been consumed. Raw Read and ReadEvents share the same queue.
func (w *Watcher) ReadEvents(p []byte) ([]Event, error) {
	n, err := w.Read(p)
	if err != nil {
		return nil, err
	}
	return ParseEvents(p[:n])
}

// Events returns a single-use sequence of events, using its own read buffer and
// no background goroutine. It waits under Read's deadline and Close rules.
// A read error is yielded once with a zero Event, then the sequence is exhausted.
// QueueOverflow and Ignored remain ordinary events with nil errors. Event names
// own their bytes and remain valid after the next iteration.
//
// Breaking a loop performs no further I/O. Reusing the same sequence resumes
// with its remaining batch; abandoning it discards those buffered events. Do
// not interleave it with other readers if order or delivery of every buffered
// event matters. A sequence is not safe for concurrent or recursive iteration.
// The caller still owns and closes the watcher. Buffered events can be yielded
// after Close; its error appears when the next kernel read is attempted.
func (w *Watcher) Events() iter.Seq2[Event, error] {
	var buffer [4096]byte
	var events []Event
	var err error
	return func(yield func(Event, error) bool) {
		if err != nil {
			return
		}
		for {
			if len(events) == 0 {
				events, err = w.ReadEvents(buffer[:])
				if err != nil {
					yield(Event{}, err)
					return
				}
			}
			event := events[0]
			events = events[1:]
			if !yield(event, nil) {
				return
			}
		}
	}
}

// Close releases the watcher and every watch and interrupts poll-blocked reads
// with os.ErrClosed. Repeated Close matches os.ErrClosed. It does not drain
// queued events or return Ignored records for the released watches.
func (w *Watcher) Close() error {
	return w.file.Close()
}

// SetReadDeadline wraps os.File.SetReadDeadline for pending and future reads.
// Zero clears it. Expired reads match os.ErrDeadlineExceeded. Watch operations
// are not affected. Close remains available to interrupt reads without a timer.
func (w *Watcher) SetReadDeadline(deadline time.Time) error {
	err := w.file.SetReadDeadline(deadline)
	if err == nil || w.file == nil {
		return err
	}

	// Construction requires polling; its deadline setter can only fail here
	// when Close wins the lifetime reference. Normalize the private poll error.
	return &os.PathError{Op: "deadline", Path: "inotify", Err: os.ErrClosed}
}
