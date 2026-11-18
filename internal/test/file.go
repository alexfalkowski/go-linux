//go:build linux

package test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/testenv"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// FileStatx independently observes Linux metadata for focused ABI comparisons.
// The file remains open and no offset or flags are changed.
func FileStatx(t *testing.T, file *os.File, mask uint32) unix.Statx_t {
	t.Helper()
	var stat unix.Statx_t
	var err error
	FileControl(t, file, func(fd uintptr) {
		err = unix.Statx(int(fd), "", unix.AT_EMPTY_PATH, int(mask), &stat)
	})
	require.NoError(t, err)
	return stat
}

// SetNoDump changes only the temporary file's dump-exclusion flag. The fixture
// filesystem must support FS_IOC_GETFLAGS and FS_IOC_SETFLAGS for its owner.
func SetNoDump(t *testing.T, file *os.File, enabled bool) {
	t.Helper()
	// x/sys omits FS_NODUMP_FL from Linux's include/uapi/linux/fs.h.
	// https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/fs.h
	const noDump = 0x40
	FileControl(t, file, func(fd uintptr) {
		flags, err := unix.IoctlGetInt(int(fd), unix.FS_IOC_GETFLAGS)
		require.NoError(t, err)
		if enabled {
			flags |= noDump
		} else {
			flags &^= noDump
		}
		require.NoError(t, unix.IoctlSetPointerInt(int(fd), unix.FS_IOC_SETFLAGS, flags))
	})
}

// BlockDevice selects an existing /dev block inode for read-only metadata
// observations. The namespace lane exposes host device inodes in its privileged
// container; rootless lanes leave this fixture unselected. No device is opened.
func BlockDevice(t *testing.T) string {
	t.Helper()
	options, err := testenv.FromEnvironment()
	require.NoError(t, err)
	if options.Profile != "namespaces" {
		t.Skip("live block-device metadata requires profile=namespaces with a visible /dev block inode")
	}
	entries, err := os.ReadDir("/dev")
	require.NoError(t, err)
	for _, entry := range entries {
		info, err := entry.Info()
		require.NoError(t, err)
		if info.Mode().Type() == os.ModeDevice {
			return filepath.Join("/dev", entry.Name())
		}
	}

	require.False(t, options.Required, "block-device metadata requires a visible /dev block inode; use the privileged namespace container")
	t.Skip("no /dev block inode is visible for metadata-only observation")
	return ""
}
