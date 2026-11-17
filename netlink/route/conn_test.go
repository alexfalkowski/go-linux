//go:build linux

package route_test

import (
	"net"
	"net/netip"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/netlink"
	"github.com/alexfalkowski/go-linux/netlink/route"
	"github.com/stretchr/testify/require"
)

func TestLiveLinkListIncludesLoopback(t *testing.T) {
	conn := test.Route(t)
	loopback := test.RouteLink(t, conn, "lo")

	require.Positive(t, loopback.Index)
	require.NotZero(t, loopback.Flags&route.Loopback)
	require.Positive(t, loopback.MTU)
}

func TestLiveAddressListsMatchRequestedFamily(t *testing.T) {
	conn := test.Route(t)

	for _, family := range []route.Family{route.IPv4, route.IPv6} {
		addresses, err := conn.Addresses(family)
		require.NoError(t, err)
		for _, address := range addresses {
			require.True(t, address.Prefix.IsValid(), "listed address prefix")
			require.Equal(t, family == route.IPv4, address.Prefix.Addr().Is4(), "requested address family")
			require.Positive(t, address.Index)
		}
	}
}

func TestLiveRouteListsMatchRequestedFamily(t *testing.T) {
	conn := test.Route(t)

	for _, family := range []route.Family{route.IPv4, route.IPv6} {
		routes, err := conn.Routes(family)
		require.NoError(t, err)
		for _, entry := range routes {
			require.True(t, entry.Destination.IsValid(), "listed route prefix")
			require.Equal(t, family == route.IPv4, entry.Destination.Addr().Is4(), "requested route family")
		}
	}
}

func TestKernelLinkIdentityAgreesWithStandardLibrary(t *testing.T) {
	conn := test.Route(t)
	link := test.RouteLink(t, conn, "lo")

	observed, err := net.InterfaceByName("lo")
	require.NoError(t, err)
	require.Equal(t, observed.Index, link.Index)
	require.EqualValues(t, observed.MTU, link.MTU)
	require.Equal(t, observed.Flags&net.FlagLoopback != 0, link.Flags&route.Loopback != 0, "native flags retain loopback identity")
}

func TestZeroAndClosedRequestOwners(t *testing.T) {
	var conn route.Conn
	_, err := conn.Links()
	require.ErrorIs(t, err, os.ErrInvalid)
	_, err = conn.Addresses(route.IPv4)
	require.ErrorIs(t, err, os.ErrInvalid)
	_, err = conn.Routes(route.IPv6)
	require.ErrorIs(t, err, os.ErrInvalid)
	require.ErrorIs(t, conn.AddLink("fixture", route.Dummy), os.ErrInvalid)
	require.ErrorIs(t, conn.ChangeLink(1, route.LinkChange{}), os.ErrInvalid)
	require.ErrorIs(t, conn.DeleteLink(1), os.ErrInvalid)
	require.ErrorIs(t, conn.AddAddress(route.AddressConfig{Index: 1, Prefix: netip.MustParsePrefix("192.0.2.1/24")}), os.ErrInvalid)
	require.ErrorIs(t, conn.AddRoute(route.RouteConfig{Destination: netip.MustParsePrefix("192.0.2.0/24")}), os.ErrInvalid)
	require.ErrorIs(t, conn.SetDeadline(time.Time{}), os.ErrInvalid)
	require.ErrorIs(t, conn.Close(), os.ErrInvalid)

	owned := test.Route(t)
	require.NoError(t, owned.Close())
	_, err = owned.Links()
	require.ErrorIs(t, err, os.ErrClosed)
	require.ErrorIs(t, owned.Close(), os.ErrClosed)
	require.ErrorIs(t, owned.SetDeadline(time.Time{}), os.ErrClosed)
}

func TestZeroRouteListenerRejectsOperations(t *testing.T) {
	var listener route.Listener
	_, err := listener.ReadEvents()
	require.ErrorIs(t, err, os.ErrInvalid)
	require.ErrorIs(t, listener.SetReadDeadline(time.Time{}), os.ErrInvalid)
	require.ErrorIs(t, listener.Close(), os.ErrInvalid)
}

func TestInvalidInputsPrecedeClosedConnectionErrors(t *testing.T) {
	conn := test.Route(t)
	require.NoError(t, conn.Close(), "invalid mutations must never reach the host network")

	for _, name := range []string{"", ".", "..", "bad/name", "bad name", "bad:name", "bad\x00name", "0123456789abcdef"} {
		require.ErrorIs(t, conn.AddLink(name, route.Dummy), os.ErrInvalid, "name %q", name)
	}
	require.ErrorIs(t, conn.AddLink("fixture", "veth"), os.ErrInvalid)
	for _, index := range []int{-1, 0, 1 << 31} {
		require.ErrorIs(t, conn.DeleteLink(index), os.ErrInvalid)
		require.ErrorIs(t, conn.ChangeLink(index, route.LinkChange{}), os.ErrInvalid)
	}
	for _, change := range []route.LinkChange{
		{Name: "bad/name"}, {Flags: route.Up}, {Change: route.Loopback}, {Change: 1 << 31},
	} {
		require.ErrorIs(t, conn.ChangeLink(1, change), os.ErrInvalid)
	}
	for _, family := range []route.Family{0, 255} {
		addresses, err := conn.Addresses(family)
		require.ErrorIs(t, err, os.ErrInvalid)
		require.Nil(t, addresses)
		routes, err := conn.Routes(family)
		require.ErrorIs(t, err, os.ErrInvalid)
		require.Nil(t, routes)
	}

	_, err := conn.Links()
	require.ErrorIs(t, err, os.ErrClosed)
}

func TestInvalidAddressConfigurations(t *testing.T) {
	conn := test.Route(t)
	require.NoError(t, conn.Close(), "validate before I/O even if a regression would make the request valid to Linux")

	for _, config := range []route.AddressConfig{
		{},
		{Index: 1},
		{Prefix: netip.MustParsePrefix("192.0.2.1/24")},
		{Index: -1, Prefix: netip.MustParsePrefix("192.0.2.1/24")},
		{Index: 1, Prefix: netip.MustParsePrefix("192.0.2.1/24"), Peer: netip.IPv6Loopback()},
		{Index: 1, Prefix: netip.MustParsePrefix("192.0.2.1/24"), Flags: route.Tentative},
		{Index: 1, Prefix: netip.MustParsePrefix("192.0.2.1/24"), Broadcast: netip.IPv6Loopback()},
		{Index: 1, Prefix: netip.MustParsePrefix("192.0.2.1/24"), Label: "bad\x00label"},
		{Index: 1, Prefix: netip.MustParsePrefix("192.0.2.1/24"), Label: "0123456789abcdef"},
		{Index: 1, Prefix: netip.MustParsePrefix("2001:db8::1/64"), Label: "fixture"},
		{Index: 1, Prefix: netip.MustParsePrefix("2001:db8::1/64"), Broadcast: netip.IPv6Loopback()},
		{Index: 1, Prefix: netip.MustParsePrefix("2001:db8::1/64"), Peer: netip.MustParseAddr("fe80::1%lo")},
	} {
		require.ErrorIs(t, conn.AddAddress(config), os.ErrInvalid, "config %+v", config)
		require.ErrorIs(t, conn.ChangeAddress(config), os.ErrInvalid)
		require.ErrorIs(t, conn.DeleteAddress(config), os.ErrInvalid)
	}
}

func TestInvalidRouteConfigurations(t *testing.T) {
	conn := test.Route(t)
	require.NoError(t, conn.Close(), "invalid mutation tests must not be able to change host routes")

	for _, config := range []route.RouteConfig{
		{},
		{Destination: netip.MustParsePrefix("192.0.2.1/24")},
		{Destination: netip.MustParsePrefix("192.0.2.0/24"), Index: -1},
		{Destination: netip.MustParsePrefix("192.0.2.0/24"), Index: 1 << 31},
		{Destination: netip.MustParsePrefix("192.0.2.0/24"), Gateway: netip.IPv6Loopback()},
		{Destination: netip.MustParsePrefix("192.0.2.0/24"), PreferredSource: netip.IPv6Loopback()},
		{Destination: netip.MustParsePrefix("192.0.2.0/24"), Type: 255},
		{Destination: netip.MustParsePrefix("2001:db8::/64"), Gateway: netip.MustParseAddr("fe80::1%lo")},
	} {
		require.ErrorIs(t, conn.AddRoute(config), os.ErrInvalid, "config %+v", config)
		require.ErrorIs(t, conn.ChangeRoute(config), os.ErrInvalid)
		require.ErrorIs(t, conn.DeleteRoute(config), os.ErrInvalid)
	}
}

func TestCreationFailuresDoNotLeak(t *testing.T) {
	before := test.Descriptors(t)
	conn, err := route.Open(netlink.Options{DatagramSize: 1})
	require.ErrorIs(t, err, os.ErrInvalid)
	require.Nil(t, conn)
	for _, groups := range [][]route.Group{nil, {0}, {route.LinkGroup, 999}} {
		listener, err := route.Listen(netlink.Options{}, groups...)
		require.ErrorIs(t, err, os.ErrInvalid)
		require.Nil(t, listener)
	}
	listener, err := route.Listen(netlink.Options{DatagramSize: 1}, route.LinkGroup)
	require.ErrorIs(t, err, os.ErrInvalid)
	require.Nil(t, listener)
	require.Equal(t, before, test.Descriptors(t))
}

func TestConcurrentListsShareConnectionSafely(t *testing.T) {
	conn := test.Route(t)
	var workers sync.WaitGroup
	results := make(chan error, 8)
	for range cap(results) {
		workers.Go(func() {
			_, err := conn.Links()
			results <- err
		})
	}
	workers.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
}

func TestExpiredDeadlineCanBeClearedBeforeNextList(t *testing.T) {
	conn := test.Route(t)

	require.NoError(t, conn.SetDeadline(time.Now().Add(-time.Second)))
	_, err := conn.Links()
	require.ErrorIs(t, err, os.ErrDeadlineExceeded)
	require.NoError(t, conn.SetDeadline(time.Time{}))
	_, err = conn.Links()
	require.NoError(t, err, "expired send leaves the connection usable")
}

func TestListenerExpiredDeadlineReturnsNoEvents(t *testing.T) {
	listener := test.RouteListener(t, route.LinkGroup)
	require.NoError(t, listener.SetReadDeadline(time.Now().Add(-time.Second)))
	events, err := listener.ReadEvents()
	require.ErrorIs(t, err, os.ErrDeadlineExceeded)
	require.Nil(t, events)
}

func TestListenerCloseInterruptsPendingEventRead(t *testing.T) {
	listener := test.RouteListener(t, route.LinkGroup)

	require.NoError(t, listener.SetReadDeadline(time.Time{}))
	pending := test.StartRead(t, listener.ReadEvents)
	require.NoError(t, listener.Close())
	result := test.Receive(t, pending)
	require.ErrorIs(t, result.Err, os.ErrClosed)
	require.Nil(t, result.Value)
	require.ErrorIs(t, listener.Close(), os.ErrClosed)
	require.ErrorIs(t, listener.SetReadDeadline(time.Time{}), os.ErrClosed)
}

func TestIncompleteDumpsDiscardPartialListsAndClose(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options netlink.Options
		want    error
	}{
		{name: "truncated datagram", options: netlink.Options{DatagramSize: 16}, want: netlink.ErrTruncated},
		{name: "response limit", options: netlink.Options{MaxResponseBytes: 16}, want: netlink.ErrLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := route.Open(tc.options)
			require.NoError(t, err)
			test.Cleanup(t, conn)
			require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))

			links, err := conn.Links()
			require.ErrorIs(t, err, tc.want)
			require.Nil(t, links)
			_, err = conn.Links()
			require.ErrorIs(t, err, os.ErrClosed)
		})
	}
}
