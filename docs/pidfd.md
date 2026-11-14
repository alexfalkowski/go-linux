# pidfd

`github.com/alexfalkowski/go-linux/pidfd` provides owned process descriptors, signals, exit observation, and eligible child reaping on Linux 7.0+. Use `os.Process` and `os/exec` for ordinary subprocess management. This package adds independent handle ownership and waits interrupted by closing the handle.

## Observe a command

Start an `exec.Cmd`, call `pidfd.Open(command.Process.Pid)`, and defer closing the returned `*pidfd.Process`. `process.WaitReady()` waits for the last thread to exit without consuming child status. Call `command.Wait()` to reap the child and complete os/exec pipe cleanup. Closing a pidfd neither kills nor reaps the process.

Opening a numeric PID can race with exit, reaping, and PID reuse. Keep the intended child alive or unreaped during acquisition. Once acquired, the handle always refers to that process identity, even after its numeric PID is reused. For acquisition during startup, set `command.SysProcAttr.PidFD` before `Start`, check the returned descriptor is not `-1`, wrap it with `os.NewFile`, and adopt it. Go maintains its own separate descriptor for the command.

See the executable [observation example](../pidfd/example_test.go) for a bounded command lifecycle and the [package documentation](../pidfd/doc.go) for the package boundary.

## Adopt an existing handle

`pidfd.Adopt(file)` consumes exclusive ownership immediately, whether it succeeds or fails. It closes the input on any validation or construction failure; callers do not need a separate error-path `file.Close()`. After calling Adopt, stop using the original file. On success, close the returned process when done. Adoption accepts blocking and nonblocking process pidfds with close-on-exec set. It rejects other file types, thread pidfds, O_PATH descriptors, and inheritable descriptors with `os.ErrInvalid`. It preserves shared file status flags and any existing file deadlines.

To obtain independent ownership from `os.Process.WithHandle`, call `unix.FcntlInt(handle, unix.F_DUPFD_CLOEXEC, 0)` inside its lifetime-protected callback. Capture both the callback error and the `WithHandle` error, then wrap the successful duplicate with `os.NewFile` and adopt it. Never adopt or close the borrowed handle itself. The executable adoption example demonstrates this flow. Do not set nonblocking mode on a duplicate: duplicates share status flags, which could affect another owner's wait.

Adoption validates pidfs identity and descriptor flags without sending a signal. It can adopt handles whose targets have already been reaped, or whose signal permissions differ from the caller's. It does not add signaling rights or turn a non-child into a waitable child.

## Signal and reap

`process.Signal(syscall.SIGTERM)` delivers a process-directed signal. Signal zero checks existence and permission without delivery. Actual Linux errors such as `EPERM`, `EINVAL`, and `ESRCH` remain available through `errors.Is` and `errors.As`; a failure is not converted into a generic unsupported result.

Use `process.Wait()` only when this package is the chosen owner of reaping, such as for a child started with `os.StartProcess`. `Wait` returns an `Exit` with the normal code, or code `-1` plus the terminating signal. `CoreDump` reports Linux's core-dump status, without guaranteeing an accessible core file. Both normal and signal termination return nil error. After pidfd reaping, release the separate `os.Process` with `Release`. Keep reaping with `Cmd.Wait` for an `exec.Cmd` child.

Multiple `WaitReady` observers can all succeed, including after reaping. Multiple `Wait` callers compete: exactly one can consume child status; others receive `ECHILD`. A live non-child also returns `ECHILD` immediately from `Wait`, although its exit can be observed with `WaitReady`. Concurrent external reapers follow the same rule. Status is not cached and `Wait` is not an idempotent query.

## Lifetime, waits, and raw access

Methods are safe concurrently, but do not copy an initialized process. The zero value returns `os.ErrInvalid`; repeated close, signaling, and waits after close match `os.ErrClosed`. Raw descriptor access retains the `os.File` semantics described below. Nil receivers are unsupported. Close interrupts pending waits; an operation already executing can still deliver its signal or reap its child.

Each process owns a pidfd and a private eventfd used to wake every pending poll wait on close. Each blocked waiter may occupy an OS thread. There is no context, deadline, or wait-many API, and preexisting deadlines on adopted files do not affect waits. This avoids changing flags shared with another descriptor owner.

`SyscallConn` implements `syscall.Conn`. Callbacks borrow a lifetime-protected descriptor: do not close or retain it, change shared status flags, or interfere with the chosen reaper. Duplicate close-on-exec inside `Control` for independent ownership. Raw connection errors retain `os.File` behavior and need not match `os.ErrClosed`; use `WaitReady` for observation rather than relying on the adopted file's Go poller registration. Close cannot interrupt caller-owned blocking syscalls inside raw callbacks.

Thread pidfds, process-group signaling, process creation, ptrace waits, stop/continue notifications, descriptor stealing with `pidfd_getfd`, and resource-usage accounting are omitted.

## Verification and prerequisites

Run `package=pidfd make specs` through the repository harness. Ordinary operations require permitted pidfd_open, pidfd_send_signal, pidfs/fcntl inspection, eventfd, poll, and waitid calls. Tests use procfs, `/bin/sh`, `/bin/cat`, and bounded test-owned children. Core-dump status and lowered descriptor limits run in subprocesses without changing host policy. A sibling-process fixture verifies non-child observation while its parent retains reaping.

The permission-denial scenario uses signal zero against a different-UID PID 1, without delivering a signal or changing its lifecycle. On root or same-UID runners, optional runs explicitly skip this scenario and `required=true` fails its prerequisite; actual cross-user permission-denial evidence remains required. No blanket skip converts syscall failures into successful coverage. The [implementation plan](../IMPLEMENTATION.md) records observed local checks and missing Linux 7.0 arm64 evidence at both Go versions. See the [design note](design/pidfd.md) for the polling tradeoff and primary sources.
