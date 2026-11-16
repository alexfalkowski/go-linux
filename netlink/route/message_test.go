//go:build linux

package route_test

import (
	"net"
	"net/netip"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/netlink"
	"github.com/alexfalkowski/go-linux/netlink/route"
	"github.com/stretchr/testify/require"
)

func TestLinkDecoderPreservesLinuxFieldsAndUnknownAttributes(t *testing.T) {
	message, attributes := test.RouteLinkRecord(t)

	link, err := route.ParseLink(message)
	require.NoError(t, err)
	require.Equal(t, route.Link{
		Name: "fixture0", Kind: "future", Index: 42, HardwareType: test.RouteEthernetHardwareType,
		MTU: 1400, Flags: route.Up | 1<<31, Change: route.Up, HardwareAddr: net.HardwareAddr{1, 2, 3, 4, 5, 6}, Attributes: attributes,
	}, link)
	event, err := route.ParseEvent(message)
	require.NoError(t, err)
	require.Equal(t, route.Event{Type: route.NewLink, Link: &link}, event)
	message.Header.Type = uint16(route.DelLink)
	event, err = route.ParseEvent(message)
	require.NoError(t, err)
	require.Equal(t, route.DelLink, event.Type)
}

func TestAddressDecoderPeerFlagsAndFallback(t *testing.T) {
	message, attributes := test.RouteAddressRecord(t)

	address, err := route.ParseAddress(message)
	require.NoError(t, err)
	require.Equal(t, route.Address{
		Index: 42, Prefix: netip.MustParsePrefix("192.0.2.1/24"), Peer: netip.MustParseAddr("192.0.2.2"),
		Broadcast: netip.MustParseAddr("192.0.2.255"), Label: "fixture0:one", Scope: route.LinkScope,
		Flags: route.NoPrefixRoute | 1<<31, Priority: 42, Attributes: attributes,
	}, address)
	event, err := route.ParseEvent(message)
	require.NoError(t, err)
	require.Equal(t, route.Event{Type: route.NewAddress, Address: &address}, event)

	message = test.RouteIPv6AddressRecord(t)
	event, err = route.ParseEvent(message)
	require.NoError(t, err)
	require.Equal(t, route.DelAddress, event.Type)
	require.Equal(t, netip.MustParsePrefix("2001:db8::1/64"), event.Address.Prefix)
	require.False(t, event.Address.Peer.IsValid(), "equal/absent peer is not synthesized")
	require.Equal(t, test.RouteSecondaryAddressFlag, event.Address.Flags, "fixed-header flags are fallback")
}

func TestRouteDecoderDefaultsSelectorsAndExtendedTable(t *testing.T) {
	message, attributes := test.RouteRecord(t)

	entry, err := route.ParseRoute(message)
	require.NoError(t, err)
	require.Equal(t, route.Route{
		Destination: netip.MustParsePrefix("198.51.100.0/24"), Source: netip.MustParsePrefix("192.0.0.0/16"),
		Gateway: netip.MustParseAddr("192.0.2.254"), PreferredSource: netip.MustParseAddr("192.0.2.1"),
		Table: 1001, Priority: 42, Index: 12, InputIndex: 13, Protocol: route.StaticProtocol, Scope: route.LinkScope,
		Type: route.Unicast, Flags: 1 << 31, TOS: 8, Attributes: attributes,
	}, entry)
	event, err := route.ParseEvent(message)
	require.NoError(t, err)
	require.Equal(t, route.Event{Type: route.NewRoute, Route: &entry}, event)

	for _, family := range []route.Family{route.IPv4, route.IPv6} {
		event, err := route.ParseEvent(test.RouteDefaultRecord(t, family))
		require.NoError(t, err)
		require.Equal(t, route.DelRoute, event.Type)
		require.Equal(t, 0, event.Route.Destination.Bits(), "absent destination is explicit default route")
		require.True(t, event.Route.Destination.Addr().IsUnspecified(), "default route family address")
		require.Equal(t, family == route.IPv4, event.Route.Destination.Addr().Is4(), "default route preserves family")
		require.Equal(t, route.LocalTable, event.Route.Table)
	}
}

func TestDecodersRejectMalformedRecords(t *testing.T) {
	for _, fixture := range test.MalformedRouteRecords(t) {
		t.Run(fixture.Name, func(t *testing.T) {
			event, err := route.ParseEvent(fixture.Message)
			require.ErrorIs(t, err, netlink.ErrMalformed)
			require.Equal(t, route.Event{}, event)
		})
	}
	_, err := route.ParseEvent(netlink.Message{Header: netlink.Header{Type: netlink.Noop}})
	require.ErrorIs(t, err, netlink.ErrUnexpectedReply)
}
