//go:build linux

package route

import (
	"encoding/binary"
	"net/netip"
	"os"

	"github.com/alexfalkowski/go-linux/netlink"
	"golang.org/x/sys/unix"
)

// ParseRoute decodes IPv4/IPv6 RTM_NEWROUTE or RTM_DELROUTE. Missing RTA_DST
// means the family's unspecified address; prefix lengths still apply. RTA_TABLE
// overrides the eight-bit header table. Unknown attributes, multipath records,
// metrics, and encapsulation are retained without interpretation. Attribute bytes
// borrow message.Data. Malformed interpreted fields match netlink.ErrMalformed
// and return no record. A parsed Route is an observation, not a replay request.
func ParseRoute(message netlink.Message) (Route, error) {
	wrongType := message.Header.Type != unix.RTM_NEWROUTE && message.Header.Type != unix.RTM_DELROUTE
	shortHeader := len(message.Data) < unix.SizeofRtMsg
	if wrongType || shortHeader {
		return Route{}, netlink.ErrMalformed
	}

	data := message.Data
	family := Family(data[0])
	if !family.valid() {
		return Route{}, netlink.ErrMalformed
	}

	attributes, err := nativeAttributes(data[unix.SizeofRtMsg:],
		unix.RTA_DST, unix.RTA_SRC, unix.RTA_GATEWAY, unix.RTA_PREFSRC, unix.RTA_OIF, unix.RTA_IIF, unix.RTA_TABLE, unix.RTA_PRIORITY)
	if err != nil {
		return Route{}, err
	}

	route := Route{
		Table: Table(data[4]), Protocol: Protocol(data[5]), Scope: Scope(data[6]), Type: RouteType(data[7]),
		Flags: RouteFlags(binary.NativeEndian.Uint32(data[8:])), TOS: data[3], Attributes: attributes,
	}
	if err := route.decodePrefixes(family, data[1], data[2]); err != nil {
		return Route{}, err
	}
	return route, nil
}

// Route is an observed routing table entry. Advanced route forms remain visible
// in Attributes; zero Index/Gateway does not imply a route has no next hops.
type Route struct {
	// Destination preserves RTA_DST and rtm_dst_len; default routes use /0.
	Destination netip.Prefix
	// Source is the optional source selector, distinct from PreferredSource.
	Source netip.Prefix
	// Gateway is the same-family RTA_GATEWAY; invalid means absent.
	Gateway netip.Addr
	// PreferredSource is RTA_PREFSRC; invalid means absent.
	PreferredSource netip.Addr
	// Attributes retains every attribute, including advanced/unknown forms.
	Attributes []netlink.Attribute
	// Index is RTA_OIF, or zero when absent.
	Index int
	// InputIndex is RTA_IIF, or zero when absent.
	InputIndex int
	// Table preserves the full table identifier, including RTA_TABLE overrides.
	Table Table
	// Priority is RTA_PRIORITY (the route metric), or zero when absent.
	Priority uint32
	// Flags preserves all rtm_flags, including unknown output bits.
	Flags RouteFlags
	// Protocol identifies the installer, including unknown output values.
	Protocol Protocol
	// Scope preserves rtm_scope.
	Scope Scope
	// Type preserves rtm_type, including advanced route forms.
	Type RouteType
	// TOS preserves rtm_tos.
	TOS uint8
}

func (r *Route) decodeAttribute(attribute netlink.Attribute, family Family) error {
	var err error
	switch attribute.ID() {
	case unix.RTA_GATEWAY:
		r.Gateway, err = parseIP(attribute.Data, family)
	case unix.RTA_PREFSRC:
		r.PreferredSource, err = parseIP(attribute.Data, family)
	case unix.RTA_TABLE:
		var table uint32
		table, err = attribute.Uint32()
		r.Table = Table(table)
	case unix.RTA_PRIORITY:
		r.Priority, err = attribute.Uint32()
	case unix.RTA_OIF, unix.RTA_IIF:
		var index uint32
		index, err = attribute.Uint32()
		if err != nil {
			return err
		} else if !validIndex(int(index)) {
			return netlink.ErrMalformed
		}
		if attribute.ID() == unix.RTA_OIF {
			r.Index = int(index)
		} else {
			r.InputIndex = int(index)
		}
	}
	return err
}

// RouteConfig describes a bounded, single-next-hop IP route. It cannot replay
// arbitrary Route observations: multipath, source selectors, TOS, nexthop IDs,
// metrics nests, encapsulation, and cross-family gateways are not inputs.
type RouteConfig struct {
	// Gateway optionally supplies a same-family, unzoned gateway.
	Gateway netip.Addr
	// PreferredSource optionally supplies a same-family, unzoned source address.
	PreferredSource netip.Addr
	// Destination must be a valid, masked IPv4/IPv6 prefix; /0 is explicit.
	Destination netip.Prefix
	// Index is the output interface; zero omits RTA_OIF for kernel selection.
	Index int
	// Table is the routing table; zero selects MainTable for all mutations.
	Table Table
	// Priority is the metric; zero omits the attribute (a wildcard on deletion).
	Priority uint32
	// Protocol is the installer; zero uses BootProtocol on add/change and is
	// unspecified (a wildcard) on delete. Other numeric protocol IDs are valid.
	Protocol Protocol
	// Scope is rtm_scope; zero means Universe, including for direct routes.
	// Callers normally select LinkScope explicitly for an IPv4 direct route.
	Scope Scope
	// Type accepts Unicast, Local, Blackhole, Unreachable, or Prohibit. Zero
	// uses Unicast on add/change and is unspecified on delete.
	Type RouteType
}

// Routes dumps one IP family's routing tables with RTM_GETROUTE. Family must
// be IPv4 or IPv6. Errors return nil, never partial/interrupted lists. Dumps
// include advanced forms through Route.Attributes, not only mutable forms.
func (c *Conn) Routes(family Family) ([]Route, error) {
	if !family.valid() {
		return nil, os.ErrInvalid
	}

	data := make([]byte, unix.SizeofRtMsg)
	data[0] = byte(family)

	messages, err := c.dump(unix.RTM_GETROUTE, data)
	if err != nil {
		return nil, err
	}

	return parseMessages(messages, func(message netlink.Message) (Route, error) {
		matchesFamily := message.Header.Type == unix.RTM_NEWROUTE && len(message.Data) > 0 && Family(message.Data[0]) == family
		if !matchesFamily {
			return Route{}, netlink.ErrUnexpectedReply
		}
		return ParseRoute(message)
	})
}

// AddRoute requests RTM_NEWROUTE with CREATE|EXCL. Invalid configuration matches
// os.ErrInvalid before I/O; EEXIST and other kernel errors preserve errno.
func (c *Conn) AddRoute(config RouteConfig) error {
	return c.route(unix.RTM_NEWROUTE, netlink.Create|netlink.Exclusive, config)
}

// ChangeRoute requests RTM_NEWROUTE with REPLACE and without CREATE. It replaces
// the route selected by Linux's family-specific route identity, including metric;
// it neither preflights existence nor deletes/recreates a route in userspace.
func (c *Conn) ChangeRoute(config RouteConfig) error {
	return c.route(unix.RTM_NEWROUTE, netlink.Replace, config)
}

// DeleteRoute requests RTM_DELROUTE. Unspecified fields can act as kernel
// selectors/wildcards; provide enough identity to select the intended entry.
// The operation removes one matching route according to kernel semantics.
func (c *Conn) DeleteRoute(config RouteConfig) error {
	return c.route(unix.RTM_DELROUTE, 0, config)
}

func (c *Conn) route(kind uint16, flags netlink.Flags, config RouteConfig) error {
	if err := config.validate(); err != nil {
		return err
	}

	family := ipFamily(config.Destination.Addr())
	config = config.normalized(kind)

	data := make([]byte, unix.SizeofRtMsg)
	data[0], data[1] = byte(family), byte(config.Destination.Bits()) //nolint:gosec // A valid prefix has at most 128 bits.
	data[5], data[6], data[7] = byte(config.Protocol), byte(config.Scope), byte(config.Type)
	if config.Table <= 255 {
		data[4] = byte(config.Table)
	}
	attributes := []netlink.Attribute{
		{Type: unix.RTA_DST, Data: config.Destination.Addr().AsSlice()},
		scalar(unix.RTA_TABLE, uint32(config.Table)),
	}
	if config.Index != 0 {
		attributes = append(attributes, scalar(unix.RTA_OIF, uint32(config.Index))) //nolint:gosec // Validated positive s32 index.
	}
	if config.Gateway.IsValid() {
		attributes = append(attributes, netlink.Attribute{Type: unix.RTA_GATEWAY, Data: config.Gateway.AsSlice()})
	}
	if config.PreferredSource.IsValid() {
		attributes = append(attributes, netlink.Attribute{Type: unix.RTA_PREFSRC, Data: config.PreferredSource.AsSlice()})
	}
	if config.Priority != 0 {
		attributes = append(attributes, scalar(unix.RTA_PRIORITY, config.Priority))
	}

	data, err := appendAttributes(data, attributes)
	if err != nil {
		return err
	}
	return c.execute(kind, flags, data)
}

func (r *Route) decodePrefixes(family Family, destinationBits, sourceBits uint8) error {
	var destination netip.Addr
	if family == IPv4 {
		destination = netip.IPv4Unspecified()
	} else {
		destination = netip.IPv6Unspecified()
	}

	var source netip.Addr
	var err error
	for _, attribute := range r.Attributes {
		switch attribute.ID() {
		case unix.RTA_DST:
			destination, err = parseIP(attribute.Data, family)
		case unix.RTA_SRC:
			source, err = parseIP(attribute.Data, family)
		default:
			err = r.decodeAttribute(attribute, family)
		}
		if err != nil {
			return err
		}
	}

	r.Destination, err = parsePrefix(destination, destinationBits)
	if err != nil {
		return err
	}

	if !source.IsValid() && sourceBits == 0 {
		return nil
	}
	r.Source, err = parsePrefix(source, sourceBits)
	return err
}

func (c RouteConfig) validate() error {
	invalidDestination := !c.Destination.IsValid() || c.Destination != c.Destination.Masked()
	invalidIndex := c.Index < 0 || (c.Index != 0 && !validIndex(c.Index))
	if invalidDestination || invalidIndex {
		return os.ErrInvalid
	}

	family := ipFamily(c.Destination.Addr())
	invalidAddress := !validIP(c.Destination.Addr(), family)
	invalidGateway := !validOptionalIP(c.Gateway, family)
	invalidSource := !validOptionalIP(c.PreferredSource, family)
	if invalidAddress || invalidGateway || invalidSource {
		return os.ErrInvalid
	}
	return c.validateType()
}

func (c RouteConfig) validateType() error {
	switch c.Type {
	case 0, Unicast, Local, Blackhole, Unreachable, Prohibit:
	default:
		return os.ErrInvalid
	}
	return nil
}

func (c RouteConfig) normalized(kind uint16) RouteConfig {
	if c.Table == 0 {
		c.Table = MainTable
	}
	if kind == unix.RTM_NEWROUTE {
		if c.Protocol == 0 {
			c.Protocol = BootProtocol
		}
		if c.Type == 0 {
			c.Type = Unicast
		}
	}
	return c
}
