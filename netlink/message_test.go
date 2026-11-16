//go:build linux

package netlink_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/netlink"
	"github.com/stretchr/testify/require"
)

func TestMessageEncodingPreservesLinuxFraming(t *testing.T) {
	messages := []netlink.Message{
		{Header: netlink.Header{Type: 30, Flags: 0x8022, Sequence: 123, PortID: 456}, Data: []byte{7}},
		{Header: netlink.Header{Type: 31}, Data: []byte{1, 2, 3, 4}},
	}
	want := append(test.NetlinkRecord(netlink.Header{Type: 30, Flags: 0x8022, Sequence: 123, PortID: 456}, 0, []byte{7}),
		test.NetlinkRecord(netlink.Header{Type: 31}, 0, []byte{1, 2, 3, 4})...)

	data, err := netlink.MarshalMessages(messages)
	require.NoError(t, err)
	require.Equal(t, want, data)

	decoded, err := netlink.UnmarshalMessages(data)
	require.NoError(t, err)
	require.Equal(t, messages, decoded)

	data[16] = 9
	require.Equal(t, byte(9), decoded[0].Data[0], "parser must document and preserve borrowed payloads")
	require.Len(t, decoded[0].Data, cap(decoded[0].Data), "appending must not overwrite the next record")
}

func TestMessageParserRejectsMalformedBatches(t *testing.T) {
	valid := test.NetlinkRecord(netlink.Header{Type: 16}, 0, []byte{1})
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{name: "short header", data: make([]byte, 15)},
		{name: "zero length", data: make([]byte, 16)},
		{name: "below header", data: test.NetlinkRecord(netlink.Header{}, 15, nil)},
		{name: "exceeds buffer", data: test.NetlinkRecord(netlink.Header{}, 21, nil)},
		{name: "overflowing wire length", data: test.NetlinkRecord(netlink.Header{}, ^uint32(0), nil)},
		{name: "partial padding", data: valid[:18]},
		{name: "trailing fragment", data: append(bytes.Clone(valid), 0)},
		{name: "malformed second", data: append(bytes.Clone(valid), make([]byte, 16)...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			messages, err := netlink.UnmarshalMessages(tc.data)
			require.ErrorIs(t, err, netlink.ErrMalformed)
			require.Nil(t, messages, "a malformed batch must not expose partial records")
		})
	}

	messages, err := netlink.UnmarshalMessages(valid[:17])
	require.NoError(t, err, "a final record may omit all its padding")
	require.Equal(t, []byte{1}, messages[0].Data)

	messages, err = netlink.UnmarshalMessages(nil)
	require.NoError(t, err)
	require.Empty(t, messages)
}

func TestAttributesPreserveUnknownIDsFlagsOrderAndDuplicates(t *testing.T) {
	attributes := []netlink.Attribute{
		{Type: 1234, Data: []byte{1}},
		{Type: 2 | netlink.Nested, Data: []byte{4, 0, 9, 0}},
		{Type: 1234 | netlink.NetworkByteOrder, Data: []byte{0x12, 0x34}},
	}
	data, err := netlink.MarshalAttributes(attributes)
	require.NoError(t, err)
	require.Equal(t, uint16(5), binary.NativeEndian.Uint16(data))
	require.Equal(t, []byte{0, 0, 0}, data[5:8], "alignment padding must be zeroed")
	require.Equal(t, uint16(8), binary.NativeEndian.Uint16(data[8:]))
	require.Equal(t, uint16(6), binary.NativeEndian.Uint16(data[16:]))

	decoded, err := netlink.UnmarshalAttributes(data)
	require.NoError(t, err)
	require.Equal(t, attributes, decoded)
	require.Equal(t, uint16(1234), decoded[2].ID())
	value, err := decoded[2].Uint16()
	require.NoError(t, err)
	require.Equal(t, uint16(0x1234), value)

	nested, err := netlink.UnmarshalAttributes(decoded[1].Data)
	require.NoError(t, err)
	require.Equal(t, uint16(9), nested[0].ID())
}

func TestAttributeMalformedLengthsAndPayloadPolicies(t *testing.T) {
	valid, err := netlink.MarshalAttributes([]netlink.Attribute{{Type: 1, Data: []byte{1}}})
	require.NoError(t, err)
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{name: "short header", data: []byte{4, 0, 1}},
		{name: "zero length", data: make([]byte, 4)},
		{name: "short length", data: binary.NativeEndian.AppendUint16(binary.NativeEndian.AppendUint16(nil, 3), 1)},
		{name: "exceeds buffer", data: binary.NativeEndian.AppendUint32(nil, 0xffff)},
		{name: "partial padding", data: valid[:6]},
		{name: "trailing fragment", data: append(bytes.Clone(valid), 0)},
		{name: "malformed second", data: append(bytes.Clone(valid), make([]byte, 4)...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attributes, err := netlink.UnmarshalAttributes(tc.data)
			require.ErrorIs(t, err, netlink.ErrMalformed)
			require.Nil(t, attributes)
		})
	}
	attributes, err := netlink.UnmarshalAttributes(valid[:5])
	require.NoError(t, err)
	require.Equal(t, []byte{1}, attributes[0].Data)

	data, err := netlink.MarshalAttributes([]netlink.Attribute{{Data: make([]byte, 65531)}})
	require.NoError(t, err)
	attributes, err = netlink.UnmarshalAttributes(data)
	require.NoError(t, err)
	require.Len(t, attributes[0].Data, 65531)
	data, err = netlink.MarshalAttributes([]netlink.Attribute{{Data: make([]byte, 65532)}})
	require.ErrorIs(t, err, os.ErrInvalid)
	require.Nil(t, data)
}

func TestAttributeScalarAndStringPolicies(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.NativeEndian, binary.BigEndian} {
		var flags netlink.AttributeType
		if order == binary.BigEndian {
			flags = netlink.NetworkByteOrder
		}
		data := make([]byte, 2)
		order.PutUint16(data, 0x1234)
		u16, err := (netlink.Attribute{Type: flags, Data: data}).Uint16()
		require.NoError(t, err)
		require.Equal(t, uint16(0x1234), u16)
		data = make([]byte, 4)
		order.PutUint32(data, 0x12345678)
		u32, err := (netlink.Attribute{Type: flags, Data: data}).Uint32()
		require.NoError(t, err)
		require.Equal(t, uint32(0x12345678), u32)
		data = make([]byte, 8)
		order.PutUint64(data, 0x123456789abcdef0)
		u64, err := (netlink.Attribute{Type: flags, Data: data}).Uint64()
		require.NoError(t, err)
		require.Equal(t, uint64(0x123456789abcdef0), u64)
	}

	for _, data := range [][]byte{nil, {1}, {1, 2, 3}, make([]byte, 9)} {
		_, err := (netlink.Attribute{Data: data}).Uint16()
		require.ErrorIs(t, err, netlink.ErrMalformed)
		_, err = (netlink.Attribute{Data: data}).Uint32()
		require.ErrorIs(t, err, netlink.ErrMalformed)
		_, err = (netlink.Attribute{Data: data}).Uint64()
		require.ErrorIs(t, err, netlink.ErrMalformed)
	}
	for _, data := range [][]byte{nil, []byte("missing"), {'a', 0, 'b', 0}} {
		_, err := (netlink.Attribute{Data: data}).String()
		require.ErrorIs(t, err, netlink.ErrMalformed)
	}
	text, err := (netlink.Attribute{Data: []byte("name\x00")}).String()
	require.NoError(t, err)
	require.Equal(t, "name", text)
	text, err = (netlink.Attribute{Data: []byte{0}}).String()
	require.NoError(t, err)
	require.Empty(t, text)
}
