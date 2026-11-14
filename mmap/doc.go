//go:build linux

// Package mmap owns Linux anonymous and file-backed memory mappings.
//
// Map borrows an os.File during construction; Anonymous needs no file. Close
// releases the mapping independently of the source file. Region implements
// io.Closer, with explicit Private/Shared modes, page protection, and advice.
//
// Bytes returns a borrowed slice, not managed Go storage. All access must stop
// before Close, and callers must coordinate access with Protect and Advise.
// Escaped slices cannot be made safe after unmapping. File truncation or
// protection violations can cause fatal faults; mapping neither freezes a file
// nor provides a memory-safety or persistence guarantee.
//
// The package targets Linux 7.0+; required runtime verification uses arm64.
// See the repository's implementation plan for current verification gaps.
package mmap
