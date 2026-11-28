//go:build linux

package socket

import (
	"bytes"
	"encoding/binary"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// x/sys's sockaddr conversion discards Unix address lengths and treats a leading
// @ as abstract even for literal filesystem names. Retain lengths when decoding
// and bypass that conversion for explicitly selected pathname bind/send calls.
// https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/un.h
func socketName(fd int) (Address, error) {
	var address [unix.SizeofSockaddrAny]byte
	size := uint32(len(address))

	_, _, errno := unix.Syscall(unix.SYS_GETSOCKNAME, uintptr(fd), uintptr(unsafe.Pointer(&address[0])), uintptr(unsafe.Pointer(&size)))
	if errno != 0 {
		return nil, errno
	}
	return decodeAddress(address[:], size)
}

func receiveMessage(fd int, p, control []byte) (int, int, int, Address, error) {
	var address [unix.SizeofSockaddrAny]byte
	message := unix.Msghdr{Name: &address[0], Namelen: uint32(len(address))}
	var vector unix.Iovec
	if len(p) > 0 {
		vector.Base = &p[0]
		vector.SetLen(len(p))
		message.Iov = &vector
		message.SetIovlen(1)
	}
	if len(control) > 0 {
		message.Control = &control[0]
		message.SetControllen(len(control))
	}

	var n uintptr
	for {
		var errno unix.Errno
		n, _, errno = unix.Syscall(unix.SYS_RECVMSG, uintptr(fd), uintptr(unsafe.Pointer(&message)), unix.MSG_DONTWAIT|unix.MSG_CMSG_CLOEXEC)
		runtime.KeepAlive(&message)
		if errno == unix.EINTR {
			continue
		}
		if errno != 0 {
			return 0, 0, 0, nil, errno
		}
		break
	}

	from, err := decodeAddress(address[:], message.Namelen)
	return int(n), int(message.Controllen), int(message.Flags), from, err //nolint:gosec // Successful results are bounded by the supplied buffers.
}

func decodeAddress(address []byte, size uint32) (Address, error) {
	if size == 0 {
		return nil, nil
	}
	if size < 2 || uint64(size) > uint64(len(address)) {
		return nil, unix.EINVAL
	}

	switch binary.NativeEndian.Uint16(address) {
	case unix.AF_UNIX:
		return unixAddress(address[2:size]), nil
	case unix.AF_NETLINK:
		if size < unix.SizeofSockaddrNetlink {
			return nil, unix.EINVAL
		}
		return NetlinkAddress{PortID: binary.NativeEndian.Uint32(address[4:8]), Groups: binary.NativeEndian.Uint32(address[8:12])}, nil
	default:
		return nil, unix.EAFNOSUPPORT
	}
}

func unixAddress(name []byte) UnixAddress {
	if len(name) > 0 && name[0] == 0 {
		return UnixAddress{Name: "@" + string(name[1:])}
	}
	if end := bytes.IndexByte(name, 0); end >= 0 {
		name = name[:end]
	}
	if len(name) > 0 && (name[0] == '@' || len(name) == len(unix.RawSockaddrUnix{}.Path)) {
		return UnixAddress{Name: string(name), Interpretation: LiteralPathname}
	}
	return UnixAddress{Name: string(name)}
}

func bind(fd int, address Address) error {
	if path, ok := address.(UnixAddress); ok && path.Interpretation == LiteralPathname {
		raw, size := pathnameAddress(path.Name)
		_, _, errno := unix.Syscall(unix.SYS_BIND, uintptr(fd), uintptr(unsafe.Pointer(&raw)), uintptr(size))
		if errno != 0 {
			return errno
		}
		return nil
	}
	return unix.Bind(fd, address.socketAddress())
}

func sendPathnameMessage(fd int, p, control []byte, name string) (int, error) {
	address, size := pathnameAddress(name)
	message := unix.Msghdr{Name: (*byte)(unsafe.Pointer(&address)), Namelen: size}
	var vector unix.Iovec
	if len(p) > 0 {
		vector.Base = &p[0]
		vector.SetLen(len(p))
		message.Iov = &vector
		message.SetIovlen(1)
	}
	if len(control) > 0 {
		message.Control = &control[0]
		message.SetControllen(len(control))
	}

	n, _, errno := unix.Syscall(unix.SYS_SENDMSG, uintptr(fd), uintptr(unsafe.Pointer(&message)), unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL)
	runtime.KeepAlive(&message)
	if errno != 0 {
		return 0, errno
	}
	return int(n), nil
}

func pathnameAddress(name string) (unix.RawSockaddrUnix, uint32) {
	var address unix.RawSockaddrUnix
	address.Family = unix.AF_UNIX
	for i := range len(name) {
		address.Path[i] = int8(name[i]) //nolint:gosec // Preserve pathname bytes in the signed ABI array.
	}
	size := uint32(2 + len(name)) //nolint:gosec // Validation bounds names to sockaddr_un.Path.
	if len(name) < len(address.Path) {
		size++
	}
	return address, size
}
