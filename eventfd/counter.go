//go:build linux

package eventfd

import (
	"encoding/binary"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// Mode selects how reads consume the kernel counter. The zero value is Default.
// Modes are exclusive, not a bitmask.
type Mode uint8

// Default returns the accumulated count on each read and resets it to zero.
const Default Mode = 0

// Semaphore returns one on each read and decrements the count by one
// (Linux EFD_SEMAPHORE).
const Semaphore Mode = 1

// New creates a counter initialized to initial using [eventfd(2)]. The caller
// owns the result and must close it. Subsequent additions can use 64-bit values.
// Unknown modes fail with os.ErrInvalid before descriptor creation.
//
// Creation atomically sets close-on-exec and nonblocking flags. Counter I/O
// waits through Go's poller rather than returning EAGAIN. If deadline support
// cannot be established, New closes the descriptor and returns the error.
// There is no fallback to blocking descriptors or weaker flags. Syscall errors
// preserve errno identity through errors.Is and errors.As.
//
// [eventfd(2)]: https://man7.org/linux/man-pages/man2/eventfd.2.html
func New(initial uint32, mode Mode) (*Counter, error) {
	flags := unix.EFD_CLOEXEC | unix.EFD_NONBLOCK
	switch mode {
	case Default:
	case Semaphore:
		flags |= unix.EFD_SEMAPHORE
	default:
		return nil, &os.PathError{Op: "eventfd2", Path: "eventfd", Err: os.ErrInvalid}
	}

	fd, err := unix.Eventfd(uint(initial), flags)
	if err != nil {
		return nil, &os.PathError{Op: "eventfd2", Path: "eventfd", Err: err}
	}

	file := os.NewFile(uintptr(fd), "eventfd")
	// NewFile can retain a descriptor even if poller registration failed.
	// Require polling so deadlines and Close can interrupt waiting I/O.
	if err := file.SetDeadline(time.Time{}); err != nil {
		file.Close()
		return nil, err
	}

	return &Counter{file: file}, nil
}

// Counter owns an eventfd. Its methods are safe for concurrent use. Multiple
// readers compete for records; sequences of calls are not transactions. Do not
// copy a Counter after first use. The zero value owns no descriptor and its
// error-returning methods report os.ErrInvalid. Nil receivers are unsupported.
//
// Valid I/O and deadline operations after Close match os.ErrClosed. Invalid
// buffer sizes take precedence over closed-state errors. Kernel failures are
// wrapped in os.PathError without losing errno identity.
type Counter struct {
	file *os.File
}

// Read consumes one [eventfd(2)] record into p. It requires at least eight
// bytes, returns exactly eight on success in native byte order, and leaves any
// remaining bytes untouched. Smaller buffers, including nil, fail with EINVAL
// without consuming the count. A zero count waits for an addition, a read
// deadline, or Close. Default mode drains the count; Semaphore consumes one.
//
// [eventfd(2)]: https://man7.org/linux/man-pages/man2/eventfd.2.html
func (c *Counter) Read(p []byte) (int, error) {
	if c.file == nil {
		return 0, os.ErrInvalid
	}
	if len(p) < 8 {
		return 0, &os.PathError{Op: "read", Path: "eventfd", Err: unix.EINVAL}
	}

	return c.file.Read(p[:8])
}

// Write adds one native-endian value using [eventfd(2)]. It requires exactly
// eight bytes and returns eight on success. Invalid sizes fail with EINVAL;
// the kernel also rejects the all-ones value with EINVAL. These errors leave
// the count unchanged; zero is allowed. Additions exceeding the maximum count
// (2^64-2) wait for a read, a write deadline, or
// Close. No partial record is added on error.
//
// [eventfd(2)]: https://man7.org/linux/man-pages/man2/eventfd.2.html
func (c *Counter) Write(p []byte) (int, error) {
	if c.file == nil {
		return 0, os.ErrInvalid
	}
	if len(p) != 8 {
		return 0, &os.PathError{Op: "write", Path: "eventfd", Err: unix.EINVAL}
	}

	return c.file.Write(p)
}

// ReadValue decodes one destructive Read. It drains the accumulated count in
// Default mode or returns one in Semaphore mode, waiting under Read's rules.
// It returns zero on error.
func (c *Counter) ReadValue() (uint64, error) {
	var record [8]byte
	if _, err := c.Read(record[:]); err != nil {
		return 0, err
	}

	return binary.NativeEndian.Uint64(record[:]), nil
}

// Add encodes value and calls Write, including its waiting and error behavior.
// Zero is allowed; the kernel rejects the all-ones value with EINVAL. Closed
// state or an expired write deadline can prevent reaching kernel validation.
func (c *Counter) Add(value uint64) error {
	var record [8]byte
	binary.NativeEndian.PutUint64(record[:], value)
	_, err := c.Write(record[:])
	return err
}

// Close releases the owned descriptor and interrupts poll-blocked I/O with an
// error matching os.ErrClosed. Repeated Close also matches os.ErrClosed.
func (c *Counter) Close() error {
	return c.file.Close()
}

// SetDeadline wraps [os.File.SetDeadline] for both reads and writes. It affects
// pending and future I/O; zero clears both deadlines. Expired I/O matches
// os.ErrDeadlineExceeded.
func (c *Counter) SetDeadline(deadline time.Time) error {
	return c.deadlineError(c.file.SetDeadline(deadline))
}

// SetReadDeadline wraps [os.File.SetReadDeadline], affecting pending and future
// reads. Zero clears the read deadline without changing the write deadline.
func (c *Counter) SetReadDeadline(deadline time.Time) error {
	return c.deadlineError(c.file.SetReadDeadline(deadline))
}

// SetWriteDeadline wraps [os.File.SetWriteDeadline], affecting pending and future
// writes. Zero clears the write deadline without changing the read deadline.
func (c *Counter) SetWriteDeadline(deadline time.Time) error {
	return c.deadlineError(c.file.SetWriteDeadline(deadline))
}

func (c *Counter) deadlineError(err error) error {
	if err == nil || c.file == nil {
		return err
	}

	// New requires a pollable file. Its deadline setters make no syscalls
	// and can only fail here if Close won the lifetime reference. os.File
	// returns the poller's private closing error without normalizing it.
	return &os.PathError{Op: "deadline", Path: "eventfd", Err: os.ErrClosed}
}
