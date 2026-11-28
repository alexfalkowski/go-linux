//go:build linux

package landlock

import (
	"os"

	"golang.org/x/sys/unix"
)

// Synchronization selects which threads receive a Restrict operation. Zero is
// invalid: callers must select their enforcement scope explicitly.
type Synchronization uint8

// CallingThread restricts the locked calling thread and its future children.
const CallingThread Synchronization = 1

// AllThreads atomically synchronizes all threads using TSYNC (ABI 8). Siblings
// receive the caller's resulting domain, replacing any divergent Landlock policy.
// Existing separate processes do not change. There is no local-scope fallback.
const AllThreads Synchronization = 2

// LogFlags selects ABI 7 audit behavior. Zero preserves Linux's defaults.
// Audit delivery depends on host configuration; no log entry is guaranteed.
type LogFlags uint32

// LogSameExecOff disables denial logging until a subsequent exec.
const LogSameExecOff LogFlags = unix.LANDLOCK_RESTRICT_SELF_LOG_SAME_EXEC_OFF

// LogNewExecOn enables denial logging after subsequent exec.
const LogNewExecOn LogFlags = unix.LANDLOCK_RESTRICT_SELF_LOG_NEW_EXEC_ON

// LogSubdomainsOff disables denial logging for subsequently created nested
// domains. Choose this according to caller configuration rather than hardcoding it.
const LogSubdomainsOff LogFlags = unix.LANDLOCK_RESTRICT_SELF_LOG_SUBDOMAINS_OFF

const supportedLog = LogSameExecOff | LogNewExecOn | LogSubdomainsOff

// PrivilegePolicy selects the no-new-privileges transition before restriction.
type PrivilegePolicy uint8

// PreserveNoNewPrivileges leaves the bit as it is. Restriction then requires the
// bit already set or effective CAP_SYS_ADMIN in the caller's user namespace.
const PreserveNoNewPrivileges PrivilegePolicy = 0

// SetNoNewPrivileges irreversibly sets PR_SET_NO_NEW_PRIVS before restriction.
// The bit remains set on failure and successful AllThreads propagates it.
const SetNoNewPrivileges PrivilegePolicy = 1

// Options controls irreversible Restrict changes. Zero options are invalid.
type Options struct {
	// Log selects audit flags; zero preserves the kernel defaults.
	Log LogFlags
	// Synchronization explicitly selects CallingThread or AllThreads.
	Synchronization Synchronization
	// Privileges selects PreserveNoNewPrivileges or SetNoNewPrivileges.
	Privileges PrivilegePolicy
}

// Restrict applies this ruleset using landlock_restrict_self(2). Lock the
// goroutine first and never unlock, including on failure. Options are validated
// before owner lifetime; unsupported ABI options fail before PR_SET_NO_NEW_PRIVS.
// A closed ruleset cannot change privileges. Successful restriction is permanent
// and survives Close and exec. Repeated calls stack domains (at most 16 layers).
// Adding rules later affects future applications, not already applied domains.
// Kernel errors preserve errno; no retries, rollback, or weaker-scope fallback
// occur. Coordinate sibling policies before AllThreads; see package GoDoc.
func (ruleset *Ruleset) Restrict(options Options) error {
	flags, err := options.flags()
	if err != nil {
		return os.NewSyscallError("landlock_restrict_self", err)
	}

	if ruleset == nil {
		return os.NewSyscallError("landlock_restrict_self", os.ErrInvalid)
	}

	ruleset.mu.Lock()
	defer ruleset.mu.Unlock()

	return ruleset.control("landlock_restrict_self", func(fd int) error {
		unsupportedLog := options.Log != 0 && ruleset.abi < 7
		unsupportedSync := options.Synchronization == AllThreads && ruleset.abi < 8
		if unsupportedLog || unsupportedSync {
			return unix.EOPNOTSUPP
		}

		if options.Privileges == SetNoNewPrivileges {
			if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
				return os.NewSyscallError("prctl(PR_SET_NO_NEW_PRIVS)", err)
			}
		}

		_, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, uintptr(fd), flags, 0)
		if errno != 0 {
			return errno
		}
		return nil
	})
}

func (options Options) flags() (uintptr, error) {
	unknownLogFlags := options.Log & ^supportedLog != 0
	invalidPrivileges := options.Privileges > SetNoNewPrivileges
	if unknownLogFlags || invalidPrivileges {
		return 0, os.ErrInvalid
	}

	flags := uintptr(options.Log)
	switch options.Synchronization {
	case CallingThread:
	case AllThreads:
		flags |= unix.LANDLOCK_RESTRICT_SELF_TSYNC
	default:
		return 0, os.ErrInvalid
	}
	return flags, nil
}
