//go:build linux

package landlock_test

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/landlock"
	"github.com/stretchr/testify/require"
)

func TestDeviceIOCTLAllowanceIsDeterminedWhenOpened(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		allow bool
	}{
		{name: "denied"},
		{name: "allowed", allow: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			test.LandlockAvailable(t, 8)
			if !test.LandlockProcess(t) {
				return
			}
			test.LockThread()
			before := test.LandlockTerminal(t)
			_, err := test.TerminalNumber(t, before)
			require.NoError(t, err, "the selected device ioctl must work before restriction")
			ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.IOCTLDev})
			if scenario.allow {
				require.NoError(t, ruleset.AddPath("/dev/ptmx", landlock.IOCTLDev))
			}

			require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread, NoNewPrivileges: true}))

			after := test.LandlockTerminal(t)
			_, err = test.TerminalNumber(t, after)
			if scenario.allow {
				require.NoError(t, err, "the allowed device must retain driver-specific ioctl access")
			} else {
				require.ErrorIs(t, err, test.ErrPermissionDenied)
			}
			_, err = test.TerminalNumber(t, before)
			require.NoError(t, err, "restriction must preserve a device descriptor's preexisting authority")
		})
	}
}

func TestFIFOCreationRequiresMakeFIFO(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	allowed, denied := test.LandlockFiles(t)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.MakeFIFO | landlock.MakeSock})
	require.NoError(t, ruleset.AddPath(filepath.Dir(allowed), landlock.MakeFIFO))
	require.NoError(t, ruleset.AddPath(filepath.Dir(denied), landlock.MakeSock))

	require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread, NoNewPrivileges: true}))

	path := filepath.Join(filepath.Dir(allowed), "fifo")
	require.NoError(t, test.MakeFIFO(path))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.ModeNamedPipe, info.Mode().Type())
	require.ErrorIs(t, test.MakeFIFO(filepath.Join(filepath.Dir(denied), "fifo")), test.ErrPermissionDenied,
		"permission to create a socket must not permit a named pipe")
}

func TestSocketCreationRequiresMakeSock(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	allowed, denied := test.LandlockFiles(t)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.MakeFIFO | landlock.MakeSock})
	require.NoError(t, ruleset.AddPath(filepath.Dir(allowed), landlock.MakeSock))
	require.NoError(t, ruleset.AddPath(filepath.Dir(denied), landlock.MakeFIFO))

	require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread, NoNewPrivileges: true}))

	connection, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(filepath.Dir(allowed), "socket"), Net: "unixgram"})
	require.NoError(t, err)
	test.Cleanup(t, connection)
	connection, err = net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(filepath.Dir(denied), "socket"), Net: "unixgram"})
	test.NoResource(t, connection, err, test.ErrPermissionDenied)
}

func TestDeviceCreationRequiresMatchingRights(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		mode  os.FileMode
		allow landlock.FilesystemAccess
		other landlock.FilesystemAccess
	}{
		{name: "character", mode: os.ModeDevice | os.ModeCharDevice, allow: landlock.MakeChar, other: landlock.MakeBlock},
		{name: "block", mode: os.ModeDevice, allow: landlock.MakeBlock, other: landlock.MakeChar},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			test.LandlockAvailable(t, 8)
			if !test.LandlockDeviceNamespace(t) {
				return
			}
			root := t.TempDir()
			test.AttachMount(t, test.Mount(t), nil, root, root)
			allowed := filepath.Join(root, "allowed")
			denied := filepath.Join(root, "denied")
			require.NoError(t, os.Mkdir(allowed, 0o700))
			require.NoError(t, os.Mkdir(denied, 0o700))
			require.NoError(t, test.MakeDevice(filepath.Join(root, "control"), scenario.mode),
				"device-node creation requires initial-user-namespace CAP_MKNOD and permitted device policy; mapped-user-namespace capabilities are insufficient")
			ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.MakeChar | landlock.MakeBlock})
			require.NoError(t, ruleset.AddPath(allowed, scenario.allow))
			require.NoError(t, ruleset.AddPath(denied, scenario.other))

			test.LockedThread(t, func() {
				require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread, NoNewPrivileges: true}))

				path := filepath.Join(allowed, "device")
				require.NoError(t, test.MakeDevice(path, scenario.mode))
				info, err := os.Stat(path)
				require.NoError(t, err)
				require.Equal(t, scenario.mode, info.Mode().Type())
				require.ErrorIs(t, test.MakeDevice(filepath.Join(denied, "device"), scenario.mode), test.ErrPermissionDenied,
					"the other device-creation right must not grant this inode type")
			})
		})
	}
}
