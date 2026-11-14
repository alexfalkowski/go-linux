//go:build linux

package epoll

import "golang.org/x/sys/unix"

// Events is a Linux epoll bitmask. Add and Modify accept the documented event
// conditions and trigger flags below; unknown bits match os.ErrInvalid.
// Wait preserves the kernel's returned condition bits, including unknown ones.
type Events uint32

// Readable requests notification that a read may proceed (EPOLLIN).
const Readable Events = unix.EPOLLIN

// Writable requests notification that a write may proceed (EPOLLOUT).
const Writable Events = unix.EPOLLOUT

// Priority requests notification of an exceptional condition (EPOLLPRI).
const Priority Events = unix.EPOLLPRI

// ReadHangup requests notification of a stream peer's write-side shutdown
// (EPOLLRDHUP). It does not imply that buffered input has been consumed.
const ReadHangup Events = unix.EPOLLRDHUP

// Error reports a source error (EPOLLERR). Linux reports it even when it was
// not requested; it is a readiness condition rather than a Wait error.
const Error Events = unix.EPOLLERR

// Hangup reports peer closure (EPOLLHUP), even when not requested. Buffered
// input may remain readable before EOF.
const Hangup Events = unix.EPOLLHUP

// LevelTriggered is the default: readiness can be reported repeatedly while
// the condition remains true. It contributes no bits to the interest mask.
const LevelTriggered Events = 0

// EdgeTriggered requests EPOLLET notifications when readiness changes. Drain
// nonblocking I/O until it would block before waiting for another edge. Go file
// I/O may itself wait; arrange bounded reads or deadlines as appropriate.
const EdgeTriggered Events = unix.EPOLLET

// OneShot disables a registration after an event is delivered (EPOLLONESHOT).
// Call Registration.Modify with the complete interest mask to rearm it.
const OneShot Events = unix.EPOLLONESHOT

// Event reports advisory readiness for one registration. Events from an already
// returned batch can outlive removal or modification; their identity never
// changes to a new registration. Event order and one event per I/O operation
// are not guaranteed.
type Event struct {
	// Registration is the exact pointer returned by Poller.Add.
	Registration *Registration
	// Events contains kernel condition bits, not the requested trigger flags.
	Events Events
}
