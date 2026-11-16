//go:build linux

package netlink_test

import (
	"os"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/netlink"
	"github.com/alexfalkowski/go-linux/socket"
	"github.com/stretchr/testify/require"
)

// TestMulticastReceptionInPrivateNetworkNamespace checks the kernel route ABI
// with test-owned native fixtures until the typed route package exists.
func TestMulticastReceptionInPrivateNetworkNamespace(t *testing.T) {
	if !test.NetworkNamespace(t) {
		return
	}
	listener := test.NetlinkLinkListener(t)

	test.BringLoopbackUp(t)

	messages, err := listener.ReadMessages()
	require.NoError(t, err)
	var found bool
	for _, message := range messages {
		name, up, err := test.NetlinkLink(message)
		require.NoError(t, err)
		require.True(t, up, "loopback notification must report UP")
		require.Zero(t, message.Header.Sequence, "multicast notifications are independent of requests")
		found = found || name == "lo"
	}
	require.True(t, found, "the private interface change must produce an authenticated kernel notification")
}

func TestListenerMembershipDeadlinesAndClose(t *testing.T) {
	group := test.NetlinkGroup(t, "nlctrl", "notify")
	listener, err := netlink.Listen(socket.Generic, netlink.Options{}, group.ID)
	require.NoError(t, err)
	test.Cleanup(t, listener)
	require.NoError(t, listener.LeaveGroup(group.ID))
	require.NoError(t, listener.JoinGroup(group.ID))
	require.NoError(t, listener.LeaveGroup(group.ID))

	pending := test.StartRead(t, listener.ReadMessages)
	require.NoError(t, listener.SetReadDeadline(time.Now().Add(-time.Second)))
	require.ErrorIs(t, test.Receive(t, pending).Err, os.ErrDeadlineExceeded)
	require.NoError(t, listener.SetReadDeadline(time.Time{}))

	pending = test.StartRead(t, listener.ReadMessages)
	require.NoError(t, listener.Close())
	require.ErrorIs(t, test.Receive(t, pending).Err, os.ErrClosed)
	require.ErrorIs(t, listener.Close(), os.ErrClosed)
	require.ErrorIs(t, listener.JoinGroup(1), os.ErrClosed)
	require.ErrorIs(t, listener.LeaveGroup(1), os.ErrClosed)
	require.ErrorIs(t, listener.SetReadDeadline(time.Time{}), os.ErrClosed)
	require.ErrorIs(t, listener.JoinGroup(0), os.ErrInvalid)
}

func TestListenerRejectsUserSpaceSenders(t *testing.T) {
	before := test.Descriptors(t)
	listener, err := netlink.Listen(socket.Usersock, netlink.Options{})
	require.NoError(t, err)
	test.Cleanup(t, listener)
	require.NoError(t, listener.SetReadDeadline(time.Now().Add(time.Second)))
	fd := test.NewDescriptor(t, before)
	destination := test.NetlinkAddress(t, fd)

	producer, err := socket.OpenNetlink(socket.Usersock, socket.NetlinkAddress{})
	require.NoError(t, err)
	test.Cleanup(t, producer)
	data, err := netlink.MarshalMessages([]netlink.Message{{Header: netlink.Header{Type: 30}, Data: []byte("untrusted")}})
	require.NoError(t, err)
	_, err = producer.Send(data, destination)
	require.NoError(t, err)

	messages, err := listener.ReadMessages()
	require.ErrorIs(t, err, netlink.ErrUnexpectedReply, "header port zero cannot authenticate a user-space sender")
	require.Nil(t, messages)

	require.NoError(t, listener.Close(), "rejecting an unexpected sender leaves a listener open")
}

func TestListenerCreationFailureReleasesJoinedMemberships(t *testing.T) {
	group := test.NetlinkGroup(t, "nlctrl", "notify")
	before := test.DescriptorCount(t)
	listener, err := netlink.Listen(socket.Generic, netlink.Options{}, group.ID, 1<<31-1)
	test.NoResource(t, listener, err, test.ErrInvalidArgument)
	require.Equal(t, before, test.DescriptorCount(t), "failure after the first membership must close the socket")

	for _, group := range []uint32{0, 1 << 31} {
		listener, err := netlink.Listen(socket.Route, netlink.Options{}, group)
		test.NoResource(t, listener, err, os.ErrInvalid)
	}
	var zero netlink.Listener
	_, err = zero.ReadMessages()
	require.ErrorIs(t, err, os.ErrInvalid)
	require.ErrorIs(t, zero.Close(), os.ErrInvalid)
	require.ErrorIs(t, zero.SetReadDeadline(time.Time{}), os.ErrInvalid)
	require.ErrorIs(t, zero.JoinGroup(1), os.ErrInvalid)
	require.ErrorIs(t, zero.LeaveGroup(1), os.ErrInvalid)
	listener, err = netlink.Listen(socket.Route, netlink.Options{DatagramSize: -1})
	test.NoResource(t, listener, err, os.ErrInvalid)
}
