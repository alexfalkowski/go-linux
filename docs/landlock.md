# landlock

`github.com/alexfalkowski/go-linux/landlock` builds and applies Linux access rulesets. The package targets Linux 7.0+ with Landlock enabled and permitted by existing security policy. It exposes [Landlock's filesystem, port, and IPC mechanisms](https://docs.kernel.org/7.0/userspace-api/landlock.html), without providing a complete sandbox or container runtime.

## Discover and build a policy

`ABI()` queries the running kernel without acquiring a resource or changing policy. Linux 7.0 implements ABI 8. ENOSYS means the syscall is absent; EOPNOTSUPP means Landlock is disabled. Existing seccomp/LSM policy can deny the query separately. Do not convert every error into an unsupported-feature result.

`New(Config)` creates an owned close-on-exec `*Ruleset`. Handled access rights are denied by default, then individual rules allow access to selected objects. Unhandled rights stay unaffected, with one historical exception: cross-directory linking/renaming requires explicit `Refer` handling and allowance. Zero Config is invalid; filesystem-only, network-only, and scope-only configurations are valid. No constructor or Add method restricts the caller.

For a policy that handles file reads/writes and TCP connections:

```go
ruleset, err := landlock.New(landlock.Config{
    HandledFS:  landlock.ReadFile | landlock.WriteFile,
    HandledNet: landlock.ConnectTCP,
})
if err != nil {
    return err
}
defer ruleset.Close()

if err := ruleset.AddPath("/srv/input", landlock.ReadFile); err != nil {
    return err
}
if err := ruleset.AddPort(443, landlock.ConnectTCP); err != nil {
    return err
}
```

The import is `github.com/alexfalkowski/go-linux/landlock`. The file hierarchy must already exist. This illustrative policy denies opening files for writing and permits reads only beneath `/srv/input` once applied; a real application must allow its runtime and workload's required access. [Executable examples](../landlock/example_test.go) demonstrate construction and restriction in the test harness.

`AddPath` opens a temporary O_PATH handle, follows symlinks, and closes the handle on every outcome. Relative paths use the current directory. Rules refer to kernel objects rather than saved path strings: renaming an allowed object preserves its allowance, and replacing its old pathname does not inherit that allowance. Linux hierarchy and mount semantics still apply. For explicit lookup restrictions, open with the library's [`file.Open`](file.md) and pass the standard file to `AddFile`; there is no raw descriptor API. `AddFile` pins the borrowed file against close/reuse during the syscall and leaves it open on success and failure. After success the kernel retains the rule independently, so the caller can close the input. Neither operation grants access broader than its mask or the ruleset's handled rights. Linux rejects directory-only rights on a nondirectory. Failed additions leave earlier rules intact; callers should finish and check the entire builder before applying it.

## Apply with explicit thread scope

During development, restrict only a disposable subprocess. On its dedicated goroutine:

```go
runtime.LockOSThread() // Intentionally never unlocked, including after errors.
if err := ruleset.Restrict(landlock.Options{
    Synchronization: landlock.AllThreads,
    NoNewPrivileges: true,
}); err != nil {
    return err
}
// Continue policy-compatible work in this child.
```

This snippet also imports `runtime`. `AllThreads` uses kernel TSYNC, available in baseline ABI 8. The kernel atomically applies the calling thread's resulting Landlock domain and logging configuration to all threads in its process. It also propagates no-new-privileges if set on the caller. There is no thread enumeration, runtime-private integration, or fallback to local enforcement.

**Coordinate sibling policies before TSYNC.** The kernel replaces divergent sibling Landlock domains with the caller's resulting domain; it does not intersect each sibling's existing policy with the new ruleset. Apply from the intended authoritative domain. Repeated applications on the calling thread still stack with that caller's existing restrictions and cannot relax them. Concurrent methods on one Ruleset serialize, but methods on different owners and application-wide policy transitions need caller coordination.

`CallingThread` covers only the calling OS thread and children subsequently created by that thread. It leaves existing sibling threads and existing separate processes unaffected. A goroutine launched after restriction can run on another unrestricted OS thread. Keep the goroutine locked until it exits, even after a failed Restrict or a recovered panic. A worker thread is destroyed when its locked goroutine exits; Go's startup thread may remain parked. Do not use `runtime.UnlockOSThread` to return a changed thread to unrelated work.

Descendants inherit the creating thread's policy across process creation and exec. Use ordinary `os/exec` synchronously on the locked goroutine when inheritance depends on that thread. Existing separate processes do not acquire a later policy, including with AllThreads. The package has no raw fork or process-start hook. Any restriction can prevent runtime I/O or other application progress, so successful kernel application alone does not establish that a policy is usable by the whole program.

`NoNewPrivileges: true` first sets the irreversible PR_SET_NO_NEW_PRIVS bit. It remains set if subsequent restriction fails. False preserves the existing bit and requires either that bit or effective CAP_SYS_ADMIN in the caller's user namespace. Invalid inputs, closed owners, and unsupported ABI options fail before attempting this transition. No operation clears the bit, retries failed enforcement, or promises rollback.

`Restrict` snapshots the current rules into a new domain. Adding further rules changes future applications rather than an already applied domain. Repeated applications stack with at most 16 layers, subject to inherited layers; exhaustion returns E2BIG. Closing a Ruleset releases its descriptor and never undoes an applied policy. Inputs borrowed for rules remain separately owned. Nil/zero owners match `os.ErrInvalid`; valid operations after close and repeated close match `os.ErrClosed`. Do not copy a Ruleset after first use.

## Rights and limits

| Policy | Named rights | Minimum ABI |
| --- | --- | --- |
| File opens and directory operations | `Execute`, `WriteFile`, `ReadFile`, `ReadDir`, `RemoveDir`, `RemoveFile`, `MakeChar`, `MakeDir`, `MakeReg`, `MakeSock`, `MakeFIFO`, `MakeBlock`, `MakeSym` | 1 |
| Cross-directory links/renames | `Refer`, plus applicable source removal/destination creation rights | 2 |
| File truncation | `Truncate`, independently of `WriteFile` | 3 |
| TCP ports | `BindTCP`, `ConnectTCP` | 4 |
| Device ioctls, with Linux's common-ioctl exemptions | `IOCTLDev` | 5 |
| Domain-local IPC | `Scoped: AbstractUnixSocket`, `Signal` | 6 |
| Audit configuration | `LogSameExecOff`, `LogNewExecOn`, `LogSubdomainsOff` | 7 |
| All-thread synchronization | `AllThreads` | 8; Linux 7.0 |
| Pathname Unix socket resolution outside the domain | `ResolveUnix` | 9; Linux 7.1, optional |
| UDP ports | `BindUDP`, `ConnectSendUDP` | 10; Linux 7.2, optional |

Newer rights are explicit and never inferred from a general "all" mask. New rejects known rights unavailable in the discovered ABI with EOPNOTSUPP and returns no owner. Unknown bits and empty/out-of-policy rule masks match `os.ErrInvalid`. A caller may deliberately choose a smaller policy after discovery; the package never discards requested restrictions. [Linux 7.1](https://github.com/torvalds/linux/blob/v7.1/include/uapi/linux/landlock.h) and [7.2 UAPI](https://github.com/torvalds/linux/blob/v7.2/include/uapi/linux/landlock.h) define the optional extensions. Newer quiet-rule and permission-category interfaces and errata discovery are outside this package.

Filesystem restrictions do not revoke already-open file access. Truncation/device-ioctl authority associated with an open file is established when it is opened. Read/write handling does not restrict metadata operations such as stat, chmod, or xattrs; mount topology and ptrace have additional kernel domain restrictions. Refer grants still obey Linux's source/destination and no-privilege-gain rules, including possible EXDEV. This interface does not implement a general syscall sandbox.

Port rules use host-order `uint16` values, apply to all IP addresses and IPv4/IPv6, and have no address filtering. Port zero allows requesting ephemeral binding, not every explicit port. BindTCP does not restrict automatic client port assignment. With optional UDP binding and sending both handled, automatic ephemeral assignment requires BindUDP on zero or an already-bound socket. TCP rights do not cover UDP; no network restriction is implied when HandledNet is zero.

Scopes have no exception rules. AbstractUnixSocket restricts connections to abstract servers outside the domain; pathname sockets are independent. Signal restricts signaling processes outside the domain, with a same-thread-group exception. Optional ResolveUnix restricts pathname Unix connect and explicit-address sends to servers outside the domain; servers created in the same domain remain accessible. These mechanisms do not revoke existing connections or isolate every IPC mechanism.

Log flags preserve Linux defaults when zero. Audit delivery depends on host configuration; acceptance of flags does not guarantee records. Kernel errors preserve errno through `os.SyscallError`; path lookup errors also retain `os.PathError`. Inspect with `errors.Is`/`errors.As`, preserving policy denials, disabled-facility errors, resource exhaustion, and failed application as distinct outcomes.

See [design decisions](design/landlock.md) and [verification](verification.md#landlock-verification).
