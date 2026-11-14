//go:build linux

package pidfd

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Open acquires a process (thread-group leader) handle using [pidfd_open(2)].
// The caller owns the result and must close it. pid must be positive and fit
// Linux's signed 32-bit PID type; invalid values match os.ErrInvalid.
//
// The descriptor is atomically close-on-exec. Acquisition by numeric PID can
// race with exit, reaping, and PID reuse: Open cannot establish that pid still
// names an earlier intended process. Once acquired, the handle cannot retarget
// a reused PID. Kernel errors retain their errno through errors.Is/errors.As.
//
// [pidfd_open(2)]: https://man7.org/linux/man-pages/man2/pidfd_open.2.html
func Open(pid int) (*Process, error) {
	if pid <= 0 || pid > math.MaxInt32 {
		return nil, &os.PathError{Op: "pidfd_open", Path: "pidfd", Err: os.ErrInvalid}
	}

	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return nil, &os.PathError{Op: "pidfd_open", Path: "pidfd", Err: err}
	}
	return Adopt(os.NewFile(uintptr(fd), "pidfd"))
}

// OpenProcess acquires an independently owned handle to process. It borrows
// process only during this call and duplicates its stable native handle while
// os.Process.WithHandle protects its lifetime. It never looks up a numeric PID,
// signals, reaps, releases, or otherwise consumes process. The caller must close
// the result and retain the original wait owner.
//
// A nil process matches os.ErrInvalid. Unavailable handles match os.ErrNoHandle;
// released or waited processes preserve WithHandle's errors. There is no PID
// fallback. Acquisition failures return nil and close every acquired descriptor.
func OpenProcess(process *os.Process) (*Process, error) {
	if process == nil {
		return nil, os.ErrInvalid
	}

	var fd int
	var duplicateErr error
	err := process.WithHandle(func(handle uintptr) {
		fd, duplicateErr = unix.FcntlInt(handle, unix.F_DUPFD_CLOEXEC, 0)
	})
	if err != nil {
		return nil, err
	}
	if duplicateErr != nil {
		return nil, os.NewSyscallError("fcntl", duplicateErr)
	}
	return Adopt(os.NewFile(uintptr(fd), "pidfd"))
}

// Adopt takes exclusive ownership of file immediately, including on failure.
// If validation or construction fails, it closes file before returning the
// error. The caller must not use or close file after calling Adopt, or use it
// concurrently with Adopt. Descriptor status flags are never changed.
//
// file must be a close-on-exec process pidfd. Linux 7.0 [pidfs] identity and
// fcntl flags are checked without signaling the target or requiring permission
// to signal it. Nil, other file types, thread pidfds, O_PATH descriptors, and
// missing close-on-exec match os.ErrInvalid. A closed file matches os.ErrClosed.
// Blocking and nonblocking pidfds are accepted; their status flags and any
// preexisting file deadlines are left unchanged. Waits use separate polling.
//
// Use OpenProcess to borrow an os.Process safely. Never adopt a borrowed
// descriptor. Adoption grants no additional reaping or signaling rights. See [pidfd_open(2)] for process-handle semantics.
//
// [pidfs]: https://github.com/torvalds/linux/blob/v7.0/fs/pidfs.c
// [pidfd_open(2)]: https://man7.org/linux/man-pages/man2/pidfd_open.2.html
func Adopt(file *os.File) (*Process, error) {
	if file == nil {
		return nil, os.ErrInvalid
	}
	process := &Process{file: file}
	err := process.control("adopt", func(fd int) error {
		var stat unix.Statfs_t
		if err := unix.Fstatfs(fd, &stat); err != nil {
			return err
		}
		if stat.Type != unix.PID_FS_MAGIC {
			return os.ErrInvalid
		}

		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
		if err != nil {
			return err
		}
		if flags&(unix.PIDFD_THREAD|unix.O_PATH) != 0 {
			return os.ErrInvalid
		}
		flags, err = unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		if err != nil {
			return err
		}
		if flags&unix.FD_CLOEXEC == 0 {
			return os.ErrInvalid
		}
		return nil
	})
	if err != nil {
		file.Close()
		return nil, err
	}

	wake, err := newWakeFile()
	if err != nil {
		file.Close()
		return nil, err
	}
	process.wake = wake
	return process, nil
}

// Process owns a process pidfd and a private eventfd for close interruption.
// Its methods are safe for concurrent use; do not copy a Process after first
// use. The zero value owns nothing and its methods match os.ErrInvalid. Nil
// receivers are unsupported. Closing a handle neither signals nor reaps its
// process. Signal and waits after Close match os.ErrClosed.
//
// WaitReady observers may all succeed. Wait callers compete with each other
// and external reapers: only one can consume child status, and the others
// receive ECHILD. An operation already in a syscall may finish when Close
// overlaps it; Close cannot undo a signal or a completed reap.
type Process struct {
	file   *os.File
	wake   *os.File
	closed atomic.Bool
}

// Signal sends signal to the referenced process using [pidfd_send_signal(2)].
// Zero checks existence and permission without delivering a signal. Negative
// or overflowing signal numbers fail with EINVAL; other invalid numbers and
// permission checks are left to Linux. EPERM and ESRCH retain their identity.
// No process-group or thread signaling mode is exposed.
//
// [pidfd_send_signal(2)]: https://man7.org/linux/man-pages/man2/pidfd_send_signal.2.html
func (p *Process) Signal(signal syscall.Signal) error {
	return p.control("pidfd_send_signal", func(fd int) error {
		if signal < 0 || signal > math.MaxInt32 {
			return unix.EINVAL
		}
		return unix.PidfdSendSignal(fd, signal, nil, 0)
	})
}

// WaitReady waits for [pidfd_open(2)] exit readiness without consuming child
// status. It also succeeds after reaping, and can observe non-children.
// Readiness means the last thread has exited; it does not grant reaping rights.
// Repeated calls succeed while the handle remains open and ready.
//
// Close interrupts pending waits with os.ErrClosed. There is no context or
// deadline API. Each blocked caller uses a kernel poll wait and can occupy an
// OS thread; no background goroutine is started. Existing deadlines on an
// adopted os.File do not affect this wait.
//
// [pidfd_open(2)]: https://man7.org/linux/man-pages/man2/pidfd_open.2.html
func (p *Process) WaitReady() error {
	return p.control("poll", func(fd int) error {
		conn, err := p.wake.SyscallConn()
		if err != nil {
			return err
		}
		var pollErr error
		err = conn.Control(func(wakeFD uintptr) {
			// Linux descriptor allocation is bounded by the signed-int fd ABI.
			//nolint:gosec // Both values are live kernel descriptors held by RawConn.Control.
			fds := []unix.PollFd{
				{Fd: int32(fd), Events: unix.POLLIN},
				{Fd: int32(wakeFD), Events: unix.POLLIN},
			}
			for {
				if p.closed.Load() {
					pollErr = os.ErrClosed
					return
				}
				_, pollErr = unix.Poll(fds, -1)
				if errors.Is(pollErr, unix.EINTR) {
					continue
				}

				if p.closed.Load() {
					pollErr = os.ErrClosed
				}

				if pollErr == nil && fds[0].Revents&(unix.POLLIN|unix.POLLHUP) == 0 {
					pollErr = unix.EIO
				}

				return
			}
		})
		if err != nil {
			return os.ErrClosed
		}
		return pollErr
	})
}

// Wait reaps an eligible child using [waitid(2)] with P_PIDFD and WEXITED.
// Normal exit and signal termination both return an Exit with nil error.
// A non-child or an already reaped child returns ECHILD, including when another
// concurrent waiter wins. Status is not cached. Errors return a zero Exit.
//
// The caller must own reaping. For an exec.Cmd child, use WaitReady here and
// Cmd.Wait to reap and finish pipe cleanup. For an os.StartProcess child,
// choose one wait owner; after this method reaps, call os.Process.Release.
// Traced children and stop/continue notifications are outside this contract.
//
// Wait uses nonblocking status checks plus WaitReady. Close interrupts pending
// readiness waits; a status check that already began may still reap. No raw
// blocking waitid is used, regardless of an adopted descriptor's status flags.
//
// [waitid(2)]: https://man7.org/linux/man-pages/man2/waitid.2.html
func (p *Process) Wait() (Exit, error) {
	for {
		var info unix.Siginfo
		err := p.control("waitid", func(fd int) error {
			for {
				err := unix.Waitid(unix.P_PIDFD, fd, &info, unix.WEXITED|unix.WNOHANG, nil)
				if !errors.Is(err, unix.EINTR) {
					return err
				}
			}
		})
		if err != nil {
			return Exit{}, err
		}

		// x/sys leaves the siginfo union opaque. Linux's SIGCHLD member has
		// pid/uid/status at the union start, aligned to a native word after
		// the three-int header on the supported amd64 and arm64 targets.
		// https://github.com/torvalds/linux/blob/v7.0/include/uapi/asm-generic/siginfo.h
		offset := 12 + int(unsafe.Sizeof(uintptr(0)))/8*4
		data := unsafe.Slice((*byte)(unsafe.Pointer(&info)), int(unsafe.Sizeof(info)))
		if binary.NativeEndian.Uint32(data[offset:offset+4]) != 0 {
			status := int(binary.NativeEndian.Uint32(data[offset+8 : offset+12]))
			if info.Code == 1 { // CLD_EXITED
				return Exit{Code: status}, nil
			}
			return Exit{Code: -1, Signal: syscall.Signal(status), CoreDump: info.Code == 3}, nil // CLD_DUMPED
		}

		if err := p.WaitReady(); err != nil {
			return Exit{}, err
		}
	}
}

// Close releases both owned descriptors and interrupts pending waits. Repeated
// Close matches os.ErrClosed. It neither kills nor reaps the process.
func (p *Process) Close() error {
	if p.file == nil {
		return os.ErrInvalid
	}
	if p.closed.Swap(true) {
		return os.ErrClosed
	}

	// Leave the wake record unread so every concurrent poll waiter sees it.
	var record [8]byte
	binary.NativeEndian.PutUint64(record[:], 1)

	_, err := p.wake.Write(record[:])
	return errors.Join(err, p.file.Close(), p.wake.Close())
}

func (p *Process) control(op string, operation func(int) error) error {
	if p.file == nil {
		return os.ErrInvalid
	}
	if p.closed.Load() {
		return &os.PathError{Op: op, Path: "pidfd", Err: os.ErrClosed}
	}

	conn, err := p.file.SyscallConn()
	if err != nil {
		return err
	}

	var syscallErr error
	err = conn.Control(func(fd uintptr) {
		syscallErr = operation(int(fd))
	})
	if err != nil {
		return &os.PathError{Op: op, Path: "pidfd", Err: os.ErrClosed}
	}
	if syscallErr != nil {
		return &os.PathError{Op: op, Path: "pidfd", Err: syscallErr}
	}
	return nil
}

// Exit describes child termination returned by Wait. Its zero value describes
// normal exit with code zero; consult Wait's error before using the result.
type Exit struct {
	// Code is the normal exit code (0–255), or -1 for signal termination.
	Code int
	// Signal is the terminating signal, or zero after normal exit.
	Signal syscall.Signal
	// CoreDump reports Linux's CLD_DUMPED status. It does not guarantee that
	// a core file was written or made accessible to the caller.
	CoreDump bool
}

// newWakeFile owns acquisition cleanup and verifies polling before the wake
// file can participate in the process owner's wait/close protocol.
func newWakeFile() (*os.File, error) {
	fd, err := unix.Eventfd(0, unix.EFD_CLOEXEC|unix.EFD_NONBLOCK)
	if err != nil {
		return nil, &os.PathError{Op: "eventfd2", Path: "eventfd", Err: err}
	}

	file := os.NewFile(uintptr(fd), "eventfd")
	if err := file.SetDeadline(time.Time{}); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}
