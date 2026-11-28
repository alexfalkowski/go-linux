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

// UnixAddressMode controls how a Unix address name is interpreted.
type UnixAddressMode uint8

// UnixAddressShorthand treats a leading @ or NUL as an abstract address.
// Other names are filesystem paths. This is the default.
const UnixAddressShorthand UnixAddressMode = 0

// LiteralPathname interprets the whole name as a filesystem path, including a
// leading @. NUL bytes are invalid.
const LiteralPathname UnixAddressMode = 1

// UnixAddress names a Unix datagram endpoint or an unnamed Unix pair.
// Like [net.UnixAddr] on Linux, Name "@app" selects the abstract socket whose
// kernel name is "\x00app"; an initial NUL is also accepted. Abstract sockets
// create no filesystem entry. Use LiteralPathname for literal filesystem names
// such as "@app".
//
// Empty means unnamed on received addresses and is invalid for ListenUnix.
// Preserve the complete address returned in Message.From when replying: Name
// alone cannot distinguish a leading-@ filesystem path from an abstract name.
type UnixAddress struct {
	// Name is a filesystem path or abstract socket name, limited by sockaddr_un.
	Name string
	// Interpretation selects UnixAddressShorthand or LiteralPathname. Receive
	// and LocalAddress select LiteralPathname for leading-@ or full-length
	// (108-byte) pathnames so replies preserve them.
	Interpretation UnixAddressMode
}

// Network returns "unixgram".
func (a UnixAddress) Network() string {
	return "unixgram"
}

// String returns the display name. Use the complete UnixAddress when sending
// a reply so the interpretation is preserved.
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
