//go:build linux

package route

import (
	"os"
	"time"

	"github.com/alexfalkowski/go-linux/netlink"
	"github.com/alexfalkowski/go-linux/socket"
	"golang.org/x/sys/unix"
)

// Group is a numeric rtnetlink membership ID, not a legacy bitmask.
type Group uint32

// LinkGroup subscribes to link changes (RTNLGRP_LINK).
const LinkGroup Group = unix.RTNLGRP_LINK

// IPv4AddressGroup subscribes to IPv4 address changes (RTNLGRP_IPV4_IFADDR).
const IPv4AddressGroup Group = unix.RTNLGRP_IPV4_IFADDR

// IPv6AddressGroup subscribes to IPv6 address changes (RTNLGRP_IPV6_IFADDR).
const IPv6AddressGroup Group = unix.RTNLGRP_IPV6_IFADDR

// IPv4RouteGroup subscribes to IPv4 route changes (RTNLGRP_IPV4_ROUTE).
const IPv4RouteGroup Group = unix.RTNLGRP_IPV4_ROUTE

// IPv6RouteGroup subscribes to IPv6 route changes (RTNLGRP_IPV6_ROUTE).
const IPv6RouteGroup Group = unix.RTNLGRP_IPV6_ROUTE

// EventType preserves the Linux RTM_* notification message type.
type EventType uint16

// NewLink reports RTM_NEWLINK: creation or a property change.
const NewLink EventType = unix.RTM_NEWLINK

// DelLink reports RTM_DELLINK.
const DelLink EventType = unix.RTM_DELLINK

// NewAddress reports RTM_NEWADDR: creation or a property change.
const NewAddress EventType = unix.RTM_NEWADDR

// DelAddress reports RTM_DELADDR.
const DelAddress EventType = unix.RTM_DELADDR

// NewRoute reports RTM_NEWROUTE: creation or replacement.
const NewRoute EventType = unix.RTM_NEWROUTE

// DelRoute reports RTM_DELROUTE.
const DelRoute EventType = unix.RTM_DELROUTE

// ParseEvent decodes one supported routing notification. Exactly one record
// pointer is set on success. Unsupported types match netlink.ErrUnexpectedReply;
// malformed records match netlink.ErrMalformed. Attribute bytes borrow input.
func ParseEvent(message netlink.Message) (Event, error) {
	event := Event{Type: EventType(message.Header.Type)}
	switch event.Type {
	case NewLink, DelLink:
		link, err := ParseLink(message)
		if err != nil {
			return Event{}, err
		}
		event.Link = &link
	case NewAddress, DelAddress:
		address, err := ParseAddress(message)
		if err != nil {
			return Event{}, err
		}
		event.Address = &address
	case NewRoute, DelRoute:
		route, err := ParseRoute(message)
		if err != nil {
			return Event{}, err
		}
		event.Route = &route
	default:
		return Event{}, netlink.ErrUnexpectedReply
	}
	return event, nil
}

// Event is one notification, not a transaction boundary or a complete history.
type Event struct {
	// Link is set only for NewLink/DelLink.
	Link *Link
	// Address is set only for NewAddress/DelAddress.
	Address *Address
	// Route is set only for NewRoute/DelRoute.
	Route *Route
	// Type distinguishes NEW (including change) from DEL notifications.
	Type EventType
}

// Listen binds a separate socket and joins one or more named Group values.
// Empty groups or unnamed group IDs match os.ErrInvalid before acquisition.
// Failure closes all acquired resources; Options follows netlink.Listen.
func Listen(options netlink.Options, groups ...Group) (*Listener, error) {
	if len(groups) == 0 {
		return nil, os.ErrInvalid
	}

	ids := make([]uint32, 0, len(groups))
	for _, group := range groups {
		switch group {
		case LinkGroup, IPv4AddressGroup, IPv6AddressGroup, IPv4RouteGroup, IPv6RouteGroup:
			ids = append(ids, uint32(group))
		default:
			return nil, os.ErrInvalid
		}
	}

	listener, err := netlink.Listen(socket.Route, options, ids...)
	if err != nil {
		return nil, err
	}
	return &Listener{listener: listener}, nil
}

// Listener owns lossy multicast reception. Methods are safe concurrently;
// concurrent readers compete for datagrams. It starts no goroutine. Zero values
// return os.ErrInvalid; valid calls after Close match os.ErrClosed. Do not copy.
type Listener struct {
	listener *netlink.Listener
}

// ReadEvents consumes a datagram and decodes all its records. Data remains valid
// after later reads/Close. Any error returns nil, with no partial batch; errors
// leave the listener open. ENOBUFS, netlink.ErrTruncated, or unsupported/malformed
// notifications require resynchronization over a separate Conn. No atomic
// subscribe-plus-dump handshake or automatic recovery is promised.
func (l *Listener) ReadEvents() ([]Event, error) {
	if l.listener == nil {
		return nil, os.ErrInvalid
	}

	messages, err := l.listener.ReadMessages()
	if err != nil {
		return nil, err
	}

	return parseMessages(messages, ParseEvent)
}

// SetReadDeadline bounds pending/future reads; zero clears it. Expiry matches
// os.ErrDeadlineExceeded and leaves the listener usable.
func (l *Listener) SetReadDeadline(deadline time.Time) error {
	if l.listener == nil {
		return os.ErrInvalid
	}
	return l.listener.SetReadDeadline(deadline)
}

// Close interrupts pending reads and releases memberships and the socket.
// Repeated Close matches os.ErrClosed; a zero value returns os.ErrInvalid.
func (l *Listener) Close() error {
	if l.listener == nil {
		return os.ErrInvalid
	}
	return l.listener.Close()
}
