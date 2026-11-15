//go:build linux

package socket

import (
	"net"
	"strconv"

	"golang.org/x/sys/unix"
)

// Address is a UnixAddress or NetlinkAddress value. It implements net.Addr;
// external implementations and pointers to these values are not accepted.
type Address interface {
	net.Addr
	socketAddress() unix.Sockaddr
}

// UnixAddress names a Unix datagram endpoint or an unnamed Unix pair.
// Like [net.UnixAddr] on Linux, Name "@app" selects the abstract socket whose
// kernel name is "\x00app"; an initial NUL is also accepted. Abstract sockets
// create no filesystem entry. With Pathname true, Name "@app" instead selects
// the literal filesystem path @app. Pathname is an address-interpretation
// selector, not a Linux socket option.
//
// Empty means unnamed on received addresses and is invalid for ListenUnix.
// Preserve the complete address returned in Message.From when replying: Name
// alone cannot distinguish a leading-@ filesystem path from an abstract name.
type UnixAddress struct {
	// Name is a filesystem path or abstract socket name, limited by sockaddr_un.
	Name string
	// Pathname interprets Name literally as a filesystem path, including a
	// leading @. NUL bytes are invalid. Receive and LocalAddress set this flag
	// for leading-@ or full-length (108-byte) pathnames so replies preserve them.
	// False preserves the @/NUL shorthand and ordinary pathname interpretation.
	Pathname bool
}

// Network returns "unixgram".
func (a UnixAddress) Network() string {
	return "unixgram"
}

// String returns Name without encoding Pathname. Use the complete UnixAddress,
// rather than rebuilding it from this display string, when sending a reply.
func (a UnixAddress) String() string {
	return a.Name
}

func (a UnixAddress) socketAddress() unix.Sockaddr {
	name := a.Name
	if len(name) > 0 && name[0] == 0 {
		name = "@" + name[1:]
	}
	return &unix.SockaddrUnix{Name: name}
}

// NetlinkAddress identifies a Netlink port and the legacy 32-bit group mask.
// A port ID is not necessarily a process ID. Port zero selects the kernel for
// Send and requests automatic port allocation for OpenNetlink.
type NetlinkAddress struct {
	// PortID is the Linux nl_pid value in the current network namespace.
	PortID uint32
	// Groups is the legacy nl_groups bitmask; use JoinGroup for larger group IDs.
	Groups uint32
}

// Network returns "netlink".
func (a NetlinkAddress) Network() string {
	return "netlink"
}

// String formats the decimal port ID and group mask, separated by a colon.
func (a NetlinkAddress) String() string {
	return strconv.FormatUint(uint64(a.PortID), 10) + ":" + strconv.FormatUint(uint64(a.Groups), 10)
}

func (a NetlinkAddress) socketAddress() unix.Sockaddr {
	return &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Pid: a.PortID, Groups: a.Groups}
}
