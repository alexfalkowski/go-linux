//go:build linux

package route_test

import (
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/netlink"
	"github.com/alexfalkowski/go-linux/netlink/route"
	"github.com/stretchr/testify/require"
)

func TestLinkMutationsAndNotifications(t *testing.T) {
	if !test.NetworkNamespace(t) {
		return
	}
	conn := test.Route(t)
	listener := test.RouteListener(t, route.LinkGroup)

	require.NoError(t, conn.AddLink("fixture0", route.Dummy))
	link := test.RouteLink(t, conn, "fixture0")
	require.Equal(t, "dummy", link.Kind)
	event := test.RouteEvent(t, listener, func(event route.Event) bool {
		return event.Type == route.NewLink && event.Link.Index == link.Index
	})
	require.Equal(t, "fixture0", event.Link.Name)
	require.Nil(t, event.Address)
	require.Nil(t, event.Route)
	require.ErrorIs(t, conn.AddLink("fixture0", route.Dummy), test.ErrExists)

	require.NoError(t, conn.ChangeLink(link.Index, route.LinkChange{Name: "fixture1", MTU: 1400}))
	changed := test.RouteLink(t, conn, "fixture1")
	require.Equal(t, link.Index, changed.Index)
	require.Equal(t, uint32(1400), changed.MTU)
	event = test.RouteEvent(t, listener, func(event route.Event) bool {
		return event.Type == route.NewLink && event.Link.Index == link.Index && event.Link.Name == "fixture1" && event.Link.MTU == 1400
	})
	require.Equal(t, changed.Name, event.Link.Name)

	require.NoError(t, conn.DeleteLink(link.Index))
	require.ErrorIs(t, conn.DeleteLink(link.Index), test.ErrNoDevice)
	links, err := conn.Links()
	require.NoError(t, err)
	for _, remaining := range links {
		require.NotEqual(t, link.Index, remaining.Index)
	}
	event = test.RouteEvent(t, listener, func(event route.Event) bool {
		return event.Type == route.DelLink && event.Link.Index == link.Index
	})
	require.Equal(t, "fixture1", event.Link.Name)
}

func TestLinkFlagChangesPreserveUnselectedFlags(t *testing.T) {
	if !test.NetworkNamespace(t) {
		return
	}
	conn := test.Route(t)
	require.NoError(t, conn.AddLink("fixture0", route.Dummy))
	link := test.RouteLink(t, conn, "fixture0")

	require.NoError(t, conn.ChangeLink(link.Index, route.LinkChange{Flags: route.Up, Change: route.Up}))
	up := test.RouteLink(t, conn, "fixture0")
	require.NotZero(t, up.Flags&route.Up)
	require.Equal(t, link.Flags & ^(route.Up|route.Running|route.LowerUp), up.Flags & ^(route.Up|route.Running|route.LowerUp), "unselected flags survive")
	require.NoError(t, conn.ChangeLink(link.Index, route.LinkChange{Change: route.Up}))
	down := test.RouteLink(t, conn, "fixture0")
	require.Zero(t, down.Flags&route.Up)
}

func TestAddressLifecycleAndNotifications(t *testing.T) {
	if !test.NetworkNamespace(t) {
		return
	}
	conn := test.Route(t)
	require.NoError(t, conn.AddLink("fixture0", route.Dummy))
	link := test.RouteLink(t, conn, "fixture0")
	listener := test.RouteListener(t, route.IPv4AddressGroup, route.IPv6AddressGroup)

	for _, scenario := range []struct {
		name   string
		family route.Family
		config route.AddressConfig
	}{
		{name: "IPv4 peer broadcast label and extended flags", family: route.IPv4, config: route.AddressConfig{
			Index: link.Index, Prefix: netip.MustParsePrefix("192.0.2.1/24"), Peer: netip.MustParseAddr("192.0.2.2"),
			Broadcast: netip.MustParseAddr("192.0.2.255"), Label: "fixture0:one", Flags: route.NoPrefixRoute, Scope: route.LinkScope,
		}},
		{name: "IPv6 no DAD", family: route.IPv6, config: route.AddressConfig{
			Index: link.Index, Prefix: netip.MustParsePrefix("2001:db8::1/64"), Flags: route.NoDAD | route.NoPrefixRoute,
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			require.NoError(t, conn.AddAddress(scenario.config))
			require.ErrorIs(t, conn.AddAddress(scenario.config), test.ErrExists)

			found := test.RouteAddress(t, conn, scenario.family, scenario.config)
			require.Equal(t, scenario.config.Peer, found.Peer)
			require.Equal(t, scenario.config.Broadcast, found.Broadcast)
			require.Equal(t, scenario.config.Label, found.Label)
			require.Equal(t, scenario.config.Flags, found.Flags&scenario.config.Flags, "requested extended flags survive")
			require.Equal(t, scenario.config.Scope, found.Scope)
			event := test.RouteEvent(t, listener, func(event route.Event) bool {
				return event.Type == route.NewAddress && event.Address.Prefix == scenario.config.Prefix
			})
			require.Equal(t, link.Index, event.Address.Index)

			require.NoError(t, conn.DeleteAddress(scenario.config))
			require.ErrorIs(t, conn.DeleteAddress(scenario.config), test.ErrAddressNotAvailable)

			addresses, err := conn.Addresses(scenario.family)
			require.NoError(t, err)
			for _, address := range addresses {
				require.NotEqual(t, scenario.config.Prefix, address.Prefix, "deleted addresses leave the dump")
			}
			event = test.RouteEvent(t, listener, func(event route.Event) bool {
				return event.Type == route.DelAddress && event.Address.Prefix == scenario.config.Prefix
			})
			require.Equal(t, link.Index, event.Address.Index)
		})
	}
}

func TestAddressReplacementChangesPriorityAndFlags(t *testing.T) {
	if !test.NetworkNamespace(t) {
		return
	}
	conn := test.Route(t)
	require.NoError(t, conn.AddLink("fixture0", route.Dummy))
	link := test.RouteLink(t, conn, "fixture0")

	for _, scenario := range []struct {
		name        string
		family      route.Family
		config      route.AddressConfig
		replacement route.AddressFlags
	}{
		{name: "IPv4 retains no-prefix-route", family: route.IPv4, replacement: route.NoPrefixRoute, config: route.AddressConfig{
			Index: link.Index, Prefix: netip.MustParsePrefix("192.0.2.1/24"), Peer: netip.MustParseAddr("192.0.2.2"),
			Broadcast: netip.MustParseAddr("192.0.2.255"), Label: "fixture0:one", Flags: route.NoPrefixRoute, Scope: route.LinkScope,
		}},
		{name: "IPv6 clears no-prefix-route", family: route.IPv6, replacement: route.NoDAD, config: route.AddressConfig{
			Index: link.Index, Prefix: netip.MustParsePrefix("2001:db8::1/64"), Flags: route.NoDAD | route.NoPrefixRoute,
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			require.NoError(t, conn.AddAddress(scenario.config))

			changed := scenario.config
			changed.Priority = 17
			changed.Flags = scenario.replacement
			require.NoError(t, conn.ChangeAddress(changed))

			found := test.RouteAddress(t, conn, scenario.family, changed)
			require.Equal(t, changed.Priority, found.Priority, "replacement changes prefix-route priority")
			require.Equal(t, changed.Flags&route.NoPrefixRoute, found.Flags&route.NoPrefixRoute, "replacement applies family-specific flags")
			require.NoError(t, conn.DeleteAddress(changed))
		})
	}
}

func TestAddressReplacementRecreatesDeletedAddress(t *testing.T) {
	if !test.NetworkNamespace(t) {
		return
	}
	conn := test.Route(t)
	require.NoError(t, conn.AddLink("fixture0", route.Dummy))
	link := test.RouteLink(t, conn, "fixture0")

	for _, scenario := range []struct {
		name   string
		family route.Family
		config route.AddressConfig
	}{
		{name: "IPv4", family: route.IPv4, config: route.AddressConfig{
			Index: link.Index, Prefix: netip.MustParsePrefix("192.0.2.1/24"), Peer: netip.MustParseAddr("192.0.2.2"),
			Broadcast: netip.MustParseAddr("192.0.2.255"), Label: "fixture0:one", Flags: route.NoPrefixRoute, Scope: route.LinkScope, Priority: 17,
		}},
		{name: "IPv6", family: route.IPv6, config: route.AddressConfig{
			Index: link.Index, Prefix: netip.MustParsePrefix("2001:db8::1/64"), Flags: route.NoDAD, Priority: 17,
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			config := scenario.config
			require.NoError(t, conn.AddAddress(config))
			require.NoError(t, conn.DeleteAddress(config))

			require.NoError(t, conn.ChangeAddress(config))

			found := test.RouteAddress(t, conn, scenario.family, config)
			require.Equal(t, config.Prefix, found.Prefix, "replacement recreates the deleted address")
			require.NoError(t, conn.DeleteAddress(config))
		})
	}
}

func TestAddressZeroPriorityReplacement(t *testing.T) {
	if !test.NetworkNamespace(t) {
		return
	}
	conn := test.Route(t)
	require.NoError(t, conn.AddLink("fixture0", route.Dummy))
	link := test.RouteLink(t, conn, "fixture0")

	for _, tc := range []struct {
		name     string
		prefix   netip.Prefix
		family   route.Family
		flags    route.AddressFlags
		priority uint32
	}{
		{name: "IPv4 resets", prefix: netip.MustParsePrefix("192.0.2.1/24"), family: route.IPv4},
		{name: "IPv6 preserves", prefix: netip.MustParsePrefix("2001:db8::1/64"), family: route.IPv6, flags: route.NoDAD, priority: 17},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := route.AddressConfig{Index: link.Index, Prefix: tc.prefix, Flags: tc.flags}
			require.NoError(t, conn.AddAddress(config))

			for _, step := range []struct {
				name      string
				requested uint32
				observed  uint32
			}{
				{name: "nonzero priority", requested: 17, observed: 17},
				{name: "zero priority", observed: tc.priority},
			} {
				t.Run(step.name, func(t *testing.T) {
					config.Priority = step.requested
					require.NoError(t, conn.ChangeAddress(config))

					addresses, err := conn.Addresses(tc.family)
					require.NoError(t, err)
					position := slices.IndexFunc(addresses, func(address route.Address) bool {
						return address.Index == link.Index && address.Prefix == tc.prefix
					})
					require.NotEqual(t, -1, position, "replaced address remains present")
					require.Equal(t, step.observed, addresses[position].Priority, "replacement follows family-specific kernel semantics")
				})
			}
		})
	}
}

func TestRouteMutationsAndNotifications(t *testing.T) {
	if !test.NetworkNamespace(t) {
		return
	}
	conn := test.Route(t)
	require.NoError(t, conn.AddLink("fixture0", route.Dummy))
	link := test.RouteLink(t, conn, "fixture0")
	require.NoError(t, conn.ChangeLink(link.Index, route.LinkChange{Flags: route.Up, Change: route.Up}))
	require.NoError(t, conn.AddAddress(route.AddressConfig{Index: link.Index, Prefix: netip.MustParsePrefix("192.0.2.1/24")}))
	require.NoError(t, conn.AddAddress(route.AddressConfig{Index: link.Index, Prefix: netip.MustParsePrefix("2001:db8:1::1/64"), Flags: route.NoDAD}))
	listener := test.RouteListener(t, route.IPv4RouteGroup, route.IPv6RouteGroup)

	for _, tc := range []struct {
		name   string
		family route.Family
		config route.RouteConfig
	}{
		{name: "IPv4 gateway extended table and source", family: route.IPv4, config: route.RouteConfig{
			Destination: netip.MustParsePrefix("198.51.100.0/24"), Gateway: netip.MustParseAddr("192.0.2.254"),
			PreferredSource: netip.MustParseAddr("192.0.2.1"), Index: link.Index, Table: 1001, Priority: 42, Protocol: route.StaticProtocol,
		}},
		{name: "IPv6 default gateway", family: route.IPv6, config: route.RouteConfig{
			Destination: netip.MustParsePrefix("::/0"), Gateway: netip.MustParseAddr("2001:db8:1::fe"),
			PreferredSource: netip.MustParseAddr("2001:db8:1::1"), Index: link.Index, Priority: 42,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, conn.AddRoute(tc.config))
			require.ErrorIs(t, conn.AddRoute(tc.config), test.ErrExists)
			routes, err := conn.Routes(tc.family)
			require.NoError(t, err)
			position := slices.IndexFunc(routes, func(entry route.Route) bool {
				return entry.Destination == tc.config.Destination && entry.Priority == tc.config.Priority
			})
			require.NotEqual(t, -1, position, "added route must appear in the dump")
			found := routes[position]
			require.Equal(t, tc.config.Gateway, found.Gateway)
			require.Equal(t, tc.config.PreferredSource, found.PreferredSource)
			require.Equal(t, tc.config.Index, found.Index)
			if tc.config.Table != 0 {
				require.Equal(t, tc.config.Table, found.Table)
			} else {
				require.Equal(t, route.MainTable, found.Table)
			}
			event := test.RouteEvent(t, listener, func(event route.Event) bool {
				return event.Type == route.NewRoute && event.Route.Destination == tc.config.Destination
			})
			require.Equal(t, link.Index, event.Route.Index)

			require.NoError(t, conn.DeleteRoute(tc.config))
			require.ErrorIs(t, conn.DeleteRoute(tc.config), test.ErrNoProcess)
			routes, err = conn.Routes(tc.family)
			require.NoError(t, err)
			for _, entry := range routes {
				require.NotEqual(t, tc.config.Destination, entry.Destination)
			}
			event = test.RouteEvent(t, listener, func(event route.Event) bool {
				return event.Type == route.DelRoute && event.Route.Destination == tc.config.Destination
			})
			require.Equal(t, link.Index, event.Route.Index)
		})
	}
}

func TestRouteReplacementRemovesGatewayAndPreferredSource(t *testing.T) {
	if !test.NetworkNamespace(t) {
		return
	}
	conn := test.Route(t)
	require.NoError(t, conn.AddLink("fixture0", route.Dummy))
	link := test.RouteLink(t, conn, "fixture0")
	require.NoError(t, conn.ChangeLink(link.Index, route.LinkChange{Flags: route.Up, Change: route.Up}))
	require.NoError(t, conn.AddAddress(route.AddressConfig{Index: link.Index, Prefix: netip.MustParsePrefix("192.0.2.1/24")}))
	require.NoError(t, conn.AddAddress(route.AddressConfig{Index: link.Index, Prefix: netip.MustParsePrefix("2001:db8:1::1/64"), Flags: route.NoDAD}))

	for _, tc := range []struct {
		name   string
		family route.Family
		config route.RouteConfig
	}{
		{name: "IPv4 gateway extended table and source", family: route.IPv4, config: route.RouteConfig{
			Destination: netip.MustParsePrefix("198.51.100.0/24"), Gateway: netip.MustParseAddr("192.0.2.254"),
			PreferredSource: netip.MustParseAddr("192.0.2.1"), Index: link.Index, Table: 1001, Priority: 42, Protocol: route.StaticProtocol,
		}},
		{name: "IPv6 default gateway", family: route.IPv6, config: route.RouteConfig{
			Destination: netip.MustParsePrefix("::/0"), Gateway: netip.MustParseAddr("2001:db8:1::fe"),
			PreferredSource: netip.MustParseAddr("2001:db8:1::1"), Index: link.Index, Priority: 42,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, conn.AddRoute(tc.config))

			changed := tc.config
			changed.Gateway = netip.Addr{}
			changed.PreferredSource = netip.Addr{}
			changed.Scope = route.LinkScope
			require.NoError(t, conn.ChangeRoute(changed))
			routes, err := conn.Routes(tc.family)
			require.NoError(t, err)
			matches := slices.DeleteFunc(routes, func(entry route.Route) bool {
				return entry.Destination != changed.Destination || entry.Priority != changed.Priority
			})
			require.Len(t, matches, 1, "replacement retains one route")
			require.False(t, matches[0].Gateway.IsValid(), "replacement removes the gateway")
			require.False(t, matches[0].PreferredSource.IsValid(), "replacement removes preferred source")

			require.NoError(t, conn.DeleteRoute(changed))
		})
	}
}

func TestRejectedKernelMutationKeepsConnectionUsable(t *testing.T) {
	if !test.NetworkNamespace(t) {
		return
	}
	conn := test.Route(t)

	require.ErrorIs(t, conn.ChangeLink(1<<31-1, route.LinkChange{MTU: 1400}), test.ErrNoDevice)
	require.ErrorIs(t, conn.AddAddress(route.AddressConfig{Index: 1<<31 - 1, Prefix: netip.MustParsePrefix("192.0.2.1/24")}), test.ErrNoDevice)
	require.ErrorIs(t, conn.AddRoute(route.RouteConfig{Index: 1<<31 - 1, Destination: netip.MustParsePrefix("192.0.2.0/24"), Scope: route.LinkScope}), test.ErrNoDevice)
	_, err := conn.Links()
	require.NoError(t, err)
}

func TestLocalAndRejectingRouteTypes(t *testing.T) {
	if !test.NetworkNamespace(t) {
		return
	}
	conn := test.Route(t)
	loopback := test.RouteLink(t, conn, "lo")
	require.NoError(t, conn.ChangeLink(loopback.Index, route.LinkChange{Flags: route.Up, Change: route.Up}))

	for _, destination := range []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("2001:db8:2::/64")} {
		for _, kind := range []route.RouteType{route.Local, route.Blackhole, route.Unreachable, route.Prohibit} {
			config := route.RouteConfig{Destination: destination, Type: kind, Table: 1002, Protocol: route.StaticProtocol}
			if kind == route.Local {
				config.Index, config.Scope = loopback.Index, route.HostScope
			}
			require.NoError(t, conn.AddRoute(config), "destination %s type %d", destination, kind)

			var family route.Family
			if destination.Addr().Is4() {
				family = route.IPv4
			} else {
				family = route.IPv6
			}
			entries, err := conn.Routes(family)
			require.NoError(t, err)
			position := slices.IndexFunc(entries, func(entry route.Route) bool {
				return entry.Destination == destination
			})
			require.NotEqual(t, -1, position)
			require.Equal(t, kind, entries[position].Type)
			require.Equal(t, route.Table(1002), entries[position].Table)

			require.NoError(t, conn.DeleteRoute(config))
		}
	}
}

func TestTruncatedNotificationReturnsNoBatchAndLeavesListenerOpen(t *testing.T) {
	if !test.NetworkNamespace(t) {
		return
	}
	conn := test.Route(t)
	listener, err := route.Listen(netlink.Options{DatagramSize: 16}, route.LinkGroup)
	require.NoError(t, err)
	test.Cleanup(t, listener)
	require.NoError(t, listener.SetReadDeadline(time.Now().Add(5*time.Second)))

	require.NoError(t, conn.AddLink("fixture0", route.Dummy))
	events, err := listener.ReadEvents()
	require.ErrorIs(t, err, netlink.ErrTruncated)
	require.Nil(t, events)
	require.NoError(t, listener.SetReadDeadline(time.Now().Add(-time.Second)), "truncation must leave listener open")
}
