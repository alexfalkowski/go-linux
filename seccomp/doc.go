//go:build linux

// Package seccomp loads bounded classic-BPF Linux syscall filters. It supports
// native amd64 and arm64 programs, prepending a mandatory architecture guard
// and rejecting the x32 syscall ABI on amd64. See
// https://man7.org/linux/man-pages/man2/seccomp.2.html.
//
// New validates and copies a program without changing kernel state. Load
// irreversibly attaches it to the calling thread or, with explicit AllThreads
// synchronization, every thread in the calling process. Existing separate
// processes are unaffected. Filters are inherited across clone and exec when
// those operations are permitted. Additional filters cannot relax earlier ones.
//
// Lock the loading goroutine with runtime.LockOSThread before Load. Leave it
// locked until it exits, including on errors and recovered panics. Never return
// a thread with changed restrictions to unrelated goroutines. CallingThread
// does not constrain other existing threads or arbitrary new goroutines.
// AllThreads requires compatible filter trees and fails without attaching the
// new filter if any thread cannot synchronize. The caller coordinates policy
// changes; the package supplies no automatic rollback or complete sandbox.
//
// Programs must permit the syscalls needed by Go's scheduler, allocator,
// garbage collector, signals, I/O, and thread/process teardown. A policy valid
// to the kernel can still crash or deadlock a Go program. Test restrictions in
// disposable subprocesses. Pointer arguments cannot be dereferenced by BPF;
// argument widths and truncation remain part of the syscall ABI.
//
// Invalid package inputs match os.ErrInvalid. Kernel errors preserve errno
// through os.SyscallError; synchronization conflicts return SynchronizationError
// with the refusing thread ID. No descriptors are acquired and there is no
// Close operation. User-notification listeners, strict mode, a policy compiler,
// and ptrace or signal-handler management are outside this package.
package seccomp
