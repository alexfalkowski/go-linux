//go:build linux

package test

import (
	"errors"
	"os"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/testenv"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

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
		require.False(t, options.Required, reason)
		t.Skip(reason)
	}
	require.NoError(t, err)
}
