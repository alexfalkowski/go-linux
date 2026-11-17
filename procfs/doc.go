//go:build linux

// Package procfs provides bounded, read-only process and system information
// through a project-owned API backed by github.com/prometheus/procfs.
//
// Open accepts an explicit proc mount or fixture directory. FS and Process are
// immutable path-based views: they retain no descriptors and need no Close.
// Their zero values and nil receivers report os.ErrInvalid from operations.
// Reads may run concurrently, but changing files, permissions, process exit,
// and PID reuse can affect every call. Neither a view nor several successful
// reads establish a stable process identity or an atomic snapshot.
//
// Filesystem errors retain errno through errors.Is and errors.As. Malformed
// selected records match ErrParse; results are discarded on error. Unknown
// output flag bits survive inspection. Roots are trusted input, not confinement
// boundaries: symlinks are followed and an alternate mount may describe another
// PID namespace. The package never mutates procfs.
//
// See the Linux [proc filesystem] documentation for the underlying interfaces.
//
// [proc filesystem]: https://github.com/torvalds/linux/blob/v7.0/Documentation/filesystems/proc.rst
package procfs
