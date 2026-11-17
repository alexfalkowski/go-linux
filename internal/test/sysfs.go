//go:build linux

package test

import (
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexfalkowski/go-linux/sysfs"
	"github.com/stretchr/testify/require"
)

// SysBlockEntry observes the target inode of a live block class entry for
// bracketing non-atomic reads. Missing entries retain their filesystem error.
func SysBlockEntry(name string) (os.FileInfo, error) {
	return os.Stat(filepath.Join("/sys/class/block", name))
}

// SysBlockIdentity independently compares a class entry with the kernel's
// major:minor symlink. Disappearance is returned for the caller to classify.
func SysBlockIdentity(name string, major, minor uint32) (bool, error) {
	classInfo, err := SysBlockEntry(name)
	if err != nil {
		return false, err
	}
	numberInfo, err := os.Stat(fmt.Sprintf("/sys/dev/block/%d:%d", major, minor))
	if err != nil {
		return false, err
	}
	return os.SameFile(classInfo, numberInfo), nil
}

// SysNetworkInterface independently observes a named interface through the
// standard library. An absent name is distinct from an inventory lookup error.
func SysNetworkInterface(name string) (net.Interface, bool, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return net.Interface{}, false, err
	}
	for _, iface := range interfaces {
		if iface.Name == name {
			return iface, true, nil
		}
	}
	return net.Interface{}, false, nil
}

// SysRoot creates synthetic devices behind relative class symlinks, including
// a block partition, distinct units, and uninterpreted future attributes.
// entries replaces or adds files relative to the root.
func SysRoot(t *testing.T, entries map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"devices/disk/dev": "8:17\n", "devices/disk/size": "19\n", "devices/disk/ro": "0\n",
		"devices/disk/queue/logical_block_size": "4096\n", "devices/disk/future": "  opaque\x00\nsecond line \n",
		"devices/disk/part/dev": "8:18\n", "devices/disk/part/size": "7\n", "devices/disk/part/ro": "1\n",
		"devices/net/ifindex": "23\n", "devices/net/mtu": "9000\n", "devices/net/type": "1\n",
		"devices/net/flags": "0x80001003\n", "devices/net/operstate": "future-state\n",
		"devices/net/speed": "-1\n", "devices/net/statistics/rx_bytes": "987654321\n",
	}
	maps.Copy(files, entries)
	for name, contents := range files {
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	}
	for name, target := range map[string]string{
		"class/block/disk": "../../devices/disk", "class/block/disk1": "../../devices/disk/part", "class/net/eth0": "../../devices/net",
	} {
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.Symlink(target, path))
	}
	return root
}

// SysBlock selects the synthetic disk through the public API.
func SysBlock(t *testing.T, root string) *sysfs.BlockDevice {
	t.Helper()
	fs, err := sysfs.Open(root)
	require.NoError(t, err)
	device, err := fs.BlockDevice("disk")
	require.NoError(t, err)
	return device
}

// SysNetwork selects the synthetic eth0 through the public API.
func SysNetwork(t *testing.T, root string) *sysfs.NetworkInterface {
	t.Helper()
	fs, err := sysfs.Open(root)
	require.NoError(t, err)
	device, err := fs.NetworkInterface("eth0")
	require.NoError(t, err)
	return device
}
