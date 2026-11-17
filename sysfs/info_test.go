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

func TestBlockSizeUsesFixedSectorsInsteadOfLogicalBlockSize(t *testing.T) {
	device := test.SysBlock(t, test.SysRoot(t, nil))

	info, err := device.Info()
	require.NoError(t, err)

	require.Equal(t, sysfs.BlockInfo{Major: 8, Minor: 17, Size: 19 * 512}, info)
}

func TestPartitionAttributesUseThePartitionClassLink(t *testing.T) {
	fs, err := sysfs.Open(test.SysRoot(t, nil))
	require.NoError(t, err)
	device, err := fs.BlockDevice("disk1")
	require.NoError(t, err)

	info, err := device.Info()
	require.NoError(t, err)

	require.Equal(t, sysfs.BlockInfo{Major: 8, Minor: 18, Size: 7 * 512, ReadOnly: true}, info)
}

func TestBlockDeviceNumbersRetainBothUint32Components(t *testing.T) {
	device := test.SysBlock(t, test.SysRoot(t, map[string]string{"devices/disk/dev": "4294967295:4294967295\n"}))

	info, err := device.Info()
	require.NoError(t, err)

	require.Equal(t, sysfs.BlockInfo{Major: ^uint32(0), Minor: ^uint32(0), Size: 19 * 512}, info)
}

func TestBlockInformationRejectsMalformedAndOverflowingValues(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		value string
	}{
		{name: "dev", value: "8"},
		{name: "dev", value: "8:1:2"},
		{name: "dev", value: "-1:2"},
		{name: "dev", value: "8:4294967296"},
		{name: "dev", value: "4294967296:1"},
		{name: "size", value: ""},
		{name: "size", value: "-1"},
		{name: "size", value: "18446744073709551616"},
		{name: "size", value: "36028797018963968"},
		{name: "ro", value: "2"},
		{name: "ro", value: "true"},
	} {
		t.Run(scenario.name+"="+scenario.value, func(t *testing.T) {
			root := test.SysRoot(t, map[string]string{"devices/disk/" + scenario.name: scenario.value})
			device := test.SysBlock(t, root)

			info, err := device.Info()
			require.ErrorIs(t, err, sysfs.ErrParse)
			require.Zero(t, info)
			test.PathError(t, err, "parse", filepath.Join(root, "class/block/disk", scenario.name))
		})
	}
}

func TestBlockParseErrorsOmitAttributeContents(t *testing.T) {
	const contents = "private-attribute-value"
	for _, scenario := range []struct {
		name      string
		attribute string
		value     string
	}{
		{name: "major", attribute: "dev", value: contents + ":17"},
		{name: "minor", attribute: "dev", value: "8:" + contents},
		{name: "size", attribute: "size", value: contents},
		{name: "ro", attribute: "ro", value: contents},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := test.SysRoot(t, map[string]string{"devices/disk/" + scenario.attribute: scenario.value})
			device := test.SysBlock(t, root)

			info, err := device.Info()
			require.ErrorIs(t, err, sysfs.ErrParse)
			require.Zero(t, info)
			test.PathError(t, err, "parse", filepath.Join(root, "class/block/disk", scenario.attribute))
			require.NotContains(t, err.Error(), contents)
		})
	}
}

func TestBlockSizeAcceptsItsLargestRepresentableByteCount(t *testing.T) {
	device := test.SysBlock(t, test.SysRoot(t, map[string]string{"devices/disk/size": "36028797018963967\n"}))

	info, err := device.Info()
	require.NoError(t, err)

	require.Equal(t, ^uint64(0)-511, info.Size)
}

func TestNetworkInformationPreservesNativeAndUnknownFlags(t *testing.T) {
	device := test.SysNetwork(t, test.SysRoot(t, nil))

	info, err := device.Info()
	require.NoError(t, err)

	require.Equal(t, sysfs.NetworkInfo{
		Index: 23, MTU: 9000, HardwareType: 1, Flags: sysfs.Up | sysfs.Broadcast | sysfs.Multicast | 0x80000000,
	}, info)
}

func TestNetworkInformationAcceptsItsFieldWidthBoundaries(t *testing.T) {
	device := test.SysNetwork(t, test.SysRoot(t, map[string]string{
		"devices/net/ifindex": "2147483647\n", "devices/net/mtu": "4294967295\n",
		"devices/net/flags": "0xffffffff\n", "devices/net/type": "65535\n",
	}))

	info, err := device.Info()
	require.NoError(t, err)

	require.Equal(t, sysfs.NetworkInfo{Index: 2147483647, MTU: ^uint32(0), Flags: ^sysfs.LinkFlags(0), HardwareType: ^uint16(0)}, info)
}

func TestNetworkInformationRejectsMalformedAndOutOfRangeValues(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		value string
	}{
		{name: "ifindex", value: "0"},
		{name: "ifindex", value: "-1"},
		{name: "ifindex", value: "2147483648"},
		{name: "mtu", value: "bad"},
		{name: "mtu", value: "4294967296"},
		{name: "flags", value: "bad"},
		{name: "flags", value: "0x100000000"},
		{name: "type", value: "65536"},
		{name: "type", value: "-1"},
	} {
		t.Run(scenario.name+"="+scenario.value, func(t *testing.T) {
			root := test.SysRoot(t, map[string]string{"devices/net/" + scenario.name: scenario.value})
			device := test.SysNetwork(t, root)

			info, err := device.Info()
			require.ErrorIs(t, err, sysfs.ErrParse)
			require.Zero(t, info)
			test.PathError(t, err, "parse", filepath.Join(root, "class/net/eth0", scenario.name))
		})
	}
}

func TestNetworkParseErrorsOmitAttributeContents(t *testing.T) {
	const contents = "private-attribute-value"
	for _, name := range []string{"ifindex", "mtu", "flags", "type"} {
		t.Run(name, func(t *testing.T) {
			root := test.SysRoot(t, map[string]string{"devices/net/" + name: contents})
			device := test.SysNetwork(t, root)

			info, err := device.Info()
			require.ErrorIs(t, err, sysfs.ErrParse)
			require.Zero(t, info)
			test.PathError(t, err, "parse", filepath.Join(root, "class/net/eth0", name))
			require.NotContains(t, err.Error(), contents)
		})
	}
}

func TestNetworkDecimalFieldsDoNotAcquireOctalInterpretation(t *testing.T) {
	device := test.SysNetwork(t, test.SysRoot(t, map[string]string{"devices/net/mtu": "09000\n"}))

	info, err := device.Info()
	require.NoError(t, err)

	require.Equal(t, uint32(9000), info.MTU)
}

func TestMissingBlockAttributesRetainErrnoAndDiscardPartialInformation(t *testing.T) {
	for _, name := range []string{"dev", "size", "ro"} {
		t.Run(name, func(t *testing.T) {
			root := test.SysRoot(t, nil)
			device := test.SysBlock(t, root)
			require.NoError(t, os.Remove(filepath.Join(root, "devices/disk", name)))

			info, err := device.Info()
			require.ErrorIs(t, err, test.ErrNotExist)
			require.Zero(t, info)
		})
	}
}

func TestMissingNetworkAttributesRetainErrnoAndDiscardPartialInformation(t *testing.T) {
	for _, name := range []string{"ifindex", "mtu", "flags", "type"} {
		t.Run(name, func(t *testing.T) {
			root := test.SysRoot(t, nil)
			device := test.SysNetwork(t, root)
			require.NoError(t, os.Remove(filepath.Join(root, "devices/net", name)))

			info, err := device.Info()
			require.ErrorIs(t, err, test.ErrNotExist)
			require.Zero(t, info)
		})
	}
}
