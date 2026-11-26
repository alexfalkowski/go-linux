# namespace

`github.com/alexfalkowski/go-linux/namespace` provides owned Linux namespace handles, inspection, identity comparison, and explicit [setns](https://man7.org/linux/man-pages/man2/setns.2.html)/[unshare](https://man7.org/linux/man-pages/man2/unshare.2.html) operations. It requires Linux 7.0+, the relevant namespace kernel configuration, and permitted procfs/ioctl/syscall access. Capabilities in the applicable user namespaces, namespace quotas, and container security policy remain kernel prerequisites. These mechanisms do not form a complete container runtime or sandbox.

## Good for

Good for inspecting isolation boundaries or explicitly entering an existing namespace. [runc uses namespaces to isolate container resource views](https://github.com/opencontainers/runc/blob/main/THREAT_MODEL.md); the application still owns thread affinity and process startup.

## Open and inspect

```go
handle, err := namespace.Current(namespace.Network)
if err != nil {
    return err
}
defer handle.Close()

kind, err := handle.Type()
if err != nil {
    return err
}
fmt.Println(kind == namespace.Network)
```

Imports are `fmt` and `github.com/alexfalkowski/go-linux/namespace`. [Executable examples](../namespace/example_test.go) also demonstrate comparing independent handles.

`Current` selects the calling thread through `/proc/thread-self/ns`, taking exactly one of `Mount`, `Cgroup`, `IPC`, `Network`, `PID`, `Time`, `User`, or `UTS`. Lock the goroutine to its OS thread before a sequence that depends on membership. `Open` follows a procfs namespace link or a bind-mounted namespace path; PID-based paths can race with exit or PID reuse. Neither operation requires a descriptor from the caller. After acquisition the handle pins that namespace independently of the original process and path.

`Type` uses `NS_GET_NSTYPE` and preserves unknown output values. `Equal` compares device/inode identity with both descriptors protected from close/reuse, including when comparing independent opens or bind mounts. It accepts a nil argument only as an error. `UserNamespace` returns an independently owned handle to the owning user namespace; on user namespaces this is the parent. `Parent` returns the parent of a PID or user namespace, with EINVAL for other types. Inaccessible ancestors return EPERM. `OwnerUID` returns a user namespace's creator UID mapped into the caller's user namespace, using the kernel overflow UID when unmapped; other types return EINVAL. These operations follow [nsfs ioctl semantics](https://man7.org/linux/man-pages/man2/ioctl_nsfs.2.html).

## Change a dedicated thread

Namespace operations act on the calling OS thread. They do not move every thread in a Go process, and starting another goroutine does not give it the caller's membership. Use a dedicated goroutine with `runtime.LockOSThread`, perform the operations synchronously there, and leave it locked until it exits. Go retires that OS thread on goroutine exit; its startup thread can remain permanently parked instead of disappearing from procfs, retaining namespace references until process exit. Do this after errors and recovered panics too; do not defer `runtime.UnlockOSThread`. Closing handles releases references without restoring membership. The package provides no automatic restoration or scoped callback API.

For example, this application function opens a routing connection in a target network namespace. Imports are `runtime`, `github.com/alexfalkowski/go-linux/namespace`, `github.com/alexfalkowski/go-linux/netlink`, and `github.com/alexfalkowski/go-linux/netlink/route`:

```go
func openRoute(target *namespace.Handle) (*route.Conn, error) {
    type result struct {
        conn *route.Conn
        err  error
    }
    done := make(chan result, 1)
    go func() {
        runtime.LockOSThread()
        if err := target.Enter(); err != nil {
            done <- result{err: err}
            return
        }
        conn, err := route.Open(netlink.Options{})
        done <- result{conn: conn, err: err}
    }()
    opened := <-done
    return opened.conn, opened.err
}
```

`target` must identify a network namespace, remain open during entry, and be caller-owned. On success the caller closes the returned connection. The socket keeps its network namespace after the goroutine exits. No new goroutine performs namespace-dependent work in this function. The [Go runtime thread contract](https://pkg.go.dev/runtime#LockOSThread) explains why exiting a locked goroutine prevents the changed thread from returning to unrelated Go work.

`Unshare(namespace.Network | namespace.UTS)` creates both namespaces in one kernel operation. Input accepts only the supported `CLONE_NEW*` bits; zero and unknown bits match `os.ErrInvalid`. Linux may also unshare related execution context: mount creation unshares filesystem attributes, and IPC creation unshares System V semaphore adjustments. No rollback is attempted, including on failure.

| Namespace | `Unshare` effect | `Enter` restrictions and effect |
| --- | --- | --- |
| Network, UTS, cgroup | Creates and selects the calling thread's namespace. | Selects the calling thread's namespace, subject to capabilities. Cgroup membership itself does not move. |
| IPC | Creates/selects IPC namespace and unshares semaphore adjustments. | Selects the calling thread's IPC namespace, subject to capabilities. |
| Mount | Creates/selects a mount namespace and unshares filesystem attributes. | Go threads share filesystem attributes; first use `Unshare(Mount)` on the dedicated thread to separate them. CAP_SYS_CHROOT and applicable CAP_SYS_ADMIN are required. Mount propagation remains unchanged; make it private before mount mutations. |
| PID | Creates a namespace for future children; the caller's active PID namespace remains unchanged. | Selects the same or a descendant PID namespace for future children. The caller remains in its active PID namespace. |
| Time | Creates a namespace for future children without moving the caller. | Requires a single-threaded process; ordinary Go programs receive EUSERS. Successful kernel entry would change active and child membership and the monotonic/boottime clock domain. |
| User | Requires a single-threaded process; ordinary Go programs receive EINVAL. | Requires a single-threaded process and unshared filesystem attributes; same-namespace reentry is forbidden. Ordinary Go programs cannot satisfy this by locking one goroutine. |

PID and time child membership can be inspected with `Open("/proc/thread-self/ns/pid_for_children")` or `time_for_children`. A fresh PID namespace's link may remain unopenable until its first child exists. That child is PID 1 and must perform the usual init/reaping duties; its exit terminates the namespace's processes. A process started synchronously with `exec.Cmd` from the locked goroutine inherits that thread's child membership. Avoid changing active time namespaces around a running Go runtime; this package does not manage clock offsets or reconcile runtime clock state.

When the mask includes `namespace.PID`, `Unshare` initializes Go's process support before changing namespace state and releases its temporary process reference before calling unshare. This keeps [Go's lazy pidfd probe](https://go.dev/src/os/pidfd_linux.go), which [clones and reaps a child](https://go.dev/src/syscall/exec_linux.go), from becoming the namespace's PID 1 and exiting before the intended init starts. Callers can start that first child synchronously with `exec.Cmd` without doing a separate process lookup first. The child still owns the usual PID 1 duties, and once it exits, later child creation in that PID namespace fails with ENOMEM.

For new processes, use `exec.Cmd.SysProcAttr` with Linux `syscall.SysProcAttr.Cloneflags`, `Unshareflags`, and UID/GID mappings. Go performs this setup before the child runtime starts. The package adds no raw fork/clone API and no process-wide namespace promise. User namespace creation belongs on that standard process-start path. No ID-map writer, hostname mutation, namespace enumeration, pidfd multi-namespace entry, or time-offset API is provided.

## Ownership, lifetime, and errors

`Open`, `Current`, `Parent`, and `UserNamespace` return one independently owned handle or nil plus an error, closing anything acquired on failure. `Adopt` immediately consumes a standard `*os.File` on success and failure; never use or close the input afterward, including on error. It requires an nsfs namespace file with FD_CLOEXEC, rejects O_PATH and other file types, and leaves status flags unchanged. This permits adopting namespace files received through typed socket file passing. Acquisitions use atomic close-on-exec; no descriptor or raw callback is exposed.

A handle must not be copied after use. Its methods protect descriptor lifetime against concurrent close; multi-operation sequences and thread affinity remain caller responsibilities. A zero handle owns nothing and matches `os.ErrInvalid`; nil receivers are unsupported. Repeated close and valid operations after close match `os.ErrClosed`. An overlapping syscall can finish after close begins, and final descriptor release may be deferred until protected operations finish.

Invalid `Current` types and `Unshare` masks fail before syscalls. `Equal` validates its receiver before its argument. Other errors preserve Linux errno with `errors.Is`/`errors.As`, normally through `os.PathError`; `Unshare` uses `os.SyscallError`. EPERM, EINVAL, EUSERS, ENOSYS, and resource-limit failures remain distinct. Missing privilege is never replaced by a weaker transition.

See [design decisions](agents/accepted-design/namespace.md) for API tradeoffs and [verification](verification.md#namespace-verification) for the isolated test workflow.
