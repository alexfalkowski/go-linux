//go:build linux

package test

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/testenv"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// MountNamespace runs the current top-level test in a private user/mount
// namespace subprocess. It returns true only in that child; the parent checks
// its result and returns false. Known unavailable prerequisites skip only in
// optional mode; required mode fails. Call before creating any mount resources.
func MountNamespace(t *testing.T) bool {
	t.Helper()

	const prefix = "mount-isolated="
	if argument := os.Args[len(os.Args)-1]; strings.HasPrefix(argument, prefix) {
		enterMountNamespace(t, strings.TrimPrefix(argument, prefix))
		return true
	}

	options, err := testenv.FromEnvironment()
	require.NoError(t, err)

	if options.Profile != "namespaces" {
		t.Skip("mount integration requires profile=namespaces; add required=true to require its prerequisites")
	}

	before, err := os.Readlink("/proc/self/ns/mnt")
	require.NoError(t, err)
	t.Cleanup(func() {
		after, err := os.Readlink("/proc/self/ns/mnt")
		require.NoError(t, err)
		require.Equal(t, before, after, "parent namespace must remain unchanged")
	})

	ctx, command := Command(t, t.Name(), "-test.v", "--", prefix+before)
	command.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:  syscall.CLONE_NEWUSER | syscall.CLONE_NEWNS,
		UidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getuid(), Size: 1}},
		GidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getgid(), Size: 1}},
	}

	output, err := command.CombinedOutput()
	require.NoError(t, ctx.Err(), "isolated mount test exceeded its time bound")

	if errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.ENOSPC) {
		reason := "profile=namespaces operation=clone(user,mount)+uid/gid-mapping; prerequisite=permitted namespaces/mappings/quota: " + err.Error()
		require.False(t, options.Required, reason)

		t.Skip(reason)
	}

	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 77 && strings.Contains(string(output), "mount prerequisite unavailable: operation=") {
		require.False(t, options.Required, string(output))

		t.Skip(string(output))
	}
	require.NoError(t, err, "isolated mount test: %s", output)
	require.Contains(t, string(output), "verified isolated mount operations")

	t.Log(string(output))
	return false
}

func mountPrerequisite(t *testing.T, operation string, err error) {
	t.Helper()
	if errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES) || errors.Is(err, unix.ENOSYS) {
		t.Logf("mount prerequisite unavailable: operation=%s: %v; prerequisite=applicable CAP_SYS_ADMIN and permitted mount syscalls; profile=namespaces", operation, err)
		os.Exit(77)
	}
	require.NoError(t, err, "prerequisite operation=%s", operation)
}

func enterMountNamespace(t *testing.T, parent string) {
	t.Helper()

	current, err := os.Readlink("/proc/self/ns/mnt")
	require.NoError(t, err)
	require.NotEqual(t, parent, current, "mount mutations must stay in a separate namespace")
	require.Zero(t, os.Getuid(), "child must have its requested user mapping")

	// This legacy call changes only fixture propagation, never mounts a
	// filesystem as a substitute for the library's modern operations.
	mountPrerequisite(t, "private propagation", unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""))
	fd, err := unix.Fsopen("tmpfs", unix.FSOPEN_CLOEXEC)
	mountPrerequisite(t, "fsopen(tmpfs)", err)

	require.NoError(t, unix.Close(fd))
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("verified isolated mount operations")
		}
	})
}
