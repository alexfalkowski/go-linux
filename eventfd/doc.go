//go:build linux

// Package eventfd provides Linux event counters with Go polling and deadlines.
// It requires Linux 7.0 or later and permitted eventfd2 and polling operations.
//
// New creates an owned Counter. Default mode drains the accumulated count on
// each read; Semaphore mode consumes one unit per read. Close the counter when
// done. Reads and writes use native-endian eight-byte records rather than an
// arbitrary byte stream. ReadValue and Add handle encoding for typed callers.
//
// Descriptors are nonblocking and close-on-exec, but Counter I/O waits through
// Go's poller until it can proceed, a deadline expires, or Close interrupts it.
// SyscallConn provides borrowed access to the same nonblocking descriptor.
//
// See [eventfd(2)] for Linux counter semantics.
//
// [eventfd(2)]: https://man7.org/linux/man-pages/man2/eventfd.2.html
package eventfd
