//go:build linux

package epoll

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// New creates an empty epoll instance and a private eventfd for Close wakeups.
// Both descriptors are atomically close-on-exec. On failure it returns nil and
// closes every acquired descriptor. The caller must close a successful result.
// Syscall errors preserve errno identity through errors.Is and errors.As.
func New() (*Poller, error) {
	fd, err := unix.EpollCreate1(unix.EPOLL_CLOEXEC)
	if err != nil {
		return nil, &os.PathError{Op: "epoll_create1", Path: "epoll", Err: err}
	}
	file := os.NewFile(uintptr(fd), "epoll")

	wakeFD, err := unix.Eventfd(0, unix.EFD_CLOEXEC|unix.EFD_NONBLOCK)
	if err != nil {
		file.Close()
		return nil, &os.PathError{Op: "eventfd2", Path: "epoll", Err: err}
	}
	wake := os.NewFile(uintptr(wakeFD), "epoll-wake")
	// An unread level-triggered wake record remains visible to every waiter.
	err = unix.EpollCtl(fd, unix.EPOLL_CTL_ADD, wakeFD, &unix.EpollEvent{Events: unix.EPOLLIN})
	if err != nil {
		file.Close()
		wake.Close()
		return nil, &os.PathError{Op: "epoll_ctl", Path: "epoll", Err: err}
	}
	return &Poller{file: file, wake: wake, registrations: make(map[uint64]*Registration)}, nil
}

// Poller owns an explicit readiness set and its registrations. Its methods and
// Registration methods are safe for concurrent use; sequences are not atomic.
// Concurrent Wait calls compete for events. Do not copy either owner after
// first use. Zero owners match os.ErrInvalid; nil receivers are unsupported.
// Operations after Close match os.ErrClosed, except argument validation takes
// precedence as documented by Add, Modify, and Wait.
type Poller struct {
	file          *os.File
	wake          *os.File
	registrations map[uint64]*Registration
	nextID        uint64
	waits         sync.WaitGroup
	mu            sync.Mutex
	closed        bool
}

// Add registers file with the complete interest mask. Zero interests request
// only the automatically reported Error and Hangup conditions. Nil file or
// unknown bits match os.ErrInvalid before checking the poller's state.
// Unsupported sources, such as ordinary disk files, retain Linux's EPERM.
//
// Add borrows file during this call and duplicates it under lifetime protection
// with F_DUPFD_CLOEXEC. It never consumes file or changes its flags or deadlines,
// even on failure. The registration owns the duplicate until it or the poller
// closes. Closing file does not remove the registration and may no longer signal
// EOF to its peer until the registration closes. Repeated additions of the same
// file create independent registrations. Each successful result must be closed
// or left to Poller.Close. Errors return nil and release any acquired duplicate.
func (p *Poller) Add(file *os.File, interests Events) (*Registration, error) {
	if file == nil || !validInterests(interests) {
		return nil, os.ErrInvalid
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.stateError(); err != nil {
		return nil, err
	}
	if p.nextID == math.MaxUint64 {
		return nil, os.NewSyscallError("epoll_ctl", unix.EOVERFLOW)
	}

	conn, err := file.SyscallConn()
	if err != nil {
		return nil, err
	}

	var fd int
	var duplicateErr error
	err = conn.Control(func(source uintptr) {
		fd, duplicateErr = unix.FcntlInt(source, unix.F_DUPFD_CLOEXEC, 0)
	})
	if err != nil {
		// RawConn.Control returns Go's private closing error without the
		// normalization provided by ordinary os.File operations.
		return nil, &os.PathError{Op: "fcntl", Path: file.Name(), Err: os.ErrClosed}
	}
	if duplicateErr != nil {
		return nil, os.NewSyscallError("fcntl", duplicateErr)
	}

	p.nextID++
	registration := &Registration{poller: p, file: os.NewFile(uintptr(fd), "epoll-source"), id: p.nextID}
	err = p.control(unix.EPOLL_CTL_ADD, registration, interests)
	if err != nil {
		registration.file.Close()
		return nil, err
	}
	p.registrations[registration.id] = registration
	return registration, nil
}

// Wait writes up to len(events) readiness results into events and returns their
// count. Only events[:n] is changed on success; errors return zero. Empty buffers
// or buffers larger than Linux's epoll_event array limit match os.ErrInvalid
// before checking state. Each caller must supply its own result buffer.
//
// A negative timeout waits indefinitely; zero polls immediately; positive values
// bound the wait using Go's monotonic clock. Expiry returns (0, nil). Kernel waits
// round positive durations up to milliseconds and may overrun due to scheduling.
// Long durations use bounded epoll waits without integer truncation. EINTR is
// retried against the original timeout. Source I/O deadlines do not affect Wait.
//
// Close interrupts every pending Wait with os.ErrClosed. Removal racing a wait
// filters events not yet translated to results; an already returned event can
// still name a removed registration. Modify can overlap an older event. No
// goroutines or signal masks are installed; a blocked call can occupy an OS
// thread. This uses baseline epoll waiting, with no newer-wait fallback.
func (p *Poller) Wait(events []Event, timeout time.Duration) (int, error) {
	if len(events) == 0 || len(events) > math.MaxInt32/int(unsafe.Sizeof(unix.EpollEvent{})) {
		return 0, os.ErrInvalid
	}

	p.mu.Lock()
	err := p.stateError()
	if err == nil {
		p.waits.Add(1)
	}
	p.mu.Unlock()
	if err != nil {
		return 0, err
	}
	defer p.waits.Done()

	return p.waitReady(events, timeout)
}

// Close interrupts all waits, waits for them to leave the kernel, and releases
// the epoll instance, wake file, and all registration duplicates. It leaves
// caller-owned source files open. Repeated Close matches os.ErrClosed. A result
// already translated before Close can still succeed. Close errors do not permit
// retrying descriptor closure; all owned resources are attempted once.
func (p *Poller) Close() error {
	p.mu.Lock()
	if err := p.stateError(); err != nil {
		p.mu.Unlock()
		return err
	}
	p.closed = true
	p.mu.Unlock()

	var record [8]byte
	binary.NativeEndian.PutUint64(record[:], 1)
	_, wakeErr := p.wake.Write(record[:])
	p.waits.Wait()

	err := errors.Join(wakeErr, p.file.Close(), p.wake.Close())
	for _, registration := range p.registrations {
		err = errors.Join(err, registration.file.Close())
	}
	// No operation accesses the map after observing the closed state.
	p.registrations = nil
	return err
}

// waitReady retains Wait's admission reference across timeout/EINTR retries
// and batches discarded after concurrent registration removal.
func (p *Poller) waitReady(events []Event, timeout time.Duration) (int, error) {
	// Elapsed time retains monotonic measurement even for MaxInt64 durations;
	// adding those durations to time.Now can overflow its monotonic component.
	started := time.Now()
	ready := make([]unix.EpollEvent, len(events))

	for {
		n, err := p.wait(ready, waitMilliseconds(timeout, started))
		count, translateErr := p.translate(events, ready[:n])
		if translateErr != nil {
			return 0, translateErr
		}
		if err != nil && !errors.Is(err, unix.EINTR) {
			return 0, err
		}
		if count > 0 {
			return count, nil
		}
		if timeout == 0 || (timeout > 0 && time.Since(started) >= timeout) {
			return 0, nil
		}
	}
}

// translate serializes event identity lookup with removal and Close. A batch
// already translated can outlive removal, but can never identify a replacement.
func (p *Poller) translate(events []Event, ready []unix.EpollEvent) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return 0, os.ErrClosed
	}

	count := 0
	for _, event := range ready {
		// Fd and Pad hold the two opaque data halves, never descriptor numbers.
		id := uint64(uint32(event.Fd)) | uint64(uint32(event.Pad))<<32 //nolint:gosec // Preserve all opaque token bits.
		if registration := p.registrations[id]; registration != nil {
			events[count] = Event{Registration: registration, Events: Events(event.Events)}
			count++
		}
	}
	return count, nil
}

// stateError requires p.mu. Closed state prevents further control operations
// and wait admissions before Close releases resources.
func (p *Poller) stateError() error {
	if p.file == nil {
		return os.ErrInvalid
	}
	if p.closed {
		return os.ErrClosed
	}
	return nil
}

// control requires p.mu, which excludes registration closure and Poller.Close.
func (p *Poller) control(operation int, registration *Registration, interests Events) error {
	event := unix.EpollEvent{
		Events: uint32(interests),
		//nolint:gosec // Split the opaque token into x/sys's two data fields.
		Fd: int32(registration.id),
		//nolint:gosec // The upper half is also an opaque bit pattern.
		Pad: int32(registration.id >> 32),
	}

	conn, err := p.file.SyscallConn()
	if err != nil {
		return err
	}

	var controlErr error
	err = conn.Control(func(fd uintptr) {
		source, err := registration.file.SyscallConn()
		if err != nil {
			controlErr = err
			return
		}
		var syscallErr error
		controlErr = source.Control(func(sourceFD uintptr) {
			syscallErr = unix.EpollCtl(int(fd), operation, int(sourceFD), &event)
		})
		if controlErr == nil {
			controlErr = syscallErr
		}
	})
	if err != nil {
		return err
	}
	if controlErr != nil {
		return &os.PathError{Op: "epoll_ctl", Path: "epoll", Err: controlErr}
	}
	return nil
}

func (p *Poller) wait(events []unix.EpollEvent, milliseconds int) (int, error) {
	conn, err := p.file.SyscallConn()
	if err != nil {
		return 0, err
	}

	var count int
	var waitErr error
	err = conn.Control(func(fd uintptr) {
		count, waitErr = unix.EpollWait(int(fd), events, milliseconds)
	})
	if err != nil {
		return 0, err
	}
	if waitErr != nil {
		return 0, &os.PathError{Op: "epoll_wait", Path: "epoll", Err: waitErr}
	}
	return count, nil
}

func waitMilliseconds(timeout time.Duration, started time.Time) int {
	if timeout < 0 {
		return -1
	}
	if timeout == 0 {
		return 0
	}
	remaining := timeout - time.Since(started)
	if remaining <= 0 {
		return 0
	}
	return int(min((remaining-1)/time.Millisecond+1, math.MaxInt32))
}

func validInterests(interests Events) bool {
	const supported = Readable | Writable | Priority | ReadHangup | Error | Hangup | EdgeTriggered | OneShot
	return interests & ^supported == 0
}
