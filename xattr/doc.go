//go:build linux

// Package xattr reads and modifies Linux extended attributes on standard files
// and directory-relative paths using the Linux 7.0 baseline at-style calls.
// Filesystem support, namespace permissions, and security policy remain visible
// as errno errors; no fallback or attribute-namespace translation is performed.
//
// Files are borrowed and stay caller-owned on success and failure. Calls pin
// their descriptor against concurrent Close, do not change offsets or flags,
// and may block in the kernel without cancellation. Separate calls are safe
// concurrently, but sequences are not transactions. Get and List return owned
// snapshots from their final read, not an atomic snapshot across operations.
//
// Errors are *os.PathError values preserving errno. Nil files match os.ErrInvalid;
// closed files match os.ErrClosed. Invalid set modes match os.ErrInvalid before
// file access; names and paths otherwise retain Linux validation and semantics.
// No descriptors are acquired, retained, or exposed by this package.
package xattr
