//go:build linux

package seccomp

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Synchronization selects the threads to receive a filter. Zero is invalid;
// callers must explicitly choose the enforcement scope.
type Synchronization uint8

// CallingThread attaches only to the locked calling thread. Other existing
// threads keep their policies; unrelated goroutines may execute on those threads.
const CallingThread Synchronization = 1

// AllThreads requests SECCOMP_FILTER_FLAG_TSYNC. Every existing thread must
// have a compatible filter tree; failures never fall back to CallingThread.
const AllThreads Synchronization = 2

// PrivilegePolicy selects the no-new-privileges transition before loading.
type PrivilegePolicy uint8

// PreserveNoNewPrivileges leaves the bit as it is. Loading then requires the
// bit already set or effective CAP_SYS_ADMIN in the caller's user namespace.
const PreserveNoNewPrivileges PrivilegePolicy = 0

// SetNoNewPrivileges irreversibly sets PR_SET_NO_NEW_PRIVS before loading. The
// bit remains set on failure and successful AllThreads propagates it.
const SetNoNewPrivileges PrivilegePolicy = 1

// LoadFlags requests additional kernel behavior when attaching a filter.
type LoadFlags uint32

// LogNonAllow requests SECCOMP_FILTER_FLAG_LOG for non-Allow results.
// Audit delivery depends on host configuration.
const LogNonAllow LoadFlags = unix.SECCOMP_FILTER_FLAG_LOG

// Options controls irreversible loading. The zero value is invalid because
// Synchronization must be explicit. By default, Load preserves the current
// no-new-privileges bit and uses the kernel's default audit policy.
type Options struct {
	// Synchronization selects CallingThread or AllThreads.
	Synchronization Synchronization
	// Privileges selects PreserveNoNewPrivileges or SetNoNewPrivileges.
	Privileges PrivilegePolicy
	// Flags may request LogNonAllow. Zero uses the kernel's default audit policy.
	Flags LoadFlags
}

// Load attaches this filter using seccomp(SECCOMP_SET_MODE_FILTER). Lock the
// goroutine before calling and never unlock it, even after failure. Nil/zero
// filters and invalid options match os.ErrInvalid before any change.
// SetNoNewPrivileges can set the privilege bit even if Load fails. Successful
// attachment is irreversible.
// Repeated loads stack filters and can reach Linux's aggregate instruction limit.
// Kernel errno is preserved; incompatible AllThreads trees return a
// SynchronizationError. There is no retry or weaker-scope fallback.
func (filter *Filter) Load(options Options) error {
	if filter == nil || len(filter.program) == 0 {
		return os.NewSyscallError("seccomp load", os.ErrInvalid)
	}

	var flags uintptr
	switch options.Synchronization {
	case CallingThread:
		flags = 0
	case AllThreads:
		flags = unix.SECCOMP_FILTER_FLAG_TSYNC
	default:
		return os.NewSyscallError("seccomp load", os.ErrInvalid)
	}
	unknownFlags := options.Flags & ^LogNonAllow != 0
	invalidPrivileges := options.Privileges > SetNoNewPrivileges
	if unknownFlags || invalidPrivileges {
		return os.NewSyscallError("seccomp load", os.ErrInvalid)
	}
	flags |= uintptr(options.Flags)

	if options.Privileges == SetNoNewPrivileges {
		if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
			return os.NewSyscallError("prctl(PR_SET_NO_NEW_PRIVS)", err)
		}
	}

	return filter.attach(flags)
}

func (filter *Filter) attach(flags uintptr) error {
	program := unix.SockFprog{Len: uint16(len(filter.program)), Filter: &filter.program[0]} //nolint:gosec // New bounds length below 4096.
	result, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, flags, uintptr(unsafe.Pointer(&program)))
	runtime.KeepAlive(filter)
	if errno != 0 {
		return os.NewSyscallError("seccomp(SECCOMP_SET_MODE_FILTER)", errno)
	}
	if result != 0 {
		return &SynchronizationError{ThreadID: int(result)}
	}
	return nil
}

// SynchronizationError reports the thread whose filter tree refused TSYNC.
// The new filter was not attached. A privilege bit requested during Load can
// still remain set on the loading thread. Ordinary errno failures instead use
// os.SyscallError. Inspect this error with errors.As.
type SynchronizationError struct {
	// ThreadID is the positive Linux TID returned by seccomp.
	ThreadID int
}

// Error describes the refusing thread without implying an errno value.
func (err *SynchronizationError) Error() string {
	return fmt.Sprintf("seccomp thread synchronization: thread %d has an incompatible filter tree", err.ThreadID)
}

// ActionAvailable probes SECCOMP_GET_ACTION_AVAIL without changing state.
// Pass one named action without data; invalid input matches os.ErrInvalid.
// An unsupported kernel action returns false, nil. Other kernel failures,
// including policy denials and ENOSYS, remain distinguishable errors.
func ActionAvailable(action Action) (bool, error) {
	hasData := uint32(action)&unix.SECCOMP_RET_DATA != 0
	supportedAction := validReturn(uint32(action))
	if hasData || !supportedAction {
		return false, os.NewSyscallError("seccomp action", os.ErrInvalid)
	}

	_, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_GET_ACTION_AVAIL, 0, uintptr(unsafe.Pointer(&action)))
	if errno != 0 {
		if errors.Is(errno, unix.EOPNOTSUPP) {
			return false, nil
		}
		return false, os.NewSyscallError("seccomp(SECCOMP_GET_ACTION_AVAIL)", errno)
	}
	return true, nil
}
