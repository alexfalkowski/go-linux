//go:build linux

package socket

import (
	"os"

	"golang.org/x/sys/unix"
)

// SetReadBuffer requests SO_RCVBUF bytes. Linux doubles the request for
// bookkeeping, enforces limits, and may clamp it. Sizes outside 1..2^31-1 match
// os.ErrInvalid. This is not a guarantee of queue capacity or losslessness.
func (c *Conn) SetReadBuffer(bytes int) error {
	return c.setBuffer(unix.SO_RCVBUF, bytes)
}

// SetWriteBuffer requests SO_SNDBUF bytes with SetReadBuffer's validation and
// Linux sizing rules.
func (c *Conn) SetWriteBuffer(bytes int) error {
	return c.setBuffer(unix.SO_SNDBUF, bytes)
}

// SetPassCredentials enables/disables SO_PASSCRED on Unix sockets. Receive then
// returns kernel-validated sender credentials. Linux may autobind an unnamed
// socket. Other families match os.ErrInvalid.
func (c *Conn) SetPassCredentials(enabled bool) error {
	if c.family != unix.AF_UNIX {
		return socketError("setsockopt", os.ErrInvalid)
	}
	return c.setBoolOption(unix.SOL_SOCKET, unix.SO_PASSCRED, enabled)
}

// PeerCredentials reads SO_PEERCRED on connected Unix pairs. Credentials are
// captured at socketpair creation, not refreshed for later changes or senders.
// Unconnected sockets retain Linux's result; other families match os.ErrInvalid.
func (c *Conn) PeerCredentials() (Credentials, error) {
	if c.family != unix.AF_UNIX {
		return Credentials{}, socketError("getsockopt", os.ErrInvalid)
	}

	var credentials *unix.Ucred
	err := c.control("getsockopt", func(fd int) error {
		var err error
		credentials, err = unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
		return err
	})
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{PID: credentials.Pid, UID: credentials.Uid, GID: credentials.Gid}, nil
}

// SetExtendedAck controls NETLINK_EXT_ACK diagnostic attributes. It does not
// decode acknowledgements. Other families match os.ErrInvalid.
func (c *Conn) SetExtendedAck(enabled bool) error {
	return c.netlinkBool(unix.NETLINK_EXT_ACK, enabled)
}

// SetCapAck controls NETLINK_CAP_ACK, allowing capped acknowledgement payloads.
// Other families match os.ErrInvalid; decoders must inspect response flags.
func (c *Conn) SetCapAck(enabled bool) error {
	return c.netlinkBool(unix.NETLINK_CAP_ACK, enabled)
}

// SetStrictCheck controls NETLINK_GET_STRICT_CHK for supported route dumps.
// Kernel validation remains protocol-specific. Other families match os.ErrInvalid.
func (c *Conn) SetStrictCheck(enabled bool) error {
	return c.netlinkBool(unix.NETLINK_GET_STRICT_CHK, enabled)
}

// JoinGroup adds a Netlink multicast membership using NETLINK_ADD_MEMBERSHIP.
// Group is a positive numeric group ID, not a bitmask; joining may require
// privilege. Invalid IDs/families match os.ErrInvalid. Kernel errno is retained.
func (c *Conn) JoinGroup(group uint32) error {
	return c.membership(unix.NETLINK_ADD_MEMBERSHIP, group)
}

// LeaveGroup drops a membership using NETLINK_DROP_MEMBERSHIP and JoinGroup's
// validation. Membership changes are individual kernel operations.
func (c *Conn) LeaveGroup(group uint32) error {
	return c.membership(unix.NETLINK_DROP_MEMBERSHIP, group)
}

func (c *Conn) setBuffer(option, bytes int) error {
	if bytes <= 0 || uint64(bytes) > 1<<31-1 {
		return socketError("setsockopt", os.ErrInvalid)
	}
	return c.control("setsockopt", func(fd int) error {
		return unix.SetsockoptInt(fd, unix.SOL_SOCKET, option, bytes)
	})
}

func (c *Conn) setBoolOption(level, option int, enabled bool) error {
	var value int
	if enabled {
		value = 1
	}
	return c.control("setsockopt", func(fd int) error {
		return unix.SetsockoptInt(fd, level, option, value)
	})
}

func (c *Conn) netlinkBool(option int, enabled bool) error {
	if c.family != unix.AF_NETLINK {
		return socketError("setsockopt", os.ErrInvalid)
	}
	return c.setBoolOption(unix.SOL_NETLINK, option, enabled)
}

func (c *Conn) membership(option int, group uint32) error {
	invalidGroup := group == 0 || group > 1<<31-1
	if c.family != unix.AF_NETLINK || invalidGroup {
		return socketError("setsockopt", os.ErrInvalid)
	}
	return c.control("setsockopt", func(fd int) error {
		return unix.SetsockoptInt(fd, unix.SOL_NETLINK, option, int(group))
	})
}
