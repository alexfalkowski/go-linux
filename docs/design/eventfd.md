# eventfd

Status: P02 complete for the available Linux 7.0 arm64 target at Go 1.27.0 and 1.27.4, with additional local Linux 7.3 amd64 checks. [IMPLEMENTATION.md](../../IMPLEMENTATION.md) owns acceptance and evidence. Shared [API conventions](api.md) apply; the [package guide](../eventfd.md) owns user workflows.

## Resource model and caller

The caller creates `eventfd.New(initial, eventfd.Default)`, optionally sets a deadline, adds notifications with `Add`, consumes them with `ReadValue`, and closes the `*eventfd.Counter`. `eventfd.Semaphore` selects decrement-by-one reads. The zero `Mode` is `Default`; exclusive modes avoid contradictory flag combinations. The constructor accepts a `uint32` matching the syscall's initial count, while records carry `uint64` values.

`Counter` holds a direct private `file *os.File` field. It exposes `io.Reader`, `io.Writer`, `io.Closer`, typed helpers, and deadline setters. Its raw descriptor and file remain private. Dependencies point only to standard Go and already-pinned x/sys. Ordinary methods call the file directly; record rules and deadline/error normalization remain local. Pidfd owns its own small eventfd wakeup implementation, so no shared owner interface or raw-access bridge is needed.

## Included operations and Linux semantics

| Surface | Operation and contract |
| --- | --- |
| `New(initial, Default)` | `eventfd2` with atomic `EFD_CLOEXEC \| EFD_NONBLOCK`; reads drain the count |
| `New(initial, Semaphore)` | Also sets `EFD_SEMAPHORE`; reads return one and decrement by one |
| `Read` / `ReadValue` | Consume one native-endian eight-byte record; an empty count waits |
| `Write` / `Add` | Atomically add one native-endian value; overflowing additions wait |
| `Close` | Release ownership and interrupt Go poll-blocked I/O |
| Deadline setters | Standard Go read/write deadline integration, including pending I/O and reset |

Reads accept buffers of at least eight bytes and return exactly eight. Writes require exactly eight bytes. The all-ones value is invalid; zero additions are allowed, including at the maximum userspace count of `2^64-2`. The Linux 7.0 implementation checks write sizes for equality, which is more precise than the manual's undersized-write description. Oversized reads do not consume multiple records. No buffering imitates a stream.

The wrapper validates buffer sizes before `os.File` I/O, including Go's special zero-length-read case. The kernel validates values and applies counter changes. `binary.NativeEndian` encodes typed helpers without unsafe pointers or copied architecture tables. Successful kernel eventfd writes are whole records, so `os.File.Write` cannot split an accepted addition; invalid lengths cannot enter its stream-oriented write loop. Standard writer errors are retained.

## Construction, ownership, errors, and concurrency

`unix.Eventfd` invokes `eventfd2`; flags are checked against the upstream 7.0 implementation and pinned amd64/arm64 bindings. Nonblocking and close-on-exec creation is atomic, without a raw-mode or inheritable opt-out. Unknown modes fail before acquisition with `os.ErrInvalid`. Creation failures wrap the actual errno in `os.PathError` with operation `eventfd2` and label `eventfd`.

`os.NewFile` accepts the acquired descriptor, detects its nonblocking flag, and attempts Go poller registration. Registration failure can leave a nonpollable file without returning an error. The constructor therefore calls `SetDeadline(time.Time{})` before publishing ownership; failure closes the file and returns the observed error, with no fallback. There are no intermediate descriptors. Invalid-mode and repeated create/close tests verify descriptor counts; post-acquisition poller rejection and resource-exhaustion errors are source-reviewed rather than injected through a new test seam.

The caller explicitly closes the counter. The zero value returns `os.ErrInvalid`; repeated close and valid I/O after close match `os.ErrClosed`. Deadline setters use no kernel syscalls, and a successfully constructed counter is known to be pollable. Their only failure on that owner is Go's internal closing error, which is normalized to a contextual `os.ErrClosed` without string matching or additional lifecycle state. Zero-value deadline errors remain `os.ErrInvalid`.

Buffer validation takes precedence after close. For valid buffers, `os.File` checks lifetime and deadlines before the kernel checks values, so a closed or expired write can supersede an all-ones-value error. Actual syscall errors preserve errno identity; no permission error is collapsed into a generic unsupported result.

Methods retain `os.File` concurrency and lifetime protection. Reads compete for the next record; additions and reads are atomic kernel operations, while multi-call sequences are not transactions. Close wakes poll-blocked I/O; an already successful operation can win a race with close. Library-internal `Control` callbacks protect lifetime when composing the counter with another facility; callers never manage those references.

## Polling, prerequisites, and omissions

High-level I/O waits in Go's runtime poller on `EAGAIN`. Empty reads and overflowing writes can be interrupted by read/write deadlines or close. Zero deadlines clear prior deadlines. Read and write deadlines are independent. Focused ABI observations in internal/test verify nonblocking flags and empty-record EAGAIN independently of Go deadlines. No contexts, polling goroutines, private runtime hooks, or background cancellation emulation are introduced.

Linux must permit `eventfd2`, counter I/O, and Go's polling operations. No extra capabilities, namespaces, sysctls, or external resources are normally needed. Tests fail on unexpected permission or configuration errors; they do not skip unsupported success paths or mutate host policy. All selected features predate the Linux 7.0 baseline; no vendor-specific extension is required.

Initial omissions: raw-mode construction, adoption/duplication helpers, descriptor inheritance controls, contexts, userspace broadcasting, wait-many orchestration, and kernel producer integration. External kernel producers and epoll registration require future typed integration; there is no public descriptor escape hatch. Kernel-generated overflow is outside this item's executable coverage because no included producer can trigger it.

## Verification

The dominant harness is external Go tests using `require` assertions, launched by `package=eventfd make specs` with race detection and JSON/JUnit/coverage reports. The executable example exercises typed accumulation and deadlines. Tests cover both modes, the full initial-value width, 64-bit accumulation, draining, byte encoding and unused buffer tails, every undersized read, invalid write lengths and values without mutation, zero/maximum values, close-on-exec and nonblocking flags, empty-record kernel `EAGAIN`, pending read/write wakeups, overflow backpressure, deadline expiry/reset/direction independence, pending deadline changes, close during blocked I/O, repeated close, zero/closed states, descriptor ownership, invalid modes, and concurrent additions.

Wait tests use short observation windows and five-second safety bounds, with real kernel operations rather than mocks. They verify consumer-visible wakeups under the observed scheduler; they cannot prove every concurrency schedule. Refactor and challenge passes, exact commands, reports, failures, and limitations are recorded in the P02 handoff. No fuzzing or benchmark is justified for this fixed-size syscall adapter without a parser or performance promise.

Required Linux 7.0 arm64 execution is verified by [Go 1.27.0 job 57](https://circleci.com/gh/alexfalkowski/go-linux/57) and [Go 1.27.4 job 58](https://circleci.com/gh/alexfalkowski/go-linux/58) for revision `1c5b6f8`. Each job's environment artifact reports actual Linux 7.0.0-1017-aws arm64; its JSON test report records 38 eventfd and 76 total test passes without failures/skips. Both jobs pass vet and amd64/arm64 compilation. This closes P02's primary-target runtime gap; full amd64 baseline runtime support remains a separate project limitation. P03 is next.

## Primary references

- [Linux 7.0 eventfd implementation](https://github.com/torvalds/linux/blob/v7.0/fs/eventfd.c): creation, flags, exact buffer constraints, atomic counter updates, semaphore mode, and readiness.
- [eventfd(2)](https://man7.org/linux/man-pages/man2/eventfd.2.html): counter and raw descriptor semantics.
- [Go os.File and NewFile](https://pkg.go.dev/os#NewFile), [deadlines](https://pkg.go.dev/os#File.SetDeadline), and [syscall.RawConn](https://pkg.go.dev/syscall#RawConn).
- [Go 1.27.4 os/file_unix.go](https://github.com/golang/go/blob/go1.27.4/src/os/file_unix.go), [os/file_posix.go](https://github.com/golang/go/blob/go1.27.4/src/os/file_posix.go), [os/rawconn.go](https://github.com/golang/go/blob/go1.27.4/src/os/rawconn.go), and [internal/poll/fd_poll_runtime.go](https://github.com/golang/go/blob/go1.27.4/src/internal/poll/fd_poll_runtime.go): inspected installed sources for registration, lifetime, and closing-error behavior.
- [x/sys/unix v0.50.0](https://pkg.go.dev/golang.org/x/sys/unix@v0.50.0): existing maintained syscall bindings and architecture flags.
