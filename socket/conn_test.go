//go:build linux

package socket_test

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/socket"
	"github.com/stretchr/testify/require"
)

var _ io.Closer = (*socket.Conn)(nil)

func TestPairsPreserveMessagesAndExposeDiscardedTails(t *testing.T) {
	for _, mode := range []socket.Kind{socket.Datagram, socket.SeqPacket} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			a, b := test.MessagePair(t, mode)
			require.Equal(t, socket.UnixAddress{}, test.Value(t, a.LocalAddress), "pairs begin unnamed")

			for _, contents := range []string{"long message", "next", ""} {
				n, err := a.Send([]byte(contents), nil)
				require.NoError(t, err)
				require.Equal(t, len(contents), n)
			}

			buffer := make([]byte, 4)
			message, err := b.Receive(buffer, 0)
			require.NoError(t, err)
			require.Equal(t, "long", string(buffer[:message.N]))
			require.NotZero(t, message.Flags&socket.Truncated, "discarded tail must be reported")

			message, err = b.Receive(buffer, 0)
			require.NoError(t, err)
			require.Equal(t, "next", string(buffer[:message.N]))
			require.Zero(t, message.Flags&socket.Truncated, "exact-size record is complete")

			message, err = b.Receive(buffer, 0)
			require.NoError(t, err)
			require.Zero(t, message.N, "empty message must be consumed")
		})
	}
}

func TestEmptyBuffersConsumeOneMessageAndReportTruncation(t *testing.T) {
	for _, mode := range []socket.Kind{socket.Datagram, socket.SeqPacket} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			a, b := test.MessagePair(t, mode)
			for _, payload := range []string{"discard", "\x00", ""} {
				_, err := a.Send([]byte(payload), nil)
				require.NoError(t, err)

				message, err := b.Receive(nil, 0)
				require.NoError(t, err)
				require.Zero(t, message.N, "empty caller buffer cannot contain copied bytes")
				require.Equal(t, payload != "", message.Flags&socket.Truncated != 0, "payload %q", payload)
			}

			_, err := a.Send([]byte("next"), nil)
			require.NoError(t, err)

			buffer := bytes.Repeat([]byte{'!'}, 16)
			message, err := b.Receive(buffer, 0)
			require.NoError(t, err)
			require.Equal(t, "next!!!!!!!!!!!!", string(buffer))
			require.Equal(t, 4, message.N)
		})
	}
}

func TestBoundUnixDatagramsReportSenderAndRetainFilesystemPaths(t *testing.T) {
	directory := t.TempDir()
	addresses := []socket.UnixAddress{
		{Name: filepath.Join(directory, "sender")},
		{Name: "@go-linux-" + filepath.Join(directory, "receiver")},
	}
	a, err := socket.ListenUnix(addresses[0])
	require.NoError(t, err)
	test.Cleanup(t, a)

	b, err := socket.ListenUnix(addresses[1])
	require.NoError(t, err)
	test.Cleanup(t, b)
	require.NoError(t, b.SetDeadline(time.Now().Add(time.Second)))
	require.Equal(t, addresses[1], test.Value(t, b.LocalAddress))

	_, err = a.Send([]byte("hello"), addresses[1])
	require.NoError(t, err)

	message, err := b.Receive(make([]byte, 16), 0)
	require.NoError(t, err)
	require.Equal(t, addresses[0], message.From)
	require.Equal(t, "unixgram", message.From.Network())

	require.NoError(t, a.Close())

	_, err = os.Lstat(addresses[0].Name)
	require.NoError(t, err, "Close must leave caller-managed paths")
}

func TestFailedBindReturnsNoOwnerAndPreservesExistingSocket(t *testing.T) {
	address := socket.UnixAddress{Name: filepath.Join(t.TempDir(), "socket")}
	a, err := socket.ListenUnix(address)
	require.NoError(t, err)
	test.Cleanup(t, a)

	before := test.DescriptorCount(t)
	for range 16 {
		b, err := socket.ListenUnix(address)
		test.NoResource(t, b, err, test.ErrAddressInUse)
	}

	require.Equal(t, before, test.DescriptorCount(t), "failed binds must close newly acquired sockets")
	require.Equal(t, address, test.Value(t, a.LocalAddress))
}

func TestAbstractUnixAddressesPreserveBinaryNamesAndReplyDestinations(t *testing.T) {
	prefix := fmt.Sprintf("@go-linux-binary-%d-", os.Getpid())
	addresses := []socket.UnixAddress{
		{Name: prefix + "\x00sender\x00"},
		{Name: prefix + "\x00one"},
		{Name: prefix + "\x00two"},
	}
	sockets := make([]*socket.Conn, 0, len(addresses))
	for _, address := range addresses {
		conn, err := socket.ListenUnix(address)
		require.NoError(t, err, "binary suffixes must identify distinct addresses")
		test.Cleanup(t, conn)
		require.NoError(t, conn.SetDeadline(time.Now().Add(time.Second)))
		require.Equal(t, address, test.Value(t, conn.LocalAddress))

		sockets = append(sockets, conn)
	}

	for i := 1; i < len(sockets); i++ {
		_, err := sockets[0].Send([]byte{byte(i)}, addresses[i])
		require.NoError(t, err)

		buffer := make([]byte, 1)
		message, err := sockets[i].Receive(buffer, 0)
		require.NoError(t, err)
		require.Equal(t, byte(i), buffer[0])
		require.Equal(t, addresses[0], message.From, "sender suffix and trailing NUL must survive recvmsg")
		require.Equal(t, addresses[0].Name, message.From.String())

		_, err = sockets[i].Send(buffer, message.From)
		require.NoError(t, err)

		message, err = sockets[0].Receive(buffer, 0)
		require.NoError(t, err)
		require.Equal(t, addresses[i], message.From)
	}

	alias := socket.UnixAddress{Name: "\x00" + addresses[0].Name[1:]}
	conn, err := socket.ListenUnix(alias)
	test.NoResource(t, conn, err, test.ErrAddressInUse)
}

func TestAuthenticatedRepliesPreserveNativeUnixPathnameNamespaces(t *testing.T) {
	for _, length := range []int{32, 107} {
		t.Run(strconv.Itoa(length), func(t *testing.T) {
			t.Chdir(t.TempDir())
			prefix := fmt.Sprintf("@reply-%d-", os.Getpid())
			name := prefix + strings.Repeat("x", length-len(prefix))
			peer := test.UnixPathPeer(t, name)
			require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))

			impostor, err := socket.ListenUnix(socket.UnixAddress{Name: name})
			require.NoError(t, err)
			test.Cleanup(t, impostor)

			server, err := socket.ListenUnix(socket.UnixAddress{Name: "service"})
			require.NoError(t, err)
			test.Cleanup(t, server)
			require.NoError(t, server.EnablePassCredentials())
			require.NoError(t, server.SetDeadline(time.Now().Add(time.Second)))

			_, err = peer.WriteToUnix([]byte("request"), &net.UnixAddr{Name: "service", Net: "unixgram"})
			require.NoError(t, err)

			message, err := server.Receive(make([]byte, 32), 0)
			require.NoError(t, err)
			require.Equal(t, socket.UnixAddress{Name: name, Interpretation: socket.LiteralPathname}, message.From, "reply addresses must retain their filesystem namespace")
			require.NotNil(t, message.Credentials)
			require.EqualValues(t, os.Getpid(), message.Credentials.PID, "authenticate the native request's process")
			require.EqualValues(t, os.Getuid(), message.Credentials.UID, "authenticate the native request's user")

			file := test.TempFile(t, 16)
			test.WriteAt(t, file, 0, "private reply")
			_, err = server.Send([]byte("reply"), message.From, file)
			require.NoError(t, err)

			payload, files := test.ReadUnixFiles(t, peer)
			require.Equal(t, "reply", string(payload))
			require.Len(t, files, 1)
			contents := make([]byte, len("private reply"))
			_, err = files[0].ReadAt(contents, 0)
			require.NoError(t, err)
			require.Equal(t, "private reply", string(contents))

			require.NoError(t, impostor.SetReadDeadline(time.Now().Add(20*time.Millisecond)))
			unexpected, err := impostor.Receive(make([]byte, 32), 1)
			for _, file := range unexpected.Files {
				test.Cleanup(t, file)
			}
			require.ErrorIs(t, err, os.ErrDeadlineExceeded, "the colliding abstract endpoint must receive neither payload nor rights")
		})
	}
}

func TestLiteralUnixPathnamesPreserveTheFullKernelLength(t *testing.T) {
	for _, prefix := range []string{"@", "p"} {
		t.Run(prefix, func(t *testing.T) {
			t.Chdir(t.TempDir())
			address := socket.UnixAddress{Name: prefix + strings.Repeat("x", 107), Interpretation: socket.LiteralPathname}
			peer, err := socket.ListenUnix(address)
			require.NoError(t, err)
			test.Cleanup(t, peer)
			require.NoError(t, peer.SetDeadline(time.Now().Add(time.Second)))
			require.Equal(t, address, test.Value(t, peer.LocalAddress))

			before := test.DescriptorCount(t)
			duplicate, err := socket.ListenUnix(address)
			test.NoResource(t, duplicate, err, test.ErrAddressInUse)
			require.Equal(t, before, test.DescriptorCount(t), "failed literal pathname binds must release their descriptor")

			server, err := socket.ListenUnix(socket.UnixAddress{Name: "service"})
			require.NoError(t, err)
			test.Cleanup(t, server)
			require.NoError(t, server.SetDeadline(time.Now().Add(time.Second)))

			_, err = peer.Send(nil, socket.UnixAddress{Name: "service"})
			require.NoError(t, err)
			message, err := server.Receive(nil, 0)
			require.NoError(t, err)
			require.Equal(t, address, message.From)
			require.Zero(t, message.N, "empty pathname datagrams must retain their record semantics")

			_, err = server.Send([]byte("reply"), message.From)
			require.NoError(t, err)
			buffer := make([]byte, 16)
			message, err = peer.Receive(buffer, 0)
			require.NoError(t, err)
			require.Equal(t, "reply", string(buffer[:message.N]))
		})
	}
}

func TestFailedLiteralPathnameSendsReleaseBorrowedDuplicates(t *testing.T) {
	t.Chdir(t.TempDir())
	server, err := socket.ListenUnix(socket.UnixAddress{Name: "service"})
	require.NoError(t, err)
	test.Cleanup(t, server)
	file := test.TempFile(t, 4)
	before := test.DescriptorCount(t)

	_, err = server.Send([]byte("missing"), socket.UnixAddress{Name: "@missing", Interpretation: socket.LiteralPathname}, file)
	require.ErrorIs(t, err, test.ErrNotExist)
	require.Equal(t, before, test.DescriptorCount(t))

	test.WriteAt(t, file, 0, "open")
}

func TestReadDeadlinesInterruptPendingReceivesAndCanBeCleared(t *testing.T) {
	a, b := test.MessagePair(t, socket.Datagram)
	pending := test.StartRead(t, func() (socket.Message, error) {
		return b.Receive(make([]byte, 8), 0)
	})
	require.NoError(t, b.SetReadDeadline(time.Now().Add(-time.Second)))
	require.ErrorIs(t, test.Receive(t, pending).Err, os.ErrDeadlineExceeded)

	require.NoError(t, b.SetReadDeadline(time.Time{}))

	_, err := a.Send([]byte("ready"), nil)
	require.NoError(t, err)

	message, err := b.Receive(make([]byte, 8), 0)
	require.NoError(t, err)
	require.Equal(t, 5, message.N)
}

func TestWriteDeadlinesAndCloseInterruptFullQueues(t *testing.T) {
	for _, trigger := range []string{"deadline", "close"} {
		t.Run(trigger, func(t *testing.T) {
			a, b := test.MessagePair(t, socket.Datagram)
			require.NoError(t, a.SetWriteBuffer(4096))

			payload := bytes.Repeat([]byte{'x'}, 1024)
			require.NoError(t, a.SetWriteDeadline(time.Now().Add(30*time.Millisecond)))

			for range 128 {
				_, err := a.Send(payload, nil)
				if err != nil {
					require.ErrorIs(t, err, os.ErrDeadlineExceeded)
					break
				}
			}

			require.NoError(t, a.SetWriteDeadline(time.Time{}))

			pending := test.StartRead(t, func() (int, error) {
				return a.Send(payload, nil)
			})
			if trigger == "close" {
				require.NoError(t, a.Close())
				require.ErrorIs(t, test.Receive(t, pending).Err, os.ErrClosed)
				return
			}

			require.NoError(t, a.SetWriteDeadline(time.Now().Add(-time.Second)))
			require.ErrorIs(t, test.Receive(t, pending).Err, os.ErrDeadlineExceeded)

			require.NoError(t, a.SetWriteDeadline(time.Time{}))

			_, err := b.Receive(make([]byte, 1024), 0)
			require.NoError(t, err)

			_, err = a.Send(payload, nil)
			require.NoError(t, err)
		})
	}
}

func TestCloseInterruptsAllPendingReceivers(t *testing.T) {
	_, b := test.MessagePair(t, socket.SeqPacket)
	first := test.StartRead(t, func() (socket.Message, error) {
		return b.Receive(make([]byte, 8), 0)
	})
	second := test.StartRead(t, func() (socket.Message, error) {
		return b.Receive(make([]byte, 8), 0)
	})
	require.NoError(t, b.Close())
	require.ErrorIs(t, test.Receive(t, first).Err, os.ErrClosed)
	require.ErrorIs(t, test.Receive(t, second).Err, os.ErrClosed)
}

func TestAddressedUnixBackpressureReturnsEAGAINAndRetainsBorrowedFiles(t *testing.T) {
	for _, destination := range []socket.UnixAddress{
		{Name: "receiver"},
		{Name: "@receiver", Interpretation: socket.LiteralPathname},
	} {
		t.Run(destination.Name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			receiver, err := socket.ListenUnix(destination)
			require.NoError(t, err)
			test.Cleanup(t, receiver)
			require.NoError(t, receiver.SetDeadline(time.Now().Add(5*time.Second)))
			queued := test.FillUnixDatagramQueue(t, "./"+destination.Name)

			sender, err := socket.ListenUnix(socket.UnixAddress{Name: "sender"})
			require.NoError(t, err)
			test.Cleanup(t, sender)
			file := test.TempFile(t, 8)
			test.WriteAt(t, file, 0, "borrowed")
			before := test.DescriptorCount(t)
			require.NoError(t, sender.SetWriteDeadline(time.Now().Add(time.Second)))

			n, err := sender.Send([]byte("retry"), destination, file)
			require.ErrorIs(t, err, test.ErrTryAgain, "an unconnected sender has no destination-capacity polling notification")
			require.Zero(t, n, "EAGAIN must not send a partial record")
			require.Equal(t, before, test.DescriptorCount(t), "EAGAIN must close all temporary file duplicates")

			buffer := make([]byte, 32)
			_, err = receiver.Receive(buffer, 0)
			require.NoError(t, err)
			require.NoError(t, sender.SetWriteDeadline(time.Time{}))

			n, err = sender.Send([]byte("retry"), destination, file)
			require.NoError(t, err, "the caller can retry after destination capacity is available")
			require.Equal(t, len("retry"), n)
			for range queued - 1 {
				message, err := receiver.Receive(buffer, 0)
				require.NoError(t, err)
				require.Equal(t, "x", string(buffer[:message.N]), "queued records must precede the retry")
			}

			message, err := receiver.Receive(buffer, 1)
			require.NoError(t, err)
			require.Equal(t, "retry", string(buffer[:message.N]))
			require.Len(t, message.Files, 1)
			test.Cleanup(t, message.Files[0])
			contents := make([]byte, 8)
			_, err = message.Files[0].ReadAt(contents, 0)
			require.NoError(t, err)
			require.Equal(t, "borrowed", string(contents), "the failed attempt must leave its borrowed input usable")
		})
	}
}

func TestZeroAndClosedSocketsRejectOperations(t *testing.T) {
	a, _ := test.MessagePair(t, socket.Datagram)
	require.NoError(t, a.Close())

	for _, scenario := range []struct {
		name string
		conn *socket.Conn
		err  error
	}{
		{name: "zero", conn: &socket.Conn{}, err: os.ErrInvalid},
		{name: "closed", conn: a, err: os.ErrClosed},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			_, err := scenario.conn.Send(nil, nil)
			require.ErrorIs(t, err, scenario.err)

			_, err = scenario.conn.Receive(nil, 0)
			require.ErrorIs(t, err, scenario.err)

			_, err = scenario.conn.LocalAddress()
			require.ErrorIs(t, err, scenario.err)

			require.ErrorIs(t, scenario.conn.Close(), scenario.err)

			require.ErrorIs(t, scenario.conn.SetDeadline(time.Time{}), scenario.err)
			require.ErrorIs(t, scenario.conn.SetReadDeadline(time.Time{}), scenario.err)
			require.ErrorIs(t, scenario.conn.SetWriteDeadline(time.Time{}), scenario.err)

			require.ErrorIs(t, scenario.conn.SetReadBuffer(4096), scenario.err)
			require.ErrorIs(t, scenario.conn.SetWriteBuffer(4096), scenario.err)
		})
	}
}

func TestInvalidSocketConstructorsReturnNoOwners(t *testing.T) {
	before := test.Descriptors(t)

	x, y, err := socket.Pair(socket.Kind(-1))
	require.ErrorIs(t, err, os.ErrInvalid)
	require.Nil(t, x)
	require.Nil(t, y)

	conn, err := socket.OpenNetlink(-1, socket.NetlinkAddress{})
	test.NoResource(t, conn, err, os.ErrInvalid)
	require.Equal(t, before, test.Descriptors(t), "failed constructors must release every descriptor")
}

func TestListenUnixRejectsInvalidAddresses(t *testing.T) {
	before := test.Descriptors(t)
	for name, address := range map[string]socket.UnixAddress{
		"empty":             {},
		"NUL in path":       {Name: "bad\x00path"},
		"long path":         {Name: strings.Repeat("x", 108)},
		"unknown mode":      {Name: "valid", Interpretation: 255},
		"empty literal":     {Interpretation: socket.LiteralPathname},
		"NUL in literal":    {Name: "\x00abstract", Interpretation: socket.LiteralPathname},
		"NUL after at sign": {Name: "@bad\x00path", Interpretation: socket.LiteralPathname},
		"long literal":      {Name: strings.Repeat("x", 109), Interpretation: socket.LiteralPathname},
	} {
		t.Run(name, func(t *testing.T) {
			conn, err := socket.ListenUnix(address)
			test.NoResource(t, conn, err, os.ErrInvalid)
		})
	}
	require.Equal(t, before, test.Descriptors(t), "invalid addresses must not retain descriptors")
}

func TestSendRejectsWrongAddressTypes(t *testing.T) {
	sender, _ := test.MessagePair(t, socket.Datagram)
	before := test.DescriptorCount(t)

	_, err := sender.Send(nil, socket.NetlinkAddress{})
	require.ErrorIs(t, err, os.ErrInvalid)
	_, err = sender.Send(nil, &socket.UnixAddress{Name: "@test"})
	require.ErrorIs(t, err, os.ErrInvalid)

	require.Equal(t, before, test.DescriptorCount(t), "invalid address types must not retain descriptors")
}

func TestSendRejectsInvalidUnixDestinations(t *testing.T) {
	sender, _ := test.MessagePair(t, socket.Datagram)
	before := test.DescriptorCount(t)
	for name, address := range map[string]socket.UnixAddress{
		"unknown mode":      {Name: "valid", Interpretation: 255},
		"empty literal":     {Interpretation: socket.LiteralPathname},
		"NUL in literal":    {Name: "\x00abstract", Interpretation: socket.LiteralPathname},
		"NUL after at sign": {Name: "@bad\x00path", Interpretation: socket.LiteralPathname},
		"long literal":      {Name: strings.Repeat("x", 109), Interpretation: socket.LiteralPathname},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := sender.Send([]byte("invalid"), address)
			require.ErrorIs(t, err, os.ErrInvalid)
		})
	}
	require.Equal(t, before, test.DescriptorCount(t), "failed sends must not retain descriptors")
}

func TestSocketBufferSizesMustBePositive(t *testing.T) {
	conn, _ := test.MessagePair(t, socket.Datagram)
	before := test.DescriptorCount(t)

	require.ErrorIs(t, conn.SetReadBuffer(0), os.ErrInvalid)
	require.ErrorIs(t, conn.SetWriteBuffer(-1), os.ErrInvalid)
	require.Equal(t, before, test.DescriptorCount(t), "invalid buffer sizes must not retain descriptors")
}

func TestUnixSocketsRejectNetlinkOptions(t *testing.T) {
	conn, _ := test.MessagePair(t, socket.Datagram)
	before := test.DescriptorCount(t)

	require.ErrorIs(t, conn.JoinGroup(1), os.ErrInvalid)
	require.ErrorIs(t, conn.LeaveGroup(1), os.ErrInvalid)
	require.ErrorIs(t, conn.EnableExtendedAck(), os.ErrInvalid)
	require.ErrorIs(t, conn.EnableCapAck(), os.ErrInvalid)
	require.ErrorIs(t, conn.EnableStrictCheck(), os.ErrInvalid)
	require.Equal(t, before, test.DescriptorCount(t), "invalid socket options must not retain descriptors")
}

func TestInvalidReceiveLimitsLeaveMessageQueued(t *testing.T) {
	sender, receiver := test.MessagePair(t, socket.Datagram)
	before := test.DescriptorCount(t)
	_, err := sender.Send([]byte("keep"), nil)
	require.NoError(t, err)

	for _, limit := range []int{-1, 254} {
		_, err := receiver.Receive(nil, limit)
		require.ErrorIs(t, err, os.ErrInvalid)
	}

	buffer := make([]byte, 8)
	message, err := receiver.Receive(buffer, 0)
	require.NoError(t, err)
	require.Equal(t, "keep", string(buffer[:message.N]), "invalid limits must leave the message intact")
	require.Equal(t, before, test.DescriptorCount(t), "invalid limits must not retain descriptors")
}

func TestSocketCreationAndPairFailureReleaseAcquiredDescriptors(t *testing.T) {
	if len(os.Args) > 1 && os.Args[len(os.Args)-1] == "socket-exhaustion" {
		// Initialize Go's process-wide poller before exhausting its descriptor slots.
		initial, _ := test.MessagePair(t, socket.Datagram)
		file := test.TempFile(t, 0)
		release := test.ExhaustDescriptors(t)
		a, b, err := socket.Pair(socket.Datagram)
		require.ErrorIs(t, err, test.ErrTooManyOpenFiles)
		require.Nil(t, a)
		require.Nil(t, b)

		_, err = initial.Send([]byte("no duplicate slot"), nil, file)
		require.ErrorIs(t, err, test.ErrTooManyOpenFiles)

		release()
		// Only one slot is free: socketpair must clean up its partial acquisition.
		a, b, err = socket.Pair(socket.Datagram)
		require.ErrorIs(t, err, test.ErrTooManyOpenFiles)
		require.Nil(t, a)
		require.Nil(t, b)

		_, err = initial.Send([]byte("only one duplicate slot"), nil, file, file)
		require.ErrorIs(t, err, test.ErrTooManyOpenFiles, "failure after the first duplicate must release it")

		conn, err := socket.OpenNetlink(socket.Route, socket.NetlinkAddress{})
		require.NoError(t, err, "the free slot must remain available after failed pair creation")

		require.NoError(t, conn.Close())
		return
	}

	ctx, command := test.Command(t, t.Name(), "socket-exhaustion")
	output, err := command.CombinedOutput()
	require.NoError(t, ctx.Err())
	require.NoError(t, err, "descriptor-exhaustion subprocess: %s", output)
}

func TestPollRegistrationFailureReturnsNoSocketsAndClosesDescriptors(t *testing.T) {
	const child = "poll-registration-denied"
	if os.Args[len(os.Args)-1] == child {
		test.RefusePollRegistration(t)
		before := test.DescriptorCount(t)
		for _, mode := range []socket.Kind{socket.Datagram, socket.SeqPacket} {
			a, b, err := socket.Pair(mode)
			test.NoResource(t, a, err, os.ErrNoDeadline)
			test.NoResource(t, b, err, os.ErrNoDeadline)
			require.Equal(t, before, test.DescriptorCount(t), "failed poll registration must close both acquired sockets")
		}

		conn, err := socket.ListenUnix(socket.UnixAddress{Name: "@poll-registration-denied"})
		test.NoResource(t, conn, err, os.ErrNoDeadline)
		require.Equal(t, before, test.DescriptorCount(t), "failed poll registration must close the Unix socket")
		conn, err = socket.OpenNetlink(socket.Generic, socket.NetlinkAddress{})
		test.NoResource(t, conn, err, os.ErrNoDeadline)
		require.Equal(t, before, test.DescriptorCount(t), "failed poll registration must close the Netlink socket")
		return
	}
	ctx, command := test.Command(t, t.Name(), child)
	output, err := command.CombinedOutput()
	require.NoError(t, ctx.Err())
	require.NoError(t, err, "poll-registration subprocess: %s", output)
}
