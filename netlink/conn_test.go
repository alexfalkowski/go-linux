//go:build linux

package netlink_test

import (
	"encoding/binary"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/netlink"
	"github.com/alexfalkowski/go-linux/socket"
	"github.com/stretchr/testify/require"
)

func TestLiveFamilyDiscoveryAgreesWithFamilyList(t *testing.T) {
	conn := test.Netlink(t, socket.Generic, netlink.Options{})
	family, err := conn.LookupFamily("nlctrl")
	require.NoError(t, err)
	require.Equal(t, netlink.ControllerID, family.ID)
	require.Equal(t, "nlctrl", family.Name)
	require.NotEmpty(t, family.Attributes)
	require.NotEmpty(t, family.Groups)

	families, err := conn.Families()
	require.NoError(t, err)
	var found bool
	for _, candidate := range families {
		if candidate.Name == family.Name {
			require.Equal(t, family, candidate)
			found = true
		}
	}
	require.True(t, found, "a completed controller dump must include nlctrl")
}

func TestLiveMultipartDumpDrainsTerminalReply(t *testing.T) {
	conn := test.Netlink(t, socket.Generic, netlink.Options{})
	request := test.NetlinkFamilyDump(t)
	response, err := conn.Dump(request)
	require.NoError(t, err)
	require.True(t, response.Complete, "dump must reach its terminal status")
	require.NotNil(t, response.Done)
	require.Zero(t, response.Done.Code)
	require.NotEmpty(t, response.Messages, "each dump is an independent observation of registered families")
	sequence := response.Messages[0].Header.Sequence
	for _, message := range response.Messages {
		require.Equal(t, sequence, message.Header.Sequence, "all dump records must match this request")
		require.NotZero(t, message.Header.Flags&netlink.Multi, "dump data must retain MULTI")
	}

	_, err = conn.LookupFamily("nlctrl")
	require.NoError(t, err, "completion must leave no unread terminal replies")
}

func TestLiveRouteDumpPreservesKernelRecords(t *testing.T) {
	conn := test.Netlink(t, socket.Route, netlink.Options{Validation: netlink.StrictValidation})
	response, err := conn.Dump(test.NetlinkLinkDump())
	require.NoError(t, err)
	require.True(t, response.Complete, "a strict route dump must reach DONE")
	require.NotNil(t, response.Done)
	var foundLoopback bool
	for _, message := range response.Messages {
		name, _, err := test.NetlinkLink(message)
		require.NoError(t, err)
		foundLoopback = foundLoopback || name == "lo"
	}
	require.True(t, foundLoopback, "read-only route enumeration must include loopback")
}

func TestSendFailureLeavesRequestOwnerOpen(t *testing.T) {
	conn := test.Netlink(t, socket.Usersock, netlink.Options{})
	for range 2 {
		response, err := conn.Execute(netlink.Message{Header: netlink.Header{Type: 30}})
		require.ErrorIs(t, err, test.ErrConnectionRefused, "NETLINK_USERSOCK has no kernel request handler")
		require.False(t, response.Complete, "an unsent request has no response")
	}
	require.NoError(t, conn.Close(), "a send failure must leave ownership with the caller")
}

func TestCompletedKernelErrorsRetainErrnoAndConnection(t *testing.T) {
	conn := test.Netlink(t, socket.Generic, netlink.Options{})
	attributes, err := netlink.MarshalAttributes([]netlink.Attribute{{Type: netlink.FamilyIDAttribute, Data: binary.NativeEndian.AppendUint16(nil, 0xffff)}})
	require.NoError(t, err)
	data, err := (netlink.GenericMessage{Command: netlink.GetFamilyCommand, Version: 2, Data: attributes}).MarshalBinary()
	require.NoError(t, err)
	response, err := conn.Execute(netlink.Message{Header: netlink.Header{Type: netlink.ControllerID}, Data: data})
	require.ErrorIs(t, err, test.ErrNotExist)
	require.True(t, response.Complete, "a kernel rejection is a completed exchange")
	require.Empty(t, response.Messages)
	require.NotNil(t, response.Acknowledgement.Request)
	require.NotZero(t, response.Acknowledgement.Request.Sequence)
	var kernel *netlink.Error
	require.ErrorAs(t, err, &kernel)
	require.Equal(t, "netlink: no such file or directory", err.Error())

	_, err = conn.LookupFamily("nlctrl")
	require.NoError(t, err, "completed errors must permit subsequent requests")
}

func TestKernelPolicyErrorsExposeExtendedAcknowledgements(t *testing.T) {
	conn := test.Netlink(t, socket.Generic, netlink.Options{})
	attributes, err := netlink.MarshalAttributes([]netlink.Attribute{{Type: netlink.FamilyIDAttribute, Data: []byte{1}}})
	require.NoError(t, err)
	data, err := (netlink.GenericMessage{Command: netlink.GetFamilyCommand, Version: 2, Data: attributes}).MarshalBinary()
	require.NoError(t, err)
	response, err := conn.Execute(netlink.Message{Header: netlink.Header{Type: netlink.ControllerID}, Data: data})
	require.ErrorIs(t, err, test.ErrRange)
	var kernel *netlink.Error
	require.ErrorAs(t, err, &kernel)
	require.NotEmpty(t, kernel.Acknowledgement.Text, "EXT_ACK must expose kernel policy diagnostics")
	require.NotEmpty(t, response.Acknowledgement.Attributes)
	require.Equal(t, uint32(20), *response.Acknowledgement.Offset)

	_, err = conn.LookupFamily("nlctrl")
	require.NoError(t, err, "completed errors must permit subsequent requests")
}

func TestConcurrentRequestsMatchTheirOwnResponses(t *testing.T) {
	conn := test.Netlink(t, socket.Generic, netlink.Options{})
	results := make(chan error, 16)
	for i := range cap(results) {
		go func() {
			if i%2 == 0 {
				_, err := conn.LookupFamily("nlctrl")
				results <- err
			} else {
				_, err := conn.Families()
				results <- err
			}
		}()
	}
	for range cap(results) {
		require.NoError(t, test.Receive(t, results), "shared requests must serialize and retain their terminal replies")
	}
}

func TestIncompleteResponsesCloseTheConnection(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options netlink.Options
		want    error
	}{
		{name: "truncated datagram", options: netlink.Options{DatagramSize: 16}, want: netlink.ErrTruncated},
		{name: "response bound", options: netlink.Options{MaxResponseBytes: 16}, want: netlink.ErrLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := test.DescriptorCount(t)
			conn := test.Netlink(t, socket.Generic, tc.options)
			request := test.NetlinkFamilyDump(t)
			response, err := conn.Dump(request)
			require.ErrorIs(t, err, tc.want)
			require.False(t, response.Complete, "loss must never look like a completed dump")
			require.Equal(t, before, test.DescriptorCount(t), "aborting a dump must close its descriptor")
			_, err = conn.Dump(request)
			require.ErrorIs(t, err, os.ErrClosed)
		})
	}
}

func TestExpiredSendDeadlineLeavesConnectionUsable(t *testing.T) {
	conn := test.Netlink(t, socket.Generic, netlink.Options{})
	require.NoError(t, conn.SetDeadline(time.Now().Add(-time.Second)))
	_, err := conn.LookupFamily("nlctrl")
	require.ErrorIs(t, err, os.ErrDeadlineExceeded)
	require.NoError(t, conn.SetDeadline(time.Now().Add(time.Second)))
	_, err = conn.LookupFamily("nlctrl")
	require.NoError(t, err, "expiry before send must leave the request connection usable")
}

func TestInvalidConnectionOptionsReturnNoOwner(t *testing.T) {
	for _, options := range []netlink.Options{
		{DatagramSize: -1},
		{DatagramSize: 15},
		{DatagramSize: 1 << 31},
		{MaxResponseBytes: -1},
		{MaxResponseBytes: 15},
		{Validation: 255},
	} {
		conn, err := netlink.Open(socket.Generic, options)
		test.NoResource(t, conn, err, os.ErrInvalid)
	}
	conn, err := netlink.Open(-1, netlink.Options{})
	test.NoResource(t, conn, err, os.ErrInvalid)
}

func TestInvalidRequestHeadersLeaveConnectionUsable(t *testing.T) {
	conn := test.Netlink(t, socket.Generic, netlink.Options{})
	for _, header := range []netlink.Header{{Type: 15}, {Type: 16, Sequence: 1}, {Type: 16, PortID: 1}, {Type: 16, Flags: netlink.Multi}, {Type: 16, Flags: 0x8000}} {
		_, err := conn.Execute(netlink.Message{Header: header})
		require.ErrorIs(t, err, os.ErrInvalid)
	}
	_, err := conn.Dump(netlink.Message{Header: netlink.Header{Type: 16, Flags: netlink.Append}})
	require.ErrorIs(t, err, os.ErrInvalid)
	_, err = conn.LookupFamily("nlctrl")
	require.NoError(t, err)
}

func TestInvalidFamilyNamesLeaveConnectionUsable(t *testing.T) {
	conn := test.Netlink(t, socket.Generic, netlink.Options{})

	for _, name := range []string{"", "embedded\x00NUL", "1234567890123456"} {
		_, err := conn.LookupFamily(name)
		require.ErrorIs(t, err, os.ErrInvalid)
	}
	_, err := conn.LookupFamily("nlctrl")
	require.NoError(t, err, "validation failures must not damage a live request connection")
}

func TestFamilyDiscoveryRejectsRouteProtocol(t *testing.T) {
	route := test.Netlink(t, socket.Route, netlink.Options{Validation: netlink.StrictValidation})
	_, err := route.LookupFamily("nlctrl")
	require.ErrorIs(t, err, os.ErrInvalid)
	_, err = route.Families()
	require.ErrorIs(t, err, os.ErrInvalid)
}

func TestZeroAndClosedRequestOwners(t *testing.T) {
	var zero netlink.Conn
	_, err := zero.Execute(netlink.Message{Header: netlink.Header{Type: 16}})
	require.ErrorIs(t, err, os.ErrInvalid)
	require.ErrorIs(t, zero.Close(), os.ErrInvalid)
	require.ErrorIs(t, zero.SetDeadline(time.Time{}), os.ErrInvalid)
	_, err = zero.Families()
	require.ErrorIs(t, err, os.ErrInvalid)

	conn := test.Netlink(t, socket.Generic, netlink.Options{})
	require.NoError(t, conn.Close())
	require.ErrorIs(t, conn.Close(), os.ErrClosed)
	require.ErrorIs(t, conn.SetDeadline(time.Time{}), os.ErrClosed)
	_, err = conn.LookupFamily("nlctrl")
	require.ErrorIs(t, err, os.ErrClosed)
	_, err = conn.Execute(netlink.Message{})
	require.ErrorIs(t, err, os.ErrInvalid, "validation takes precedence over closed state")
}

func TestNetlinkCreationFailureLeavesNoOwner(t *testing.T) {
	if os.Args[len(os.Args)-1] == "netlink-exhaustion" {
		conn := test.Netlink(t, socket.Generic, netlink.Options{})
		require.NoError(t, conn.Close())
		release := test.ExhaustDescriptors(t)

		conn, err := netlink.Open(socket.Generic, netlink.Options{})
		test.NoResource(t, conn, err, test.ErrTooManyOpenFiles)
		listener, err := netlink.Listen(socket.Generic, netlink.Options{})
		test.NoResource(t, listener, err, test.ErrTooManyOpenFiles)

		release()
		conn, err = netlink.Open(socket.Generic, netlink.Options{})
		require.NoError(t, err, "failed constructors must leave the released slot usable")
		require.NoError(t, conn.Close())
		return
	}
	ctx, command := test.Command(t, t.Name(), "netlink-exhaustion")
	output, err := command.CombinedOutput()
	require.NoError(t, ctx.Err())
	require.NoError(t, err, "descriptor-exhaustion subprocess: %s", output)
}

func TestNetlinkControlFailuresReleaseAcquiredSocket(t *testing.T) {
	const prefix = "netlink-deny="
	if argument := os.Args[len(os.Args)-1]; strings.HasPrefix(argument, prefix) {
		before := test.Descriptors(t)
		test.RefuseNetlinkControl(t, strings.TrimPrefix(argument, prefix))

		conn, err := netlink.Open(socket.Generic, netlink.Options{Validation: netlink.StrictValidation})
		test.NoResource(t, conn, err, test.ErrOperationNotPermitted)
		require.Equal(t, before, test.Descriptors(t), "failed setup must close the acquired socket")
		return
	}
	name := t.Name()
	for _, operation := range []string{"bind", "extended ACK", "capped ACK", "strict check", "address"} {
		t.Run(operation, func(t *testing.T) {
			ctx, command := test.Command(t, name, prefix+operation)
			output, err := command.CombinedOutput()
			require.NoError(t, ctx.Err())
			require.NoError(t, err, "syscall-denial subprocess: %s", output)
		})
	}
}
