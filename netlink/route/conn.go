//go:build linux

package route

import (
	"os"
	"time"

	"github.com/alexfalkowski/go-linux/netlink"
	"github.com/alexfalkowski/go-linux/socket"
)

// Open binds a NETLINK_ROUTE request connection in the calling thread's current
// network namespace. Options and failure cleanup follow netlink.Open.
func Open(options netlink.Options) (*Conn, error) {
	conn, err := netlink.Open(socket.Route, options)
	if err != nil {
		return nil, err
	}
	return &Conn{conn: conn}, nil
}

// Conn owns a request connection. Methods are safe concurrently; requests
// serialize. Deadlines bound I/O, not waiting for that serialization. The zero
// value returns os.ErrInvalid. Valid calls after Close match os.ErrClosed.
// Do not copy after use. Close releases the socket, not configured objects.
type Conn struct {
	conn *netlink.Conn
}

// Close interrupts pending I/O and releases the connection. Repeated Close
// matches os.ErrClosed; an uninitialized Conn returns os.ErrInvalid.
func (c *Conn) Close() error {
	if c.conn == nil {
		return os.ErrInvalid
	}
	return c.conn.Close()
}

// SetDeadline bounds pending and future request I/O; zero clears it. An
// incomplete exchange (including a receive timeout) closes the connection.
func (c *Conn) SetDeadline(deadline time.Time) error {
	if c.conn == nil {
		return os.ErrInvalid
	}
	return c.conn.SetDeadline(deadline)
}

func (c *Conn) dump(kind uint16, data []byte) ([]netlink.Message, error) {
	if c.conn == nil {
		return nil, os.ErrInvalid
	}

	response, err := c.conn.Dump(netlink.Message{Header: netlink.Header{Type: kind}, Data: data})
	if err != nil {
		return nil, err
	}
	return response.Messages, nil
}

func (c *Conn) execute(kind uint16, flags netlink.Flags, data []byte) error {
	if c.conn == nil {
		return os.ErrInvalid
	}

	_, err := c.conn.Execute(netlink.Message{Header: netlink.Header{Type: kind, Flags: flags}, Data: data})
	return err
}
