//go:build linux

package inotify

import (
	"bytes"
	"encoding/binary"
	"os"

	"golang.org/x/sys/unix"
)

// Mask contains Linux IN_* event conditions or watch options. AddWatch rejects
// unknown or output-only bits; ParseEvents preserves all returned bits.
type Mask uint32

// Access reports file access (IN_ACCESS).
const Access Mask = unix.IN_ACCESS

// Modify reports content changes (IN_MODIFY).
const Modify Mask = unix.IN_MODIFY

// Attrib reports metadata changes (IN_ATTRIB).
const Attrib Mask = unix.IN_ATTRIB

// CloseWrite reports closing a file opened for writing (IN_CLOSE_WRITE).
const CloseWrite Mask = unix.IN_CLOSE_WRITE

// CloseNoWrite reports closing a file not opened for writing (IN_CLOSE_NOWRITE).
const CloseNoWrite Mask = unix.IN_CLOSE_NOWRITE

// Open reports opening a file or directory (IN_OPEN).
const Open Mask = unix.IN_OPEN

// MovedFrom reports the old name in a watched directory (IN_MOVED_FROM).
const MovedFrom Mask = unix.IN_MOVED_FROM

// MovedTo reports the new name in a watched directory (IN_MOVED_TO).
const MovedTo Mask = unix.IN_MOVED_TO

// Create reports a child created in a watched directory (IN_CREATE).
const Create Mask = unix.IN_CREATE

// Delete reports a child removed from a watched directory (IN_DELETE).
const Delete Mask = unix.IN_DELETE

// DeleteSelf reports deletion of the watched object itself (IN_DELETE_SELF).
const DeleteSelf Mask = unix.IN_DELETE_SELF

// MoveSelf reports moving the watched object itself (IN_MOVE_SELF).
const MoveSelf Mask = unix.IN_MOVE_SELF

// Unmount reports unmounting the watched object's filesystem (IN_UNMOUNT).
// It is output-only and is followed by Ignored.
const Unmount Mask = unix.IN_UNMOUNT

// QueueOverflow reports lost events (IN_Q_OVERFLOW), with Watch equal to -1.
// It is output-only; callers decide how to reconcile their state.
const QueueOverflow Mask = unix.IN_Q_OVERFLOW

// Ignored reports explicit or automatic watch removal (IN_IGNORED).
// It is output-only and may follow already queued events for the watch.
const Ignored Mask = unix.IN_IGNORED

// IsDir reports that the event concerns a directory (IN_ISDIR), output-only.
const IsDir Mask = unix.IN_ISDIR

// CloseEvents selects either close event (IN_CLOSE).
const CloseEvents Mask = unix.IN_CLOSE

// MoveEvents selects either rename event (IN_MOVE).
const MoveEvents Mask = unix.IN_MOVE

// AllEvents selects all requestable event conditions (IN_ALL_EVENTS), without
// watch options or output-only conditions.
const AllEvents Mask = unix.IN_ALL_EVENTS

// OnlyDir requires a directory, failing with ENOTDIR otherwise (IN_ONLYDIR).
const OnlyDir Mask = unix.IN_ONLYDIR

// DontFollow watches a symlink itself rather than its target (IN_DONT_FOLLOW).
const DontFollow Mask = unix.IN_DONT_FOLLOW

// ExclUnlink suppresses child events after unlink from a watched directory
// (IN_EXCL_UNLINK).
const ExclUnlink Mask = unix.IN_EXCL_UNLINK

// MaskCreate fails with EEXIST if the inode already has a watch on this watcher
// (IN_MASK_CREATE). It cannot be combined with MaskAdd.
const MaskCreate Mask = unix.IN_MASK_CREATE

// MaskAdd adds conditions to an existing watch instead of replacing its mask
// (IN_MASK_ADD). It cannot be combined with MaskCreate.
const MaskAdd Mask = unix.IN_MASK_ADD

// OneShot removes the watch after one event (IN_ONESHOT), then reports Ignored.
const OneShot Mask = unix.IN_ONESHOT

// WatchID is a Linux watch descriptor, scoped to its Watcher. It owns no
// resource independently. IDs may be reused after removal, even while old
// events remain queued. QueueOverflow events use -1 instead of a watch ID.
type WatchID int32

// Event is one inotify record. It preserves Linux identity and event bits
// without tracking paths, pairing renames, or suppressing removal/overflow.
type Event struct {
	// Name is a child name relative to the watched directory, or empty for an
	// event on the watched object. It owns its bytes, excludes NUL padding, and
	// need not be UTF-8. It is not a resolved or necessarily still-existing path.
	Name string
	// Watch identifies the watch within its Watcher, or -1 for QueueOverflow.
	Watch WatchID
	// Mask contains all kernel event bits, including unknown bits.
	Mask Mask
	// Cookie correlates MovedFrom and MovedTo; zero means no rename cookie.
	// Partners can be absent, separated by other events, or in different reads.
	Cookie uint32
}

// ParseEvents decodes a complete batch of native-endian [inotify(7)] records.
// Empty input returns nil, nil. Truncated headers/names, missing name NULs, or
// nonzero bytes after a name's first NUL return no events and an error matching
// os.ErrInvalid. Unknown masks and signed watch IDs are preserved. Names are
// copied, so the caller may reuse p after return. No record is buffered for a
// later call; pass only the bytes returned by Read.
//
// [inotify(7)]: https://man7.org/linux/man-pages/man7/inotify.7.html
func ParseEvents(p []byte) ([]Event, error) {
	var events []Event
	for len(p) != 0 {
		if len(p) < unix.SizeofInotifyEvent {
			return nil, &os.PathError{Op: "decode", Path: "inotify", Err: os.ErrInvalid}
		}
		length := binary.NativeEndian.Uint32(p[12:16])
		if uint64(length) > uint64(len(p))-unix.SizeofInotifyEvent {
			return nil, &os.PathError{Op: "decode", Path: "inotify", Err: os.ErrInvalid}
		}
		name := p[unix.SizeofInotifyEvent : unix.SizeofInotifyEvent+int(length)]
		if len(name) != 0 {
			end := bytes.IndexByte(name, 0)
			if end < 0 || bytes.Count(name[end:], []byte{0}) != len(name)-end {
				return nil, &os.PathError{Op: "decode", Path: "inotify", Err: os.ErrInvalid}
			}
			name = name[:end]
		}
		events = append(events, Event{
			Watch:  WatchID(int32(binary.NativeEndian.Uint32(p[:4]))), //nolint:gosec // Linux wd is signed, including -1 for overflow.
			Mask:   Mask(binary.NativeEndian.Uint32(p[4:8])),
			Cookie: binary.NativeEndian.Uint32(p[8:12]),
			Name:   string(name),
		})
		p = p[unix.SizeofInotifyEvent+int(length):]
	}
	return events, nil
}
