//go:build linux

package route

import (
	"encoding/binary"
	"net/netip"
	"os"
	"strings"

	"github.com/alexfalkowski/go-linux/netlink"
	"golang.org/x/sys/unix"
)

// AddressFlags preserves Linux IFA_F_* bits, including unknown output bits.
type AddressFlags uint32

// NoDAD disables IPv6 duplicate address detection (IFA_F_NODAD).
const NoDAD AddressFlags = unix.IFA_F_NODAD

// NoPrefixRoute suppresses the automatic prefix route (IFA_F_NOPREFIXROUTE).
const NoPrefixRoute AddressFlags = unix.IFA_F_NOPREFIXROUTE

// Tentative reports IPv6 duplicate address detection in progress (IFA_F_TENTATIVE).
const Tentative AddressFlags = unix.IFA_F_TENTATIVE

// Permanent reports an address without a finite valid lifetime (IFA_F_PERMANENT).
const Permanent AddressFlags = unix.IFA_F_PERMANENT

// ParseAddress decodes IPv4/IPv6 RTM_NEWADDR or RTM_DELADDR. The local address
// comes from IFA_LOCAL when present, otherwise IFA_ADDRESS. A differing
// IFA_ADDRESS becomes Peer.
// IFA_FLAGS overrides the fixed header's eight bits. All original attributes
// borrow message.Data. Missing addresses, invalid prefixes, duplicate interpreted
// attributes, or wrong widths match netlink.ErrMalformed and return no record.
func ParseAddress(message netlink.Message) (Address, error) {
	if (message.Header.Type != unix.RTM_NEWADDR && message.Header.Type != unix.RTM_DELADDR) || len(message.Data) < unix.SizeofIfAddrmsg {
		return Address{}, netlink.ErrMalformed
	}

	data := message.Data
	family := Family(data[0])
	if !family.valid() {
		return Address{}, netlink.ErrMalformed
	}

	attributes, err := nativeAttributes(data[unix.SizeofIfAddrmsg:], unix.IFA_LOCAL, unix.IFA_ADDRESS, unix.IFA_BROADCAST, unix.IFA_LABEL, unix.IFA_FLAGS, unix.IFA_RT_PRIORITY)
	if err != nil {
		return Address{}, err
	}

	address := Address{Index: int(binary.NativeEndian.Uint32(data[4:])), Flags: AddressFlags(data[2]), Scope: Scope(data[3]), Attributes: attributes}
	if !validIndex(address.Index) {
		return Address{}, netlink.ErrMalformed
	}

	if err := address.decodeAttributes(family, data[1]); err != nil {
		return Address{}, err
	}
	return address, nil
}

// Address is one observed IP interface address, not a masked network prefix.
type Address struct {
	// Prefix preserves the local host address and Linux prefix length.
	Prefix netip.Prefix
	// Peer is a differing IFA_ADDRESS; invalid means absent or equal to Prefix.Addr().
	Peer netip.Addr
	// Broadcast is IFA_BROADCAST; invalid means absent.
	Broadcast netip.Addr
	// Label is IFA_LABEL; empty means absent.
	Label string
	// Attributes retains all original attributes, including cache lifetimes.
	Attributes []netlink.Attribute
	// Index identifies the interface in this connection's network namespace.
	Index int
	// Flags preserves the full IFA_FLAGS value, or the fixed-header fallback.
	Flags AddressFlags
	// Priority is IFA_RT_PRIORITY, the automatic prefix route's metric.
	Priority uint32
	// Scope is the Linux address scope, including unknown output values.
	Scope Scope
}

// AddressConfig describes an address mutation. Finite lifetimes and IPv6 address
// generation policy are outside this operation set. Add/change request permanent
// lifetime by omitting IFA_CACHEINFO; Linux controls family-specific semantics.
type AddressConfig struct {
	// Prefix contains the local host address and prefix length; it must be valid
	// IPv4/IPv6 without a zone. Host bits are preserved, not masked away.
	Prefix netip.Prefix
	// Peer supplies a same-family peer address; invalid uses Prefix.Addr().
	Peer netip.Addr
	// Broadcast supplies an optional IPv4 broadcast address; invalid omits it.
	Broadcast netip.Addr
	// Label optionally supplies a NUL-free IPv4 label of at most 15 bytes.
	Label string
	// Index must be a positive interface index fitting a Linux signed s32.
	Index int
	// Flags accepts NoDAD and NoPrefixRoute; zero uses kernel defaults.
	Flags AddressFlags
	// Priority supplies IFA_RT_PRIORITY, the automatic prefix route's metric.
	// Zero requests the kernel default on creation. On replacement, zero resets
	// IPv4 priority to its default but preserves an existing IPv6 priority.
	Priority uint32
	// Scope supplies the Linux scope; zero means Universe.
	Scope Scope
}

// Addresses lists one IP family's addresses using RTM_GETADDR. Family must be
// IPv4 or IPv6. Errors return nil rather than partial or interrupted results.
func (c *Conn) Addresses(family Family) ([]Address, error) {
	if !family.valid() {
		return nil, os.ErrInvalid
	}

	data := make([]byte, unix.SizeofIfAddrmsg)
	data[0] = byte(family)

	messages, err := c.dump(unix.RTM_GETADDR, data)
	if err != nil {
		return nil, err
	}

	return parseMessages(messages, func(message netlink.Message) (Address, error) {
		if message.Header.Type != unix.RTM_NEWADDR || len(message.Data) == 0 || Family(message.Data[0]) != family {
			return Address{}, netlink.ErrUnexpectedReply
		}
		return ParseAddress(message)
	})
}

// AddAddress requests RTM_NEWADDR with CREATE|EXCL, preserving EEXIST. Invalid
// configuration matches os.ErrInvalid before I/O; kernel errors retain errno.
func (c *Conn) AddAddress(config AddressConfig) error {
	return c.address(unix.RTM_NEWADDR, netlink.Create|netlink.Exclusive, config)
}

// ChangeAddress requests RTM_NEWADDR with REPLACE. Linux can create an absent
// address even without CREATE. IPv4 replacement updates lifetime and prefix-route
// priority but retains existing flags, scope, label, broadcast, and peer; IPv6
// has its own modification rules. No existence preflight or rollback is offered.
func (c *Conn) ChangeAddress(config AddressConfig) error {
	return c.address(unix.RTM_NEWADDR, netlink.Replace, config)
}

// DeleteAddress requests RTM_DELADDR using the supplied local/peer identity and
// prefix. It does not remove the interface. Errors preserve kernel errno.
func (c *Conn) DeleteAddress(config AddressConfig) error {
	return c.address(unix.RTM_DELADDR, 0, config)
}

func (c *Conn) address(kind uint16, flags netlink.Flags, config AddressConfig) error {
	if err := config.validate(); err != nil {
		return err
	}
	local := config.Prefix.Addr()
	family := ipFamily(local)
	var peer netip.Addr
	if config.Peer.IsValid() {
		peer = config.Peer
	} else {
		peer = local
	}
	data := make([]byte, unix.SizeofIfAddrmsg)
	data[0], data[1] = byte(family), byte(config.Prefix.Bits()) //nolint:gosec // A valid prefix has at most 128 bits.
	data[2], data[3] = byte(config.Flags&0xff), byte(config.Scope)
	binary.NativeEndian.PutUint32(data[4:], uint32(config.Index)) //nolint:gosec // Validated positive s32 index.
	attributes := []netlink.Attribute{
		{Type: unix.IFA_LOCAL, Data: local.AsSlice()},
		{Type: unix.IFA_ADDRESS, Data: peer.AsSlice()},
		scalar(unix.IFA_FLAGS, uint32(config.Flags)),
		scalar(unix.IFA_RT_PRIORITY, config.Priority),
	}
	if config.Broadcast.IsValid() {
		attributes = append(attributes, netlink.Attribute{Type: unix.IFA_BROADCAST, Data: config.Broadcast.AsSlice()})
	}
	if config.Label != "" {
		attributes = append(attributes, netlink.Attribute{Type: unix.IFA_LABEL, Data: append([]byte(config.Label), 0)})
	}
	data, err := appendAttributes(data, attributes)
	if err != nil {
		return err
	}
	return c.execute(kind, flags, data)
}

func (a *Address) decodeAttributes(family Family, bits uint8) error {
	var err error
	var local, peer netip.Addr
	for _, attribute := range a.Attributes {
		switch attribute.ID() {
		case unix.IFA_LOCAL:
			local, err = parseIP(attribute.Data, family)
		case unix.IFA_ADDRESS:
			peer, err = parseIP(attribute.Data, family)
		default:
			err = a.decodeAttribute(attribute, family)
		}
		if err != nil {
			return err
		}
	}
	if !local.IsValid() {
		local = peer
	}
	a.Prefix, err = parsePrefix(local, bits)
	if err != nil {
		return err
	}
	if peer.IsValid() && peer != local {
		a.Peer = peer
	}
	return nil
}

func (a *Address) decodeAttribute(attribute netlink.Attribute, family Family) error {
	var err error
	switch attribute.ID() {
	case unix.IFA_BROADCAST:
		a.Broadcast, err = parseIP(attribute.Data, family)
	case unix.IFA_LABEL:
		a.Label, err = attribute.String()
	case unix.IFA_FLAGS:
		var flags uint32
		flags, err = attribute.Uint32()
		a.Flags = AddressFlags(flags)
	case unix.IFA_RT_PRIORITY:
		a.Priority, err = attribute.Uint32()
	}
	return err
}

func (c AddressConfig) validate() error {
	if !c.Prefix.IsValid() || !validIndex(c.Index) || c.Flags & ^(NoDAD|NoPrefixRoute) != 0 {
		return os.ErrInvalid
	}

	local := c.Prefix.Addr()
	family := ipFamily(local)
	if !validIP(local, family) || !validOptionalIP(c.Peer, family) {
		return os.ErrInvalid
	}
	return c.validateIPv4Attributes(family)
}

func (c AddressConfig) validateIPv4Attributes(family Family) error {
	if c.Broadcast.IsValid() && (family != IPv4 || !validIP(c.Broadcast, IPv4)) {
		return os.ErrInvalid
	}
	if c.Label != "" && (family != IPv4 || len(c.Label) >= unix.IFNAMSIZ || strings.ContainsRune(c.Label, 0)) {
		return os.ErrInvalid
	}
	return nil
}
