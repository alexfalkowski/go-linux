//go:build linux

package landlock

import (
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ABI queries LANDLOCK_CREATE_RULESET_VERSION without acquiring a descriptor
// or applying restrictions. It returns the highest kernel ABI, including newer
// versions unknown to this package. Errors return zero and preserve errno:
// ENOSYS means absent, EOPNOTSUPP disabled; policy denials stay distinguishable.
func ABI() (int, error) {
	version, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 {
		return 0, os.NewSyscallError("landlock_create_ruleset version", errno)
	}
	return int(version), nil
}

// New creates an owned, atomically close-on-exec ruleset using
// landlock_create_ruleset(2). It does not change thread restrictions. Invalid
// Config matches os.ErrInvalid before ABI discovery; unavailable named rights
// match EOPNOTSUPP. No rights are silently removed. Failure returns nil and
// retains no descriptor. The caller must Close a successful result.
func New(config Config) (*Ruleset, error) {
	if !config.valid() {
		return nil, os.NewSyscallError("landlock_create_ruleset", os.ErrInvalid)
	}

	version, err := ABI()
	if err != nil {
		return nil, err
	}
	if version < config.minimumABI() {
		return nil, os.NewSyscallError("landlock_create_ruleset", unix.EOPNOTSUPP)
	}

	attr := unix.LandlockRulesetAttr{
		Handled_access_fs: uint64(config.HandledFS), Handled_access_net: uint64(config.HandledNet), Scoped: uint64(config.Scoped),
	}
	// The maintained binding includes newer quiet fields. Pass only the ABI 6
	// prefix; its three uint64 fields also accept zero extension on older ABIs.
	size := unsafe.Offsetof(attr.Quiet_access_fs)

	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&attr)), size, 0)
	if errno != 0 {
		return nil, os.NewSyscallError("landlock_create_ruleset", errno)
	}
	return &Ruleset{file: os.NewFile(fd, "landlock"), config: config, abi: version}, nil
}

// Ruleset owns a kernel ruleset and implements io.Closer. Do not copy it after
// first use. Methods serialize with Close; sequences require caller coordination.
// Nil/zero owners match os.ErrInvalid. Valid operations on closed owners and
// repeated Close match os.ErrClosed. Argument validation precedes owner checks.
type Ruleset struct {
	file   *os.File
	config Config
	abi    int
	mu     sync.Mutex
}

// AddPath allows access to the file or directory hierarchy identified by path,
// using landlock_add_rule(2). It opens O_PATH internally, following symlinks;
// relative paths use the caller's current directory. All temporary resources
// are released on success and failure. This is path lookup, not a secure path
// resolver: use file.Open and AddFile for explicit resolution policy.
// Access must be nonzero and a subset of HandledFS, or match os.ErrInvalid.
// Linux rejects directory-only rights on nondirectories. A failed add does not
// remove earlier rules. Rules identify objects rather than saved path strings.
func (ruleset *Ruleset) AddPath(path string, access FilesystemAccess) error {
	if ruleset == nil || !ruleset.validFilesystem(access) {
		return os.NewSyscallError("landlock_add_rule path", os.ErrInvalid)
	}

	ruleset.mu.Lock()
	defer ruleset.mu.Unlock()

	return ruleset.control("landlock_add_rule path", func(fd int) error {
		parent, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
		if err != nil {
			return &os.PathError{Op: "open", Path: path, Err: err}
		}
		defer unix.Close(parent) // Never retry close; even failure releases our reference.
		return addPath(fd, parent, access)
	})
}

// AddFile borrows file to allow access to its object/hierarchy with the same
// rights and kernel validation as AddPath. An O_PATH file from file.Open is
// accepted. The input stays open on every outcome. It is pinned against Close
// during the add; after success the caller may close it independently. Nil input
// matches os.ErrInvalid. AddFile does not reopen file.Name().
func (ruleset *Ruleset) AddFile(file *os.File, access FilesystemAccess) error {
	if ruleset == nil || !ruleset.validFilesystem(access) || file == nil {
		return os.NewSyscallError("landlock_add_rule file", os.ErrInvalid)
	}

	ruleset.mu.Lock()
	defer ruleset.mu.Unlock()

	return ruleset.control("landlock_add_rule file", func(fd int) error {
		return fileControl(file, func(parent int) error {
			return addPath(fd, parent, access)
		})
	})
}

// AddPort allows access on port in host byte order using a NET_PORT rule.
// Port zero allows requests for ephemeral binding; it does not allow arbitrary
// destination ports or all explicit local ports. Access must be nonzero and a
// subset of HandledNet, or match os.ErrInvalid. Rules apply to all IP addresses
// and both IPv4/IPv6; automatic TCP client binding is not BindTCP enforcement.
func (ruleset *Ruleset) AddPort(port uint16, access NetworkAccess) error {
	if ruleset == nil || access == 0 || access & ^ruleset.config.HandledNet != 0 {
		return os.NewSyscallError("landlock_add_rule port", os.ErrInvalid)
	}

	ruleset.mu.Lock()
	defer ruleset.mu.Unlock()

	return ruleset.control("landlock_add_rule port", func(fd int) error {
		// x/sys has no landlock_net_port_attr binding. Linux 7.0 defines two
		// consecutive uint64 fields and LANDLOCK_RULE_NET_PORT as 2.
		attr := struct{ Access, Port uint64 }{uint64(access), uint64(port)}
		_, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, uintptr(fd), 2, uintptr(unsafe.Pointer(&attr)), 0, 0, 0)
		if errno != 0 {
			return errno
		}
		return nil
	})
}

// Close releases this ruleset descriptor. Applied restrictions persist, and
// borrowed inputs stay open. Close waits for this owner's in-flight methods.
func (ruleset *Ruleset) Close() error {
	if ruleset == nil {
		return os.ErrInvalid
	}

	ruleset.mu.Lock()
	defer ruleset.mu.Unlock()

	return ruleset.file.Close()
}

func (ruleset *Ruleset) validFilesystem(access FilesystemAccess) bool {
	return access != 0 && access & ^ruleset.config.HandledFS == 0
}

func (ruleset *Ruleset) control(op string, operation func(int) error) error {
	return os.NewSyscallError(op, fileControl(ruleset.file, operation))
}

func addPath(fd, parent int, access FilesystemAccess) error {
	attr := unix.LandlockPathBeneathAttr{Allowed_access: uint64(access), Parent_fd: int32(parent)} //nolint:gosec // Linux descriptors fit int32.
	_, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, uintptr(fd), unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&attr)), 0, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

// fileControl pins descriptor lifetime and normalizes os's private closing error.
func fileControl(file *os.File, operation func(int) error) error {
	conn, err := file.SyscallConn()
	if err != nil {
		return err
	}

	var callErr error
	err = conn.Control(func(fd uintptr) {
		callErr = operation(int(fd))
	})
	if err != nil {
		return os.ErrClosed
	}
	return callErr
}
