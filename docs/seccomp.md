# seccomp

`github.com/alexfalkowski/go-linux/seccomp` constructs and loads classic-BPF syscall filters on native amd64 and arm64 Linux 7.0+. It uses [seccomp(2)](https://man7.org/linux/man-pages/man2/seccomp.2.html) and [PR_SET_NO_NEW_PRIVS](https://man7.org/linux/man-pages/man2/PR_SET_NO_NEW_PRIVS.2const.html). Loading requires kernel filter support, permission under existing security policy, and either no-new-privileges or effective CAP_SYS_ADMIN in the caller's user namespace. These mechanisms do not constitute a complete sandbox.

## Good for

Good for restricting the native syscalls a workload may invoke. [runc applies seccomp policies during container setup](https://github.com/opencontainers/runc/blob/main/THREAT_MODEL.md); callers must choose filters that accommodate both the workload and the Go runtime.

## Construct a native program

Construction validates and copies instructions without a syscall or policy change. Supply the architecture for which the program's syscall numbers were chosen. `NativeArchitecture` returns `AMD64` or `ARM64`; other architectures return EOPNOTSUPP. `New` rejects an architecture that differs from the running Go program's ABI, but cannot infer whether arbitrary instruction constants describe the caller's intended syscalls. Use native constants with the native architecture.

For example, deny `getpgid` while allowing other native syscalls:

```go
architecture, err := seccomp.NativeArchitecture()
if err != nil {
    return err
}
filter, err := seccomp.New(architecture, []seccomp.Instruction{
    {Code: seccomp.LoadWord, K: seccomp.SyscallOffset},
    {Code: seccomp.JumpEqual, K: unix.SYS_GETPGID, Jf: 1},
    {Code: seccomp.Return, K: uint32(seccomp.Errno) | uint32(unix.EACCES)},
    {Code: seccomp.Return, K: uint32(seccomp.Allow)},
})
if err != nil {
    return err
}
```

Imports are `github.com/alexfalkowski/go-linux/seccomp` and `golang.org/x/sys/unix`; the latter supplies native syscall numbers and errno constants, without a copied syscall table. This small deny-list demonstrates mechanics, not a security policy. Production restrictions need a policy designed for the whole application. [Executable examples](../seccomp/example_test.go) demonstrate construction and action discovery safely.

The loader prepends an architecture check that terminates the entire process with SIGSYS on another audit ABI. On amd64 it also terminates calls carrying the x32 syscall bit, since x32 shares the x86-64 audit architecture. These guards cannot be removed or bypassed by jumps within the caller's body. They also prevent exec into a different syscall ABI. The body starts with accumulator zero.

## Load with explicit scope

During development, perform loading and restricted work only in a disposable subprocess. Inside that child's dedicated goroutine:

```go
runtime.LockOSThread() // Intentionally never unlocked, including on failure.
err := filter.Load(seccomp.Options{
    Synchronization: seccomp.AllThreads,
    NoNewPrivileges: true,
})
if err != nil {
    return err
}
// Continue the child's policy-compatible work here.
```

`AllThreads` requests kernel TSYNC for all existing threads of the calling process. Every thread must have a compatible filter tree; attaching identical filters separately can still create incompatible branches. A conflict returns `*SynchronizationError` with the refusing Linux `ThreadID`, and the new filter is not attached anywhere. Use `errors.As` for that case. There is no retry or fallback to a narrower scope. The caller coordinates competing loads and policy transitions.

`CallingThread` explicitly restricts only the loading thread. Lock its goroutine before loading and keep it locked until it exits; do not defer `runtime.UnlockOSThread`. Other existing threads keep their policies, and a new goroutine may run on one of those threads. This mode cannot promise restrictions for the whole Go program. Go destroys ordinary locked threads on goroutine exit; its startup thread may instead remain parked until process exit. Neither case rolls back restrictions inherited by descendants.

`NoNewPrivileges: true` first sets the calling thread's irreversible bit. That change persists even if the subsequent seccomp call fails, including a TSYNC conflict. Successful TSYNC propagates the bit when it is set on the loading thread. False leaves the existing bit intact and requires that bit or CAP_SYS_ADMIN. Invalid filter/scope inputs are rejected before any change. `Log: true` requests SECCOMP_FILTER_FLAG_LOG; actual audit records depend on administrator policy.

Successful loads are irreversible. Repeated calls stack filters, subject to the kernel's aggregate instruction limit; an Allow result cannot override an earlier higher-precedence denial. There is no descriptor, `Close`, unload, automatic restoration, or success idempotency. `Filter` is immutable, owns a copy of its instructions, and may be shared by concurrent loads; kernel policy still needs caller coordination. Use a pointer and do not copy it after use. Nil and zero filters cannot be loaded.

## Instructions and actions

The bounded representation supports aligned native-endian 32-bit absolute loads, constant loads, constant bitwise AND, unconditional forward jumps, unsigned equal/greater/greater-or-equal/bit-intersection comparisons, and constant returns. Programs contain 1 through `MaxInstructions` (4089) body instructions and end in Return. The reserved guard keeps the complete program within Linux's 4096-instruction limit. Unknown opcodes, out-of-body jump targets, ignored nonzero fields, computed returns, index register operations, and scratch memory are rejected.

`LoadWord.K` is an aligned byte offset within the 64-byte `seccomp_data`. Named offsets select the syscall number, audit architecture, instruction pointer, and arguments. On both supported little-endian ABIs, argument n's low/high words are at `ArgumentsOffset+8*n` and `ArgumentsOffset+8*n+4`. Every argument occupies 64 bits in seccomp data even when the syscall truncates it to 32 bits. Filters cannot dereference pointers, inspect path strings, or prevent pointer-content races. Handle scalar widths and alternate argument encodings deliberately.

`Jt`, `Jf`, and unconditional `Jump.K` count instructions skipped from the following instruction. Both conditional destinations must stay in the body. Return K contains `uint32(action)` combined with optional low 16-bit data:

| Action | Effect and data |
| --- | --- |
| `KillProcess` | Terminate the entire thread group with SIGSYS. Data must be zero. |
| `KillThread` | Terminate only the triggering thread. Data must be zero. This can strand locks and corrupt runtime state; prefer KillProcess for termination. |
| `Trap` | Deliver synchronous SIGSYS without executing the syscall; data is si_errno. Go's synchronous fatal-signal path terminates the process; this package installs no signal handler. |
| `Errno` | Skip the syscall and return data as errno, clamped by Linux to 4095. Zero reports success without performing the operation. |
| `Trace` | Notify an external PTRACE_O_TRACESECCOMP tracer with data, or return ENOSYS when no tracer is attached. This package manages no tracer. |
| `Log` | Allow and request logging, subject to host policy. Data must be zero. |
| `Allow` | Allow unless another stacked filter has higher precedence. Data must be zero. |

`ActionAvailable` probes a single named action without data; it returns false without an error for kernel EOPNOTSUPP, and preserves other failures. Unknown actions and user notification are rejected during construction instead of relying on Linux's fatal unknown-action behavior. Notification listeners/brokers, strict mode, speculative-execution opt-out, and a policy compiler remain outside this package.

Against the [Linux 7.0 seccomp UAPI](https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/seccomp.h), the loader exposes TSYNC and LOG only. NEW_LISTENER, notification-size queries, receive/respond/ID-validation/ADDFD ioctls, listener flags, and WAIT_KILLABLE_RECV are absent. TSYNC_ESRCH is not selected: synchronization conflicts retain the documented refusing-TID result. There is no notification descriptor hidden behind an existing `Filter` operation.

## Runtime, inheritance, and errors

A valid filter can crash or deadlock Go by denying scheduler, futex, memory, signal, time, I/O, or thread-exit syscalls. Runtime needs vary by Go release, architecture, enabled race instrumentation, and application behavior. The package does not silently add syscall exceptions or claim that an example filter is sufficient for a Go application. A successful load only establishes the requested kernel filter operation.

When clone/fork and exec are allowed, descendants inherit the creating thread's filter tree; exec preserves it, and no-new-privileges prevents ordinary exec privilege gains. Existing separate processes are unaffected. Use standard `os/exec`, synchronously on the locked goroutine when inheritance depends on that thread. There is no raw fork wrapper or child-start hook in this package.

Package validation errors match `os.ErrInvalid` through `os.SyscallError`. Kernel EPERM, EACCES, EINVAL, ENOSYS, resource errors, and policy-denied probes remain distinguishable with `errors.Is`/`errors.As`. A positive refusing TID is a `SynchronizationError`, not successful loading or a fabricated errno. The package never retries a failed irreversible operation or weakens the requested policy.

See [design decisions](agents/accepted-design/seccomp.md) and [verification](verification.md#seccomp-verification).
