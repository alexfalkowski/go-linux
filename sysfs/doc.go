//go:build linux

// Package sysfs provides bounded, read-only block-device and network-interface
// information with an explicit sysfs or fixture root.
//
// FS, BlockDevice, and NetworkInterface are immutable path-based views, safe for
// concurrent use. They retain no descriptors and need no Close. Nil receivers
// and zero views report os.ErrInvalid from operations. Names and identifiers
// can be reused; symlinks, removal, and replacement affect every lookup. Neither
// a view nor a successful multi-file read establishes stable identity or an
// atomic snapshot. Roots are trusted input, not confinement boundaries.
//
// Filesystem errors preserve errno through errors.Is and errors.As. Invalid
// selected values match ErrParse, and failed operations return no partial
// result. ReadAttribute preserves uninterpreted bytes, including whitespace and
// unknown formats; it reads at most 1 MiB and rejects larger files with ErrLimit.
// The package exposes no mutation or driver-specific control operations.
//
// See the Linux [sysfs documentation] for the underlying interface.
//
// [sysfs documentation]: https://github.com/torvalds/linux/blob/v7.0/Documentation/filesystems/sysfs.rst
package sysfs
