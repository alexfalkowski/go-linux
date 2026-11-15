//go:build linux

package fanotify

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

// Info is an extensible Linux fanotify_event_info_header record. The initial
// descriptor mode requests no additional records. Unknown types are retained
// without interpretation; they do not enable additional reporting modes.
type Info struct {
	// Data owns the record payload, excluding the four-byte header.
	Data []byte
	// Type preserves the Linux FAN_EVENT_INFO_TYPE_* identifier.
	Type uint8
}

// Event owns one notification and, except for QueueOverflow, a read-only file.
// Use through a pointer and do not copy after first use. Mask, PID, and Info are
// observations; changing them does not change the pending permission decision.
// Do not mutate observations concurrently with their readers. Methods are safe
// concurrently. ReadEvents transfers ownership; Events lends it temporarily.
// The zero value owns nothing and methods match os.ErrInvalid. After Close,
// methods match os.ErrClosed. Nil receivers are unsupported.
type Event struct {
	file  *os.File
	group *Group
	// Info preserves ordered extension records with independently owned bytes.
	Info []Info
	// Mask preserves all returned FAN_* bits, including unknown conditions.
	Mask Mask
	// PID is the originating process ID in the listener's PID namespace, or
	// zero if unavailable. It is an observation, not a stable process handle.
	PID        int
	fd         int
	mu         sync.Mutex
	permission bool
	answered   bool
	closed     bool
}

// ReadAt reads the event's file without sharing or changing its offset. It
// implements io.ReaderAt. Linux's event file avoids reopening a raced pathname.
// Reading this kernel-supplied file does not generate more fanotify events.
// Events without files match ENODATA. EOF and filesystem errors retain ordinary
// os.File.ReadAt semantics; file I/O itself has no deadline guarantee.
func (e *Event) ReadAt(p []byte, offset int64) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.fileError(); err != nil {
		return 0, err
	}
	return e.file.ReadAt(p, offset)
}

// Stat inspects the event's open file. Events without files match ENODATA.
// The returned metadata is an observation, not an atomic snapshot with ReadAt.
func (e *Event) Stat() (os.FileInfo, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.fileError(); err != nil {
		return nil, err
	}
	return e.file.Stat()
}

// Respond writes one Allow or Deny decision to this event's group. Unknown
// decisions, notifications, and already answered events match os.ErrInvalid.
// Events without a file match ENODATA instead.
// Invalid decisions take precedence over event state. A successful response
// does not close the event file. Failure leaves the event caller-owned and
// unanswered. Closing the group can race a response and allows pending access.
// A process killed while awaiting a response can cause Linux ENOENT.
func (e *Event) Respond(decision Decision) error {
	if decision != Allow && decision != Deny {
		return os.ErrInvalid
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.fileError(); err != nil {
		return err
	}
	if !e.permission || e.answered {
		return os.ErrInvalid
	}
	return e.respond(decision)
}

// Close attempts Deny for an unanswered permission event, then always closes
// its file. Repeated Close matches os.ErrClosed. If Deny fails, Close also closes
// the group to release blocked requests, which Linux allows. It returns the
// response/file errors joined, preserving errors.Is. Even on error the event
// is consumed and Close must not be retried. Overflow events also need Close.
func (e *Event) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.group == nil {
		return os.ErrInvalid
	}
	if e.closed {
		return os.ErrClosed
	}

	e.closed = true

	var responseErr, closeErr error
	if e.permission && !e.answered {
		responseErr = e.respond(Deny)
		if responseErr != nil {
			e.group.Close()
		}
	}
	if e.file != nil {
		closeErr = e.file.Close()
	}
	return errors.Join(responseErr, closeErr)
}

func (e *Event) fileError() error {
	switch {
	case e.group == nil:
		return os.ErrInvalid
	case e.closed:
		return os.ErrClosed
	case e.file == nil:
		return unix.ENODATA
	default:
		return nil
	}
}

func (e *Event) respond(decision Decision) error {
	var response [8]byte
	binary.NativeEndian.PutUint32(response[:4], uint32(e.fd)) //nolint:gosec // The descriptor came from a nonnegative signed 32-bit kernel field.
	binary.NativeEndian.PutUint32(response[4:], uint32(decision))

	n, err := e.group.file.Write(response[:])
	if err == nil && n != len(response) {
		err = io.ErrShortWrite
	}
	if err == nil {
		e.answered = true
	}
	return err
}
