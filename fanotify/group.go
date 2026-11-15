//go:build linux

package fanotify

import (
	"errors"
	"iter"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// New creates an empty owned group with [fanotify_init(2)]. Zero class selects
// Notification. Content enables permission marks. Unknown classes match
// os.ErrInvalid. All supported classes require initial-user-namespace
// CAP_SYS_ADMIN; kernel and security errors retain their errno.
//
// The group is nonblocking and close-on-exec; event files are read-only and
// atomically close-on-exec. Go polling and read deadlines are required before
// returning. Failure closes acquired resources and returns no group. The caller
// must close a successful result. FID, PIDFD, audit, pre-content, unlimited
// queues/marks, and unprivileged reporting modes are not enabled.
//
// [fanotify_init(2)]: https://man7.org/linux/man-pages/man2/fanotify_init.2.html
func New(class Class) (*Group, error) {
	if class != Notification && class != Content {
		return nil, &os.PathError{Op: "fanotify_init", Path: "fanotify", Err: os.ErrInvalid}
	}

	fd, err := unix.FanotifyInit(uint(class)|unix.FAN_CLOEXEC|unix.FAN_NONBLOCK, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_LARGEFILE)
	if err != nil {
		return nil, &os.PathError{Op: "fanotify_init", Path: "fanotify", Err: err}
	}

	file := os.NewFile(uintptr(fd), "fanotify")
	if err := file.SetReadDeadline(time.Time{}); err != nil {
		file.Close()
		return nil, err
	}
	return &Group{file: file, class: class}, nil
}

// Group owns a fanotify queue and its marks. Use through a pointer; do not copy
// after first use. Methods are safe concurrently; readers compete for batches
// and sequences need caller coordination. The zero value owns nothing and
// reports os.ErrInvalid. Nil receivers are unsupported. Valid operations after
// Close match os.ErrClosed; for initialized groups, invalid arguments take
// precedence over closed state. Errors preserve errno through errors.Is/As.
type Group struct {
	file  *os.File
	class Class
}

// AddMark adds mask bits to an inode, mount, or filesystem mark using
// [fanotify_mark(2)]. Repeated calls extend an existing mask. At least one event
// condition is required. Permission conditions require Content. Unknown bits,
// conflicting scope flags, and NUL-containing paths match os.ErrInvalid.
// Empty paths match ENOENT. Paths are resolved at each call
// relative to the current working directory unless absolute; none is retained.
// Failure leaves the group caller-owned. Marks do not filter by event PID.
//
// [fanotify_mark(2)]: https://man7.org/linux/man-pages/man2/fanotify_mark.2.html
func (g *Group) AddMark(path string, mask Mask, flags MarkFlags) error {
	return g.mark(path, mask, flags, unix.FAN_MARK_ADD)
}

// RemoveMark removes mask bits from the selected mark. The validation and path
// rules of AddMark apply. Removing all mask bits, including OnDir and
// EventOnChild, removes the mark; removing only event conditions can leave it
// installed. Queued events and delivered permission requests are not canceled.
// Missing marks report Linux ENOENT rather than succeeding idempotently.
func (g *Group) RemoveMark(path string, mask Mask, flags MarkFlags) error {
	return g.mark(path, mask, flags, unix.FAN_MARK_REMOVE)
}

// Flush removes all marks in one scope: zero for inodes, Mount, or Filesystem.
// Other flags match os.ErrInvalid. Queued events and outstanding permission
// requests remain; there is no atomic flush-and-drain operation.
func (g *Group) Flush(scope MarkFlags) error {
	if g.file == nil {
		return os.ErrInvalid
	}
	if scope != 0 && scope != Mount && scope != Filesystem {
		return &os.PathError{Op: "fanotify_mark", Path: "fanotify", Err: os.ErrInvalid}
	}
	return g.applyMark("", 0, uint(scope)|unix.FAN_MARK_FLUSH)
}

// ReadEvents waits for a batch of whole records, a read deadline, or Close.
// The caller supplies scratch space of at least 24 bytes; 64 KiB is recommended
// for extensible records. A buffer too small for the next record returns EINVAL
// without consuming it. No raw descriptors or records escape to the caller.
// Returned events own their information bytes and files independently of p.
// EVERY returned event must be closed, including notifications and overflow.
//
// A malformed batch returns no events, closes identifiable delivered files,
// and closes the group to release unanswerable kernel requests. Group closure
// permits outstanding operations. Malformed records match os.ErrInvalid;
// unsupported metadata ABI versions match EPROTO. There is no partial result.
func (g *Group) ReadEvents(p []byte) ([]*Event, error) {
	if g.file == nil {
		return nil, os.ErrInvalid
	}
	if len(p) < unix.FAN_EVENT_METADATA_LEN {
		return nil, &os.PathError{Op: "read", Path: "fanotify", Err: unix.EINVAL}
	}

	n, err := g.file.Read(p)
	if err != nil {
		return nil, err
	}
	return g.decode(p[:n])
}

// Events lends each event until the loop body returns, then closes it. Respond,
// ReadAt, and Stat must finish within that iteration. Do not retain an event for
// asynchronous use; use ReadEvents for explicit ownership instead. No goroutine
// is started. Each iteration invocation reads from the group's shared queue.
//
// Breaking closes the current event and every unread event in its batch,
// denying unanswered permission requests. These events are not resumed on the
// next invocation. Read or cleanup errors yield nil and the error once, then
// end the invocation. On early exit cleanup cannot yield errors; a failed deny
// closes the group. Panicking loop bodies also run batch cleanup.
func (g *Group) Events() iter.Seq2[*Event, error] {
	return func(yield func(*Event, error) bool) {
		buffer := make([]byte, 64*1024)
		for {
			events, err := g.ReadEvents(buffer)
			if err != nil {
				yield(nil, err)
				return
			}
			if !lendEvents(events, yield) {
				return
			}
		}
	}
}

// Close releases marks and the group, interrupts blocked reads, and lets Linux
// ALLOW all outstanding permission requests. It does not close event files
// already transferred by ReadEvents; their owners still must close them.
// Repeated Close matches os.ErrClosed. Close is never retried after an error.
func (g *Group) Close() error {
	return g.file.Close()
}

// SetReadDeadline bounds pending and future reads. Zero clears the deadline;
// expiry matches os.ErrDeadlineExceeded. Mark and response operations are not
// affected. Go poller integration is mandatory at creation.
func (g *Group) SetReadDeadline(deadline time.Time) error {
	err := g.file.SetReadDeadline(deadline)
	if err == nil || g.file == nil {
		return err
	}
	return &os.PathError{Op: "deadline", Path: "fanotify", Err: os.ErrClosed}
}

func (g *Group) mark(path string, mask Mask, flags MarkFlags, operation uint) error {
	if g.file == nil {
		return os.ErrInvalid
	}

	const allowedFlags = Mount | Filesystem | DontFollow | OnlyDir
	unknownFlags := flags &^ allowedFlags
	conflictingScopes := flags&(Mount|Filesystem) == Mount|Filesystem
	unknownMask := mask &^ (conditions | OnDir | EventOnChild)
	missingConditions := mask&conditions == 0
	invalidPermissionClass := mask&permissions != 0 && g.class != Content
	if unknownFlags != 0 || conflictingScopes || unknownMask != 0 || missingConditions || invalidPermissionClass {
		return &os.PathError{Op: "fanotify_mark", Path: path, Err: os.ErrInvalid}
	}
	if path == "" {
		// x/sys maps an empty pathname to NULL, selecting descriptor targeting.
		// This path-only operation must not accidentally select that mode.
		return &os.PathError{Op: "fanotify_mark", Path: path, Err: unix.ENOENT}
	}
	if _, err := unix.BytePtrFromString(path); err != nil {
		return &os.PathError{Op: "fanotify_mark", Path: path, Err: os.ErrInvalid}
	}
	return g.applyMark(path, mask, uint(flags)|operation)
}

func (g *Group) applyMark(path string, mask Mask, flags uint) error {
	conn, err := g.file.SyscallConn()
	if err != nil {
		return err
	}

	var callErr error
	err = conn.Control(func(fd uintptr) {
		callErr = unix.FanotifyMark(int(fd), flags, uint64(mask), unix.AT_FDCWD, path)
	})
	if err != nil {
		return &os.PathError{Op: "fanotify_mark", Path: path, Err: os.ErrClosed}
	}
	if callErr != nil {
		return &os.PathError{Op: "fanotify_mark", Path: path, Err: callErr}
	}
	return nil
}

func lendEvents(events []*Event, yield func(*Event, error) bool) bool {
	defer func() {
		for _, event := range events {
			event.Close()
		}
	}()

	for _, event := range events {
		more := yield(event, nil)
		err := event.Close()
		if !more {
			return false
		}
		if err != nil && !errors.Is(err, os.ErrClosed) {
			yield(nil, err)
			return false
		}
	}
	return true
}
