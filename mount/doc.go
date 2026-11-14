//go:build linux

// Package mount provides descriptor-based filesystem configuration and mount
// operations on Linux 7.0+. It has no legacy mount backend.
//
// Open acquires a Config. SetFlag and SetString configure it, Create prepares
// the filesystem, and Mount acquires a detached Handle. Clone independently
// acquires a detached copy of an existing tree. SetAttributes and Attach are
// explicit operations. Stat and Attributes inspect the current mount. Callers
// own both resources and must close each one. Unmount removes an attachment.
// Closing an attached handle does not unmount it. Failed acquisitions return
// nil and retain no newly acquired descriptor; borrowed inputs remain open.
//
// Callers supply namespace isolation, capabilities, path policy, and unmount
// orchestration. Operations require applicable CAP_SYS_ADMIN and filesystem
// support; permission/filter errors are preserved without fallback. Config
// failures can include kernel diagnostics in an Error without losing errno
// identity. No syscall deadline, cancellation, or transaction is promised.
package mount
