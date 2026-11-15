//go:build linux

package socket

import (
	"errors"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Kind selects a Unix socket-pair mode. Modes are exclusive, not flags.
type Kind int

// Datagram preserves independent messages (SOCK_DGRAM).
const Datagram Kind = unix.SOCK_DGRAM

// SeqPacket preserves ordered messages on a connection (SOCK_SEQPACKET).
const SeqPacket Kind = unix.SOCK_SEQPACKET

// Protocol selects an AF_NETLINK protocol. Nonnegative Linux protocol numbers
// are accepted; unavailable protocols fail with their original errno.
type Protocol int

// Route selects NETLINK_ROUTE for routing and link messages.
const Route Protocol = unix.NETLINK_ROUTE

// Usersock selects NETLINK_USERSOCK for user-space messaging.
const Usersock Protocol = unix.NETLINK_USERSOCK

// Generic selects NETLINK_GENERIC for Generic Netlink families.
const Generic Protocol = unix.NETLINK_GENERIC

// Pair creates connected Unix sockets using [socketpair(2)]. Only Datagram and
// SeqPacket are supported. Both returned owners must be closed. Failure closes
// both ends and returns neither. Invalid modes match os.ErrInvalid.
//
// [socketpair(2)]: https://man7.org/linux/man-pages/man2/socketpair.2.html
func Pair(mode Kind) (*Conn, *Conn, error) {
	if mode != Datagram && mode != SeqPacket {
		return nil, nil, socketError("socketpair", os.ErrInvalid)
	}

	fds, err := unix.Socketpair(unix.AF_UNIX, int(mode)|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, nil, socketError("socketpair", err)
	}

	a, err := newConn(fds[0], unix.AF_UNIX)
	if err != nil {
		unix.Close(fds[1])
		return nil, nil, err
	}

	b, err := newConn(fds[1], unix.AF_UNIX)
	if err != nil {
		a.Close()
		return nil, nil, err
	}
	return a, b, nil
}

// ListenUnix binds an owned Unix datagram socket using [bind(2)]. Empty names and
// filesystem names containing NULs match os.ErrInvalid. A leading @ or NUL
// selects the abstract namespace unless Pathname is set; embedded NULs are
// preserved only in abstract names. A successful filesystem binding leaves a
// socket path that the caller must unlink after Close; Close never removes or
// replaces paths. See UnixAddress for the @ shorthand and literal-path selector.
// Failure closes every acquired descriptor and returns no owner.
//
// [bind(2)]: https://man7.org/linux/man-pages/man2/bind.2.html
func ListenUnix(address UnixAddress) (*Conn, error) {
	if !validUnixAddress(address) {
		return nil, socketError("bind", os.ErrInvalid)
	}
	return openBound(unix.AF_UNIX, 0, address)
}

// OpenNetlink binds an owned SOCK_RAW Netlink socket using [netlink(7)].
// PortID zero requests allocation; LocalAddress returns the assigned ID.
// Groups joins the legacy mask at bind time and may require privilege. Negative
// protocols match os.ErrInvalid. Failure closes all acquired resources.
//
// [netlink(7)]: https://man7.org/linux/man-pages/man7/netlink.7.html
func OpenNetlink(protocol Protocol, address NetlinkAddress) (*Conn, error) {
	if protocol < 0 || uint64(protocol) > 1<<31-1 {
		return nil, socketError("socket", os.ErrInvalid)
	}
	return openBound(unix.AF_NETLINK, int(protocol), address)
}

// Conn owns a nonblocking, close-on-exec message socket with Go polling.
// Methods are safe concurrently; concurrent receivers compete for messages.
// Call sequences, socket options, and request/response matching require caller
// coordination. Do not copy a Conn after use. The zero value owns nothing and
// returns os.ErrInvalid; nil receivers are unsupported. Valid operations after
// Close match os.ErrClosed. Invalid arguments take precedence over closed state.
//
// Conn implements io.Closer, but not net.Conn, net.PacketConn, io.Reader, or
// io.Writer: message boundaries, ancillary ownership, and truncation must remain
// visible. No underlying descriptor or raw callback is exposed.
type Conn struct {
	raw    syscall.RawConn
	file   *os.File
	family int
}

// LocalAddress reads getsockname(2); Unix pairs return an unnamed UnixAddress.
func (c *Conn) LocalAddress() (Address, error) {
	var address Address
	err := c.control("getsockname", func(fd int) error {
		var err error
		address, err = socketName(fd)
		return err
	})
	if err != nil {
		return nil, err
	}
	return address, nil
}

// Close releases this descriptor and interrupts pending Send/Receive with
// os.ErrClosed. It does not close sent or previously received files. Repeated
// Close matches os.ErrClosed. A zero Conn returns os.ErrInvalid.
func (c *Conn) Close() error {
	return c.file.Close()
}

// SetDeadline wraps os.File.SetDeadline for pending and future message I/O.
// Zero clears both deadlines; expiry matches os.ErrDeadlineExceeded.
func (c *Conn) SetDeadline(deadline time.Time) error {
	return c.pollError("deadline", c.file.SetDeadline(deadline))
}

// SetReadDeadline wraps os.File.SetReadDeadline without changing write deadlines.
func (c *Conn) SetReadDeadline(deadline time.Time) error {
	return c.pollError("deadline", c.file.SetReadDeadline(deadline))
}

// SetWriteDeadline wraps os.File.SetWriteDeadline without changing read deadlines.
func (c *Conn) SetWriteDeadline(deadline time.Time) error {
	return c.pollError("deadline", c.file.SetWriteDeadline(deadline))
}

func (c *Conn) control(op string, call func(int) error) error {
	if c.file == nil {
		return os.ErrInvalid
	}

	var callErr error
	err := c.raw.Control(func(fd uintptr) {
		callErr = call(int(fd))
	})
	if err != nil {
		return c.pollError(op, err)
	}
	return socketError(op, callErr)
}

func (c *Conn) pollError(op string, err error) error {
	if err == nil || c.file == nil {
		return err
	}
	// RawConn and deadline setters expose the poller's private closing error.
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		err = os.ErrClosed
	}
	return socketError(op, err)
}

func openBound(family, protocol int, address Address) (*Conn, error) {
	var mode int
	if family == unix.AF_NETLINK {
		mode = unix.SOCK_RAW
	} else {
		mode = unix.SOCK_DGRAM
	}

	fd, err := unix.Socket(family, mode|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, protocol)
	if err != nil {
		return nil, socketError("socket", err)
	}

	c, err := newConn(fd, family)
	if err != nil {
		return nil, err
	}

	if err := c.control("bind", func(fd int) error {
		return bind(fd, address)
	}); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func newConn(fd, family int) (*Conn, error) {
	file := os.NewFile(uintptr(fd), "socket")
	// NewFile may retain a descriptor after failed poll registration.
	if err := file.SetDeadline(time.Time{}); err != nil {
		file.Close()
		return nil, err
	}

	raw, err := file.SyscallConn()
	if err != nil {
		file.Close()
		return nil, err
	}
	return &Conn{file: file, raw: raw, family: family}, nil
}

func socketError(op string, err error) error {
	if err == nil {
		return nil
	}
	return &os.PathError{Op: op, Path: "socket", Err: err}
}

func validUnixAddress(address UnixAddress) bool {
	name := address.Name
	maximum := len(unix.RawSockaddrUnix{}.Path)
	empty := len(name) == 0
	tooLong := len(name) > maximum
	fullLengthImplicit := len(name) == maximum && !address.Pathname
	if empty || tooLong || fullLengthImplicit {
		return false
	}
	abstract := !address.Pathname && (name[0] == '@' || name[0] == 0)
	if abstract {
		return true
	}
	for i := range len(name) {
		if name[i] == 0 {
			return false
		}
	}
	return true
}
