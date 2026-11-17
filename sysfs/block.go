//go:build linux

package sysfs

import (
	"math"
	"os"
	"strconv"
	"strings"
)

// BlockDevice is a concurrent-safe class/block path view with no Close or
// persistent resources. Its name can later refer to a different device.
type BlockDevice struct {
	path string
}

// BlockInfo contains selected Linux block attributes, including partition
// attributes when the selected class entry is a partition.
type BlockInfo struct {
	// Size is capacity in bytes, converted from size's fixed 512-byte sectors.
	// It does not depend on the device's logical or physical block size.
	Size uint64
	// Major is the device number's major component from dev.
	Major uint32
	// Minor is the device number's minor component from dev.
	Minor uint32
	// ReadOnly reports whether ro is 1; 0 means writes are not disabled here.
	// It does not establish filesystem permissions or media writeability.
	ReadOnly bool
}

// Info reads dev, size, and ro as a non-atomic observation. All three must be
// present. Invalid numbers, invalid booleans, and byte overflow match ErrParse;
// filesystem errors preserve errno. Failure discards the entire result.
//
// See the Linux [block ABI] for the underlying attributes.
//
// [block ABI]: https://github.com/torvalds/linux/blob/v7.0/Documentation/ABI/stable/sysfs-block
func (device *BlockDevice) Info() (BlockInfo, error) {
	if device == nil {
		return BlockInfo{}, os.ErrInvalid
	}

	data, err := device.ReadAttribute("dev")
	if err != nil {
		return BlockInfo{}, err
	}

	majorText, minorText, found := strings.Cut(strings.TrimSpace(string(data)), ":")
	if !found {
		return BlockInfo{}, parseError(device.path, "dev")
	}

	major, err := strconv.ParseUint(majorText, 10, 32)
	if err != nil {
		return BlockInfo{}, parseError(device.path, "dev")
	}

	minor, err := strconv.ParseUint(minorText, 10, 32)
	if err != nil {
		return BlockInfo{}, parseError(device.path, "dev")
	}

	sectors, err := unsignedAttribute(device.path, "size", 10, 64)
	if err != nil {
		return BlockInfo{}, err
	}
	if sectors > math.MaxUint64/512 {
		return BlockInfo{}, parseError(device.path, "size")
	}

	readOnly, err := unsignedAttribute(device.path, "ro", 10, 1)
	if err != nil {
		return BlockInfo{}, err
	}
	return BlockInfo{Size: sectors * 512, Major: uint32(major), Minor: uint32(minor), ReadOnly: readOnly == 1}, nil
}

// Attributes lists immediate regular-file attribute names in lexical order,
// without reading them. It excludes subdirectories and symlinks; use a relative
// path with ReadAttribute for nested attributes such as queue/logical_block_size.
func (device *BlockDevice) Attributes() ([]string, error) {
	if device == nil {
		return nil, os.ErrInvalid
	}
	return attributes(device.path)
}

// ReadAttribute reads one relative UTF-8 attribute path without interpreting its
// bytes. Empty, absolute, dot, and dot-dot components match os.ErrInvalid.
// Symlinks are followed within the trusted tree. Nonregular files are rejected;
// attributes over 1 MiB match ErrLimit. Missing or unreadable attributes retain
// their filesystem errors. The returned bytes belong exclusively to the caller.
func (device *BlockDevice) ReadAttribute(name string) ([]byte, error) {
	if device == nil {
		return nil, os.ErrInvalid
	}
	return readAttribute(device.path, name)
}
