//go:build linux

package netlink

import (
	"os"
	"time"

	"github.com/alexfalkowski/go-linux/socket"
	"golang.org/x/sys/unix"
)

// Listen binds a separate multicast connection and joins positive numeric group
// IDs (not masks). Groups may be empty for later JoinGroup calls. Membership
// failures close everything and return no listener. Group permissions and
// availability are protocol-specific; errno is preserved. Options follow Open.
func Listen(protocol socket.Protocol, options Options, groups ...uint32) (*Listener, error) {
	for _, group := range groups {
		if group == 0 || group > 1<<31-1 {
			return nil, os.ErrInvalid
		}
	}

	conn, options, err := open(protocol, options)
	if err != nil {
		return nil, err
	}

	for _, group := range groups {
		if err := conn.JoinGroup(group); err != nil {
			conn.Close()
			return nil, err
		}
	}
	return &Listener{conn: conn, size: options.DatagramSize}, nil
}

// Listener owns a multicast socket. Methods are safe concurrently; concurrent
// readers compete for datagrams. No subscription is lossless: ENOBUFS, Overrun,
// and ErrTruncated mean the consumer must resynchronize using a separate Conn.
// Zero values return os.ErrInvalid. Do not copy after use.
type Listener struct {
	conn *socket.Conn
	size int
}

// ReadMessages consumes one datagram of authenticated kernel messages. Returned
// data remains valid after later reads. Truncation/malformed framing returns no
// partial batch. NLMSG_OVERRUN returns the batch and ENOBUFS; other controls and
// unknown types are retained. Errors do not close the listener. The destination
// multicast group is not reported; use separate listeners to distinguish groups.
func (l *Listener) ReadMessages() ([]Message, error) {
	if l.conn == nil {
		return nil, os.ErrInvalid
	}

	messages, _, err := receive(l.conn, l.size)
	if err != nil {
		return nil, err
	}

	for _, message := range messages {
		if message.Header.Type == Overrun {
			return messages, unix.ENOBUFS
		}
	}
	return messages, nil
}

// JoinGroup adds NETLINK_ADD_MEMBERSHIP through socket.Conn.JoinGroup.
// IDs must be in 1..2^31-1; invalid IDs match os.ErrInvalid.
func (l *Listener) JoinGroup(group uint32) error {
	invalidGroup := group == 0 || group > 1<<31-1
	if invalidGroup || l.conn == nil {
		return os.ErrInvalid
	}
	return l.conn.JoinGroup(group)
}

// LeaveGroup drops NETLINK_DROP_MEMBERSHIP through socket.Conn.LeaveGroup,
// using JoinGroup's validation. Already queued notifications may remain.
func (l *Listener) LeaveGroup(group uint32) error {
	invalidGroup := group == 0 || group > 1<<31-1
	if invalidGroup || l.conn == nil {
		return os.ErrInvalid
	}
	return l.conn.LeaveGroup(group)
}

// SetReadDeadline bounds pending/future reads. Zero clears the deadline;
// expiry matches os.ErrDeadlineExceeded and leaves the listener open.
func (l *Listener) SetReadDeadline(deadline time.Time) error {
	if l.conn == nil {
		return os.ErrInvalid
	}
	return l.conn.SetReadDeadline(deadline)
}

// Close releases membership and the descriptor, interrupting pending reads.
// Repeated Close and valid operations after closure match os.ErrClosed.
func (l *Listener) Close() error {
	if l.conn == nil {
		return os.ErrInvalid
	}
	return l.conn.Close()
}
