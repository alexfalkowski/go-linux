//go:build linux

package landlock

import "golang.org/x/sys/unix"

// FilesystemAccess is a mask of LANDLOCK_ACCESS_FS rights. Combine named bits
// with |. Zero handles no filesystem access; it grants nothing in an Add rule.
type FilesystemAccess uint64

// Execute permits executing files (ABI 1).
const Execute FilesystemAccess = unix.LANDLOCK_ACCESS_FS_EXECUTE

// WriteFile permits opening files for writing (ABI 1); truncation is separate.
const WriteFile FilesystemAccess = unix.LANDLOCK_ACCESS_FS_WRITE_FILE

// ReadFile permits opening files for reading (ABI 1).
const ReadFile FilesystemAccess = unix.LANDLOCK_ACCESS_FS_READ_FILE

// ReadDir permits opening and reading directories (ABI 1).
const ReadDir FilesystemAccess = unix.LANDLOCK_ACCESS_FS_READ_DIR

// RemoveDir permits removing or renaming directories beneath a rule (ABI 1).
const RemoveDir FilesystemAccess = unix.LANDLOCK_ACCESS_FS_REMOVE_DIR

// RemoveFile permits unlinking or renaming files beneath a rule (ABI 1).
const RemoveFile FilesystemAccess = unix.LANDLOCK_ACCESS_FS_REMOVE_FILE

// MakeChar permits creating character devices beneath a rule (ABI 1).
const MakeChar FilesystemAccess = unix.LANDLOCK_ACCESS_FS_MAKE_CHAR

// MakeDir permits creating directories beneath a rule (ABI 1).
const MakeDir FilesystemAccess = unix.LANDLOCK_ACCESS_FS_MAKE_DIR

// MakeReg permits creating regular files beneath a rule (ABI 1).
const MakeReg FilesystemAccess = unix.LANDLOCK_ACCESS_FS_MAKE_REG

// MakeSock permits creating pathname Unix sockets beneath a rule (ABI 1).
const MakeSock FilesystemAccess = unix.LANDLOCK_ACCESS_FS_MAKE_SOCK

// MakeFIFO permits creating named pipes beneath a rule (ABI 1).
const MakeFIFO FilesystemAccess = unix.LANDLOCK_ACCESS_FS_MAKE_FIFO

// MakeBlock permits creating block devices beneath a rule (ABI 1).
const MakeBlock FilesystemAccess = unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK

// MakeSym permits creating symbolic links beneath a rule (ABI 1).
const MakeSym FilesystemAccess = unix.LANDLOCK_ACCESS_FS_MAKE_SYM

// Refer permits cross-directory links/renames subject to Linux's additional
// source/destination checks (ABI 2). It is denied even when not handled.
const Refer FilesystemAccess = unix.LANDLOCK_ACCESS_FS_REFER

// Truncate permits truncating files (ABI 3), independently of WriteFile.
const Truncate FilesystemAccess = unix.LANDLOCK_ACCESS_FS_TRUNCATE

// IOCTLDev permits device-specific ioctl operations (ABI 5). Common descriptor
// and selected filesystem ioctls are exempt; this is not an ioctl allowlist.
const IOCTLDev FilesystemAccess = unix.LANDLOCK_ACCESS_FS_IOCTL_DEV

// ResolveUnix permits connecting to pathname Unix sockets and sending datagrams
// to them outside the domain (ABI 9, Linux 7.1). Servers created in the same
// domain remain accessible. It is optional beyond the Linux 7.0 baseline.
const ResolveUnix FilesystemAccess = unix.LANDLOCK_ACCESS_FS_RESOLVE_UNIX

const supportedFilesystem = Execute | WriteFile | ReadFile | ReadDir | RemoveDir | RemoveFile |
	MakeChar | MakeDir | MakeReg | MakeSock | MakeFIFO | MakeBlock | MakeSym | Refer | Truncate | IOCTLDev | ResolveUnix

// NetworkAccess is a mask of LANDLOCK_ACCESS_NET rights on port numbers.
// Rules apply to every address using the port; they do not filter IP addresses.
type NetworkAccess uint64

// BindTCP permits explicitly binding TCP sockets to a local port (ABI 4).
const BindTCP NetworkAccess = unix.LANDLOCK_ACCESS_NET_BIND_TCP

// ConnectTCP permits connecting TCP sockets to a remote port (ABI 4).
const ConnectTCP NetworkAccess = unix.LANDLOCK_ACCESS_NET_CONNECT_TCP

// BindUDP permits binding UDP sockets to a local port (ABI 10, Linux 7.2).
// This optional right is not available at the Linux 7.0 baseline.
const BindUDP NetworkAccess = unix.LANDLOCK_ACCESS_NET_BIND_UDP

// ConnectSendUDP permits UDP connect and datagram sends to a remote port
// (ABI 10, Linux 7.2). When BindUDP is also handled, automatic ephemeral binding
// needs BindUDP on port zero or an already-bound socket.
const ConnectSendUDP NetworkAccess = unix.LANDLOCK_ACCESS_NET_CONNECT_SEND_UDP

const supportedNetwork = BindTCP | ConnectTCP | BindUDP | ConnectSendUDP

// Scope restricts IPC to the resulting Landlock domain and its descendants.
// These ABI 6 restrictions have no per-object exception rules.
type Scope uint64

// AbstractUnixSocket prevents connections to abstract Unix sockets outside the
// domain. Filesystem Unix sockets are separate from this scope.
const AbstractUnixSocket Scope = unix.LANDLOCK_SCOPE_ABSTRACT_UNIX_SOCKET

// Signal prevents sending signals to processes outside the domain. It does not
// prevent signaling threads in the same thread group.
const Signal Scope = unix.LANDLOCK_SCOPE_SIGNAL

const supportedScopes = AbstractUnixSocket | Signal

// Config selects denied-by-default access rights and IPC scopes. The zero
// configuration is invalid. Config never adds implicit rights or removes bits
// to fit a kernel. A network-only or scope-only configuration is valid.
type Config struct {
	// HandledFS selects filesystem rights; zero leaves these unhandled except Refer.
	HandledFS FilesystemAccess
	// HandledNet selects port rights; zero leaves networking unhandled.
	HandledNet NetworkAccess
	// Scoped selects domain-local IPC restrictions; zero adds none.
	Scoped Scope
}

func (config Config) valid() bool {
	knownFilesystem := config.HandledFS & ^supportedFilesystem == 0
	knownNetwork := config.HandledNet & ^supportedNetwork == 0
	knownScopes := config.Scoped & ^supportedScopes == 0
	nonempty := config.HandledFS != 0 || config.HandledNet != 0 || config.Scoped != 0
	return knownFilesystem && knownNetwork && knownScopes && nonempty
}

func (config Config) minimumABI() int {
	switch {
	case config.HandledNet&(BindUDP|ConnectSendUDP) != 0:
		return 10
	case config.HandledFS&ResolveUnix != 0:
		return 9
	case config.Scoped != 0:
		return 6
	case config.HandledFS&IOCTLDev != 0:
		return 5
	case config.HandledNet != 0:
		return 4
	case config.HandledFS&Truncate != 0:
		return 3
	case config.HandledFS&Refer != 0:
		return 2
	default:
		return 1
	}
}
