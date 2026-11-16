//go:build linux

package test

import (
	"encoding/binary"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/netlink"
	"github.com/alexfalkowski/go-linux/socket"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// RefuseNetlinkControl makes one native operation return EPERM on the current
// thread of a bounded fixture child. Keep the goroutine locked until it exits;
// Go then retires the restricted thread. No parent or host policy is changed.
func RefuseNetlinkControl(t *testing.T, operation string) {
	t.Helper()
	require.True(t, strings.HasPrefix(os.Args[len(os.Args)-1], "netlink-deny="), "syscall restriction requires a dedicated child")
	var number, option uint32
	switch operation {
	case "bind":
		number = unix.SYS_BIND
	case "address":
		number = unix.SYS_GETSOCKNAME
	case "extended ACK":
		number, option = unix.SYS_SETSOCKOPT, unix.NETLINK_EXT_ACK
	case "capped ACK":
		number, option = unix.SYS_SETSOCKOPT, unix.NETLINK_CAP_ACK
	case "strict check":
		number, option = unix.SYS_SETSOCKOPT, unix.NETLINK_GET_STRICT_CHK
	default:
		require.FailNow(t, "unknown Netlink restriction", operation)
	}
	filters := []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0}, // seccomp_data.nr
	}
	if option == 0 {
		filters = append(filters, unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: number, Jf: 1})
	} else {
		filters = append(filters,
			unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: number, Jf: 3},
			unix.SockFilter{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 32}, // setsockopt's option argument
			unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: option, Jf: 1})
	}
	filters = append(filters,
		unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW})
	restrictThread(t, filters)
}

// Netlink opens a request connection with cleanup and bounded I/O.
func Netlink(t *testing.T, protocol socket.Protocol, options netlink.Options) *netlink.Conn {
	t.Helper()
	conn, err := netlink.Open(protocol, options)
	require.NoError(t, err)
	Cleanup(t, conn)
	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
	return conn
}

// NetlinkGroup discovers a named group through the library's public controller API.
func NetlinkGroup(t *testing.T, family, name string) netlink.Group {
	t.Helper()
	conn := Netlink(t, socket.Generic, netlink.Options{})
	record, err := conn.LookupFamily(family)
	require.NoError(t, err)
	for _, group := range record.Groups {
		if group.Name == name {
			return group
		}
	}
	require.FailNow(t, "required Generic Netlink group is absent", "%s/%s", family, name)
	return netlink.Group{}
}

// NetlinkFamilyDump builds a controller GETFAMILY request using the public codecs.
func NetlinkFamilyDump(t *testing.T) netlink.Message {
	t.Helper()
	data, err := (netlink.GenericMessage{Command: netlink.GetFamilyCommand, Version: 2}).MarshalBinary()
	require.NoError(t, err)
	return netlink.Message{Header: netlink.Header{Type: netlink.ControllerID}, Data: data}
}

// NetlinkFamilyRequest builds a valid read-only controller lookup request.
func NetlinkFamilyRequest(t *testing.T, name string) netlink.Message {
	t.Helper()
	attributes, err := netlink.MarshalAttributes([]netlink.Attribute{{Type: netlink.FamilyNameAttribute, Data: append([]byte(name), 0)}})
	require.NoError(t, err)
	data, err := (netlink.GenericMessage{Command: netlink.GetFamilyCommand, Version: 2, Data: attributes}).MarshalBinary()
	require.NoError(t, err)
	return netlink.Message{Header: netlink.Header{Type: netlink.ControllerID}, Data: data}
}

// DropNetlinkAcknowledgements attaches a kernel socket filter to a test-owned
// descriptor. Data replies still arrive; ACKs are dropped before queueing. This
// exercises pending-reply behavior without production hooks or a fake kernel.
func DropNetlinkAcknowledgements(t *testing.T, fd int) {
	t.Helper()
	kind := uint32(binary.BigEndian.Uint16(binary.NativeEndian.AppendUint16(nil, netlink.ErrorMessage)))
	filters := []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_H | unix.BPF_ABS, K: 4},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: kind, Jf: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: 0},
		{Code: unix.BPF_RET | unix.BPF_K, K: ^uint32(0)},
	}
	require.NoError(t, unix.SetsockoptSockFprog(fd, unix.SOL_SOCKET, unix.SO_ATTACH_FILTER, &unix.SockFprog{Len: 4, Filter: &filters[0]}))
}

// TrimNetlinkDatagrams makes the kernel shorten incoming records on only this
// test socket. The unchanged wire-length field then tests malformed framing,
// rather than Receive's user-buffer MSG_TRUNC path.
func TrimNetlinkDatagrams(t *testing.T, fd int, size uint32) {
	t.Helper()
	filter := unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: size}
	require.NoError(t, unix.SetsockoptSockFprog(fd, unix.SOL_SOCKET, unix.SO_ATTACH_FILTER, &unix.SockFprog{Len: 1, Filter: &filter}))
}

// QueueNetlinkAcknowledgement sends a read-only control request on a test-owned
// socket. The real kernel queues its ACK before the next public API operation.
func QueueNetlinkAcknowledgement(t *testing.T, fd int, sequence uint32) {
	t.Helper()
	SendNetlinkRequest(t, fd, netlink.Message{Header: netlink.Header{
		Type: netlink.Noop, Flags: netlink.Request | netlink.Acknowledge, Sequence: sequence,
	}})
}

// SendNetlinkRequest independently queues a real kernel exchange on a test-owned
// socket for focused protocol-ordering fixtures. It never changes library state.
func SendNetlinkRequest(t *testing.T, fd int, message netlink.Message) {
	t.Helper()
	request, err := netlink.MarshalMessages([]netlink.Message{message})
	require.NoError(t, err)
	n, err := unix.SendmsgN(fd, request, nil, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}, unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL)
	require.NoError(t, err)
	require.Len(t, request, n)
}

// NetlinkLinkListener owns a bounded route notification fixture. Linux route
// group selection stays here until netlink/route provides typed subscriptions.
func NetlinkLinkListener(t *testing.T) *netlink.Listener {
	t.Helper()
	listener, err := netlink.Listen(socket.Route, netlink.Options{}, unix.RTNLGRP_LINK)
	require.NoError(t, err)
	Cleanup(t, listener)
	require.NoError(t, listener.SetReadDeadline(time.Now().Add(5*time.Second)))
	return listener
}

// NetlinkOverflowListener fills a test-owned route listener with private
// loopback notifications. Only this socket's receive buffer is reduced; host
// limits and host network state are never changed. Call in NetworkNamespace.
func NetlinkOverflowListener(t *testing.T) *netlink.Listener {
	t.Helper()
	require.True(t, strings.HasPrefix(os.Args[len(os.Args)-1], "network-isolated="), "network mutation requires the isolated fixture child")
	before := Descriptors(t)
	listener := NetlinkLinkListener(t)
	fd := NewDescriptor(t, before)
	require.NoError(t, unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, 1024))

	producer, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	require.NoError(t, err)
	OwnDescriptor(t, producer, "netlink-overflow-fixture")
	request, err := unix.NewIfreq("lo")
	require.NoError(t, err)
	require.NoError(t, unix.IoctlIfreq(producer, unix.SIOCGIFFLAGS, request))
	for range 32 {
		request.SetUint16(request.Uint16() ^ unix.IFF_UP)
		require.NoError(t, unix.IoctlIfreq(producer, unix.SIOCSIFFLAGS, request))
	}
	return listener
}

// NetlinkLinkDump supplies the native RTM_GETLINK fixed-header fixture.
func NetlinkLinkDump() netlink.Message {
	return netlink.Message{Header: netlink.Header{Type: unix.RTM_GETLINK}, Data: make([]byte, unix.SizeofIfInfomsg)}
}

// NetlinkLinkSet builds an unchanged SETLINK operation for an ACK-only fixture.
// No flags or attributes are changed. Call only in NetworkNamespace.
func NetlinkLinkSet(index int) netlink.Message {
	data := make([]byte, unix.SizeofIfInfomsg)
	binary.NativeEndian.PutUint32(data[4:], uint32(index)) //nolint:gosec // Interface indices fit the Linux u32 field.
	return netlink.Message{Header: netlink.Header{Type: unix.RTM_SETLINK}, Data: data}
}

// NetlinkAddressDump supplies the native IPv4 RTM_GETADDR fixture.
func NetlinkAddressDump(sequence uint32) netlink.Message {
	return netlink.Message{
		Header: netlink.Header{Type: unix.RTM_GETADDR, Flags: netlink.Request | netlink.Acknowledge | netlink.DumpFlags, Sequence: sequence},
		Data:   []byte{unix.AF_INET, 0, 0, 0, 0, 0, 0, 0},
	}
}

// NetlinkLink independently observes a native route record for focused ABI checks.
// Name and the IFF_UP observation are values; native layouts remain in this helper.
func NetlinkLink(message netlink.Message) (string, bool, error) {
	if message.Header.Type != unix.RTM_NEWLINK || len(message.Data) < unix.SizeofIfInfomsg {
		return "", false, os.ErrInvalid
	}
	up := binary.NativeEndian.Uint32(message.Data[8:])&unix.IFF_UP != 0
	attributes, err := netlink.UnmarshalAttributes(message.Data[unix.SizeofIfInfomsg:])
	if err != nil {
		return "", false, err
	}
	for _, attribute := range attributes {
		if attribute.ID() == unix.IFLA_IFNAME {
			name, err := attribute.String()
			return name, up, err
		}
	}
	return "", false, os.ErrInvalid
}

// NetlinkRecord independently encodes a Linux nlmsghdr fixture from public
// header fields. Length zero derives the normal length; nonzero overrides it
// to describe deliberately malformed framing.
func NetlinkRecord(header netlink.Header, length uint32, payload []byte) []byte {
	data := make([]byte, (unix.NLMSG_HDRLEN+len(payload)+3)&^3)
	if length == 0 {
		length = uint32(unix.NLMSG_HDRLEN + len(payload)) //nolint:gosec // Test fixtures are bounded.
	}
	binary.NativeEndian.PutUint32(data, length)
	binary.NativeEndian.PutUint16(data[4:], header.Type)
	binary.NativeEndian.PutUint16(data[6:], uint16(header.Flags))
	binary.NativeEndian.PutUint32(data[8:], header.Sequence)
	binary.NativeEndian.PutUint32(data[12:], header.PortID)
	copy(data[unix.NLMSG_HDRLEN:], payload)
	return data
}

// NetlinkStatus encodes the signed 32-bit kernel acknowledgement status.
func NetlinkStatus(status int32) []byte {
	return binary.NativeEndian.AppendUint32(nil, uint32(status)) //nolint:gosec // Preserve the signed Linux wire representation.
}

// NetlinkAddress independently observes a fixture's bound port for an ABI sender
// authentication check; ordinary callers never need descriptor access.
func NetlinkAddress(t *testing.T, fd int) socket.NetlinkAddress {
	t.Helper()
	address, err := unix.Getsockname(fd)
	require.NoError(t, err)
	peer, ok := address.(*unix.SockaddrNetlink)
	require.True(t, ok, "fixture must own an AF_NETLINK socket")
	return socket.NetlinkAddress{PortID: peer.Pid, Groups: peer.Groups}
}

// BringLoopbackUp uses an independent ioctl fixture before netlink/route exists.
// It requires NetworkNamespace's child and never changes the host interface.
func BringLoopbackUp(t *testing.T) {
	t.Helper()
	require.True(t, strings.HasPrefix(os.Args[len(os.Args)-1], "network-isolated="), "network mutation requires the isolated fixture child")
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	require.NoError(t, err)
	OwnDescriptor(t, fd, "loopback-fixture")
	request, err := unix.NewIfreq("lo")
	require.NoError(t, err)
	require.NoError(t, unix.IoctlIfreq(fd, unix.SIOCGIFFLAGS, request))
	request.SetUint16(request.Uint16() | unix.IFF_UP)
	require.NoError(t, unix.IoctlIfreq(fd, unix.SIOCSIFFLAGS, request))
}

// AddLoopbackAddress creates one fixture address in NetworkNamespace's child.
// Before netlink/route exists, it uses P13's protocol message API directly.
// Namespace teardown removes the address; no host route or address is changed.
func AddLoopbackAddress(t *testing.T, conn *netlink.Conn, index int, ordinal uint16) {
	t.Helper()
	require.True(t, strings.HasPrefix(os.Args[len(os.Args)-1], "network-isolated="), "network mutation requires the isolated fixture child")
	address := []byte{127, 64, byte(ordinal >> 8), byte(ordinal & 0xff)}
	attributes, err := netlink.MarshalAttributes([]netlink.Attribute{
		{Type: unix.IFA_LOCAL, Data: address},
		{Type: unix.IFA_ADDRESS, Data: address},
	})
	require.NoError(t, err)
	data := make([]byte, unix.SizeofIfAddrmsg+len(attributes))
	data[0], data[1], data[3] = unix.AF_INET, 32, unix.RT_SCOPE_HOST
	binary.NativeEndian.PutUint32(data[4:], uint32(index)) //nolint:gosec // Kernel interface index is positive and fits u32.
	copy(data[unix.SizeofIfAddrmsg:], attributes)
	_, err = conn.Execute(netlink.Message{
		Header: netlink.Header{Type: unix.RTM_NEWADDR, Flags: netlink.Create | netlink.Exclusive},
		Data:   data,
	})
	require.NoError(t, err)
}
