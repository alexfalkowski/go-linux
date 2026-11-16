//go:build linux

package netlink_test

import (
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/netlink"
	"github.com/alexfalkowski/go-linux/socket"
	"github.com/stretchr/testify/require"
)

func TestKernelFilteredAcknowledgementsCauseReceiveTimeoutAndCleanup(t *testing.T) {
	before := test.Descriptors(t)
	conn := test.Netlink(t, socket.Generic, netlink.Options{})
	test.DropNetlinkAcknowledgements(t, test.NewDescriptor(t, before))
	request := test.NetlinkFamilyRequest(t, "nlctrl")
	require.NoError(t, conn.SetDeadline(time.Now().Add(250*time.Millisecond)))

	response, err := conn.Execute(request)
	require.ErrorIs(t, err, os.ErrDeadlineExceeded)
	require.Len(t, response.Messages, 1, "a data reply must arrive before waiting for the filtered ACK")
	require.False(t, response.Complete)
	require.Nil(t, response.Acknowledgement)
	require.Equal(t, before, test.Descriptors(t), "receive timeout must close the incomplete connection")
	_, err = conn.Execute(request)
	require.ErrorIs(t, err, os.ErrClosed)
}

func TestCloseInterruptsRequestWaitingForFilteredAcknowledgement(t *testing.T) {
	before := test.Descriptors(t)
	conn := test.Netlink(t, socket.Generic, netlink.Options{})
	test.DropNetlinkAcknowledgements(t, test.NewDescriptor(t, before))
	request := test.NetlinkFamilyRequest(t, "nlctrl")
	pending := test.StartRead(t, func() (netlink.Response, error) {
		return conn.Execute(request)
	})

	require.NoError(t, conn.Close())
	result := test.Receive(t, pending)
	require.ErrorIs(t, result.Err, os.ErrClosed)
	require.False(t, result.Value.Complete)
	require.Equal(t, before, test.Descriptors(t), "close must release the waiting request socket")
}

func TestKernelTrimmedReplyClosesMalformedConnection(t *testing.T) {
	before := test.Descriptors(t)
	conn := test.Netlink(t, socket.Generic, netlink.Options{})
	test.TrimNetlinkDatagrams(t, test.NewDescriptor(t, before), 8)

	response, err := conn.Execute(test.NetlinkFamilyRequest(t, "nlctrl"))
	require.ErrorIs(t, err, netlink.ErrMalformed, "kernel socket filtering must exercise malformed wire framing")
	require.False(t, response.Complete)
	require.Empty(t, response.Messages)
	require.Equal(t, before, test.Descriptors(t), "malformed replies must close their connection")
}

func TestKernelUnmatchedAcknowledgementsCloseConnection(t *testing.T) {
	for _, scenario := range []struct {
		name          string
		wrongSequence bool
	}{
		{name: "sequence", wrongSequence: true},
		{name: "echoed request"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			before := test.Descriptors(t)
			conn := test.Netlink(t, socket.Generic, netlink.Options{})
			fd := test.NewDescriptor(t, before)
			request := test.NetlinkFamilyRequest(t, "nlctrl")
			probe, err := conn.Execute(request)
			require.NoError(t, err)
			var sequence uint32
			if scenario.wrongSequence {
				sequence = 0
			} else {
				sequence = probe.Acknowledgement.Request.Sequence + 1
			}
			test.QueueNetlinkAcknowledgement(t, fd, sequence)

			response, err := conn.Execute(request)
			require.ErrorIs(t, err, netlink.ErrUnexpectedReply)
			require.False(t, response.Complete)
			require.Equal(t, before, test.Descriptors(t), "unmatched kernel replies must close their connection")
		})
	}
}

func TestKernelSuccessAcknowledgementDoesNotCompleteADump(t *testing.T) {
	if !test.NetworkNamespace(t) {
		return
	}
	loopback, err := net.InterfaceByName("lo")
	require.NoError(t, err)
	conn := test.Netlink(t, socket.Route, netlink.Options{})
	require.NoError(t, conn.SetDeadline(time.Now().Add(250*time.Millisecond)))

	// SETLINK with no changed fields succeeds with an ACK only. Running it
	// through Dump proves that an ACK cannot substitute for multipart DONE.
	response, err := conn.Dump(test.NetlinkLinkSet(loopback.Index))
	require.ErrorIs(t, err, os.ErrDeadlineExceeded)
	require.NotNil(t, response.Acknowledgement)
	require.Zero(t, response.Acknowledgement.Code)
	require.Nil(t, response.Done)
	require.False(t, response.Complete)
	require.ErrorIs(t, conn.Close(), os.ErrClosed, "the incomplete exchange must already have closed the connection")
}

func TestKernelAcknowledgementBeforeDumpCompletion(t *testing.T) {
	if !test.NetworkNamespace(t) {
		return
	}
	loopback, err := net.InterfaceByName("lo")
	require.NoError(t, err)
	before := test.Descriptors(t)
	conn := test.Netlink(t, socket.Route, netlink.Options{})
	fd := test.NewDescriptor(t, before)
	probe, err := conn.Dump(test.NetlinkLinkDump())
	require.NoError(t, err)
	request := test.NetlinkLinkSet(loopback.Index)
	queued := request
	queued.Header.Sequence = probe.Messages[0].Header.Sequence + 1
	queued.Header.Flags = netlink.Request | netlink.Acknowledge | netlink.DumpFlags

	// Queue a real success ACK before installing the filter. A separately
	// primed kernel dump supplies data and DONE with the same sequence; the
	// public request's duplicate ACK is filtered before it enters the queue.
	test.SendNetlinkRequest(t, fd, queued)
	test.DropNetlinkAcknowledgements(t, fd)
	dump := test.NetlinkLinkDump()
	dump.Header.Sequence = queued.Header.Sequence
	dump.Header.Flags = netlink.Request | netlink.DumpFlags
	test.SendNetlinkRequest(t, fd, dump)

	response, err := conn.Dump(request)
	require.NoError(t, err)
	require.True(t, response.Complete)
	require.NotNil(t, response.Acknowledgement)
	require.NotNil(t, response.Done, "an earlier success ACK must not terminate the dump")
	require.NotEmpty(t, response.Messages)
	_, err = conn.Dump(test.NetlinkLinkDump())
	require.NoError(t, err, "the complete exchange must leave no stale replies")
}

func TestKernelInterruptedDumpDrainsAndKeepsConnectionUsable(t *testing.T) {
	if !test.NetworkNamespace(t) {
		return
	}
	loopback, err := net.InterfaceByName("lo")
	require.NoError(t, err)
	before := test.Descriptors(t)
	conn := test.Netlink(t, socket.Route, netlink.Options{})
	fd := test.NewDescriptor(t, before)
	probe, err := conn.Dump(test.NetlinkLinkDump())
	require.NoError(t, err)
	test.DropNetlinkAcknowledgements(t, fd)
	mutator := test.Netlink(t, socket.Route, netlink.Options{})
	require.NoError(t, mutator.SetDeadline(time.Now().Add(20*time.Second)))
	for ordinal := range uint16(1024) {
		test.AddLoopbackAddress(t, mutator, loopback.Index, ordinal)
	}

	// Prime a genuine kernel dump, then change its generation before reading.
	// Its sequence matches the next public request. The socket filter discards
	// that request's EBUSY ACK while the primed dump is active, retaining the
	// real data/interruption/DONE stream for Conn.Dump to consume.
	test.SendNetlinkRequest(t, fd, test.NetlinkAddressDump(probe.Messages[0].Header.Sequence+1))
	test.AddLoopbackAddress(t, mutator, loopback.Index, 1024)
	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))

	response, err := conn.Dump(test.NetlinkAddressDump(0))
	require.ErrorIs(t, err, netlink.ErrDumpInterrupted)
	require.True(t, response.Complete, "interrupted results must still drain to DONE")
	require.NotNil(t, response.Done)
	require.NotEmpty(t, response.Messages)
	response, err = conn.Dump(test.NetlinkLinkDump())
	require.NoError(t, err, "draining interruption must leave no stale replies")
	require.True(t, response.Complete)
}

func TestKernelMulticastQueueOverflowReportsLostNotifications(t *testing.T) {
	if !test.NetworkNamespace(t) {
		return
	}
	listener := test.NetlinkOverflowListener(t)

	messages, err := listener.ReadMessages()
	require.ErrorIs(t, err, test.ErrNoBufferSpace, "a full kernel receive queue must report lost notifications")
	require.Nil(t, messages)

	messages, err = listener.ReadMessages()
	require.NoError(t, err, "reporting overflow must leave the listener usable")
	require.NotEmpty(t, messages, "overflow must preserve notifications already queued")

	conn := test.Netlink(t, socket.Route, netlink.Options{})
	response, err := conn.Dump(test.NetlinkLinkDump())
	require.NoError(t, err, "a separate request connection must be available for resynchronization")
	require.True(t, response.Complete)
}

// TestKernelDumpInterruptionInPrivateNetworkNamespace is an ABI observation.
// The independent socket transport deliberately pauses between sending a dump
// and receiving it. Linux's dump continues on recvmsg, so a private address
// mutation between batches changes inet_base_seq and emits DUMP_INTR.
// https://github.com/torvalds/linux/blob/v7.0/net/ipv4/devinet.c
func TestKernelDumpInterruptionInPrivateNetworkNamespace(t *testing.T) {
	if !test.NetworkNamespace(t) {
		return
	}
	loopback, err := net.InterfaceByName("lo")
	require.NoError(t, err)
	mutator := test.Netlink(t, socket.Route, netlink.Options{})
	require.NoError(t, mutator.SetDeadline(time.Now().Add(20*time.Second)))
	for ordinal := range uint16(1024) {
		test.AddLoopbackAddress(t, mutator, loopback.Index, ordinal)
	}
	dumper, err := socket.OpenNetlink(socket.Route, socket.NetlinkAddress{})
	require.NoError(t, err)
	test.Cleanup(t, dumper)
	require.NoError(t, dumper.SetDeadline(time.Now().Add(5*time.Second)))
	request, err := netlink.MarshalMessages([]netlink.Message{test.NetlinkAddressDump(123)})
	require.NoError(t, err)
	_, err = dumper.Send(request, socket.NetlinkAddress{})
	require.NoError(t, err)

	// The initial skb has been generated but the remaining addresses do not fit.
	test.AddLoopbackAddress(t, mutator, loopback.Index, 1024)

	var interrupted, complete bool
	for range 64 {
		buffer := make([]byte, 65536)
		datagram, err := dumper.Receive(buffer, 0)
		require.NoError(t, err)
		require.Zero(t, datagram.Flags&socket.Truncated, "ABI observation must not lose the marker to truncation")
		require.Zero(t, datagram.From.(socket.NetlinkAddress).PortID, "only the kernel may establish this ABI result")
		messages, err := netlink.UnmarshalMessages(buffer[:datagram.N])
		require.NoError(t, err)
		for _, message := range messages {
			require.Equal(t, uint32(123), message.Header.Sequence)
			interrupted = interrupted || message.Header.Flags&netlink.DumpInterrupted != 0
			if message.Header.Type == netlink.Done {
				_, err := netlink.ParseAcknowledgement(message)
				if !errors.Is(err, netlink.ErrDumpInterrupted) {
					require.NoError(t, err)
				}
				complete = true
			}
		}
		if complete {
			break
		}
	}
	require.True(t, complete, "a bounded dump must reach NLMSG_DONE")
	require.True(t, interrupted, "the kernel must report the fixture's mid-dump address mutation")
}
