//go:build linux

package sysfs_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/sysfs"
	"github.com/stretchr/testify/require"
)

func TestAttributeDiscoveryListsUnknownFilesWithoutReadingThem(t *testing.T) {
	root := test.SysRoot(t, nil)
	device := test.SysBlock(t, root)
	require.NoError(t, os.Chmod(filepath.Join(root, "devices/disk/future"), 0))
	require.NoError(t, os.Symlink("size", filepath.Join(root, "devices/disk/link")))

	names, err := device.Attributes()
	require.NoError(t, err)

	require.Equal(t, []string{"dev", "future", "ro", "size"}, names)
}

func TestUnknownAttributesRetainAllBytesAndIndependentStorage(t *testing.T) {
	contents := "  " + strings.Repeat("opaque\x00\n", 40) + " tail \n"
	device := test.SysBlock(t, test.SysRoot(t, map[string]string{"devices/disk/future": contents}))

	data, err := device.ReadAttribute("future")
	require.NoError(t, err)
	require.Equal(t, contents, string(data))

	again, err := device.ReadAttribute("future")
	require.NoError(t, err)

	data[0] = 'x'
	require.Equal(t, contents, string(again), "each read owns its bytes")
}

func TestNestedAndSymlinkAttributesRemainReadable(t *testing.T) {
	root := test.SysRoot(t, nil)
	device := test.SysBlock(t, root)
	require.NoError(t, os.Symlink("size", filepath.Join(root, "devices/disk/link")))

	nested, err := device.ReadAttribute("queue/logical_block_size")
	require.NoError(t, err)
	require.Equal(t, "4096\n", string(nested))

	linked, err := device.ReadAttribute("link")
	require.NoError(t, err)
	require.Equal(t, "19\n", string(linked))
}

func TestAttributeReadsEnforceTheirExactSizeBoundary(t *testing.T) {
	for _, length := range []int{0, 1 << 20, 1<<20 + 1} {
		t.Run(strconv.Itoa(length)+" bytes", func(t *testing.T) {
			contents := strings.Repeat("x", length)
			device := test.SysBlock(t, test.SysRoot(t, map[string]string{"devices/disk/future": contents}))

			data, err := device.ReadAttribute("future")
			if length > 1<<20 {
				require.ErrorIs(t, err, sysfs.ErrLimit)
				require.Nil(t, data)
				return
			}
			require.NoError(t, err)
			require.Equal(t, contents, string(data))
		})
	}
}

func TestAttributeReadsRejectInvalidRelativePaths(t *testing.T) {
	root := test.SysRoot(t, nil)
	block := test.SysBlock(t, root)
	network := test.SysNetwork(t, root)
	for _, name := range []string{"", ".", "..", "../size", "/size", "queue/../size", "queue//size", "queue/./size", "size\x00"} {
		t.Run(name, func(t *testing.T) {
			data, err := block.ReadAttribute(name)
			require.ErrorIs(t, err, os.ErrInvalid)
			require.Nil(t, data)

			data, err = network.ReadAttribute(name)
			require.ErrorIs(t, err, os.ErrInvalid)
			require.Nil(t, data)
		})
	}
}

func TestAttributeReadsRejectDirectories(t *testing.T) {
	root := test.SysRoot(t, nil)
	device := test.SysBlock(t, root)

	data, err := device.ReadAttribute("queue")
	require.ErrorIs(t, err, os.ErrInvalid)
	require.Nil(t, data)
}

func TestAttributeReadsRejectFIFOWithoutBlocking(t *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-2] != "sysfs-fifo" {
		ctx, command := test.Command(t, t.Name(), "--", "sysfs-fifo", test.FIFO(t))
		output, err := command.CombinedOutput()
		require.NoError(t, ctx.Err(), "reading a FIFO attribute must not wait for a writer")
		require.NoError(t, err, "%s", output)
		return
	}
	root := test.SysRoot(t, nil)
	device := test.SysBlock(t, root)
	require.NoError(t, os.Symlink(os.Args[len(os.Args)-1], filepath.Join(root, "devices/disk/pipe")))

	data, err := device.ReadAttribute("pipe")
	require.ErrorIs(t, err, os.ErrInvalid)
	require.Nil(t, data)
}

func TestMissingOptionalNetworkAttributesDoNotPreventBasicInformation(t *testing.T) {
	root := test.SysRoot(t, nil)
	device := test.SysNetwork(t, root)
	require.NoError(t, os.Remove(filepath.Join(root, "devices/net/speed")))

	info, err := device.Info()
	require.NoError(t, err)
	require.Equal(t, 23, info.Index)

	data, err := device.ReadAttribute("speed")
	require.ErrorIs(t, err, test.ErrNotExist)
	require.Nil(t, data)
}

func TestNetworkAttributesRetainUnknownStatesAndNativeUnits(t *testing.T) {
	device := test.SysNetwork(t, test.SysRoot(t, nil))

	names, err := device.Attributes()
	require.NoError(t, err)
	require.Contains(t, names, "operstate")
	require.NotContains(t, names, "statistics")

	state, err := device.ReadAttribute("operstate")
	require.NoError(t, err)
	require.Equal(t, "future-state\n", string(state))

	speed, err := device.ReadAttribute("speed")
	require.NoError(t, err)
	require.Equal(t, "-1\n", string(speed), "unavailable speed must not become a byte rate or unsigned value")

	count, err := device.ReadAttribute("statistics/rx_bytes")
	require.NoError(t, err)
	require.Equal(t, "987654321\n", string(count))
}
