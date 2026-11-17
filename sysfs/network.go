//go:build linux

package sysfs

import (
	"os"

	"golang.org/x/sys/unix"
)

// LinkFlags preserves Linux IFF_* bits, including unknown bits. These values
// differ from net.Flags; sysfs flags omit Linux's volatile operational bits.
type LinkFlags uint32

// Up is the administrative IFF_UP bit, not evidence of an operational link.
const Up LinkFlags = unix.IFF_UP

// Broadcast indicates broadcast support (IFF_BROADCAST).
const Broadcast LinkFlags = unix.IFF_BROADCAST

// Loopback identifies a loopback device (IFF_LOOPBACK).
const Loopback LinkFlags = unix.IFF_LOOPBACK

// PointToPoint identifies a point-to-point device (IFF_POINTOPOINT).
const PointToPoint LinkFlags = unix.IFF_POINTOPOINT

// NoARP disables address resolution (IFF_NOARP).
const NoARP LinkFlags = unix.IFF_NOARP

// Multicast indicates multicast support (IFF_MULTICAST).
const Multicast LinkFlags = unix.IFF_MULTICAST

// NetworkInterface is a concurrent-safe class/net path view. It owns no
// resources and requires no Close. Interface names and indexes can be reused.
type NetworkInterface struct {
	path string
}

// NetworkInfo contains basic interface identity, packet size, and flags.
// Driver-specific and potentially unavailable link properties remain available
// through explicit ReadAttribute calls, with their individual errors intact.
type NetworkInfo struct {
	// Index is the positive signed-32-bit ifindex, which can be reused.
	Index int
	// MTU is the maximum transmission unit in bytes, excluding link headers.
	MTU uint32
	// Flags contains the reported flags, including unrecognized bits.
	Flags LinkFlags
	// HardwareType is the Linux ARPHRD_* type, including unknown values.
	HardwareType uint16
}

// Info reads ifindex, mtu, flags, and type as a non-atomic observation. All four
// are required. Invalid or out-of-range values match ErrParse; filesystem errors
// preserve errno. Failure discards the entire result. It does not read optional
// carrier, speed, address, or driver attributes, whose availability varies.
//
// See the Linux [network class ABI] for the underlying attributes.
//
// [network class ABI]: https://github.com/torvalds/linux/blob/v7.0/Documentation/ABI/testing/sysfs-class-net
func (device *NetworkInterface) Info() (NetworkInfo, error) {
	if device == nil {
		return NetworkInfo{}, os.ErrInvalid
	}

	index, err := unsignedAttribute(device.path, "ifindex", 10, 31)
	if err != nil {
		return NetworkInfo{}, err
	}
	if index == 0 {
		return NetworkInfo{}, parseError(device.path, "ifindex")
	}

	mtu, err := unsignedAttribute(device.path, "mtu", 10, 32)
	if err != nil {
		return NetworkInfo{}, err
	}

	// Only flags uses the ABI's hexadecimal spelling; decimal fields must not
	// acquire octal interpretation merely because a fixture has leading zeroes.
	flags, err := unsignedAttribute(device.path, "flags", 0, 32)
	if err != nil {
		return NetworkInfo{}, err
	}

	hardwareType, err := unsignedAttribute(device.path, "type", 10, 16)
	if err != nil {
		return NetworkInfo{}, err
	}
	//nolint:gosec // Each value was parsed with the corresponding field's bit limit.
	return NetworkInfo{Index: int(index), MTU: uint32(mtu), Flags: LinkFlags(flags), HardwareType: uint16(hardwareType)}, nil
}

// Attributes lists immediate regular-file attribute names in lexical order,
// without reading them, with the same behavior as BlockDevice.Attributes.
func (device *NetworkInterface) Attributes() ([]string, error) {
	if device == nil {
		return nil, os.ErrInvalid
	}
	return attributes(device.path)
}

// ReadAttribute reads an uninterpreted relative attribute with the same path,
// size, error, and ownership contracts as BlockDevice.ReadAttribute.
func (device *NetworkInterface) ReadAttribute(name string) ([]byte, error) {
	if device == nil {
		return nil, os.ErrInvalid
	}
	return readAttribute(device.path, name)
}
