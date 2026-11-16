//go:build linux

package route

import (
	"encoding/binary"
	"math"
	"net/netip"
	"os"
	"strings"

	"github.com/alexfalkowski/go-linux/netlink"
	"golang.org/x/sys/unix"
)

// Family selects an IP address family. Only IPv4 and IPv6 are accepted inputs;
// zero is invalid, so a dump never silently mixes other routing families.
type Family uint8

// IPv4 selects AF_INET.
const IPv4 Family = unix.AF_INET

// IPv6 selects AF_INET6.
const IPv6 Family = unix.AF_INET6

// Scope preserves a Linux RT_SCOPE_* value. Zero is global scope.
type Scope uint8

// Universe is global scope (RT_SCOPE_UNIVERSE).
const Universe Scope = unix.RT_SCOPE_UNIVERSE

// LinkScope limits an address or route to its link (RT_SCOPE_LINK).
const LinkScope Scope = unix.RT_SCOPE_LINK

// HostScope limits an address or route to the local host (RT_SCOPE_HOST).
const HostScope Scope = unix.RT_SCOPE_HOST

// Table is a Linux routing table identifier, including extended 32-bit IDs.
type Table uint32

// MainTable is the conventional main routing table (RT_TABLE_MAIN).
const MainTable Table = unix.RT_TABLE_MAIN

// LocalTable contains local and broadcast routes (RT_TABLE_LOCAL).
const LocalTable Table = unix.RT_TABLE_LOCAL

// Protocol identifies who installed a route (RTPROT_*), not an IP protocol.
type Protocol uint8

// KernelProtocol identifies kernel-installed routes (RTPROT_KERNEL).
const KernelProtocol Protocol = unix.RTPROT_KERNEL

// BootProtocol is the default for routes installed without a specified protocol.
const BootProtocol Protocol = unix.RTPROT_BOOT

// StaticProtocol identifies administratively configured routes (RTPROT_STATIC).
const StaticProtocol Protocol = unix.RTPROT_STATIC

// RouteType preserves a Linux RTN_* route type.
type RouteType uint8

// Unicast forwards packets toward a destination (RTN_UNICAST).
const Unicast RouteType = unix.RTN_UNICAST

// Local delivers packets locally (RTN_LOCAL).
const Local RouteType = unix.RTN_LOCAL

// Blackhole silently discards packets (RTN_BLACKHOLE).
const Blackhole RouteType = unix.RTN_BLACKHOLE

// Unreachable rejects packets as unreachable (RTN_UNREACHABLE).
const Unreachable RouteType = unix.RTN_UNREACHABLE

// Prohibit rejects packets administratively (RTN_PROHIBIT).
const Prohibit RouteType = unix.RTN_PROHIBIT

// RouteFlags preserves all received rtm_flags bits, including unknown bits.
// Route mutations do not accept these observation and lookup flags.
type RouteFlags uint32

func (f Family) valid() bool {
	return f == IPv4 || f == IPv6
}

func validIndex(index int) bool {
	return index > 0 && index <= math.MaxInt32
}

func validName(name string) bool {
	return name != "" && len(name) < unix.IFNAMSIZ && name != "." && name != ".." && !strings.ContainsAny(name, "\x00/ :\t\n\r\v\f")
}

func ipFamily(address netip.Addr) Family {
	if address.Is4() {
		return IPv4
	}
	return IPv6
}

func validIP(address netip.Addr, family Family) bool {
	return address.IsValid() && address.Zone() == "" && ipFamily(address) == family
}

func validOptionalIP(address netip.Addr, family Family) bool {
	return !address.IsValid() || validIP(address, family)
}

func parseIP(data []byte, family Family) (netip.Addr, error) {
	address, ok := netip.AddrFromSlice(data)
	if !ok || !family.valid() || ipFamily(address) != family {
		return netip.Addr{}, netlink.ErrMalformed
	}
	return address, nil
}

func parsePrefix(address netip.Addr, bits uint8) (netip.Prefix, error) {
	prefix := netip.PrefixFrom(address, int(bits))
	if !prefix.IsValid() {
		return netip.Prefix{}, netlink.ErrMalformed
	}
	return prefix, nil
}

func scalar(kind netlink.AttributeType, value uint32) netlink.Attribute {
	return netlink.Attribute{Type: kind, Data: binary.NativeEndian.AppendUint32(nil, value)}
}

// nativeAttributes enforces singleton/native-order policy only on fields that
// this package interprets. Unknown IDs and their flags remain untouched.
func nativeAttributes(data []byte, known ...uint16) ([]netlink.Attribute, error) {
	attributes, err := netlink.UnmarshalAttributes(data)
	if err != nil {
		return nil, err
	}

	seen := make(map[uint16]bool)
	for _, attribute := range attributes {
		for _, id := range known {
			if attribute.ID() != id {
				continue
			}
			if seen[id] || attribute.Type&netlink.NetworkByteOrder != 0 {
				return nil, netlink.ErrMalformed
			}
			seen[id] = true
		}
	}
	return attributes, nil
}

func appendAttributes(header []byte, attributes []netlink.Attribute) ([]byte, error) {
	data, err := netlink.MarshalAttributes(attributes)
	if err != nil {
		return nil, err
	}
	if len(data) > math.MaxInt-len(header) {
		return nil, os.ErrInvalid
	}
	return append(header, data...), nil
}

// parseMessages returns a complete decoded batch or nil on the first error.
func parseMessages[T any](messages []netlink.Message, parse func(netlink.Message) (T, error)) ([]T, error) {
	records := make([]T, 0, len(messages))
	for _, message := range messages {
		record, err := parse(message)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}
