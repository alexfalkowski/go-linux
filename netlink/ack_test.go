//go:build linux

package netlink_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/netlink"
	"github.com/stretchr/testify/require"
)

func TestAcknowledgementsDecodeCappedAndUncappedErrors(t *testing.T) {
	request := test.NetlinkRecord(netlink.Header{Type: 30, Flags: netlink.Request, Sequence: 42}, 19, []byte{1, 2, 3})
	attributes, err := netlink.MarshalAttributes([]netlink.Attribute{
		{Type: netlink.AckTextAttribute, Data: []byte("bad attribute\x00")},
		{Type: netlink.AckOffsetAttribute, Data: binary.NativeEndian.AppendUint32(nil, 20)},
		{Type: netlink.AckCookieAttribute, Data: []byte{1, 2, 0, 4}},
		{Type: 5, Data: binary.NativeEndian.AppendUint32(nil, 99)}, // NLMSGERR_ATTR_MISS_TYPE in Linux 7.0 UAPI.
		{Type: 123, Data: []byte{9}},
	})
	require.NoError(t, err)

	for _, tc := range []struct {
		name    string
		flags   netlink.Flags
		request []byte
	}{
		{name: "uncapped error", flags: netlink.AckTLVs, request: request},
		{name: "capped error", flags: netlink.Capped | netlink.AckTLVs, request: request[:16]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := append(test.NetlinkStatus(-int32(test.ErrInvalidArgument)), tc.request...)
			payload = append(payload, attributes...)
			ack, err := netlink.ParseAcknowledgement(netlink.Message{Header: netlink.Header{Type: netlink.ErrorMessage, Flags: tc.flags}, Data: payload})
			require.ErrorIs(t, err, test.ErrInvalidArgument)
			var kernel *netlink.Error
			require.ErrorAs(t, err, &kernel)
			require.Equal(t, ack, kernel.Acknowledgement)
			require.Equal(t, "netlink: invalid argument: bad attribute", err.Error())
			require.Equal(t, "bad attribute", ack.Text)
			require.Equal(t, uint32(20), *ack.Offset)
			require.Equal(t, []byte{1, 2, 0, 4}, ack.Cookie)
			require.Len(t, ack.Attributes, 5, "unknown and missing-type TLVs must survive")
			require.Equal(t, &netlink.Header{Type: 30, Flags: netlink.Request, Sequence: 42}, ack.Request)
		})
	}
}

func TestSuccessfulAcknowledgementsPreserveWarnings(t *testing.T) {
	request := test.NetlinkRecord(netlink.Header{Type: 30}, 500, nil)
	warnings, err := netlink.MarshalAttributes([]netlink.Attribute{{Type: netlink.AckTextAttribute, Data: []byte("warning\x00")}})
	require.NoError(t, err)
	// Success omits the request payload even when the capped bit is absent.
	payload := append(test.NetlinkStatus(0), request...)
	payload = append(payload, warnings...)
	ack, err := netlink.ParseAcknowledgement(netlink.Message{Header: netlink.Header{Type: netlink.ErrorMessage, Flags: netlink.AckTLVs}, Data: payload})
	require.NoError(t, err)
	require.Zero(t, ack.Code)
	require.Equal(t, "warning", ack.Text)
}

func TestDumpTerminalStatusPreservesErrorsAndInterruption(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int32
		flags  netlink.Flags
	}{
		{name: "complete"},
		{name: "interrupted", flags: netlink.DumpInterrupted},
		{name: "kernel failure", status: -int32(test.ErrNoSpace)},
		{name: "failure and interrupted", status: -int32(test.ErrNoSpace), flags: netlink.DumpInterrupted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ack, err := netlink.ParseAcknowledgement(netlink.Message{Header: netlink.Header{Type: netlink.Done, Flags: tc.flags}, Data: test.NetlinkStatus(tc.status)})
			require.Nil(t, ack.Request, "DONE does not echo a request header")
			if tc.status != 0 {
				require.ErrorIs(t, err, test.ErrNoSpace)
			}
			if tc.flags&netlink.DumpInterrupted != 0 {
				require.ErrorIs(t, err, netlink.ErrDumpInterrupted)
			}
			if tc.status == 0 && tc.flags == 0 {
				require.NoError(t, err)
			}
		})
	}
}

func TestDumpTerminalStatusPreservesWarnings(t *testing.T) {
	warnings, err := netlink.MarshalAttributes([]netlink.Attribute{{Type: netlink.AckTextAttribute, Data: []byte("warning\x00")}})
	require.NoError(t, err)

	ack, err := netlink.ParseAcknowledgement(netlink.Message{Header: netlink.Header{Type: netlink.Done, Flags: netlink.AckTLVs}, Data: append(test.NetlinkStatus(0), warnings...)})
	require.NoError(t, err)
	require.Equal(t, "warning", ack.Text)
}

func TestMalformedAcknowledgementsReturnNoDiagnostics(t *testing.T) {
	request := test.NetlinkRecord(netlink.Header{Type: 30}, 0, nil)
	valid := append(test.NetlinkStatus(-int32(test.ErrInvalidArgument)), request...)
	badLength := bytes.Clone(valid)
	binary.NativeEndian.PutUint32(badLength[4:], 15)
	missingPayload := bytes.Clone(valid)
	binary.NativeEndian.PutUint32(missingPayload[4:], ^uint32(0))
	for _, tc := range []struct {
		name    string
		kind    uint16
		flags   netlink.Flags
		payload []byte
	}{
		{name: "wrong type", kind: 16, payload: valid},
		{name: "short status", kind: netlink.Done, payload: []byte{0, 0, 0}},
		{name: "positive status", kind: netlink.Done, payload: test.NetlinkStatus(1)},
		{name: "out of errno range", kind: netlink.Done, payload: test.NetlinkStatus(-4096)},
		{name: "missing echoed header", kind: netlink.ErrorMessage, payload: valid[:19]},
		{name: "bad echoed length", kind: netlink.ErrorMessage, payload: badLength},
		{name: "missing uncapped payload", kind: netlink.ErrorMessage, payload: missingPayload},
		{name: "malformed TLV", kind: netlink.ErrorMessage, flags: netlink.AckTLVs, payload: append(bytes.Clone(valid), 0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ack, err := netlink.ParseAcknowledgement(netlink.Message{Header: netlink.Header{Type: tc.kind, Flags: tc.flags}, Data: tc.payload})
			require.ErrorIs(t, err, netlink.ErrMalformed)
			require.Equal(t, netlink.Acknowledgement{}, ack)
		})
	}
	for _, attribute := range []netlink.Attribute{
		{Type: netlink.AckTextAttribute, Data: []byte("no NUL")},
		{Type: netlink.AckOffsetAttribute, Data: []byte{1, 2, 3}},
	} {
		attributes, err := netlink.MarshalAttributes([]netlink.Attribute{attribute})
		require.NoError(t, err)
		_, err = netlink.ParseAcknowledgement(netlink.Message{
			Header: netlink.Header{Type: netlink.Done, Flags: netlink.AckTLVs}, Data: append(test.NetlinkStatus(0), attributes...),
		})
		require.ErrorIs(t, err, netlink.ErrMalformed)
	}
}
