//go:build linux

package socket_test

import (
	"os"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/socket"
	"github.com/stretchr/testify/require"
)

func TestNetlinkEndpointsPreserveAddressesAndMessages(t *testing.T) {
	a := test.NetlinkSocket(t, socket.Usersock)
	b := test.NetlinkSocket(t, socket.Usersock)
	from := test.Value(t, a.LocalAddress).(socket.NetlinkAddress)
	to := test.Value(t, b.LocalAddress).(socket.NetlinkAddress)
	require.NotZero(t, from.PortID, "bind must allocate a port")
	require.NotEqual(t, from.PortID, to.PortID, "same-process sockets need distinct port IDs")
	require.Equal(t, "netlink", from.Network())
	require.Contains(t, from.String(), ":0")

	_, err := a.Send([]byte("netlink transport"), to)
	require.NoError(t, err)

	buffer := make([]byte, 64)
	message, err := b.Receive(buffer, 0)
	require.NoError(t, err)
	require.Equal(t, "netlink transport", string(buffer[:message.N]))
	require.Equal(t, from, message.From)
}

func TestNetlinkReceiveReportsDiscardedPayload(t *testing.T) {
	a := test.NetlinkSocket(t, socket.Usersock)
	b := test.NetlinkSocket(t, socket.Usersock)

	_, err := a.Send([]byte{0}, test.Value(t, b.LocalAddress))
	require.NoError(t, err)

	message, err := b.Receive(nil, 0)
	require.NoError(t, err)
	require.Zero(t, message.N)
	require.NotZero(t, message.Flags&socket.Truncated)
}

func TestNetlinkSendRejectsInvalidDestinations(t *testing.T) {
	conn := test.NetlinkSocket(t, socket.Usersock)

	for _, destination := range []socket.Address{nil, socket.UnixAddress{Name: "@wrong-family"}} {
		_, err := conn.Send(nil, destination)
		require.ErrorIs(t, err, os.ErrInvalid)
	}
}

func TestNetlinkSendRejectsEmptyPayload(t *testing.T) {
	conn := test.NetlinkSocket(t, socket.Usersock)

	_, err := conn.Send(nil, test.Value(t, conn.LocalAddress))
	require.ErrorIs(t, err, test.ErrNoData)
}

func TestNetlinkRejectsUnixCredentialOperations(t *testing.T) {
	conn := test.NetlinkSocket(t, socket.Usersock)

	require.ErrorIs(t, conn.SetPassCredentials(true), os.ErrInvalid)
	_, err := conn.PeerCredentials()
	require.ErrorIs(t, err, os.ErrInvalid)
}

func TestNetlinkDeadlineInterruptsPendingRead(t *testing.T) {
	conn := test.NetlinkSocket(t, socket.Route)
	pending := test.StartRead(t, func() (socket.Message, error) {
		return conn.Receive(make([]byte, 64), 0)
	})

	require.NoError(t, conn.SetDeadline(time.Now().Add(-time.Second)))
	require.ErrorIs(t, test.Receive(t, pending).Err, os.ErrDeadlineExceeded)
}

func TestNetlinkCloseInterruptsPendingRead(t *testing.T) {
	conn := test.NetlinkSocket(t, socket.Route)
	require.NoError(t, conn.SetDeadline(time.Time{}))
	pending := test.StartRead(t, func() (socket.Message, error) {
		return conn.Receive(make([]byte, 64), 0)
	})

	require.NoError(t, conn.Close())
	require.ErrorIs(t, test.Receive(t, pending).Err, os.ErrClosed)
}

func TestNetlinkBackpressureReturnsEAGAINAndAllowsRetry(t *testing.T) {
	producer, err := socket.OpenNetlink(socket.Usersock, socket.NetlinkAddress{})
	require.NoError(t, err)
	test.Cleanup(t, producer)
	receiver, err := socket.OpenNetlink(socket.Usersock, socket.NetlinkAddress{})
	require.NoError(t, err)
	test.Cleanup(t, receiver)
	require.NoError(t, receiver.SetReadBuffer(1024))
	require.NoError(t, receiver.SetReadDeadline(time.Now().Add(5*time.Second)))
	destination := test.Value(t, receiver.LocalAddress)
	require.NoError(t, producer.SetWriteDeadline(time.Now().Add(time.Second)))

	var sendErr error
	var queued int
	for range 128 {
		_, sendErr = producer.Send([]byte("queued"), destination)
		if sendErr != nil {
			break
		}
		queued++
	}
	require.ErrorIs(t, sendErr, test.ErrTryAgain, "destination backpressure must remain visible without a false deadline timeout")
	require.Positive(t, queued, "backpressure must follow successful sends")

	sender, err := socket.OpenNetlink(socket.Usersock, socket.NetlinkAddress{})
	require.NoError(t, err)
	test.Cleanup(t, sender)
	require.NoError(t, sender.SetWriteDeadline(time.Now().Add(time.Second)))

	n, err := sender.Send([]byte("not queued"), destination)
	require.ErrorIs(t, err, test.ErrTryAgain, "a fresh sender must also return EAGAIN while the destination is full")
	require.Zero(t, n, "EAGAIN must not transfer a partial message")

	buffer := make([]byte, 32)
	for range queued {
		message, err := receiver.Receive(buffer, 0)
		require.NoError(t, err)
		require.Equal(t, "queued", string(buffer[:message.N]), "EAGAIN must leave the receiver's queued records intact")
	}
	require.NoError(t, sender.SetWriteDeadline(time.Time{}))

	_, err = sender.Send([]byte("retry"), destination)
	require.NoError(t, err)
	message, err := receiver.Receive(buffer, 0)
	require.NoError(t, err)
	require.Equal(t, "retry", string(buffer[:message.N]))
}

func TestNetlinkAcknowledgementOptionsCanBeToggled(t *testing.T) {
	conn, err := socket.OpenNetlink(socket.Route, socket.NetlinkAddress{})
	require.NoError(t, err)
	test.Cleanup(t, conn)
	for _, enabled := range []bool{true, false} {
		require.NoError(t, conn.SetExtendedAck(enabled))

		require.NoError(t, conn.SetCapAck(enabled))

		require.NoError(t, conn.SetStrictCheck(enabled))
	}
}

func TestNetlinkSocketBuffersCanBeSet(t *testing.T) {
	conn := test.NetlinkSocket(t, socket.Route)

	require.NoError(t, conn.SetReadBuffer(65536))
	require.NoError(t, conn.SetWriteBuffer(65536))
}

func TestNetlinkReadOnlyGroupMembership(t *testing.T) {
	conn := test.NetlinkSocket(t, socket.Route)

	require.NoError(t, conn.JoinGroup(1), "RTNLGRP_LINK subscription does not mutate network state")
	require.NoError(t, conn.LeaveGroup(1))
	require.ErrorIs(t, conn.JoinGroup(0), os.ErrInvalid)
	require.ErrorIs(t, conn.LeaveGroup(1<<31), os.ErrInvalid)
}

func TestNetlinkDuplicateBindReturnsNoOwner(t *testing.T) {
	conn := test.NetlinkSocket(t, socket.Route)

	other, err := socket.OpenNetlink(socket.Route, test.Value(t, conn.LocalAddress).(socket.NetlinkAddress))
	test.NoResource(t, other, err, test.ErrAddressInUse)
}

func TestCreatedSocketsAreNonblockingAndClosedOnExec(t *testing.T) {
	before := test.Descriptors(t)
	conn, err := socket.OpenNetlink(socket.Route, socket.NetlinkAddress{})
	require.NoError(t, err)
	test.Cleanup(t, conn)

	fd := test.NewDescriptor(t, before)
	test.CloseOnExec(t, fd)
	require.True(t, test.DescriptorNonblocking(t, fd))
}
