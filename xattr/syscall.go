//go:build linux

package xattr

import (
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// xattrArgs is Linux's fixed-width xattr_args from
// https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/xattr.h.
// x/sys v0.50.0 provides syscall numbers but no at-style wrappers or layout.
// Keep the pointer's storage pinned: the ABI carries it inside a uint64.
type xattrArgs struct {
	value uint64
	size  uint32
	flags uint32
}

func valueAt(call uintptr, fd int, path, name string, value []byte, mode SetMode, options PathOptions) (int, error) {
	pathPtr, err := unix.BytePtrFromString(path)
	if err != nil {
		return 0, err
	}

	namePtr, err := unix.BytePtrFromString(name)
	if err != nil {
		return 0, err
	}
	if len(value) > 65536 {
		return 0, unix.E2BIG
	}

	var pins runtime.Pinner
	defer pins.Unpin()

	var address uint64
	if len(value) != 0 {
		pins.Pin(&value[0])
		address = uint64(uintptr(unsafe.Pointer(&value[0])))
	}

	// The bound above prevents truncation to the ABI's 32-bit size.
	args := xattrArgs{value: address, size: uint32(len(value)), flags: uint32(mode)} //nolint:gosec
	n, _, errno := unix.Syscall6(call, uintptr(fd), uintptr(unsafe.Pointer(pathPtr)), pathFlags(path, options),
		uintptr(unsafe.Pointer(namePtr)), uintptr(unsafe.Pointer(&args)), unsafe.Sizeof(args))
	if errno != 0 {
		return 0, errno
	}
	return int(n), nil
}

func listAt(fd int, path string, buffer []byte, options PathOptions) (int, error) {
	pathPtr, err := unix.BytePtrFromString(path)
	if err != nil {
		return 0, err
	}

	n, _, errno := unix.Syscall6(unix.SYS_LISTXATTRAT, uintptr(fd), uintptr(unsafe.Pointer(pathPtr)), pathFlags(path, options),
		uintptr(unsafe.Pointer(unsafe.SliceData(buffer))), uintptr(len(buffer)), 0)
	if errno != 0 {
		return 0, errno
	}
	return int(n), nil
}

func removeAt(fd int, path, name string, options PathOptions) error {
	pathPtr, err := unix.BytePtrFromString(path)
	if err != nil {
		return err
	}

	namePtr, err := unix.BytePtrFromString(name)
	if err != nil {
		return err
	}

	_, _, errno := unix.Syscall6(unix.SYS_REMOVEXATTRAT, uintptr(fd), uintptr(unsafe.Pointer(pathPtr)), pathFlags(path, options),
		uintptr(unsafe.Pointer(namePtr)), 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func pathFlags(path string, options PathOptions) uintptr {
	var flags uintptr
	if path == "" {
		flags |= unix.AT_EMPTY_PATH
	}
	if options.NoFollow {
		flags |= unix.AT_SYMLINK_NOFOLLOW
	}
	return flags
}
