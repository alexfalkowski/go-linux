//go:build linux

// Package memfd creates Linux memory-backed files with irreversible file seals.
// It requires Linux 7.0 or later and permitted memfd_create and fcntl syscalls.
//
// New returns a sealable, non-executable file by default. File supports ordinary
// Go I/O, seeking, sizing, and metadata without exposing its descriptor. Map
// creates an independently owned memory region. Close each owner explicitly.
//
// Seals affect the underlying inode, including other descriptors and mappings
// referring to it. They cannot be removed. SealExec locks execute permission
// bits; it does not prevent executable memory mappings. File seals are not a
// general memory-execution sandbox.
//
// See [memfd_create(2)] and [F_GET_SEALS(2const)] for Linux semantics.
//
// [memfd_create(2)]: https://man7.org/linux/man-pages/man2/memfd_create.2.html
// [F_GET_SEALS(2const)]: https://man7.org/linux/man-pages/man2/F_GET_SEALS.2const.html
package memfd
