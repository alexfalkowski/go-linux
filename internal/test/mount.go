//go:build linux

package test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexfalkowski/go-linux/mount"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// MountAttributesName describes a supported restriction mask in test output.
func MountAttributesName(attributes mount.Attributes) string {
	var names []string
	for _, attribute := range []struct {
		name string
		bit  mount.Attributes
	}{
		{name: "read only", bit: mount.ReadOnly},
		{name: "no set-ID", bit: mount.NoSUID},
		{name: "no devices", bit: mount.NoDev},
		{name: "no execution", bit: mount.NoExec},
	} {
		if attributes&attribute.bit != 0 {
			names = append(names, attribute.name)
		}
	}

	if len(names) == 0 {
		return "no restrictions"
	}
	return strings.Join(names, ", ")
}

// MountFilesystem inspects a mount's filesystem through its observed descriptor.
func MountFilesystem(t *testing.T, fd int) unix.Statfs_t {
	t.Helper()

	var stat unix.Statfs_t
	require.NoError(t, unix.Fstatfs(fd, &stat))
	return stat
}

// AttachMount attaches a handle and registers unmount before directory cleanup.
// absolute is the resolved target used only for fixture cleanup.
// Call only inside MountNamespace.
func AttachMount(t *testing.T, handle *mount.Handle, dir *os.File, path, absolute string) {
	t.Helper()

	require.NoError(t, handle.Attach(dir, path))
	t.Cleanup(func() {
		require.NoError(t, mount.Unmount(absolute, mount.Detach))
	})
}

// MountedTree creates an isolated tmpfs tree with a child mount at nested.
// nested/value contains "lower" underneath that child and "upper" within it.
// Cleanup unmounts both trees before removing their temporary directories.
// Call only inside MountNamespace.
func MountedTree(t *testing.T) string {
	t.Helper()

	path := t.TempDir()
	AttachMount(t, Mount(t), nil, path, path)
	nested := filepath.Join(path, "nested")
	require.NoError(t, os.Mkdir(nested, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(nested, "value"), []byte("lower"), 0o600))

	AttachMount(t, Mount(t), nil, nested, nested)
	require.NoError(t, os.WriteFile(filepath.Join(nested, "value"), []byte("upper"), 0o600))
	return path
}

// ClonedMount attaches a detached copy at a temporary path and registers cleanup
// for the handle, attachment, and directory. Call only inside MountNamespace.
func ClonedMount(t *testing.T, source string, flags mount.CloneFlags) (*mount.Handle, string) {
	t.Helper()

	handle, err := mount.Clone(nil, source, flags)
	require.NoError(t, err)
	Cleanup(t, handle)
	path := t.TempDir()
	AttachMount(t, handle, nil, path, path)
	return handle, path
}

// QueueMountDiagnostics fills the real kernel diagnostic queue without draining
// it. This ABI fixture verifies the queue's bounded ordering independently of
// Config's automatic error collection; ordinary callers use SetFlag instead.
func QueueMountDiagnostics(t *testing.T, fd, count int) {
	t.Helper()

	for index := range count {
		require.ErrorIs(t, unix.FsconfigSetFlag(fd, fmt.Sprintf("unknown%d", index)), unix.EINVAL)
	}
}
