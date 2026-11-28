//go:build linux

package test

import (
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/alexfalkowski/go-linux/internal/testenv"
	"github.com/alexfalkowski/go-linux/socket"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// FillUnixDatagramQueue fills a filesystem endpoint from independent native
// producers, keeping their queued references alive until test cleanup. It reads
// the existing host limit and never changes it; generation is bounded at 65537.
func FillUnixDatagramQueue(t *testing.T, name string) int {
	t.Helper()
	value, err := os.ReadFile("/proc/sys/net/unix/max_dgram_qlen")
	require.NoError(t, err)
	limit, err := strconv.Atoi(strings.TrimSpace(string(value)))
	require.NoError(t, err)
	require.GreaterOrEqual(t, limit, 0, "Unix datagram queue limit must be nonnegative")
	if limit > 65536 {
		options, err := testenv.FromEnvironment()
		require.NoError(t, err)
		require.False(t, options.Required(), "backpressure fixture requires max_dgram_qlen <= 65536; observed %d", limit)
		t.Skipf("backpressure fixture bounds queue generation; host max_dgram_qlen=%d", limit)
	}

	var queued int
	for queued <= limit {
		fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
		require.NoError(t, err)
		OwnDescriptor(t, fd, "unix-queue-producer")
		require.NoError(t, unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_SNDBUF, 1<<20))

		before := queued
		for queued <= limit {
			err := unix.Sendmsg(fd, []byte{'x'}, nil, &unix.SockaddrUnix{Name: name}, unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL)
			if errors.Is(err, unix.EAGAIN) {
				break
			}
			require.NoError(t, err)
			queued++
		}
		require.Greater(t, queued, before, "a fresh producer must make progress before reaching the observed queue limit")
	}
	return queued
}

// UnixPathPeer binds a native Unix datagram pathname without interpreting @.
// It independently exercises the kernel address format used by non-Go peers.
func UnixPathPeer(t *testing.T, name string) *net.UnixConn {
	t.Helper()
	require.NotEmpty(t, name)

	var address unix.RawSockaddrUnix
	require.LessOrEqual(t, len(name), len(address.Path))
	address.Family = unix.AF_UNIX
	for i := range len(name) {
		address.Path[i] = int8(name[i]) //nolint:gosec // Preserve each pathname byte in the signed ABI array.
	}
	size := 2 + len(name)
	if len(name) < len(address.Path) {
		size++
	}

	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	require.NoError(t, err)
	file := OwnDescriptor(t, fd, "native-unix-path")

	_, _, errno := unix.Syscall(unix.SYS_BIND, uintptr(fd), uintptr(unsafe.Pointer(&address)), uintptr(size))
	require.Zero(t, errno, "bind native pathname")

	connection, err := net.FilePacketConn(file)
	require.NoError(t, err)
	Cleanup(t, connection)
	require.NoError(t, file.Close())

	peer, ok := connection.(*net.UnixConn)
	require.True(t, ok, "a Unix datagram descriptor must produce a Unix connection")
	require.NoError(t, peer.SetDeadline(time.Now().Add(5*time.Second)))
	return peer
}

// ReadUnixFiles receives a native peer's payload and owns all received rights
// for test cleanup, leaving the scenario's file-content assertions to its caller.
func ReadUnixFiles(t *testing.T, peer *net.UnixConn) ([]byte, []*os.File) {
	t.Helper()
	buffer := make([]byte, 128)
	control := make([]byte, unix.CmsgSpace(253*4))
	n, controlN, flags, _, err := peer.ReadMsgUnix(buffer, control)
	require.NoError(t, err)
	require.Zero(t, flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC), "native reply must fit its receive buffers")

	messages, err := unix.ParseSocketControlMessage(control[:controlN])
	require.NoError(t, err)
	var files []*os.File
	for _, message := range messages {
		fds, err := unix.ParseUnixRights(&message)
		require.NoError(t, err)
		for _, fd := range fds {
			files = append(files, OwnDescriptor(t, fd, "native-unix-right"))
		}
	}
	return buffer[:n], files
}

// MessagePair creates typed Unix peers with cleanup and bounded I/O.
func MessagePair(t *testing.T, mode socket.Kind) (*socket.Conn, *socket.Conn) {
	t.Helper()
	a, b, err := socket.Pair(mode)
	require.NoError(t, err)
	Cleanup(t, a)
	Cleanup(t, b)
	require.NoError(t, a.SetDeadline(time.Now().Add(5*time.Second)))
	require.NoError(t, b.SetDeadline(time.Now().Add(5*time.Second)))
	return a, b
}

// NetlinkSocket creates a bound transport endpoint with cleanup and bounded I/O.
func NetlinkSocket(t *testing.T, protocol socket.Protocol) *socket.Conn {
	t.Helper()
	conn, err := socket.OpenNetlink(protocol, socket.NetlinkAddress{})
	require.NoError(t, err)
	Cleanup(t, conn)
	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
	return conn
}

// SocketPair creates owned local stream files for focused Linux readiness
// checks. Both ends are atomically nonblocking and close-on-exec.
func SocketPair(t *testing.T) (*os.File, *os.File) {
	t.Helper()

	descriptors, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	require.NoError(t, err)
	return OwnDescriptor(t, descriptors[0], "socket"), OwnDescriptor(t, descriptors[1], "socket-peer")
}

// ShutdownWrite supplies a peer half-close for a focused EPOLLRDHUP check.
func ShutdownWrite(t *testing.T, file *os.File) {
	t.Helper()

	var err error
	FileControl(t, file, func(fd uintptr) {
		err = unix.Shutdown(int(fd), unix.SHUT_WR)
	})
	require.NoError(t, err)
}

// SendUrgent supplies one out-of-band byte for a focused EPOLLPRI check.
// It requires CONFIG_AF_UNIX_OOB in Linux 7.0's net/unix/af_unix.c; required
// profiles fail rather than skip when this fixture prerequisite is absent.
// https://github.com/torvalds/linux/blob/v7.0/net/unix/af_unix.c
func SendUrgent(t *testing.T, file *os.File) {
	t.Helper()

	var err error
	FileControl(t, file, func(fd uintptr) {
		err = unix.Sendmsg(int(fd), []byte{'!'}, nil, nil, unix.MSG_OOB)
	})
	if errors.Is(err, unix.EOPNOTSUPP) {
		options, optionsErr := testenv.FromEnvironment()
		require.NoError(t, optionsErr)
		const reason = "EPOLLPRI socket fixture: sendmsg(MSG_OOB) returned EOPNOTSUPP; requires CONFIG_AF_UNIX_OOB; profile=rootless"
		require.False(t, options.Required(), reason)
		t.Skip(reason)
	}
	require.NoError(t, err)
}
