//go:build linux

package netlink_test

import (
	"encoding/binary"
	"testing"

	"github.com/alexfalkowski/go-linux/netlink"
	"github.com/stretchr/testify/require"
)

func TestGenericHeaderRoundTripOwnsDecodedPayload(t *testing.T) {
	want := netlink.GenericMessage{Command: 42, Version: 7, Reserved: 0x1234, Data: []byte{1, 2, 3}}
	data, err := want.MarshalBinary()
	require.NoError(t, err)
	require.Equal(t, []byte{42, 7}, data[:2])
	require.Equal(t, uint16(0x1234), binary.NativeEndian.Uint16(data[2:]))
	var got netlink.GenericMessage
	err = got.UnmarshalBinary(data)
	require.NoError(t, err)
	require.Equal(t, want, got)
	data[4] = 99
	require.Equal(t, byte(1), got.Data[0], "BinaryUnmarshaler must own retained bytes")
}

func TestShortGenericHeadersLeaveReceiverUnchanged(t *testing.T) {
	want := netlink.GenericMessage{Command: 42, Version: 7, Reserved: 0x1234, Data: []byte{1, 2, 3}}
	got := want
	data := make([]byte, 4)

	for size := range 4 {
		err := got.UnmarshalBinary(data[:size])
		require.ErrorIs(t, err, netlink.ErrMalformed)
		require.Equal(t, want, got, "a short input must leave the receiver unchanged")
	}
}

func TestControllerPoliciesPreserveUnknownAttributes(t *testing.T) {
	groupAttributes, err := netlink.MarshalAttributes([]netlink.Attribute{
		{Type: netlink.GroupNameAttribute, Data: []byte("notify\x00")},
		{Type: netlink.GroupIDAttribute, Data: binary.NativeEndian.AppendUint32(nil, 123)},
		{Type: 300, Data: []byte("future")},
	})
	require.NoError(t, err)
	groups, err := netlink.MarshalAttributes([]netlink.Attribute{{Type: 1, Data: groupAttributes}})
	require.NoError(t, err)
	attributes := []netlink.Attribute{
		{Type: netlink.FamilyIDAttribute, Data: binary.NativeEndian.AppendUint16(nil, 50)},
		{Type: netlink.FamilyNameAttribute, Data: []byte("fixture\x00")},
		{Type: netlink.FamilyVersionAttribute, Data: binary.NativeEndian.AppendUint32(nil, 3)},
		{Type: netlink.FamilyHeaderSizeAttribute, Data: binary.NativeEndian.AppendUint32(nil, 8)},
		{Type: netlink.FamilyMaxAttribute, Data: binary.NativeEndian.AppendUint32(nil, 99)},
		{Type: netlink.FamilyGroupsAttribute, Data: groups},
		{Type: 300, Data: []byte("future")},
	}
	data, err := netlink.MarshalAttributes(attributes)
	require.NoError(t, err)
	generic, err := (netlink.GenericMessage{Command: netlink.NewFamilyCommand, Version: 2, Data: data}).MarshalBinary()
	require.NoError(t, err)
	family, err := netlink.ParseFamily(netlink.Message{Header: netlink.Header{Type: netlink.ControllerID}, Data: generic})
	require.NoError(t, err)
	require.Equal(t, netlink.Family{
		ID: 50, Name: "fixture", Version: 3, HeaderSize: 8, MaxAttribute: 99,
		Groups: []netlink.Group{{ID: 123, Name: "notify"}}, Attributes: attributes,
	}, family)
}

func TestControllerPoliciesRejectMalformedKnownAttributes(t *testing.T) {
	valid := []netlink.Attribute{
		{Type: netlink.FamilyIDAttribute, Data: binary.NativeEndian.AppendUint16(nil, 50)},
		{Type: netlink.FamilyNameAttribute, Data: []byte("fixture\x00")},
	}
	for _, tc := range []struct {
		name       string
		attributes []netlink.Attribute
	}{
		{name: "missing identity"},
		{name: "bad ID width", attributes: []netlink.Attribute{{Type: netlink.FamilyIDAttribute, Data: []byte{1}}}},
		{name: "unterminated name", attributes: []netlink.Attribute{valid[0], {Type: netlink.FamilyNameAttribute, Data: []byte("name")}}},
		{name: "long name", attributes: []netlink.Attribute{valid[0], {Type: netlink.FamilyNameAttribute, Data: make([]byte, 17)}}},
		{name: "duplicate known ID", attributes: []netlink.Attribute{valid[0], valid[1], valid[0]}},
		{name: "bad native order", attributes: []netlink.Attribute{{Type: netlink.FamilyIDAttribute | netlink.NetworkByteOrder, Data: []byte{0, 50}}, valid[1]}},
		{name: "bad group framing", attributes: []netlink.Attribute{valid[0], valid[1], {Type: netlink.FamilyGroupsAttribute, Data: []byte{1}}}},
		{name: "missing group identity", attributes: []netlink.Attribute{valid[0], valid[1], {Type: netlink.FamilyGroupsAttribute, Data: []byte{4, 0, 1, 0}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := netlink.MarshalAttributes(tc.attributes)
			require.NoError(t, err)
			generic, err := (netlink.GenericMessage{Command: netlink.NewFamilyCommand, Data: data}).MarshalBinary()
			require.NoError(t, err)
			_, err = netlink.ParseFamily(netlink.Message{Header: netlink.Header{Type: netlink.ControllerID}, Data: generic})
			require.ErrorIs(t, err, netlink.ErrMalformed)
		})
	}
	for _, data := range [][]byte{{1}, {netlink.GetFamilyCommand, 2, 0, 0}, {netlink.NewFamilyCommand, 2, 1, 0}} {
		_, err := netlink.ParseFamily(netlink.Message{Header: netlink.Header{Type: netlink.ControllerID}, Data: data})
		require.ErrorIs(t, err, netlink.ErrMalformed)
	}
	_, err := netlink.ParseFamily(netlink.Message{Header: netlink.Header{Type: 30}})
	require.ErrorIs(t, err, netlink.ErrMalformed)
}
