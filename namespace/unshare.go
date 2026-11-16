//go:build linux

package namespace

import (
	"os"

	"golang.org/x/sys/unix"
)

// Unshare creates namespaces with unshare(CLONE_NEW*), using types combined
// with |. Zero or unknown bits match os.ErrInvalid before any syscall. No
// namespace descriptor is acquired. The caller must follow the dedicated
// locked-goroutine rule in the package documentation, including on failure;
// there is no rollback.
//
// Mount also unshares filesystem attributes, and IPC unshares System V semaphore
// adjustments, as required by Linux. PID and Time create namespaces for future
// children without moving the caller; inspect /proc/thread-self/ns/pid_for_children
// or time_for_children with Open. User creation requires a single-threaded
// process and is unsuitable for ordinary Go programs; use os/exec and
// syscall.SysProcAttr for creation/mappings before the child runtime starts.
// When PID is requested, Go's process support is initialized before namespace
// state changes so its lazy pidfd probe cannot become the new namespace's PID 1.
// Any temporary process reference is released before unshare is called.
// Capabilities, kernel configuration, quotas, and security policy apply. Errors
// wrap os.SyscallError and preserve Linux errno; partial effects are not undone.
func Unshare(types Type) error {
	if types == 0 || types & ^supportedTypes != 0 {
		return os.NewSyscallError("unshare", os.ErrInvalid)
	}
	if types&PID != 0 {
		process, err := os.FindProcess(os.Getpid())
		if err != nil {
			return os.NewSyscallError("unshare", err)
		}
		if err := process.Release(); err != nil {
			return os.NewSyscallError("unshare", err)
		}
	}
	return os.NewSyscallError("unshare", unix.Unshare(int(types)))
}
