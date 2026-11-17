//go:build linux

package cgroup

import (
	"errors"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// Open acquires an owned handle to a cgroup v2 directory. path follows ordinary
// symlink resolution; it is not a confinement boundary. Other filesystems match
// os.ErrInvalid. Acquisition is atomically close-on-exec and retains no resource
// on failure. It neither creates a hierarchy nor establishes delegation.
func Open(path string) (*Group, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return checkedGroup(os.NewFile(uintptr(fd), path))
}

// Group owns one directory reference and implements io.Closer. Do not copy it
// after first use. Methods are safe concurrently, including Close; sequences
// require caller coordination. The zero value owns nothing and matches
// os.ErrInvalid. Nil receivers are unsupported. Invalid arguments precede closed
// state; valid operations after Close match os.ErrClosed. Removal by another
// owner can leave an open handle whose operations return kernel errors.
type Group struct {
	file *os.File
}

// New creates a single child named name and opens an independent owned handle.
// name must be a single nonempty component other than . or .., without slash
// or NUL. No controllers or permissions are changed. A failure after creation
// removes the child and closes acquired descriptors; cleanup errors are joined
// with the primary error. Coordinate child creation with other hierarchy owners.
func (g *Group) New(name string) (*Group, error) {
	if !validName(name) {
		return nil, &os.PathError{Op: "mkdir", Path: name, Err: os.ErrInvalid}
	}

	var child *Group
	err := g.control("mkdir", name, func(fd int) error {
		if err := unix.Mkdirat(fd, name, 0o755); err != nil {
			return err
		}

		opened, err := unix.Openat(fd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return errors.Join(err, unix.Unlinkat(fd, name, unix.AT_REMOVEDIR))
		}

		child = &Group{file: os.NewFile(uintptr(opened), name)}
		return nil
	})
	return child, err
}

// Open acquires an independent handle to one existing child. name follows New's
// component rules; symlinks are rejected. Closing the parent does not close the
// child. No resources are retained on failure.
func (g *Group) Open(name string) (*Group, error) {
	if !validName(name) {
		return nil, &os.PathError{Op: "open", Path: name, Err: os.ErrInvalid}
	}

	file, err := g.openFile(name, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return nil, err
	}
	return checkedGroup(file)
}

// Remove removes one child by name with rmdir semantics. It must contain no
// live processes or child groups; control files do not prevent removal. Handles
// to that child remain independently owned. Missing children fail with ENOENT.
// This never recursively removes a subtree or kills/migrates processes.
func (g *Group) Remove(name string) error {
	if !validName(name) {
		return &os.PathError{Op: "rmdir", Path: name, Err: os.ErrInvalid}
	}
	return g.control("rmdir", name, func(fd int) error {
		return unix.Unlinkat(fd, name, unix.AT_REMOVEDIR)
	})
}

// Processes reads cgroup.procs as a snapshot of process IDs in the caller's PID
// namespace. Order is unspecified and duplicates are possible during migration
// or PID reuse. Zero entries represent processes invisible in that namespace.
// Processes may exit immediately. Threaded cgroups can fail with
// EOPNOTSUPP. Errors return no partial list.
func (g *Group) Processes() ([]int, error) {
	data, err := g.read("cgroup.procs")
	if err != nil {
		return nil, err
	}

	var processes []int
	for field := range strings.FieldsSeq(string(data)) {
		id, err := strconv.ParseInt(field, 10, 32)
		if err != nil || id < 0 {
			return nil, &os.PathError{Op: "decode", Path: "cgroup.procs", Err: os.ErrInvalid}
		}

		processes = append(processes, int(id))
	}
	return processes, nil
}

// AddProcess writes one positive PID to cgroup.procs, moving all its threads.
// Zero (implicit caller selection) and IDs outside positive int32 match
// os.ErrInvalid. The process is not stopped: coordinate startup and migration
// when work must wait for limits. PID identity and permissions are checked by
// Linux at write time; common-ancestor delegation and domain constraints apply.
// Existing memory charges are not moved. Close never reverses a migration.
func (g *Group) AddProcess(pid int) error {
	if pid <= 0 || int64(pid) > 1<<31-1 {
		return &os.PathError{Op: "write", Path: "cgroup.procs", Err: os.ErrInvalid}
	}
	return g.write("cgroup.procs", []byte(strconv.Itoa(pid)))
}

// Close releases only the local directory reference. Repeated Close matches
// os.ErrClosed. It does not remove the group or undo any kernel changes.
func (g *Group) Close() error {
	return g.file.Close()
}

func (g *Group) read(name string) ([]byte, error) {
	file, err := g.openFile(name, unix.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

func (g *Group) write(name string, data []byte) error {
	file, err := g.openFile(name, unix.O_WRONLY)
	if err != nil {
		return err
	}

	// A control command is one write: never retry a suffix after a short write.
	n, err := file.Write(data)
	if err != nil {
		return errors.Join(err, file.Close())
	} else if n != len(data) {
		err = io.ErrShortWrite
	}
	return errors.Join(err, file.Close())
}

func (g *Group) openFile(name string, flags int) (*os.File, error) {
	var opened int
	err := g.control("openat", name, func(fd int) error {
		var err error
		opened, err = unix.Openat(fd, name, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		return err
	})
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(opened), name), nil
}

func (g *Group) control(op, name string, operation func(int) error) error {
	conn, err := g.file.SyscallConn()
	if err != nil {
		return &os.PathError{Op: op, Path: name, Err: err}
	}

	var callErr error
	err = conn.Control(func(fd uintptr) {
		callErr = operation(int(fd))
	})
	if err != nil {
		return &os.PathError{Op: op, Path: name, Err: os.ErrClosed}
	}
	if callErr != nil {
		return &os.PathError{Op: op, Path: name, Err: callErr}
	}
	return nil
}

func validName(name string) bool {
	validComponent := name != "" && name != "." && name != ".."
	containsInvalidByte := strings.ContainsAny(name, "/\x00")
	return validComponent && !containsInvalidByte
}

func checkedGroup(file *os.File) (*Group, error) {
	group := &Group{file: file}
	err := group.control("fstatfs", file.Name(), func(fd int) error {
		var stat unix.Statfs_t
		if err := unix.Fstatfs(fd, &stat); err != nil {
			return err
		}
		if stat.Type != unix.CGROUP2_SUPER_MAGIC {
			return os.ErrInvalid
		}
		return nil
	})
	if err != nil {
		group.Close()
		return nil, err
	}
	return group, nil
}
