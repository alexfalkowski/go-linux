//go:build linux

package namespace

import "golang.org/x/sys/unix"

// Type identifies a Linux namespace using its CLONE_NEW* value. Current takes
// one type; Unshare accepts types combined with |. Zero and unknown input bits
// are invalid. Type inspection preserves unknown kernel values.
type Type uint32

// Mount identifies mount namespaces (CLONE_NEWNS).
const Mount Type = unix.CLONE_NEWNS

// Cgroup identifies cgroup namespaces (CLONE_NEWCGROUP).
const Cgroup Type = unix.CLONE_NEWCGROUP

// IPC identifies System V IPC and POSIX message queue namespaces (CLONE_NEWIPC).
const IPC Type = unix.CLONE_NEWIPC

// Network identifies network namespaces (CLONE_NEWNET).
const Network Type = unix.CLONE_NEWNET

// PID identifies process ID namespaces (CLONE_NEWPID).
const PID Type = unix.CLONE_NEWPID

// Time identifies monotonic/boottime clock namespaces (CLONE_NEWTIME).
const Time Type = unix.CLONE_NEWTIME

// User identifies user ID, group ID, and capability namespaces (CLONE_NEWUSER).
const User Type = unix.CLONE_NEWUSER

// UTS identifies hostname and domain name namespaces (CLONE_NEWUTS).
const UTS Type = unix.CLONE_NEWUTS

const supportedTypes = Mount | Cgroup | IPC | Network | PID | Time | User | UTS

func namespaceName(kind Type) string {
	switch kind {
	case Mount:
		return "mnt"
	case Cgroup:
		return "cgroup"
	case IPC:
		return "ipc"
	case Network:
		return "net"
	case PID:
		return "pid"
	case Time:
		return "time"
	case User:
		return "user"
	case UTS:
		return "uts"
	default:
		return ""
	}
}
