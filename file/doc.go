//go:build linux

// Package file provides bounded Linux file extensions while reusing os.File
// for ownership and ordinary I/O. Prefer os and os.Root for ordinary operations.
//
// Open exposes openat2 resolution restrictions. Stat and StatAt query statx
// validity masks, birth time, attributes, and mount identity. Rename exposes
// renameat2 replacement and exchange modes. Fallocate reserves storage and
// changes file ranges. Fadvise and Readahead guide kernel caching. Fdatasync
// synchronizes file data, SyncFS synchronizes the containing filesystem, and
// Flock applies whole-file advisory locks.
// The module supports Linux 7.0 or later. Filesystem and security policy can
// still reject individual operations.
//
// Input files are borrowed only during each call and remain caller-owned.
// Concurrent Close is safe: each syscall pins its input descriptors or returns
// an error matching os.ErrClosed. Calls may block in the kernel; no deadline
// or context cancellation is promised. Sequences are not transactions.
// Errors preserve errno identity through os.PathError or os.LinkError.
//
// Only Open enforces resolution restrictions, and only for that lookup.
// StatAt and Rename use ordinary Linux path resolution and do not confine
// paths beneath their directory arguments. An open file does not freeze its
// contents, metadata, or directory entries.
package file
