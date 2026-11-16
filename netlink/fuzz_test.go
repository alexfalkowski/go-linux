//go:build linux

package netlink_test

import (
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/netlink"
	"github.com/stretchr/testify/require"
)

// FuzzUnmarshalMessages exercises arbitrary wire lengths, alignment, and batch tails.
func FuzzUnmarshalMessages(f *testing.F) {
	f.Add([]byte{})
	f.Add(test.NetlinkRecord(netlink.Header{Type: 30, Flags: 0x8002, Sequence: 123}, 0, []byte{1, 2, 3}))
	f.Add(test.NetlinkRecord(netlink.Header{}, ^uint32(0), nil))
	f.Fuzz(func(t *testing.T, data []byte) {
		messages, err := netlink.UnmarshalMessages(data)
		if err != nil {
			require.ErrorIs(t, err, netlink.ErrMalformed)
			require.Nil(t, messages)
			return
		}
		require.LessOrEqual(t, len(messages), len(data)/16, "every message needs a complete header")
		encoded, err := netlink.MarshalMessages(messages)
		require.NoError(t, err)
		decoded, err := netlink.UnmarshalMessages(encoded)
		require.NoError(t, err)
		require.Equal(t, messages, decoded, "round trips must retain all decoded fields and bytes")
	})
}

// FuzzUnmarshalAttributes exercises arbitrary nested/unknown IDs and length fields.
func FuzzUnmarshalAttributes(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{5, 0, 123, 0, 1, 0, 0, 0})
	f.Add([]byte{4, 0, 1, 128})
	f.Fuzz(func(t *testing.T, data []byte) {
		attributes, err := netlink.UnmarshalAttributes(data)
		if err != nil {
			require.ErrorIs(t, err, netlink.ErrMalformed)
			require.Nil(t, attributes)
			return
		}
		require.LessOrEqual(t, len(attributes), len(data)/4, "every attribute needs a complete header")
		encoded, err := netlink.MarshalAttributes(attributes)
		require.NoError(t, err)
		decoded, err := netlink.UnmarshalAttributes(encoded)
		require.NoError(t, err)
		require.Equal(t, attributes, decoded)
	})
}

// FuzzParseAcknowledgement exercises status/echoed-length/TLV interactions.
func FuzzParseAcknowledgement(f *testing.F) {
	f.Add(netlink.Done, uint16(netlink.DumpInterrupted), test.NetlinkStatus(0))
	f.Add(netlink.ErrorMessage, uint16(netlink.Capped), append(test.NetlinkStatus(-22), test.NetlinkRecord(netlink.Header{Type: 30}, 0, nil)...))
	f.Add(netlink.Done, uint16(0), []byte{})
	f.Fuzz(func(t *testing.T, kind, flags uint16, data []byte) {
		ack, err := netlink.ParseAcknowledgement(netlink.Message{Header: netlink.Header{Type: kind, Flags: netlink.Flags(flags)}, Data: data})
		if err == nil {
			require.Zero(t, ack.Code, "success must never hide a negative kernel status")
		}
		if ack.Code != 0 {
			require.ErrorIs(t, err, ack.Code)
		}
	})
}

// FuzzParseFamily exercises the bounded controller policy and nested groups.
func FuzzParseFamily(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{netlink.NewFamilyCommand, 2, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		family, err := netlink.ParseFamily(netlink.Message{Header: netlink.Header{Type: netlink.ControllerID}, Data: data})
		if err != nil {
			require.ErrorIs(t, err, netlink.ErrMalformed)
			return
		}
		require.GreaterOrEqual(t, family.ID, uint16(16), "family must have a usable ID")
		require.NotEmpty(t, family.Name)
		require.Less(t, len(family.Name), 16, "controller names must fit GENL_NAMSIZ")
		require.NotContains(t, family.Name, "\x00")
	})
}
