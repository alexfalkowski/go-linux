//go:build linux

package timerfd

import (
	"encoding/binary"
	"io"
	"math"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Clock selects a Linux timer clock. The zero value is Monotonic.
// Clock values are exclusive, not a bitmask or raw Linux clock IDs.
type Clock uint8

// Monotonic measures time since an unspecified origin, excluding suspend time
// (Linux CLOCK_MONOTONIC). It is unaffected by discontinuous wall-clock changes.
const Monotonic Clock = 0

// Realtime measures wall-clock time since the Unix epoch (Linux CLOCK_REALTIME).
// Absolute timers follow changes to this clock.
const Realtime Clock = 1

// Boottime is like Monotonic but includes suspend time (Linux CLOCK_BOOTTIME).
// It does not wake a suspended system.
const Boottime Clock = 2

// SetFlags controls absolute arming. The zero value selects no extra behavior.
type SetFlags uint8

// CancelOnSet requests ECANCELED on a read after a discontinuous Realtime
// clock change (Linux TFD_TIMER_CANCEL_ON_SET). It is valid only for SetAt
// on a Realtime timer. It does not detect gradual clock adjustments.
const CancelOnSet SetFlags = 1

// Now queries clock using [clock_gettime(2)]. Unknown clocks fail with
// os.ErrInvalid. The result belongs to that Linux clock domain, including
// the caller's time namespace, rather than Go's private monotonic origin.
//
// [clock_gettime(2)]: https://man7.org/linux/man-pages/man2/clock_gettime.2.html
func Now(clock Clock) (Instant, error) {
	id, err := clockID(clock)
	if err != nil {
		return Instant{}, &os.PathError{Op: "clock_gettime", Path: "timerfd", Err: err}
	}

	var value unix.Timespec
	if err := unix.ClockGettime(id, &value); err != nil {
		return Instant{}, &os.PathError{Op: "clock_gettime", Path: "timerfd", Err: err}
	}
	seconds, nanoseconds := value.Unix()
	return Instant{Clock: clock, Seconds: seconds, Nanoseconds: nanoseconds}, nil
}

// Instant is an absolute reading of Clock in seconds and nanoseconds from its
// origin. Nanoseconds must be in [0, 1e9). SetAt requires a nonnegative value
// representable as signed 64-bit nanoseconds; larger values fail with EOVERFLOW
// instead of being silently clamped by Linux. A zero timestamp disarms.
// Realtime uses the Unix epoch; other clocks must not use time.Time.Unix values.
type Instant struct {
	// Clock must match the timer even when the timestamp disarms it.
	Clock Clock

	// Seconds counts whole seconds from Clock's origin.
	Seconds int64

	// Nanoseconds is the fractional part, normalized into [0, 1e9).
	Nanoseconds int64
}

// State reports relative time until the next expiration and the repeat
// interval, even when armed absolutely. An expired one-shot has zero
// Remaining; this does not imply that there are no unread expirations.
type State struct {
	// Remaining is relative to the query, regardless of the arming mode.
	Remaining time.Duration

	// Interval is the repeat period; zero selects a one-shot.
	Interval time.Duration
}

// New creates a disarmed timer using [timerfd_create(2)]. The caller owns the
// result and must close it. Unknown clocks fail with os.ErrInvalid before
// creation. Descriptors are atomically nonblocking and close-on-exec, while
// Read waits through Go's poller. If polling/deadlines cannot be established,
// New closes the descriptor and returns the error without a fallback.
//
// [timerfd_create(2)]: https://man7.org/linux/man-pages/man2/timerfd_create.2.html
func New(clock Clock) (*Timer, error) {
	id, err := clockID(clock)
	if err != nil {
		return nil, &os.PathError{Op: "timerfd_create", Path: "timerfd", Err: err}
	}
	fd, err := unix.TimerfdCreate(int(id), unix.TFD_CLOEXEC|unix.TFD_NONBLOCK)
	if err != nil {
		return nil, &os.PathError{Op: "timerfd_create", Path: "timerfd", Err: err}
	}

	file := os.NewFile(uintptr(fd), "timerfd")
	// NewFile can retain a descriptor even if Go poller registration fails.
	if err := file.SetReadDeadline(time.Time{}); err != nil {
		file.Close()
		return nil, err
	}
	return &Timer{file: file, clock: clock}, nil
}

// Timer owns a Linux timerfd. Its methods are safe for concurrent use; reads
// compete for counts and sequences of calls are not transactions. Do not copy
// a Timer after first use. The zero value owns no descriptor and reports
// os.ErrInvalid. Nil receivers are unsupported.
//
// Valid operations after Close match os.ErrClosed. Invalid arguments take
// precedence after Close. Syscall failures retain errno through os.PathError
// for errors.Is/errors.As; permission errors are not classified as unsupported.
type Timer struct {
	file  *os.File
	clock Clock
}

// SetAfter arms a relative timer using [timerfd_settime(2)] and returns its
// previous state from the same syscall. Zero initial disarms, even if interval
// is nonzero; zero interval selects a one-shot. Negative durations fail with
// os.ErrInvalid without changing the timer. Arming resets unread expirations.
//
// Setters may report EOVERFLOW after the change has taken effect if the old
// kernel state cannot fit time.Duration. On error the returned State is zero.
// A pending clock cancellation can cause ECANCELED even though Linux applied
// the new settings; inspect State before deciding whether to retry.
//
// [timerfd_settime(2)]: https://man7.org/linux/man-pages/man2/timerfd_create.2.html
func (t *Timer) SetAfter(initial, interval time.Duration) (State, error) {
	if t.file == nil {
		return State{}, os.ErrInvalid
	}
	if initial < 0 || interval < 0 {
		return State{}, &os.PathError{Op: "timerfd_settime", Path: "timerfd", Err: os.ErrInvalid}
	}
	return t.set(0, unix.NsecToTimespec(int64(initial)), interval)
}

// SetAt arms an absolute timer using [timerfd_settime(2)] and returns its
// previous relative state. The timestamp's clock must match this timer; use
// Now to obtain the correct origin. Zero seconds and nanoseconds disarm;
// a nonzero past deadline expires immediately. Interval is always relative.
//
// Negative or unnormalized timestamps, mismatched clocks, negative intervals,
// and invalid flags fail with os.ErrInvalid. Timestamps beyond signed 64-bit
// nanoseconds fail with EOVERFLOW. These validation errors leave the timer
// unchanged. CancelOnSet is accepted only on Realtime timers. Other setter
// effects and errors follow SetAfter, including ECANCELED after rearming.
//
// [timerfd_settime(2)]: https://man7.org/linux/man-pages/man2/timerfd_create.2.html
func (t *Timer) SetAt(deadline Instant, interval time.Duration, flags SetFlags) (State, error) {
	if t.file == nil {
		return State{}, os.ErrInvalid
	}
	if deadline.Clock != t.clock || interval < 0 || flags & ^CancelOnSet != 0 || (flags != 0 && t.clock != Realtime) {
		return State{}, &os.PathError{Op: "timerfd_settime", Path: "timerfd", Err: os.ErrInvalid}
	}
	value, err := duration(deadline.Seconds, deadline.Nanoseconds)
	if err != nil {
		return State{}, &os.PathError{Op: "timerfd_settime", Path: "timerfd", Err: err}
	}
	options := unix.TFD_TIMER_ABSTIME
	if flags&CancelOnSet != 0 {
		options |= unix.TFD_TIMER_CANCEL_ON_SET
	}
	return t.set(options, unix.NsecToTimespec(int64(value)), interval)
}

// Disarm stops the timer, clears its interval and unread expirations, and
// returns its previous state using SetAfter(0, 0). It does not wake a waiting
// Read; use a read deadline or Close to interrupt a reader.
func (t *Timer) Disarm() (State, error) {
	return t.SetAfter(0, 0)
}

// State inspects the timer using [timerfd_gettime(2)] without consuming unread
// expirations. It returns EOVERFLOW and a zero State if a kernel duration is
// unrepresentable. It is a snapshot, not a transaction with arming or reads.
//
// [timerfd_gettime(2)]: https://man7.org/linux/man-pages/man2/timerfd_create.2.html
func (t *Timer) State() (State, error) {
	var value unix.ItimerSpec
	err := t.control("timerfd_gettime", func(fd int) error {
		return unix.TimerfdGettime(fd, &value)
	})
	if err != nil {
		return State{}, err
	}
	return state("timerfd_gettime", &value)
}

// Read consumes one [timerfd_create(2)] expiration record. It requires at
// least eight bytes and normally returns eight in native byte order, leaving
// the buffer tail untouched. Smaller buffers, including nil, fail with EINVAL
// without consumption. With no expirations it waits for an expiration, a read
// deadline, or Close. Rearming/disarming resets unread counts.
//
// A canceled Realtime timer returns ECANCELED. Without CancelOnSet, a backward
// Realtime jump can instead produce (0, nil); this is not permanent EOF.
//
// [timerfd_create(2)]: https://man7.org/linux/man-pages/man2/timerfd_create.2.html
func (t *Timer) Read(p []byte) (int, error) {
	if t.file == nil {
		return 0, os.ErrInvalid
	}
	if len(p) < 8 {
		return 0, &os.PathError{Op: "read", Path: "timerfd", Err: unix.EINVAL}
	}
	n, err := t.file.Read(p[:8])
	// os.File turns a successful zero-byte syscall into EOF. A timerfd has
	// no permanent EOF; restore its exceptional clock-change result.
	if err == io.EOF {
		return n, nil
	}
	return n, err
}

// ReadExpirations decodes one destructive Read, returning zero on error.
// A zero-byte clock-change result becomes io.ErrNoProgress, not a count or EOF.
func (t *Timer) ReadExpirations() (uint64, error) {
	var record [8]byte
	n, err := t.Read(record[:])
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, &os.PathError{Op: "read", Path: "timerfd", Err: io.ErrNoProgress}
	}
	return binary.NativeEndian.Uint64(record[:]), nil
}

// Close releases the owned descriptor and interrupts poll-blocked reads with
// os.ErrClosed. Repeated Close also matches os.ErrClosed. The kernel destroys
// the timer when its last descriptor closes. Close does not cancel an active
// raw callback; callbacks must return promptly.
func (t *Timer) Close() error {
	return t.file.Close()
}

// SyscallConn returns the owned file's [syscall.RawConn], implementing
// syscall.Conn. Callbacks borrow the descriptor: do not close it, retain its
// number, or clear nonblocking mode. Duplicate it within Control for independent
// ownership; duplicates share timer state, counts, and file status flags.
//
// Direct nonblocking reads in Control return EAGAIN without expirations.
// RawConn.Read uses Go polling when its callback returns false on EAGAIN.
// Capture syscall errors separately from callback errors. Raw connection errors
// retain os.File's behavior and need not match os.ErrClosed after close.
func (t *Timer) SyscallConn() (syscall.RawConn, error) {
	return t.file.SyscallConn()
}

// SetReadDeadline wraps [os.File.SetReadDeadline] for pending and future reads.
// Zero clears it; expired reads match os.ErrDeadlineExceeded. This Go I/O
// deadline does not change the kernel timer or affect arming/state operations.
func (t *Timer) SetReadDeadline(deadline time.Time) error {
	err := t.file.SetReadDeadline(deadline)
	if err == nil || t.file == nil {
		return err
	}
	// New requires polling; deadline setters can then fail only on close.
	return &os.PathError{Op: "deadline", Path: "timerfd", Err: os.ErrClosed}
}

func (t *Timer) set(flags int, initial unix.Timespec, interval time.Duration) (State, error) {
	value := unix.ItimerSpec{Value: initial, Interval: unix.NsecToTimespec(int64(interval))}
	var old unix.ItimerSpec
	err := t.control("timerfd_settime", func(fd int) error {
		return unix.TimerfdSettime(fd, flags, &value, &old)
	})
	if err != nil {
		return State{}, err
	}
	return state("timerfd_settime", &old)
}

func (t *Timer) control(op string, operation func(int) error) error {
	conn, err := t.SyscallConn()
	if err != nil {
		return err
	}
	var syscallErr error
	err = conn.Control(func(fd uintptr) { syscallErr = operation(int(fd)) })
	if err != nil {
		// RawControl can fail only to acquire the file's lifetime reference.
		return &os.PathError{Op: op, Path: "timerfd", Err: os.ErrClosed}
	}
	if syscallErr != nil {
		return &os.PathError{Op: op, Path: "timerfd", Err: syscallErr}
	}
	return nil
}

func clockID(clock Clock) (int32, error) {
	switch clock {
	case Monotonic:
		return unix.CLOCK_MONOTONIC, nil
	case Realtime:
		return unix.CLOCK_REALTIME, nil
	case Boottime:
		return unix.CLOCK_BOOTTIME, nil
	default:
		return 0, os.ErrInvalid
	}
}

func duration(seconds, nanoseconds int64) (time.Duration, error) {
	if seconds < 0 || nanoseconds < 0 || nanoseconds >= 1e9 {
		return 0, os.ErrInvalid
	}
	if seconds > math.MaxInt64/int64(time.Second) || (seconds == math.MaxInt64/int64(time.Second) && nanoseconds > math.MaxInt64%int64(time.Second)) {
		return 0, unix.EOVERFLOW
	}
	return time.Duration(seconds*1e9 + nanoseconds), nil
}

func state(op string, value *unix.ItimerSpec) (State, error) {
	remaining, err := duration(value.Value.Unix())
	if err != nil {
		return State{}, &os.PathError{Op: op, Path: "timerfd", Err: err}
	}
	interval, err := duration(value.Interval.Unix())
	if err != nil {
		return State{}, &os.PathError{Op: op, Path: "timerfd", Err: err}
	}
	return State{Remaining: remaining, Interval: interval}, nil
}
