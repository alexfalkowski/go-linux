//go:build linux

package test

import (
	"errors"
	"flag"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/internal/testenv"
	"github.com/alexfalkowski/go-linux/landlock"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// LandlockCreateSyscall identifies ABI discovery/creation for policy fixtures.
const LandlockCreateSyscall = unix.SYS_LANDLOCK_CREATE_RULESET

// LandlockAddSyscall identifies rule addition for policy fixtures.
const LandlockAddSyscall = unix.SYS_LANDLOCK_ADD_RULE

// LandlockRestrictSyscall identifies enforcement for policy fixtures.
const LandlockRestrictSyscall = unix.SYS_LANDLOCK_RESTRICT_SELF

// LandlockAvailable checks actual ABI discovery. Disabled/absent Landlock is
// optional unless required coverage was selected. Unexpected policy errors fail.
// Newer optional ABIs skip on baseline runners even in required mode.
func LandlockAvailable(t *testing.T, minimum int) int {
	t.Helper()
	version, err := landlock.ABI()
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EOPNOTSUPP) {
		options, optionErr := testenv.FromEnvironment()
		require.NoError(t, optionErr)
		require.False(t, options.Required, "required Landlock unavailable: %v", err)
		t.Skipf("Landlock unavailable: %v", err)
	}
	require.NoError(t, err)
	require.GreaterOrEqual(t, version, 8, "Linux 7.0 baseline requires ABI 8")
	if version < minimum {
		t.Skipf("optional Landlock ABI %d requires newer than observed ABI %d", minimum, version)
	}
	return version
}

// LandlockProcess confines irreversible policy changes to a bounded child.
func LandlockProcess(t *testing.T) bool {
	t.Helper()
	const marker = "landlock-isolated"
	if os.Args[len(os.Args)-1] == marker {
		return true
	}
	arguments := []string{"-test.v"}
	if testing.CoverMode() != "" {
		arguments = append(arguments, "-test.gocoverdir="+t.TempDir())
	}
	arguments = append(arguments, "--", marker)
	ctx, command := Command(t, t.Name(), arguments...)
	output, err := command.CombinedOutput()
	require.NoError(t, ctx.Err(), "Landlock scenario exceeded its time bound")
	require.NoError(t, err, "isolated Landlock scenario: %s", output)
	t.Log(string(output))
	return false
}

// LandlockRuleset creates a policy builder and registers its independent cleanup.
// In coverage children it allows the harness's exact private output directory
// for the handled filesystem rights, so TSYNC and exec can still report results.
func LandlockRuleset(t *testing.T, config landlock.Config) *landlock.Ruleset {
	t.Helper()
	LandlockAvailable(t, 8)
	ruleset, err := landlock.New(config)
	require.NoError(t, err)
	Cleanup(t, ruleset)
	if config.HandledFS != 0 && LandlockCoverageDirectory() != "" {
		require.NoError(t, ruleset.AddPath(LandlockCoverageDirectory(), config.HandledFS))
	}
	return ruleset
}

// LandlockCoverageDirectory returns the test harness's explicitly supplied
// output directory. Nested exec fixtures use it to retain their reporting access.
func LandlockCoverageDirectory() string {
	return flag.Lookup("test.gocoverdir").Value.String()
}

// LandlockFiles creates two ordinary files in separate private hierarchies.
func LandlockFiles(t *testing.T) (string, string) {
	t.Helper()
	allowed := filepath.Join(t.TempDir(), "allowed")
	denied := filepath.Join(t.TempDir(), "denied")
	require.NoError(t, os.WriteFile(allowed, []byte("allowed"), 0o600))
	require.NoError(t, os.WriteFile(denied, []byte("denied"), 0o600))
	return allowed, denied
}

// LandlockReadThread holds an existing locked sibling for repeated open
// probe or cleanup. Optional initial restrictions create a divergent domain.
// Only use inside LandlockProcess; worker teardown never returns a changed
// thread to the runtime's pool.
func LandlockReadThread(t *testing.T, path string, initial *landlock.Ruleset) func() error {
	t.Helper()
	require.Equal(t, "landlock-isolated", os.Args[len(os.Args)-1])
	ready := make(chan error, 1)
	probe := make(chan struct{})
	release := make(chan struct{})
	result := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		LockThread()
		defer close(done)
		if initial != nil {
			if err := initial.Restrict(landlock.Options{Synchronization: landlock.CallingThread, NoNewPrivileges: true}); err != nil {
				ready <- err
				return
			}
		}
		ready <- nil
		for {
			select {
			case <-probe:
				file, err := os.Open(path)
				if err != nil {
					result <- err
					continue
				}
				result <- file.Close()
			case <-release:
				return
			}
		}
	}()
	t.Cleanup(func() {
		close(release)
		Receive(t, done)
	})
	require.NoError(t, Receive(t, ready))
	return func() error {
		probe <- struct{}{}
		return Receive(t, result)
	}
}

// LandlockABI independently queries the kernel for the focused ABI scenario.
func LandlockABI(t *testing.T) int {
	t.Helper()
	version, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	require.Zero(t, errno)
	return int(version)
}

// TCPListener creates a private loopback listener for port enforcement.
func TCPListener(t *testing.T) *net.TCPListener {
	t.Helper()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	Cleanup(t, listener)
	require.NoError(t, listener.SetDeadline(time.Now().Add(5*time.Second)))
	return listener
}

// UDPPeer creates a bounded private loopback socket for datagram enforcement.
func UDPPeer(t *testing.T) *net.UDPConn {
	t.Helper()
	peer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	Cleanup(t, peer)
	require.NoError(t, peer.SetDeadline(time.Now().Add(5*time.Second)))
	return peer
}

// LandlockUnixPeer creates a test-owned socket inside or outside a domain. Temp
// directory uniqueness also supplies the abstract name; no host socket is used.
func LandlockUnixPeer(t *testing.T, abstract bool) *net.UnixConn {
	t.Helper()
	name := filepath.Join(t.TempDir(), "socket")
	if abstract {
		name = "@" + name
	}
	peer, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: name, Net: "unixgram"})
	require.NoError(t, err)
	Cleanup(t, peer)
	require.NoError(t, peer.SetDeadline(time.Now().Add(5*time.Second)))
	return peer
}

// RefuseLandlockCreation denies only actual creation after allowing ABI
// discovery. It confines a kernel policy failure to a bounded fixture child.
func RefuseLandlockCreation(t *testing.T) {
	t.Helper()
	require.Equal(t, "landlock-isolated", os.Args[len(os.Args)-1])
	restrictThread(t, []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.SYS_LANDLOCK_CREATE_RULESET, Jf: 3},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 32}, // flags argument
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: 0, Jf: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EACCES)},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
	})
}
