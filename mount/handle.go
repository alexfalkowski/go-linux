//go:build linux

package mount

import (
	"os"

	"golang.org/x/sys/unix"
)

// Attributes is a bitmask of per-mount restrictions, combined with |. Zero
// requests no additional restrictions; filesystem and kernel policy still apply.
type Attributes uint32

// ReadOnly requests MOUNT_ATTR_RDONLY, prohibiting writes through this mount.
const ReadOnly Attributes = unix.MOUNT_ATTR_RDONLY

// NoSUID requests MOUNT_ATTR_NOSUID, disabling set-ID and file capabilities.
const NoSUID Attributes = unix.MOUNT_ATTR_NOSUID

// NoDev requests MOUNT_ATTR_NODEV, disabling device-special file access.
const NoDev Attributes = unix.MOUNT_ATTR_NODEV

// NoExec requests MOUNT_ATTR_NOEXEC, prohibiting direct program execution.
const NoExec Attributes = unix.MOUNT_ATTR_NOEXEC

const supportedAttributes = ReadOnly | NoSUID | NoDev | NoExec

// AttributeChange changes restrictions on a handle's mount. Zero requests no
// change. Unknown or overlapping Set/Clear bits match os.ErrInvalid.
type AttributeChange struct {
	// Set adds restrictions; Clear removes them subject to kernel policy.
	Set Attributes
	// Clear removes restrictions without changing other attributes.
	Clear Attributes
	// Recursive applies the change to the entire tree rather than its root.
	Recursive bool
}

// CloneFlags controls which mounts Clone copies. Zero copies only the named
// mount, excluding child mounts. Unknown bits match os.ErrInvalid.
type CloneFlags uint32

// Recursive includes child mounts in Clone (AT_RECURSIVE).
const Recursive CloneFlags = unix.AT_RECURSIVE

// UnmountFlags selects unmount behavior. Zero requires the mount to be unused.
// Unknown bits match os.ErrInvalid before a syscall.
type UnmountFlags uint8

// Detach removes the mount from the namespace immediately, releasing it when
// its last reference disappears (MNT_DETACH). Open files and handles can keep
// the detached mount alive.
const Detach UnmountFlags = unix.MNT_DETACH

// Unmount removes the mount at path using umount2. The caller supplies the
// namespace and required privileges. It neither closes a Handle nor consumes
// open files; closing a handle alone never unmounts an attached mount.
// Errors wrap os.PathError and preserve Linux errno identity.
func Unmount(path string, flags UnmountFlags) error {
	if flags & ^Detach != 0 {
		return &os.PathError{Op: "umount2", Path: path, Err: os.ErrInvalid}
	}
	if err := unix.Unmount(path, int(flags)); err != nil {
		return &os.PathError{Op: "umount2", Path: path, Err: err}
	}
	return nil
}

// Clone acquires a detached copy with open_tree(OPEN_TREE_CLONE|CLOEXEC).
// The caller owns the returned handle. On error it returns nil and retains no
// acquired descriptor. dir is borrowed during lookup; nil selects the current
// working directory, and absolute paths ignore dir in kernel lookup. An empty
// path selects dir itself and requires a nonnil dir. Symlinks and automounts
// follow open_tree defaults; this is not a confined path-resolution API.
// Closing dir after success leaves the handle valid. Errors wrap os.PathError.
func Clone(dir *os.File, path string, flags CloneFlags) (*Handle, error) {
	if flags & ^Recursive != 0 || (path == "" && dir == nil) {
		return nil, &os.PathError{Op: "open_tree", Path: path, Err: os.ErrInvalid}
	}

	options := uint(unix.OPEN_TREE_CLONE|unix.OPEN_TREE_CLOEXEC) | uint(flags)
	if path == "" {
		options |= unix.AT_EMPTY_PATH
	}

	var result int
	err := directoryControl(dir, "open_tree", path, func(fd int) error {
		var err error
		result, err = unix.OpenTree(fd, path, options)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &Handle{file: os.NewFile(uintptr(result), "mount")}, nil
}

// Handle owns a mount descriptor and implements io.Closer.
// Do not copy it after first use. Methods protect descriptor lifetime against
// concurrent Close; sequences and path lookup are not transactions. Kernel
// policy governs repeated attachment and changes to attached mounts. A zero
// Handle owns nothing and error-returning methods match os.ErrInvalid.
type Handle struct {
	file *os.File
}

// Stat returns a snapshot of the metadata for the mount's root, including
// while detached. It follows os.File.Stat's lifetime and error semantics.
func (h *Handle) Stat() (os.FileInfo, error) {
	return h.file.Stat()
}

// Attributes queries the current ReadOnly, NoSUID, NoDev, and NoExec restrictions
// using fstatfs, including while detached. Other kernel flags are omitted.
// Changes through another mount reference can make the snapshot stale.
// Errors return zero and preserve errno identity through os.PathError.
func (h *Handle) Attributes() (Attributes, error) {
	var stat unix.Statfs_t
	err := fileControl(h.file, "fstatfs", "mount", func(fd int) error {
		return unix.Fstatfs(fd, &stat)
	})
	if err != nil {
		return 0, err
	}

	var attributes Attributes
	for _, flag := range []struct {
		stat      int64
		attribute Attributes
	}{
		{unix.ST_RDONLY, ReadOnly},
		{unix.ST_NOSUID, NoSUID},
		{unix.ST_NODEV, NoDev},
		{unix.ST_NOEXEC, NoExec},
	} {
		if stat.Flags&flag.stat != 0 {
			attributes |= flag.attribute
		}
	}
	return attributes, nil
}

// SetAttributes uses mount_setattr with AT_EMPTY_PATH on this handle. Unknown
// bits and overlapping masks fail before the syscall. Kernel failures can
// follow partial application; no rollback is promised. Other attributes,
// propagation, and ID mappings are left untouched.
func (h *Handle) SetAttributes(change AttributeChange) error {
	if (change.Set|change.Clear) & ^supportedAttributes != 0 || change.Set&change.Clear != 0 {
		return &os.PathError{Op: "mount_setattr", Path: "mount", Err: os.ErrInvalid}
	}
	flags := uint(unix.AT_EMPTY_PATH)
	if change.Recursive {
		flags |= unix.AT_RECURSIVE
	}
	return fileControl(h.file, "mount_setattr", "mount", func(fd int) error {
		return unix.MountSetattr(fd, "", flags, &unix.MountAttr{
			Attr_set: uint64(change.Set), Attr_clr: uint64(change.Clear),
		})
	})
}

// Attach moves this mount to the target using move_mount. dir has Clone's
// borrowed-directory/nil/empty-path rules. Target symlinks and automounts are
// not followed by move_mount. Path components still require caller policy.
// Success retains the handle and persists after Close; failure also leaves
// the handle caller-owned. This method never unmounts or closes dir.
func (h *Handle) Attach(dir *os.File, path string) error {
	if path == "" && dir == nil {
		return &os.PathError{Op: "move_mount", Path: path, Err: os.ErrInvalid}
	}
	flags := unix.MOVE_MOUNT_F_EMPTY_PATH
	if path == "" {
		flags |= unix.MOVE_MOUNT_T_EMPTY_PATH
	}
	return fileControl(h.file, "move_mount", path, func(fd int) error {
		return directoryControl(dir, "move_mount", path, func(target int) error {
			return unix.MoveMount(fd, "", target, path, flags)
		})
	})
}

// Close releases the local descriptor. An unattached tree is released when
// its last reference disappears; attached mounts persist until unmounted by
// the caller or their namespace is destroyed. Repeated Close matches
// os.ErrClosed. Close does not interrupt syscalls already using the descriptor.
// It can return while protected callbacks still hold a reference; final kernel
// descriptor release is then deferred until those callbacks finish.
func (h *Handle) Close() error {
	return h.file.Close()
}

// fileControl pins owned and borrowed descriptors during syscalls and
// normalizes Go's private raw-connection closing error, as in memfd/pidfd.
func fileControl(file *os.File, op, path string, operation func(int) error) error {
	conn, err := file.SyscallConn()
	if err != nil {
		return &os.PathError{Op: op, Path: path, Err: err}
	}

	var callErr error
	err = conn.Control(func(fd uintptr) {
		callErr = operation(int(fd))
	})
	if err != nil {
		return &os.PathError{Op: op, Path: path, Err: os.ErrClosed}
	}
	if callErr != nil {
		return &os.PathError{Op: op, Path: path, Err: callErr}
	}
	return nil
}

func directoryControl(dir *os.File, op, path string, operation func(int) error) error {
	if dir != nil {
		return fileControl(dir, op, path, operation)
	}
	if err := operation(unix.AT_FDCWD); err != nil {
		return &os.PathError{Op: op, Path: path, Err: err}
	}
	return nil
}
