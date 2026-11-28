//go:build linux

// Package mmap owns Linux anonymous and file-backed memory mappings.
//
// Map borrows an os.File during construction; Anonymous needs no file. Close
// releases the mapping independently of the source file. Region implements
// io.Closer, with explicit Private/Shared modes, page protection, advice,
// synchronization, memory locking, and page residency inspection.
//
// Bytes returns a borrowed slice, not managed Go storage. All access must stop
// before Close, and callers must coordinate access with Protect and Advise.
// Coordinate writers with Sync to control which writes are included. File
// truncation or protection violations can cause fatal faults.
//
// The package targets Linux 7.0+ on native amd64 and arm64.
// See docs/verification-limits.md in the repository for execution limits.
package mmap
