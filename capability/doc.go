//go:build linux

// Package capability inspects and changes the calling Linux thread's capability
// sets. It wraps capget, capset, and the capability prctl operations without
// changing other threads, UIDs, securebits, or executable file capabilities.
// See https://man7.org/linux/man-pages/man7/capabilities.7.html.
//
// Lock a goroutine with runtime.LockOSThread before a sequence of inspections.
// For mutations, use a dedicated locked goroutine and leave it locked until it
// exits, including on errors and recovered panics. Go then retires that thread;
// unlocking would expose unrelated goroutines to the changed credentials.
// Other existing threads are unaffected, and a new goroutine need not inherit
// the changed state. This package makes no process-wide restriction promise.
//
// Child processes started synchronously on the locked goroutine inherit its
// credentials subject to Linux exec transformations. Permitted/effective sets
// are not generally preserved across exec. Ambient capabilities, file
// capabilities, UID 0 rules, securebits, and no-new-privileges affect the result.
// Use os/exec for process creation; this package supplies no fork/exec wrapper,
// automatic rollback, or complete sandbox.
//
// All operations preserve Linux errno through os.SyscallError. Invalid input
// masks match os.ErrInvalid before a syscall. No descriptors or other resources
// are acquired. Independent inspections do not form an atomic snapshot.
package capability
