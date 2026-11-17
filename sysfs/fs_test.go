//go:build linux

package sysfs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/sysfs"
	"github.com/stretchr/testify/require"
)

func TestOpenRejectsInvalidRoots(t *testing.T) {
	for _, root := range []string{"", test.TempFile(t, 0).Name()} {
		t.Run(root, func(t *testing.T) {
			fs, err := sysfs.Open(root)
			test.NoResource(t, fs, err, os.ErrInvalid)
		})
	}
}

func TestOpenRetainsMissingRootErrno(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent")

	fs, err := sysfs.Open(path)
	test.NoResource(t, fs, err, test.ErrNotExist)
	require.Equal(t, test.ErrNotExist, test.ErrorNumber(t, err))
	test.PathError(t, err, "stat", path)
}

func TestRelativeRootRemainsIndependentOfWorkingDirectory(t *testing.T) {
	root := test.SysRoot(t, nil)
	t.Chdir(filepath.Dir(root))
	fs, err := sysfs.Open(filepath.Base(root))
	require.NoError(t, err)
	t.Chdir(t.TempDir())

	device, err := fs.BlockDevice("disk")
	require.NoError(t, err)
	info, err := device.Info()
	require.NoError(t, err)
	require.Equal(t, uint64(19*512), info.Size)

	networks, err := fs.NetworkInterfaces()
	require.NoError(t, err)
	require.Equal(t, []string{"eth0"}, networks)
}

func TestEnumerationIncludesSymlinksAndPartitionsInOrder(t *testing.T) {
	root := test.SysRoot(t, map[string]string{"class/block/ordinary": "ignored", "class/net/ordinary": "ignored"})
	require.NoError(t, os.Mkdir(filepath.Join(root, "class/net/aaa"), 0o700))
	require.NoError(t, os.Symlink("missing", filepath.Join(root, "class/block/broken")))
	fs, err := sysfs.Open(root)
	require.NoError(t, err)

	blocks, err := fs.BlockDevices()
	require.NoError(t, err)
	require.Equal(t, []string{"broken", "disk", "disk1"}, blocks)

	networks, err := fs.NetworkInterfaces()
	require.NoError(t, err)
	require.Equal(t, []string{"aaa", "eth0"}, networks)
}

func TestDeviceLookupRejectsInvalidNames(t *testing.T) {
	fs, err := sysfs.Open(test.SysRoot(t, nil))
	require.NoError(t, err)
	for _, name := range []string{"", ".", "..", "/disk", "../disk", "disk/part", "bad\x00name"} {
		t.Run(name, func(t *testing.T) {
			block, err := fs.BlockDevice(name)
			test.NoResource(t, block, err, os.ErrInvalid)

			network, err := fs.NetworkInterface(name)
			test.NoResource(t, network, err, os.ErrInvalid)
		})
	}
}

func TestDeviceLookupRejectsRegularFiles(t *testing.T) {
	fs, err := sysfs.Open(test.SysRoot(t, map[string]string{"class/block/file": "", "class/net/file": ""}))
	require.NoError(t, err)

	block, err := fs.BlockDevice("file")
	test.NoResource(t, block, err, os.ErrInvalid)

	network, err := fs.NetworkInterface("file")
	test.NoResource(t, network, err, os.ErrInvalid)
}

func TestDeviceLookupRetainsMissingAndDanglingErrors(t *testing.T) {
	root := test.SysRoot(t, nil)
	require.NoError(t, os.Symlink("missing", filepath.Join(root, "class/block/dangling")))
	require.NoError(t, os.Symlink("missing", filepath.Join(root, "class/net/dangling")))
	fs, err := sysfs.Open(root)
	require.NoError(t, err)
	for _, name := range []string{"missing", "dangling"} {
		t.Run(name, func(t *testing.T) {
			block, err := fs.BlockDevice(name)
			test.NoResource(t, block, err, test.ErrNotExist)

			network, err := fs.NetworkInterface(name)
			test.NoResource(t, network, err, test.ErrNotExist)
		})
	}
}

func TestRemovedRootMakesEnumerationFail(t *testing.T) {
	root := test.SysRoot(t, nil)
	fs, err := sysfs.Open(root)
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(root))

	blocks, err := fs.BlockDevices()
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Nil(t, blocks)

	networks, err := fs.NetworkInterfaces()
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Nil(t, networks)
}

func TestUninitializedFilesystemsRejectOperations(t *testing.T) {
	for _, fs := range []*sysfs.FS{nil, {}} {
		blocks, err := fs.BlockDevices()
		require.ErrorIs(t, err, os.ErrInvalid)
		require.Nil(t, blocks)

		networks, err := fs.NetworkInterfaces()
		require.ErrorIs(t, err, os.ErrInvalid)
		require.Nil(t, networks)

		block, err := fs.BlockDevice("disk")
		test.NoResource(t, block, err, os.ErrInvalid)

		network, err := fs.NetworkInterface("eth0")
		test.NoResource(t, network, err, os.ErrInvalid)
	}
}
