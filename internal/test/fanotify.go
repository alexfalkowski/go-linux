//go:build linux

package test

import (
	"encoding/binary"
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/testenv"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// FanotifyNamespace reexecutes a test in a private mount namespace while
// retaining initial-user-namespace capabilities. Unlike MountNamespace, it
// cannot obtain the required fanotify privilege through a new user namespace.
// Only profile=namespaces selects these tests; required=true forbids skips.
func FanotifyNamespace(t *testing.T) bool {
	t.Helper()
	const prefix = "fanotify-isolated="
	if argument := os.Args[len(os.Args)-1]; strings.HasPrefix(argument, prefix) {
		current, err := os.Readlink("/proc/self/ns/mnt")
		require.NoError(t, err)
		require.NotEqual(t, strings.TrimPrefix(argument, prefix), current)
		require.NoError(t, unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""))
		t.Cleanup(func() {
			if !t.Failed() {
				t.Log("verified isolated fanotify operations")
			}
		})
		return true
	}
	options, err := testenv.FromEnvironment()
	require.NoError(t, err)
	if options.Profile != "namespaces" {
		t.Skip("fanotify descriptor integration requires profile=namespaces and initial-user-namespace CAP_SYS_ADMIN")
	}
	fanotifyPrivilege(t, options.Required)
	before, err := os.Readlink("/proc/self/ns/mnt")
	require.NoError(t, err)
	ctx, command := Command(t, t.Name(), "-test.v", "--", prefix+before)
	command.SysProcAttr = &syscall.SysProcAttr{Cloneflags: syscall.CLONE_NEWNS}
	output, err := command.CombinedOutput()
	require.NoError(t, ctx.Err(), "isolated fanotify test exceeded its time bound")
	unavailable := errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.ENOSPC)
	if unavailable {
		require.False(t, options.Required, "mount namespace prerequisite: %v", err)
		t.Skipf("fanotify isolation requires permitted clone(mount): %v", err)
	}
	require.NoError(t, err, "isolated fanotify test: %s", output)
	require.Contains(t, string(output), "verified isolated fanotify operations")
	after, err := os.Readlink("/proc/self/ns/mnt")
	require.NoError(t, err)
	require.Equal(t, before, after, "parent mount namespace must remain unchanged")
	t.Log(string(output))
	return false
}

// FanotifyRecord independently encodes a Linux metadata version 3 fixture.
// The caller owns any nonnegative fd placed into it until the decoder consumes
// the record. No production parser may be exposed merely to accept these bytes.
func FanotifyRecord(fd, pid int32, mask uint64, info []byte) []byte {
	record := make([]byte, 24+len(info))
	binary.NativeEndian.PutUint32(record[:4], uint32(len(record))) //nolint:gosec // Test records are bounded by their allocated slice.
	record[4] = 3
	binary.NativeEndian.PutUint16(record[6:8], 24)
	binary.NativeEndian.PutUint64(record[8:16], mask)
	binary.NativeEndian.PutUint32(record[16:20], uint32(fd))  //nolint:gosec // Preserve the signed Linux field bits.
	binary.NativeEndian.PutUint32(record[20:24], uint32(pid)) //nolint:gosec // Preserve the signed Linux field bits.
	copy(record[24:], info)
	return record
}

func fanotifyPrivilege(t *testing.T, required bool) {
	t.Helper()
	status, err := os.ReadFile("/proc/self/status")
	require.NoError(t, err)
	var effective uint64
	for line := range strings.SplitSeq(string(status), "\n") {
		if value, ok := strings.CutPrefix(line, "CapEff:"); ok {
			effective, err = strconv.ParseUint(strings.TrimSpace(value), 16, 64)
			require.NoError(t, err)
		}
	}
	if effective&(1<<unix.CAP_SYS_ADMIN) == 0 {
		reason := "fanotify descriptor mode requires initial-user-namespace CAP_SYS_ADMIN; observed CapEff lacks CAP_SYS_ADMIN"
		require.False(t, required, reason)
		t.Skip(reason)
	}
}
