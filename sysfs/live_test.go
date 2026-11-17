//go:build linux

package sysfs_test

import (
	"errors"
	"net"
	"os"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/sysfs"
	"github.com/stretchr/testify/require"
)

func TestLiveNetworkInformationMatchesAvailableInterfaces(t *testing.T) {
	fs, err := sysfs.Open("/sys")
	require.NoError(t, err)
	names, err := fs.NetworkInterfaces()
	require.NoError(t, err)
	observed := 0

	for _, name := range names {
		for range 3 {
			before, found, err := test.SysNetworkInterface(name)
			require.NoError(t, err)
			if !found {
				break // An enumerated interface may have disappeared.
			}

			device, err := fs.NetworkInterface(name)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			require.NoError(t, err)
			info, err := device.Info()
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			require.NoError(t, err)

			after, found, err := test.SysNetworkInterface(name)
			require.NoError(t, err)
			if !found {
				break
			}
			stableIdentity := before.Index == after.Index
			stableMTU := before.MTU == after.MTU
			if !stableIdentity || !stableMTU {
				continue // Retry only independently observed changes.
			}

			require.Equal(t, before.Index, info.Index, "%s: stable interface index", name)
			require.EqualValues(t, before.MTU, info.MTU, "%s: stable interface MTU", name)
			observed++
			break
		}
	}
	require.Positive(t, observed, "the live sysfs mount must expose at least one stably observable interface")
}

func TestLiveBlockInformationReadsAvailableDevicesWithoutAssumingInventory(t *testing.T) {
	fs, err := sysfs.Open("/sys")
	require.NoError(t, err)
	names, err := fs.BlockDevices()
	require.NoError(t, err)

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			before, err := test.SysBlockEntry(name)
			if errors.Is(err, os.ErrNotExist) {
				return
			}
			require.NoError(t, err)

			device, err := fs.BlockDevice(name)
			if errors.Is(err, os.ErrNotExist) {
				return
			}
			require.NoError(t, err)
			info, err := device.Info()
			if errors.Is(err, os.ErrNotExist) {
				return
			}
			require.NoError(t, err)

			attributes, err := device.Attributes()
			if errors.Is(err, os.ErrNotExist) {
				return
			}
			require.NoError(t, err)

			matches, identityErr := test.SysBlockIdentity(name, info.Major, info.Minor)
			if identityErr != nil {
				require.ErrorIs(t, identityErr, os.ErrNotExist, "unexpected identity lookup failure")
			}
			after, err := test.SysBlockEntry(name)
			if errors.Is(err, os.ErrNotExist) {
				return
			}
			require.NoError(t, err)
			if !os.SameFile(before, after) {
				return // The class name was reused during this observation.
			}

			require.NoError(t, identityErr, "a stable class entry must have a device-number identity")
			require.True(t, matches, "reported device number must identify this stable class entry")
			require.Zero(t, info.Size%512, "sysfs capacity has fixed 512-byte units")
			require.Contains(t, attributes, "dev")
			require.Contains(t, attributes, "size")
			require.Contains(t, attributes, "ro")
		})
	}
}

func TestLiveUnavailableLoopbackSpeedPreservesKernelReadErrno(t *testing.T) {
	fs, err := sysfs.Open("/sys")
	require.NoError(t, err)
	interfaces, err := net.Interfaces()
	require.NoError(t, err)
	observed := 0

	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback == 0 {
			continue
		}
		device, err := fs.NetworkInterface(iface.Name)
		require.NoError(t, err)
		before := test.DescriptorCount(t)

		for range 8 {
			data, err := device.ReadAttribute("speed")
			require.ErrorIs(t, err, test.ErrInvalidArgument)
			require.Equal(t, test.ErrInvalidArgument, test.ErrorNumber(t, err))
			require.Nil(t, data)
		}

		require.Equal(t, before, test.DescriptorCount(t), "kernel read errors must not retain descriptors")
		observed++
	}
	require.Positive(t, observed, "the current network namespace must expose its loopback interface")
}
