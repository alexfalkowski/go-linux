//go:build linux

package route

import (
	"encoding/binary"
	"net"
	"os"

	"github.com/alexfalkowski/go-linux/netlink"
	"golang.org/x/sys/unix"
)

// LinkFlags preserves Linux IFF_* bits, rather than net.Flags' different values.
type LinkFlags uint32

// Up is the administrative IFF_UP bit.
const Up LinkFlags = unix.IFF_UP

// Broadcast indicates broadcast support (IFF_BROADCAST).
const Broadcast LinkFlags = unix.IFF_BROADCAST

// Loopback identifies a loopback device (IFF_LOOPBACK).
const Loopback LinkFlags = unix.IFF_LOOPBACK

// PointToPoint identifies a point-to-point device (IFF_POINTOPOINT).
const PointToPoint LinkFlags = unix.IFF_POINTOPOINT

// Running reports operational resources (IFF_RUNNING).
const Running LinkFlags = unix.IFF_RUNNING

// NoARP disables address resolution (IFF_NOARP).
const NoARP LinkFlags = unix.IFF_NOARP

// Promiscuous requests promiscuous reception (IFF_PROMISC).
const Promiscuous LinkFlags = unix.IFF_PROMISC

// AllMulticast requests all multicast reception (IFF_ALLMULTI).
const AllMulticast LinkFlags = unix.IFF_ALLMULTI

// Multicast indicates multicast support (IFF_MULTICAST).
const Multicast LinkFlags = unix.IFF_MULTICAST

// LowerUp reports the driver's carrier state (IFF_LOWER_UP).
const LowerUp LinkFlags = unix.IFF_LOWER_UP

// LinkKind selects a supported virtual link creation mechanism.
type LinkKind string

// Dummy creates a software dummy device. It may require the dummy kernel module.
const Dummy LinkKind = "dummy"

// ParseLink decodes RTM_NEWLINK or RTM_DELLINK. Fixed fields and all attributes
// are preserved; interpreted singleton fields must have valid widths/strings.
// Unknown link kinds are retained. Malformed input matches netlink.ErrMalformed
// and returns no record. Attribute and hardware-address bytes borrow message.Data.
func ParseLink(message netlink.Message) (Link, error) {
	wrongType := message.Header.Type != unix.RTM_NEWLINK && message.Header.Type != unix.RTM_DELLINK
	shortHeader := len(message.Data) < unix.SizeofIfInfomsg
	if wrongType || shortHeader {
		return Link{}, netlink.ErrMalformed
	}

	data := message.Data
	attributes, err := nativeAttributes(data[unix.SizeofIfInfomsg:], unix.IFLA_IFNAME, unix.IFLA_MTU, unix.IFLA_ADDRESS, unix.IFLA_LINKINFO)
	if err != nil {
		return Link{}, err
	}

	index := int32(binary.NativeEndian.Uint32(data[4:])) //nolint:gosec // ifi_index is a signed Linux s32.
	if index <= 0 {
		return Link{}, netlink.ErrMalformed
	}

	link := Link{
		Index: int(index), Family: data[0], HardwareType: binary.NativeEndian.Uint16(data[2:]),
		Flags: LinkFlags(binary.NativeEndian.Uint32(data[8:])), Change: LinkFlags(binary.NativeEndian.Uint32(data[12:])), Attributes: attributes,
	}
	if err := link.decodeAttributes(); err != nil {
		return Link{}, err
	}
	return link, nil
}

// Link is an observed Linux interface. Index can be reused after deletion.
type Link struct {
	// Name is IFLA_IFNAME, or empty when absent from the message.
	Name string
	// Kind is IFLA_INFO_KIND, or empty for devices without link-kind metadata.
	Kind string
	// HardwareAddr is the variable-length IFLA_ADDRESS, not necessarily Ethernet.
	HardwareAddr net.HardwareAddr
	// Attributes retains all attributes, including unknown IDs and nested data.
	Attributes []netlink.Attribute
	// Index is the positive Linux interface index.
	Index int
	// MTU is IFLA_MTU in bytes, or zero when absent.
	MTU uint32
	// Flags preserves all ifi_flags bits, including unknown bits.
	Flags LinkFlags
	// Change preserves the ifi_change mask supplied in this observation.
	Change LinkFlags
	// HardwareType preserves the ARPHRD_* interface type.
	HardwareType uint16
	// Family preserves ifi_family; ordinary link dumps request AF_UNSPEC.
	Family uint8
}

// LinkChange changes selected link properties. Zero fields leave properties
// untouched, except Flags is applied wherever Change has a bit set.
type LinkChange struct {
	// Name renames the device; empty leaves its name unchanged.
	Name string
	// MTU sets the MTU in bytes; zero leaves it unchanged.
	MTU uint32
	// Flags supplies the values selected by Change; other bits must be zero.
	Flags LinkFlags
	// Change selects Up, NoARP, Promiscuous, AllMulticast, or Multicast bits.
	Change LinkFlags
}

// Links lists interfaces with RTM_GETLINK. Errors, interrupted dumps, or malformed
// records return nil, never a partial list. Results are not an atomic snapshot.
func (c *Conn) Links() ([]Link, error) {
	messages, err := c.dump(unix.RTM_GETLINK, make([]byte, unix.SizeofIfInfomsg))
	if err != nil {
		return nil, err
	}

	return parseMessages(messages, func(message netlink.Message) (Link, error) {
		if message.Header.Type != unix.RTM_NEWLINK {
			return Link{}, netlink.ErrUnexpectedReply
		}
		return ParseLink(message)
	})
}

// AddLink creates a named Dummy link using RTM_NEWLINK with CREATE|EXCL. Other
// kinds, invalid names, or names above 15 bytes match os.ErrInvalid before I/O.
// The kernel assigns the index; list Links to observe it. No local resource is
// returned, and an ambiguous exchange error cannot establish whether it exists.
func (c *Conn) AddLink(name string, kind LinkKind) error {
	if !validName(name) || kind != Dummy {
		return os.ErrInvalid
	}

	info, err := netlink.MarshalAttributes([]netlink.Attribute{{Type: unix.IFLA_INFO_KIND, Data: append([]byte(kind), 0)}})
	if err != nil {
		return err
	}

	data, err := appendAttributes(make([]byte, unix.SizeofIfInfomsg), []netlink.Attribute{
		{Type: unix.IFLA_IFNAME, Data: append([]byte(name), 0)},
		{Type: unix.IFLA_LINKINFO | netlink.Nested, Data: info},
	})
	if err != nil {
		return err
	}
	return c.execute(unix.RTM_NEWLINK, netlink.Create|netlink.Exclusive, data)
}

// ChangeLink applies RTM_NEWLINK to an existing positive index. Invalid indices,
// names, unsupported mask bits, or Flags outside Change match os.ErrInvalid
// before I/O. Multiple properties are not a transaction; a kernel error may
// follow a partial change. Linux determines device-specific restrictions.
func (c *Conn) ChangeLink(index int, change LinkChange) error {
	const supported = Up | NoARP | Promiscuous | AllMulticast | Multicast
	invalidIndex := !validIndex(index)
	invalidName := change.Name != "" && !validName(change.Name)
	unknownFlags := change.Change & ^supported != 0
	unselectedFlags := change.Flags & ^change.Change != 0
	if invalidIndex || invalidName || unknownFlags || unselectedFlags {
		return os.ErrInvalid
	}

	data := make([]byte, unix.SizeofIfInfomsg)
	binary.NativeEndian.PutUint32(data[4:], uint32(index)) //nolint:gosec // Validated positive s32 index.
	binary.NativeEndian.PutUint32(data[8:], uint32(change.Flags))
	binary.NativeEndian.PutUint32(data[12:], uint32(change.Change))

	var attributes []netlink.Attribute
	if change.Name != "" {
		attributes = append(attributes, netlink.Attribute{Type: unix.IFLA_IFNAME, Data: append([]byte(change.Name), 0)})
	}
	if change.MTU != 0 {
		attributes = append(attributes, scalar(unix.IFLA_MTU, change.MTU))
	}

	data, err := appendAttributes(data, attributes)
	if err != nil {
		return err
	}
	return c.execute(unix.RTM_NEWLINK, 0, data)
}

// DeleteLink removes a positive interface index with RTM_DELLINK. Invalid
// indices match os.ErrInvalid before I/O; the kernel decides removability.
// Associated addresses and routes may be removed as a consequence.
func (c *Conn) DeleteLink(index int) error {
	if !validIndex(index) {
		return os.ErrInvalid
	}

	data := make([]byte, unix.SizeofIfInfomsg)
	binary.NativeEndian.PutUint32(data[4:], uint32(index)) //nolint:gosec // Validated positive s32 index.
	return c.execute(unix.RTM_DELLINK, 0, data)
}

func parseLinkKind(data []byte) (string, error) {
	attributes, err := nativeAttributes(data, unix.IFLA_INFO_KIND)
	if err != nil {
		return "", err
	}

	for _, attribute := range attributes {
		if attribute.ID() == unix.IFLA_INFO_KIND {
			return attribute.String()
		}
	}
	return "", nil
}

func (l *Link) decodeAttributes() error {
	var err error
	for _, attribute := range l.Attributes {
		switch attribute.ID() {
		case unix.IFLA_IFNAME:
			l.Name, err = attribute.String()
			if err != nil {
				return err
			} else if !validName(l.Name) {
				return netlink.ErrMalformed
			}
		case unix.IFLA_MTU:
			l.MTU, err = attribute.Uint32()
		case unix.IFLA_ADDRESS:
			l.HardwareAddr = net.HardwareAddr(attribute.Data)
		case unix.IFLA_LINKINFO:
			l.Kind, err = parseLinkKind(attribute.Data)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
