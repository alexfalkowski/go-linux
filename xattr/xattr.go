//go:build linux

package xattr

import (
	"bytes"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// SetMode selects one atomic set operation. Modes cannot be combined.
type SetMode uint32

// Upsert creates an attribute or replaces its entire value; it is the default.
const Upsert SetMode = 0

// Create requires the attribute to be absent (XATTR_CREATE); otherwise EEXIST.
const Create SetMode = unix.XATTR_CREATE

// Replace requires an existing attribute (XATTR_REPLACE); otherwise ENODATA.
const Replace SetMode = unix.XATTR_REPLACE

// PathOptions controls only the final component of an at-style lookup.
// Zero follows symlinks. These options do not confine untrusted paths.
type PathOptions struct {
	// NoFollow selects the final symlink itself. Intermediate links are followed.
	// An empty path selects the borrowed handle itself regardless of this option.
	NoFollow bool
}

// Get returns the complete value of name on file, including an unlinked file.
// O_PATH handles return EBADF. A present empty value returns a nonnil empty slice;
// an absent or inaccessible attribute preserves the kernel error (often ENODATA).
// See GetAt for sizing and retry behavior.
func Get(file *os.File, name string) ([]byte, error) {
	return GetAt(file, "", name, PathOptions{})
}

// GetAt returns the complete value using getxattrat. A live nonnil dir is
// required, even for absolute paths. Relative paths use dir; absolute paths
// ignore its directory identity; an empty path selects dir itself. Paths pass
// unchanged, including .. and trailing slashes; this does not provide confinement.
//
// Each of at most four attempts queries size and then reads with a nonzero
// buffer, including for empty values. ERANGE from concurrent growth retries;
// exhaustion returns ERANGE with no partial value. Allocation is bounded to
// Linux's 64 KiB limit; larger results return E2BIG. Other errors return at once.
// Each lookup can resolve a different inode during concurrent path replacement.
//
// Linux interface: https://github.com/torvalds/linux/blob/v7.0/fs/xattr.c
func GetAt(dir *os.File, path, name string, options PathOptions) ([]byte, error) {
	var value []byte
	err := control(dir, func(fd int) error {
		var err error
		value, err = readValue(func(buffer []byte) (int, error) {
			return valueAt(unix.SYS_GETXATTRAT, fd, path, name, buffer, Upsert, options)
		})
		return err
	})
	if err != nil {
		return nil, &os.PathError{Op: "getxattrat", Path: path, Err: err}
	}
	return value, nil
}

// Set atomically sets name on file according to mode. Nil and empty values
// both store a present empty attribute; neither removes it. The input bytes
// are borrowed only for the call and must not be modified concurrently.
func Set(file *os.File, name string, value []byte, mode SetMode) error {
	return SetAt(file, "", name, value, mode, PathOptions{})
}

// SetAt atomically sets name using setxattrat. Path resolution is as in GetAt.
// Unknown or combined modes match os.ErrInvalid before borrowing dir.
// Filesystem size/space limits and namespace permissions retain their errno;
// no existence check, read-modify-write, or automatic retry is performed.
// Nil and empty values store an empty attribute. Success does not promise
// crash durability. value must not be modified concurrently with this call.
//
// Linux interface: https://github.com/torvalds/linux/blob/v7.0/fs/xattr.c
func SetAt(dir *os.File, path, name string, value []byte, mode SetMode, options PathOptions) error {
	var err error
	switch mode {
	case Upsert, Create, Replace:
		err = control(dir, func(fd int) error {
			_, err := valueAt(unix.SYS_SETXATTRAT, fd, path, name, value, mode, options)
			return err
		})
	default:
		err = os.ErrInvalid
	}
	if err != nil {
		return &os.PathError{Op: "setxattrat", Path: path, Err: err}
	}
	return nil
}

// List returns visible attribute names on file in kernel order. Names include
// namespace prefixes. An empty result is a nonnil empty slice. Listing need
// not include names hidden by security policy, and does not read their values.
func List(file *os.File) ([]string, error) {
	return ListAt(file, "", PathOptions{})
}

// ListAt lists visible names using listxattrat. Path resolution and bounded
// sizing retries are as in GetAt, with the same 64 KiB allocation bound.
// Ordering is unspecified; names can disappear before a subsequent Get.
// Errors return no partial list. Malformed kernel name records return EIO.
//
// Linux interface: https://man7.org/linux/man-pages/man2/listxattr.2.html
// At-style interface: https://github.com/torvalds/linux/blob/v7.0/fs/xattr.c
func ListAt(dir *os.File, path string, options PathOptions) ([]string, error) {
	var value []byte
	err := control(dir, func(fd int) error {
		var err error
		value, err = readValue(func(buffer []byte) (int, error) {
			return listAt(fd, path, buffer, options)
		})
		return err
	})
	if err != nil {
		return nil, &os.PathError{Op: "listxattrat", Path: path, Err: err}
	}

	names := make([]string, 0)
	for len(value) != 0 {
		end := bytes.IndexByte(value, 0)
		if end <= 0 {
			return nil, &os.PathError{Op: "listxattrat", Path: path, Err: unix.EIO}
		}
		names = append(names, string(value[:end]))
		value = value[end+1:]
	}
	return names, nil
}

// Remove deletes name from file. Removing an absent attribute preserves ENODATA;
// use Set with an empty value to retain a present, empty attribute instead.
func Remove(file *os.File, name string) error {
	return RemoveAt(file, "", name, PathOptions{})
}

// RemoveAt deletes name using removexattrat. Path resolution is as in GetAt.
// Filesystem and permission errors remain observable. No retry or emulation
// is performed, and success does not promise crash durability.
//
// Linux interface: https://github.com/torvalds/linux/blob/v7.0/fs/xattr.c
func RemoveAt(dir *os.File, path, name string, options PathOptions) error {
	err := control(dir, func(fd int) error {
		return removeAt(fd, path, name, options)
	})
	if err != nil {
		return &os.PathError{Op: "removexattrat", Path: path, Err: err}
	}
	return nil
}

func control(file *os.File, operation func(int) error) error {
	conn, err := file.SyscallConn()
	if err != nil {
		return err
	}

	var callErr error
	if err := conn.Control(func(fd uintptr) {
		callErr = operation(int(fd))
	}); err != nil {
		// Match os.File even when RawConn returns its private closing error.
		return os.ErrClosed
	}
	return callErr
}

func readValue(read func([]byte) (int, error)) ([]byte, error) {
	const maximum = 65536

	for range 4 {
		size, err := read(nil)
		if err != nil {
			return nil, err
		}
		if size > maximum {
			return nil, unix.E2BIG
		}

		// A zero-size read is another size query, not an empty-value read.
		buffer := make([]byte, max(size, 1))
		n, err := read(buffer)
		if errors.Is(err, unix.ERANGE) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return buffer[:n], nil
	}

	return nil, unix.ERANGE
}
