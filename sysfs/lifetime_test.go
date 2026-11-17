//go:build linux

package sysfs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/internal/testenv"
	"github.com/alexfalkowski/go-linux/sysfs"
	"github.com/stretchr/testify/require"
)

func TestDeviceViewsObserveDisappearanceAndReplacement(t *testing.T) {
	root := test.SysRoot(t, nil)
	block := test.SysBlock(t, root)
	network := test.SysNetwork(t, root)
	require.NoError(t, os.RemoveAll(filepath.Join(root, "devices")))

	blockInfo, err := block.Info()
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Zero(t, blockInfo)
	networkInfo, err := network.Info()
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Zero(t, networkInfo)
	blockNames, err := block.Attributes()
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Nil(t, blockNames)
	networkNames, err := network.Attributes()
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Nil(t, networkNames)

	replacementRoot := test.SysRoot(t, map[string]string{
		"devices/disk/dev": "9:99\n", "devices/disk/size": "0\n", "devices/disk/ro": "1\n", "devices/net/ifindex": "24\n",
	})
	require.NoError(t, os.Rename(filepath.Join(replacementRoot, "devices"), filepath.Join(root, "devices")))

	blockInfo, err = block.Info()
	require.NoError(t, err)
	require.Equal(t, sysfs.BlockInfo{Major: 9, Minor: 99, ReadOnly: true}, blockInfo, "path views must expose replacement")

	networkInfo, err = network.Info()
	require.NoError(t, err)
	require.Equal(t, 24, networkInfo.Index, "a retained interface name must not imply stable identity")
}

func TestDeviceViewsFollowRetargetedClassLinks(t *testing.T) {
	root := test.SysRoot(t, nil)
	block := test.SysBlock(t, root)
	network := test.SysNetwork(t, root)
	blockInfo, err := block.Info()
	require.NoError(t, err)
	require.Equal(t, sysfs.BlockInfo{Major: 8, Minor: 17, Size: 19 * 512}, blockInfo)
	networkInfo, err := network.Info()
	require.NoError(t, err)
	require.Equal(t, 23, networkInfo.Index)

	replacementRoot := test.SysRoot(t, map[string]string{
		"devices/disk/dev": "9:99\n", "devices/disk/size": "0\n", "devices/disk/ro": "1\n", "devices/net/ifindex": "24\n",
	})
	require.NoError(t, os.Rename(filepath.Join(replacementRoot, "devices"), filepath.Join(root, "replacement")))
	for name, target := range map[string]string{
		"class/block/disk": "../../replacement/disk", "class/net/eth0": "../../replacement/net",
	} {
		path := filepath.Join(root, name)
		require.NoError(t, os.Remove(path))
		require.NoError(t, os.Symlink(target, path))
	}

	blockInfo, err = block.Info()
	require.NoError(t, err)
	require.Equal(t, sysfs.BlockInfo{Major: 9, Minor: 99, ReadOnly: true}, blockInfo, "a retained view must follow the new block target")

	networkInfo, err = network.Info()
	require.NoError(t, err)
	require.Equal(t, 24, networkInfo.Index, "a retained view must follow the new network target")
}

func TestConcurrentDeviceReadsReturnIndependentResults(t *testing.T) {
	root := test.SysRoot(t, nil)
	block := test.SysBlock(t, root)
	network := test.SysNetwork(t, root)
	fs, err := sysfs.Open(root)
	require.NoError(t, err)
	const workers = 16
	results := make(chan error, workers)

	for range workers {
		go func() {
			_, err := fs.NetworkInterfaces()
			if err != nil {
				results <- err
				return
			}
			_, err = network.Info()
			if err != nil {
				results <- err
				return
			}
			data, err := block.ReadAttribute("future")
			if err != nil {
				results <- err
				return
			}
			data[0] = 'x'
			results <- nil
		}()
	}
	for range workers {
		require.NoError(t, test.Receive(t, results))
	}

	data, err := block.ReadAttribute("future")
	require.NoError(t, err)
	require.Equal(t, "  opaque\x00\nsecond line \n", string(data))
}

func TestSuccessAndFailureReleaseEveryTemporaryDescriptor(t *testing.T) {
	root := test.SysRoot(t, map[string]string{"devices/disk/future": strings.Repeat("x", 1<<20+1)})
	before := test.DescriptorCount(t)
	block := test.SysBlock(t, root)
	network := test.SysNetwork(t, root)
	for range 8 {
		_, err := block.Info()
		require.NoError(t, err)

		_, err = network.Info()
		require.NoError(t, err)

		_, err = block.ReadAttribute("queue")
		require.ErrorIs(t, err, os.ErrInvalid)

		_, err = network.ReadAttribute("missing")
		require.ErrorIs(t, err, os.ErrNotExist)

		data, err := block.ReadAttribute("future")
		require.ErrorIs(t, err, sysfs.ErrLimit)
		require.Nil(t, data)
	}

	require.Equal(t, before, test.DescriptorCount(t), "neither successful reads nor read failures retain descriptors")
}

func TestPermissionFailuresPreserveErrno(t *testing.T) {
	if os.Geteuid() == 0 {
		options, err := testenv.FromEnvironment()
		require.NoError(t, err)
		const reason = "permission denial requires an ordinary user without DAC override"
		require.False(t, options.Required, reason)
		t.Skip(reason)
	}
	root := test.SysRoot(t, nil)
	device := test.SysNetwork(t, root)
	require.NoError(t, os.Chmod(filepath.Join(root, "devices/net/mtu"), 0))

	info, err := device.Info()
	require.ErrorIs(t, err, test.ErrPermissionDenied)
	require.Zero(t, info)
	require.Equal(t, test.ErrPermissionDenied, test.ErrorNumber(t, err))
}

func TestUninitializedDeviceViewsRejectOperations(t *testing.T) {
	for _, device := range []*sysfs.BlockDevice{nil, {}} {
		info, err := device.Info()
		require.ErrorIs(t, err, os.ErrInvalid)
		require.Zero(t, info)

		names, err := device.Attributes()
		require.ErrorIs(t, err, os.ErrInvalid)
		require.Nil(t, names)

		data, err := device.ReadAttribute("size")
		require.ErrorIs(t, err, os.ErrInvalid)
		require.Nil(t, data)
	}
	for _, device := range []*sysfs.NetworkInterface{nil, {}} {
		info, err := device.Info()
		require.ErrorIs(t, err, os.ErrInvalid)
		require.Zero(t, info)

		names, err := device.Attributes()
		require.ErrorIs(t, err, os.ErrInvalid)
		require.Nil(t, names)

		data, err := device.ReadAttribute("mtu")
		require.ErrorIs(t, err, os.ErrInvalid)
		require.Nil(t, data)
	}
}
