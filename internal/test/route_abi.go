//go:build linux

package test

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"testing"

	"github.com/alexfalkowski/go-linux/netlink"
	"github.com/alexfalkowski/go-linux/netlink/route"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// RouteEthernetHardwareType is the Linux ARPHRD_ETHER value for ABI assertions.
const RouteEthernetHardwareType uint16 = unix.ARPHRD_ETHER

// RouteSecondaryAddressFlag is the legacy fixed-header IFA_F_SECONDARY bit.
const RouteSecondaryAddressFlag route.AddressFlags = unix.IFA_F_SECONDARY

// RouteLinkRecord encodes a link with future flags/kind and duplicate unknown attributes.
func RouteLinkRecord(tb testing.TB) (netlink.Message, []netlink.Attribute) {
	tb.Helper()

	header := make([]byte, unix.SizeofIfInfomsg)
	header[0] = unix.AF_UNSPEC
	binary.NativeEndian.PutUint16(header[2:], unix.ARPHRD_ETHER)
	binary.NativeEndian.PutUint32(header[4:], 42)
	binary.NativeEndian.PutUint32(header[8:], unix.IFF_UP|1<<31)
	binary.NativeEndian.PutUint32(header[12:], unix.IFF_UP)
	info, err := netlink.MarshalAttributes([]netlink.Attribute{{Type: unix.IFLA_INFO_KIND, Data: []byte("future\x00")}})
	require.NoError(tb, err)
	attributes := []netlink.Attribute{
		{Type: unix.IFLA_IFNAME, Data: []byte("fixture0\x00")},
		{Type: unix.IFLA_MTU, Data: binary.NativeEndian.AppendUint32(nil, 1400)},
		{Type: unix.IFLA_ADDRESS, Data: []byte{1, 2, 3, 4, 5, 6}},
		{Type: unix.IFLA_LINKINFO, Data: info},
		{Type: 300 | netlink.NetworkByteOrder, Data: []byte{9}},
		{Type: 300, Data: []byte{8}},
	}
	message := routeMessage(tb, unix.RTM_NEWLINK, header, attributes...)

	return message, attributes
}

// RouteAddressRecord encodes local/peer distinction and overriding extended flags.
func RouteAddressRecord(tb testing.TB) (netlink.Message, []netlink.Attribute) {
	tb.Helper()

	header := []byte{unix.AF_INET, 24, unix.IFA_F_SECONDARY, unix.RT_SCOPE_LINK, 42, 0, 0, 0}
	binary.NativeEndian.PutUint32(header[4:], 42)
	attributes := []netlink.Attribute{
		{Type: unix.IFA_ADDRESS, Data: []byte{192, 0, 2, 2}},
		{Type: unix.IFA_LOCAL, Data: []byte{192, 0, 2, 1}},
		{Type: unix.IFA_BROADCAST, Data: []byte{192, 0, 2, 255}},
		{Type: unix.IFA_LABEL, Data: []byte("fixture0:one\x00")},
		{Type: unix.IFA_FLAGS, Data: binary.NativeEndian.AppendUint32(nil, unix.IFA_F_NOPREFIXROUTE|1<<31)},
		{Type: unix.IFA_RT_PRIORITY, Data: binary.NativeEndian.AppendUint32(nil, 42)},
		{Type: 300, Data: []byte{9}},
	}
	message := routeMessage(tb, unix.RTM_NEWADDR, header, attributes...)

	return message, attributes
}

// RouteRecord encodes selectors, a gateway, and a 32-bit table override.
func RouteRecord(tb testing.TB) (netlink.Message, []netlink.Attribute) {
	tb.Helper()

	header := []byte{unix.AF_INET, 24, 16, 8, unix.RT_TABLE_MAIN, unix.RTPROT_STATIC, unix.RT_SCOPE_LINK, unix.RTN_UNICAST, 0, 0, 0, 0}
	binary.NativeEndian.PutUint32(header[8:], 1<<31)
	attributes := []netlink.Attribute{
		{Type: unix.RTA_DST, Data: []byte{198, 51, 100, 0}},
		{Type: unix.RTA_SRC, Data: []byte{192, 0, 0, 0}},
		{Type: unix.RTA_GATEWAY, Data: []byte{192, 0, 2, 254}},
		{Type: unix.RTA_PREFSRC, Data: []byte{192, 0, 2, 1}},
		{Type: unix.RTA_TABLE, Data: binary.NativeEndian.AppendUint32(nil, 1001)},
		{Type: unix.RTA_PRIORITY, Data: binary.NativeEndian.AppendUint32(nil, 42)},
		{Type: unix.RTA_OIF, Data: binary.NativeEndian.AppendUint32(nil, 12)},
		{Type: unix.RTA_IIF, Data: binary.NativeEndian.AppendUint32(nil, 13)},
		{Type: unix.RTA_MULTIPATH, Data: []byte{9}},
		{Type: 300 | netlink.Nested, Data: []byte{8}},
	}
	message := routeMessage(tb, unix.RTM_NEWROUTE, header, attributes...)

	return message, attributes
}

// RouteFixture names one malformed Linux route message and owns its bytes.
type RouteFixture struct {
	// Name describes the violated wire contract.
	Name string
	// Message is the malformed record passed to the public decoder.
	Message netlink.Message
}

// MalformedRouteRecords supplies invalid fixed headers and interpreted attributes.
func MalformedRouteRecords(tb testing.TB) []RouteFixture {
	tb.Helper()
	return append(malformedRouteAttributes(tb), malformedRouteHeaders(tb)...)
}

// RouteIPv6AddressRecord encodes a delete notification with only IFA_ADDRESS.
func RouteIPv6AddressRecord(tb testing.TB) netlink.Message {
	tb.Helper()
	header := []byte{unix.AF_INET6, 64, unix.IFA_F_SECONDARY, 0, 0, 0, 0, 0}
	binary.NativeEndian.PutUint32(header[4:], 42)
	return routeMessage(tb, unix.RTM_DELADDR, header, netlink.Attribute{Type: unix.IFA_ADDRESS, Data: netip.MustParseAddr("2001:db8::1").AsSlice()})
}

// RouteDefaultRecord encodes a delete notification without destination attributes.
func RouteDefaultRecord(tb testing.TB, family route.Family) netlink.Message {
	tb.Helper()
	header := make([]byte, unix.SizeofRtMsg)
	header[0], header[4] = byte(family), unix.RT_TABLE_LOCAL
	return routeMessage(tb, unix.RTM_DELROUTE, header)
}

// routeMessage assembles a fixed Linux header and independently selected
// attributes for focused route decoder ABI tests, without production hooks.
func routeMessage(tb testing.TB, kind uint16, header []byte, attributes ...netlink.Attribute) netlink.Message {
	tb.Helper()
	data, err := netlink.MarshalAttributes(attributes)
	require.NoError(tb, err)
	return netlink.Message{Header: netlink.Header{Type: kind}, Data: append(header, data...)}
}

func malformedRouteAttributes(tb testing.TB) []RouteFixture {
	tb.Helper()

	link := make([]byte, unix.SizeofIfInfomsg)
	binary.NativeEndian.PutUint32(link[4:], 42)
	address := []byte{unix.AF_INET, 24, 0, 0, 0, 0, 0, 0}
	binary.NativeEndian.PutUint32(address[4:], 42)
	ip := netlink.Attribute{Type: unix.IFA_LOCAL, Data: []byte{192, 0, 2, 1}}
	routeHeader := make([]byte, unix.SizeofRtMsg)
	routeHeader[0] = unix.AF_INET
	fixtures := make([]RouteFixture, 0, 20)
	for _, tc := range []struct {
		name       string
		header     []byte
		attributes []netlink.Attribute
		kind       uint16
	}{
		{name: "link scalar width", kind: unix.RTM_NEWLINK, header: link, attributes: []netlink.Attribute{{Type: unix.IFLA_MTU, Data: []byte{1}}}},
		{name: "link duplicate name", kind: unix.RTM_NEWLINK, header: link, attributes: []netlink.Attribute{
			{Type: unix.IFLA_IFNAME, Data: []byte("lo\x00")}, {Type: unix.IFLA_IFNAME, Data: []byte("lo\x00")},
		}},
		{name: "link unterminated name", kind: unix.RTM_NEWLINK, header: link, attributes: []netlink.Attribute{{Type: unix.IFLA_IFNAME, Data: []byte("lo")}}},
		{name: "link invalid name", kind: unix.RTM_NEWLINK, header: link, attributes: []netlink.Attribute{{Type: unix.IFLA_IFNAME, Data: []byte("bad/name\x00")}}},
		{name: "link bad nest", kind: unix.RTM_NEWLINK, header: link, attributes: []netlink.Attribute{{Type: unix.IFLA_LINKINFO, Data: []byte{1}}}},
		{name: "link wrong endian", kind: unix.RTM_NEWLINK, header: link, attributes: []netlink.Attribute{{Type: unix.IFLA_MTU | netlink.NetworkByteOrder, Data: make([]byte, 4)}}},
		{name: "address missing IP", kind: unix.RTM_NEWADDR, header: address},
		{name: "address duplicate IP", kind: unix.RTM_NEWADDR, header: address, attributes: []netlink.Attribute{ip, ip}},
		{name: "address wrong IP width", kind: unix.RTM_NEWADDR, header: address, attributes: []netlink.Attribute{{Type: unix.IFA_LOCAL, Data: []byte{1}}}},
		{name: "address wrong family width", kind: unix.RTM_NEWADDR, header: address, attributes: []netlink.Attribute{{Type: unix.IFA_LOCAL, Data: make([]byte, 16)}}},
		{name: "address bad flags", kind: unix.RTM_NEWADDR, header: address, attributes: []netlink.Attribute{ip, {Type: unix.IFA_FLAGS, Data: []byte{1}}}},
		{name: "address bad label", kind: unix.RTM_NEWADDR, header: address, attributes: []netlink.Attribute{ip, {Type: unix.IFA_LABEL, Data: []byte("bad")}}},
		{name: "address bad priority", kind: unix.RTM_NEWADDR, header: address, attributes: []netlink.Attribute{ip, {Type: unix.IFA_RT_PRIORITY, Data: []byte{1}}}},
		{name: "route bad table", kind: unix.RTM_NEWROUTE, header: routeHeader, attributes: []netlink.Attribute{{Type: unix.RTA_TABLE, Data: []byte{1}}}},
		{name: "route bad priority", kind: unix.RTM_NEWROUTE, header: routeHeader, attributes: []netlink.Attribute{{Type: unix.RTA_PRIORITY, Data: []byte{1}}}},
		{name: "route bad gateway", kind: unix.RTM_NEWROUTE, header: routeHeader, attributes: []netlink.Attribute{{Type: unix.RTA_GATEWAY, Data: make([]byte, 16)}}},
		{name: "route bad source", kind: unix.RTM_NEWROUTE, header: routeHeader, attributes: []netlink.Attribute{{Type: unix.RTA_PREFSRC, Data: []byte{1}}}},
		{name: "route invalid index", kind: unix.RTM_NEWROUTE, header: routeHeader, attributes: []netlink.Attribute{
			{Type: unix.RTA_OIF, Data: binary.NativeEndian.AppendUint32(nil, 1<<31)},
		}},
		{name: "route bad index width", kind: unix.RTM_NEWROUTE, header: routeHeader, attributes: []netlink.Attribute{{Type: unix.RTA_IIF, Data: []byte{1}}}},
		{name: "route duplicate destination", kind: unix.RTM_NEWROUTE, header: routeHeader, attributes: []netlink.Attribute{
			{Type: unix.RTA_DST, Data: make([]byte, 4)}, {Type: unix.RTA_DST, Data: make([]byte, 4)},
		}},
	} {
		fixtures = append(fixtures, RouteFixture{Name: tc.name, Message: routeMessage(tb, tc.kind, tc.header, tc.attributes...)})
	}

	return fixtures
}

func malformedRouteHeaders(tb testing.TB) []RouteFixture {
	tb.Helper()
	ip := netlink.Attribute{Type: unix.IFA_LOCAL, Data: []byte{192, 0, 2, 1}}
	var fixtures []RouteFixture

	for _, tc := range []struct {
		kind uint16
		size int
	}{
		{kind: unix.RTM_NEWLINK, size: unix.SizeofIfInfomsg},
		{kind: unix.RTM_NEWADDR, size: unix.SizeofIfAddrmsg},
		{kind: unix.RTM_NEWROUTE, size: unix.SizeofRtMsg},
	} {
		for size := range tc.size {
			fixtures = append(fixtures, RouteFixture{
				Name:    fmt.Sprintf("short header type %d size %d", tc.kind, size),
				Message: netlink.Message{Header: netlink.Header{Type: tc.kind}, Data: make([]byte, size)},
			})
		}
		data := make([]byte, tc.size+1)
		data[0] = unix.AF_INET
		binary.NativeEndian.PutUint32(data[4:], 42)
		fixtures = append(fixtures, RouteFixture{
			Name:    fmt.Sprintf("trailing attribute fragment type %d", tc.kind),
			Message: netlink.Message{Header: netlink.Header{Type: tc.kind}, Data: data},
		})
	}
	for i, header := range [][]byte{
		{0, 0, 0, 0, 42, 0, 0, 0},
		{unix.AF_INET, 33, 0, 0, 42, 0, 0, 0},
		{unix.AF_INET, 24, 0, 0, 0, 0, 0, 0},
	} {
		fixtures = append(fixtures, RouteFixture{Name: fmt.Sprintf("invalid address header %d", i), Message: routeMessage(tb, unix.RTM_NEWADDR, header, ip)})
	}
	for i, header := range [][]byte{
		make([]byte, 12),
		{unix.AF_INET, 33, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{unix.AF_INET, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{unix.AF_INET6, 129, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
	} {
		fixtures = append(fixtures, RouteFixture{Name: fmt.Sprintf("invalid route header %d", i), Message: routeMessage(tb, unix.RTM_NEWROUTE, header)})
	}
	fixtures = append(fixtures, RouteFixture{Name: "zero link index", Message: routeMessage(tb, unix.RTM_NEWLINK, make([]byte, unix.SizeofIfInfomsg))})
	return fixtures
}
