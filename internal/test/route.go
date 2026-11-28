//go:build linux

package test

import (
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/netlink"
	"github.com/alexfalkowski/go-linux/netlink/route"
	"github.com/stretchr/testify/require"
)

// Route opens a typed routing connection with cleanup and bounded request I/O.
func Route(t *testing.T) *route.Conn {
	t.Helper()
	conn, err := route.Open(netlink.Options{Validation: netlink.StrictValidation})
	require.NoError(t, err)
	Cleanup(t, conn)
	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
	return conn
}

// RouteListener joins selected typed groups with cleanup and bounded reading.
func RouteListener(t *testing.T, groups ...route.Group) *route.Listener {
	t.Helper()
	listener, err := route.Listen(netlink.Options{}, groups...)
	require.NoError(t, err)
	Cleanup(t, listener)
	require.NoError(t, listener.SetReadDeadline(time.Now().Add(5*time.Second)))
	return listener
}

// RouteEvent consumes notifications until the scenario's predicate matches.
// Unmatched events and the rest of the matching datagram are discarded; the
// listener's deadline bounds the wait. Use before triggering the next mutation.
func RouteEvent(t *testing.T, listener *route.Listener, matches func(route.Event) bool) route.Event {
	t.Helper()
	for {
		events, err := listener.ReadEvents()
		require.NoError(t, err)
		for _, event := range events {
			if matches(event) {
				return event
			}
		}
	}
}

// RouteLink observes an existing named interface using typed route listing.
func RouteLink(t *testing.T, conn *route.Conn, name string) route.Link {
	t.Helper()
	links, err := conn.Links()
	require.NoError(t, err)
	for _, link := range links {
		if link.Name == name {
			return link
		}
	}
	require.FailNow(t, "interface missing", name)
	return route.Link{}
}

// RouteAddress observes an address identified by its interface and prefix.
// Other configuration fields remain for the scenario's behavioral assertions.
func RouteAddress(t *testing.T, conn *route.Conn, family route.Family, config route.AddressConfig) route.Address {
	t.Helper()
	addresses, err := conn.Addresses(family)
	require.NoError(t, err)
	for _, address := range addresses {
		if address.Index == config.Index && address.Prefix == config.Prefix {
			return address
		}
	}
	require.FailNow(t, "address missing", config.Prefix)
	return route.Address{}
}
