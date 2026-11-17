# capability

`github.com/alexfalkowski/go-linux/capability` inspects and changes the calling Linux thread's capability sets. It requires Linux 7.0+ and permitted [capget/capset](https://man7.org/linux/man-pages/man2/capget.2.html) and capability [prctl](https://man7.org/linux/man-pages/man2/prctl.2.html) operations. Capabilities grant authority in applicable user namespaces; a successful query does not establish permission for a particular facility operation. These mechanisms do not form a complete sandbox.

## Inspect thread credentials

```go
runtime.LockOSThread()
defer runtime.UnlockOSThread() // Inspection only; no credentials changed.

sets, err := capability.Current()
if err != nil {
    return err
}
fmt.Println(sets.Effective&capability.NetAdmin != 0)

bounded, err := capability.Bounding(capability.NetAdmin)
if err != nil {
    return err
}
fmt.Println(bounded)
```

Imports are `fmt`, `runtime`, and `github.com/alexfalkowski/go-linux/capability`. [Executable examples](../capability/example_test.go) demonstrate inspection and mask operations.

`Current` reads permitted, effective, and inheritable masks in one version-3 `capget` call. It returns zero `Sets` on error. `Bounding` and `Ambient` query membership for exactly one capability, returning false on error. Lock the goroutine before related observations so it cannot move between threads. Multiple calls, including a series of membership queries, do not form an atomic snapshot.

`Set` is a 64-bit mask; constants such as `Chown`, `NetAdmin`, `MACOverride`, and `BPF` select bits, rather than numeric CAP_* indexes. Combine with `|`, test with `&`, and remove with `&^`. All 41 Linux 7.0 capabilities through `CheckpointRestore` are named in GoDoc. Zero is a valid empty set for `SetCurrent`. Operations on one capability reject zero, multiple bits, and unknown bits. Input accepts only the named baseline capabilities; `Current` preserves unknown output bits within the 64-bit ABI. A snapshot from a newer kernel containing unknown bits must be handled explicitly before replacement; the package never silently masks them away.

## Change a dedicated thread

Mutations belong on a dedicated goroutine locked with `runtime.LockOSThread` before inspection or mutation. Perform subsequent credential-dependent work synchronously on that goroutine, and leave it locked until it exits, including after failure or recovered panic. Do not defer `runtime.UnlockOSThread`. Go retires a locked thread when its goroutine exits; the startup thread may remain permanently parked until process exit. See the [Go thread contract](https://pkg.go.dev/runtime#LockOSThread).

For example, inside that dedicated goroutine:

```go
runtime.LockOSThread() // Intentionally never unlocked.
sets, err := capability.Current()
if err != nil {
    return err
}
sets.Effective &^= capability.NetRaw
if err := capability.SetCurrent(sets); err != nil {
    return err
}
// Perform the application's synchronous work here, then return.
```

This removes only this thread's effective CAP_NET_RAW. It retains the permitted bit, allowing the thread to enable it again. Existing other threads keep their credentials, and starting a new goroutine does not guarantee it receives these sets. Locking one goroutine does not restrict the whole Go program. The package offers explicit operations without a scoped callback, automatic restoration, thread enumeration, or process-wide broadcast.

`SetCurrent` replaces permitted, effective, and inheritable sets together in one syscall. `Sets{}` clears all three. Linux checks the transition: effective must be a subset of the proposed permitted set, permitted bits cannot be regained with `capset`, and inheritable additions depend on current authority and bounding membership. Permission failures preserve EPERM. Lowering permitted or inheritable bits also lowers matching ambient bits. The bounding set remains unchanged.

## Bounding and ambient sets

`DropBounding` uses [PR_CAPBSET_DROP](https://man7.org/linux/man-pages/man2/PR_CAPBSET_DROP.2const.html). It requires effective CAP_SETPCAP, including when the bit is already absent. There is no operation to raise bounding membership. A drop leaves current permitted, effective, inheritable, and ambient sets intact; it is not an immediate removal of authority. Existing inheritable and ambient capabilities can still contribute across exec, so a bounding drop alone is not a complete restriction policy.

`RaiseAmbient`, `LowerAmbient`, and `ClearAmbient` use [PR_CAP_AMBIENT](https://man7.org/linux/man-pages/man2/PR_CAP_AMBIENT.2const.html). Raising requires the bit in both permitted and inheritable sets and can be refused by SECBIT_NO_CAP_AMBIENT_RAISE. Raising ambient does not enable the effective bit immediately. Lowering an absent bit and clearing an empty set succeed; lowering or clearing leaves the other capability sets unchanged. Each per-bit operation takes exactly one named capability. Multiple changes are separate syscalls with no rollback.

## Process creation and exec

Use `os/exec` for process creation. Start a child synchronously from the dedicated locked goroutine when it must inherit that thread's credentials. Linux then applies its [exec capability transformations](https://man7.org/linux/man-pages/man7/capabilities.7.html): permitted/effective masks are generally recomputed; ambient capabilities can carry through an ordinary executable; privileged executables can clear ambient state. UID 0 rules, file capabilities, securebits, and no-new-privileges affect the result. Clearing effective or permitted sets immediately before exec does not by itself promise that exec cannot regain authority. Bounding drops are inherited across exec but do not clear existing inheritable or ambient bits.

For ambient setup performed solely at child startup, the standard Linux `syscall.SysProcAttr.AmbientCaps` field remains available through `exec.Cmd`. That field takes CAP_* numeric indexes, whereas this package's constants are masks. This package changes existing thread state; it does not replace Go's process creation or supply a capability policy compiler.

File capability encoding, UID/GID changes, securebits mutation, no-new-privileges configuration, and inspection/mutation by arbitrary PID or TID are outside this package. They are not applied implicitly. In particular, the package never changes executable attributes or host-wide security policy.

## Errors and lifetime

All values own no resources and require no `Close`. Independent calls use no shared mutable package state. The caller owns thread affinity, multi-operation coordination, and any synchronization around a mutable `Sets` value.

Invalid masks match `os.ErrInvalid` before a syscall; other errors use `os.SyscallError` and preserve Linux errno for `errors.Is`/`errors.As`. EPERM, EINVAL, ENOSYS, security-policy denials, and resource failures remain distinguishable. The package does not retry a failed transition, weaken restrictions, or undo earlier successful operations in a sequence.

See [design decisions](design/capability.md) and [verification](verification.md#capability-verification).
