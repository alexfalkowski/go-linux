//go:build linux

package route_test

import (
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/netlink"
	"github.com/alexfalkowski/go-linux/netlink/route"
	"github.com/stretchr/testify/require"
)

func FuzzParseEvent(f *testing.F) {
	link, _ := test.RouteLinkRecord(f)
	address, _ := test.RouteAddressRecord(f)
	entry, _ := test.RouteRecord(f)
	for _, message := range []netlink.Message{link, address, entry, test.RouteIPv6AddressRecord(f), test.RouteDefaultRecord(f, route.IPv6)} {
		f.Add(message.Header.Type, message.Data)
	}
	for _, fixture := range test.MalformedRouteRecords(f) {
		f.Add(fixture.Message.Header.Type, fixture.Message.Data)
	}

	f.Fuzz(func(t *testing.T, kind uint16, data []byte) {
		event, err := route.ParseEvent(netlink.Message{Header: netlink.Header{Type: kind}, Data: data})
		if err != nil {
			require.Equal(t, route.Event{}, event, "malformed records must not leak partial typed state")
			return
		}
		require.EqualValues(t, kind, event.Type)
		var records int
		if event.Link != nil {
			records++
			require.Positive(t, event.Link.Index)
		}
		if event.Address != nil {
			records++
			require.True(t, event.Address.Prefix.IsValid(), "decoded address prefix")
		}
		if event.Route != nil {
			records++
			require.True(t, event.Route.Destination.IsValid(), "decoded route prefix")
		}
		require.Equal(t, 1, records, "exactly one typed record per successful event")
	})
}
