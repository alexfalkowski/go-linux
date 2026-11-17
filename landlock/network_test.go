//go:build linux

package landlock_test

import (
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/landlock"
	"github.com/stretchr/testify/require"
)

func TestTCPConnectAllowsOnlySelectedPort(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	allowed := test.TCPListener(t)
	denied := test.TCPListener(t)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledNet: landlock.ConnectTCP})
	port := allowed.Addr().(*net.TCPAddr).AddrPort().Port()
	require.NoError(t, ruleset.AddPort(port, landlock.ConnectTCP))

	require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread, NoNewPrivileges: true}))

	dialer := net.Dialer{Timeout: time.Second}
	connection, err := dialer.DialContext(t.Context(), "tcp4", allowed.Addr().String())
	require.NoError(t, err)
	require.NoError(t, connection.Close())
	connection, err = dialer.DialContext(t.Context(), "tcp4", denied.Addr().String())
	require.ErrorIs(t, err, test.ErrPermissionDenied)
	require.Nil(t, connection)
}

func TestTCPBindZeroAllowsEphemeralButNotExplicitPort(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	denied := test.TCPListener(t)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledNet: landlock.BindTCP})
	require.NoError(t, ruleset.AddPort(0, landlock.BindTCP))

	require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread, NoNewPrivileges: true}))

	allowed := test.TCPListener(t)
	require.Positive(t, allowed.Addr().(*net.TCPAddr).Port)
	var config net.ListenConfig
	listener, err := config.Listen(t.Context(), "tcp4", denied.Addr().String())
	require.ErrorIs(t, err, test.ErrPermissionDenied, "port zero is not a wildcard; denial precedes address-in-use")
	require.Nil(t, listener)
}

func TestUDPOptionalRightsEnforceDestinationPort(t *testing.T) {
	test.LandlockAvailable(t, 10)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	allowed := test.UDPPeer(t)
	denied := test.UDPPeer(t)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledNet: landlock.BindUDP | landlock.ConnectSendUDP})
	require.NoError(t, ruleset.AddPort(0, landlock.BindUDP))
	port := allowed.LocalAddr().(*net.UDPAddr).AddrPort().Port()
	require.NoError(t, ruleset.AddPort(port, landlock.ConnectSendUDP))

	require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread, NoNewPrivileges: true}))

	sender := test.UDPPeer(t)
	_, err := sender.WriteToUDP([]byte("allowed"), allowed.LocalAddr().(*net.UDPAddr))
	require.NoError(t, err)
	buffer := make([]byte, 32)
	count, _, err := allowed.ReadFromUDP(buffer)
	require.NoError(t, err)
	require.Equal(t, "allowed", string(buffer[:count]))
	_, err = sender.WriteToUDP([]byte("denied"), denied.LocalAddr().(*net.UDPAddr))
	require.ErrorIs(t, err, test.ErrPermissionDenied)
}

func TestAbstractUnixScopeDeniesOutsideDomainAndAllowsInside(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	outside := test.LandlockUnixPeer(t, true)
	ruleset := test.LandlockRuleset(t, landlock.Config{Scoped: landlock.AbstractUnixSocket})

	require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread, NoNewPrivileges: true}))

	inside := test.LandlockUnixPeer(t, true)
	connection, err := net.DialUnix("unixgram", nil, inside.LocalAddr().(*net.UnixAddr))
	require.NoError(t, err)
	require.NoError(t, connection.Close())
	connection, err = net.DialUnix("unixgram", nil, outside.LocalAddr().(*net.UnixAddr))
	require.ErrorIs(t, err, test.ErrOperationNotPermitted)
	require.Nil(t, connection)
}

func TestResolveUnixOptionalRightAllowsSelectedPath(t *testing.T) {
	test.LandlockAvailable(t, 9)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	allowed := test.LandlockUnixPeer(t, false)
	denied := test.LandlockUnixPeer(t, false)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ResolveUnix})
	require.NoError(t, ruleset.AddPath(filepath.Dir(allowed.LocalAddr().String()), landlock.ResolveUnix))

	require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread, NoNewPrivileges: true}))

	connection, err := net.DialUnix("unixgram", nil, allowed.LocalAddr().(*net.UnixAddr))
	require.NoError(t, err)
	require.NoError(t, connection.Close())
	connection, err = net.DialUnix("unixgram", nil, denied.LocalAddr().(*net.UnixAddr))
	require.ErrorIs(t, err, test.ErrPermissionDenied)
	require.Nil(t, connection)
}
