//go:build linux

package namespace

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// Current opens the calling thread's active namespace of kind through
// /proc/thread-self/ns. kind must be exactly one known Type. Lock the goroutine
// to its OS thread first when composing this with thread-sensitive operations.
// Procfs access and ptrace permissions apply; numeric process lookup is avoided.
// The caller owns the returned handle. On error no resource is returned or kept.
func Current(kind Type) (*Handle, error) {
	name := namespaceName(kind)
	if name == "" {
		return nil, &os.PathError{Op: "open", Path: "/proc/thread-self/ns", Err: os.ErrInvalid}
	}
	return Open("/proc/thread-self/ns/" + name)
}

// Open acquires an owned namespace handle from a procfs namespace link or a
// bind-mounted namespace file. Symlinks are followed; path resolution is not
// confined. PID-based paths can race with process exit/reuse. pid_for_children
// and time_for_children paths select future-child membership rather than active
// membership and can be opened explicitly. An unpopulated pid_for_children link
// can fail with ENOENT. Descriptors are atomically close-on-exec; nonblocking
// open prevents FIFO paths from hanging before validation. On error all acquired
// resources are closed and nil is returned. Errors wrap os.PathError.
func Open(path string) (*Handle, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}

	// Only path acquisition needs O_NONBLOCK (notably for invalid FIFO inputs).
	// Namespace handles never use Go polling; avoid initializing its descriptors
	// when constructing the owner under descriptor exhaustion.
	if err := unix.SetNonblock(fd, false); err != nil {
		unix.Close(fd)
		return nil, &os.PathError{Op: "fcntl", Path: path, Err: err}
	}
	return Adopt(os.NewFile(uintptr(fd), path))
}

// Adopt consumes file on success and failure. The caller must neither use nor
// close it afterward, nor use it concurrently with Adopt. file must be an nsfs
// namespace descriptor with FD_CLOEXEC; O_PATH and other files match
// os.ErrInvalid. Nil matches os.ErrInvalid and closed files match os.ErrClosed.
// Flags and offsets are left unchanged. On failure file is closed and nil is
// returned. Kernel errors preserve errno identity.
func Adopt(file *os.File) (*Handle, error) {
	if file == nil {
		return nil, os.ErrInvalid
	}

	handle := &Handle{file: file}
	err := handle.control("adopt", func(fd int) error {
		var stat unix.Statfs_t
		if err := unix.Fstatfs(fd, &stat); err != nil {
			return err
		}
		if stat.Type != unix.NSFS_MAGIC {
			return os.ErrInvalid
		}

		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		if err != nil {
			return err
		}
		if flags&unix.FD_CLOEXEC == 0 {
			return os.ErrInvalid
		}

		flags, err = unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
		if err != nil {
			return err
		}
		if flags&unix.O_PATH != 0 {
			return os.ErrInvalid
		}

		_, err = unix.IoctlRetInt(fd, unix.NS_GET_NSTYPE)
		if errors.Is(err, unix.ENOTTY) {
			return os.ErrInvalid
		}
		return err
	})
	if err != nil {
		file.Close()
		return nil, err
	}
	return handle, nil
}

// Handle owns a namespace reference and implements io.Closer. Do not copy it
// after first use. Methods protect descriptor lifetime against concurrent Close;
// sequences still require coordination. Zero handles match os.ErrInvalid; nil
// receivers are unsupported. Closing does not undo namespace changes or end
// membership. No descriptor access or raw callbacks are exposed.
type Handle struct {
	file *os.File
}

// Type inspects the namespace with NS_GET_NSTYPE. Unknown output values are
// preserved. On error it returns zero and preserves errno identity.
func (h *Handle) Type() (Type, error) {
	var kind int
	err := h.control("NS_GET_NSTYPE", func(fd int) error {
		var err error
		kind, err = unix.IoctlRetInt(fd, unix.NS_GET_NSTYPE)
		return err
	})
	if err != nil {
		return 0, err
	}
	return Type(kind), err //nolint:gosec // NS_GET_NSTYPE returns a CLONE_NEW* int bit.
}

// Equal compares device and inode identity while both handles remain pinned.
// It works across separate opens and namespace bind mounts. A nil other matches
// os.ErrInvalid. Errors return false; the receiver is validated first.
func (h *Handle) Equal(other *Handle) (bool, error) {
	var equal bool
	err := h.control("fstat", func(fd int) error {
		if other == nil {
			return os.ErrInvalid
		}
		return other.control("fstat", func(otherFD int) error {
			var first, second unix.Stat_t
			if err := unix.Fstat(fd, &first); err != nil {
				return err
			}
			if err := unix.Fstat(otherFD, &second); err != nil {
				return err
			}
			equal = first.Dev == second.Dev && first.Ino == second.Ino
			return nil
		})
	})
	return equal, err
}

// UserNamespace opens the owning user namespace with NS_GET_USERNS. For a user
// namespace this is its parent. Kernel visibility restrictions can return EPERM.
// The returned handle is independently owned; failure retains no new resource.
func (h *Handle) UserNamespace() (*Handle, error) {
	return h.related("NS_GET_USERNS", unix.NS_GET_USERNS)
}

// Parent opens the parent of a PID or user namespace with NS_GET_PARENT. Other
// types fail with EINVAL. An inaccessible parent, including beyond the caller's
// namespace scope, fails with EPERM. The result is independently owned; failure
// retains no new resource. No process ancestry or membership change is implied.
func (h *Handle) Parent() (*Handle, error) {
	return h.related("NS_GET_PARENT", unix.NS_GET_PARENT)
}

// OwnerUID returns a user namespace's creator UID mapped into the calling
// thread's user namespace using NS_GET_OWNER_UID. Unmapped UIDs use the kernel
// overflow UID. Other namespace types fail with EINVAL. Errors return zero.
func (h *Handle) OwnerUID() (uint32, error) {
	var uid uint32
	err := h.control("NS_GET_OWNER_UID", func(fd int) error {
		var err error
		uid, err = unix.IoctlGetUint32(fd, unix.NS_GET_OWNER_UID)
		return err
	})
	return uid, err
}

// Enter applies setns to the calling OS thread using this handle's actual
// namespace type. The caller must follow the dedicated locked-goroutine rule
// in the package documentation, even after failure. Close never restores state.
//
// Network, IPC, UTS, cgroup, and mount entry change the calling thread. Mount
// entry requires unshared filesystem attributes (Unshare(Mount) provides them)
// and applicable CAP_SYS_ADMIN/CAP_SYS_CHROOT. User entry requires a
// single-threaded process without shared filesystem attributes and cannot
// reenter the same user namespace. PID entry only changes future children and
// permits the same or descendant namespace. Time entry requires a single-threaded
// process and changes its active and future-child time namespace. LockOSThread
// alone satisfies none of these kernel restrictions. Use os/exec for child
// setup; no pidfd multi-namespace entry, rollback, or capability changes are
// synthesized. Kernel errno, including EPERM and EINVAL, is preserved.
func (h *Handle) Enter() error {
	return h.control("setns", func(fd int) error {
		kind, err := unix.IoctlRetInt(fd, unix.NS_GET_NSTYPE)
		if err != nil {
			return err
		}
		return unix.Setns(fd, kind)
	})
}

// Close releases this reference. Repeated Close and valid operations after
// closure match os.ErrClosed. An overlapping protected operation can complete;
// final descriptor release may be deferred until it finishes. Close never
// restores membership or destroys a namespace still referenced elsewhere.
func (h *Handle) Close() error {
	return h.file.Close()
}

func (h *Handle) related(op string, request uint) (*Handle, error) {
	var result int
	err := h.control(op, func(fd int) error {
		var err error
		result, err = unix.IoctlRetInt(fd, request)
		return err
	})
	if err != nil {
		return nil, err
	}
	return Adopt(os.NewFile(uintptr(result), "namespace"))
}

func (h *Handle) control(op string, operation func(int) error) error {
	conn, err := h.file.SyscallConn()
	if err != nil {
		return &os.PathError{Op: op, Path: "namespace", Err: err}
	}

	var callErr error
	err = conn.Control(func(fd uintptr) {
		callErr = operation(int(fd))
	})
	if err != nil {
		return &os.PathError{Op: op, Path: "namespace", Err: os.ErrClosed}
	}
	if callErr != nil {
		return &os.PathError{Op: op, Path: "namespace", Err: callErr}
	}
	return nil
}
