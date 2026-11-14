# timerfd

Status: P03 complete for the available Linux 7.0 arm64 target; isolated clock-change verification remains open for R01. [IMPLEMENTATION.md](../../IMPLEMENTATION.md) owns acceptance and evidence; the [package guide](../timerfd.md) owns caller workflows. Shared [API conventions](api.md) apply.

## Resource model and caller

The caller creates `timerfd.New(timerfd.Monotonic)`, arms with `SetAfter`, consumes `ReadExpirations`, and closes the owned `*timerfd.Timer`. For absolute scheduling, `Now(clock)` returns a tagged Linux `Instant` for `SetAt`. `Timer` holds a private `*os.File` and immutable clock selection. Dependencies point to standard Go packages and the existing pinned `golang.org/x/sys/unix`; no new dependency, public descriptor superclass, future `epoll` dependency, lock, or background worker is needed.

The private owner exposes `io.Reader`, `io.Closer`, `syscall.Conn`, typed expiration reads, and a read deadline. Embedding `os.File` would expose unsupported writes and misleading generic file operations. The shape follows `eventfd` while keeping timer-specific arming and clock semantics local. Shared internals remain deferred until P04 demonstrates a useful common boundary.

## Included operations and naming

| Surface | Linux operation and contract |
| --- | --- |
| `New(clock)` | `timerfd_create` with atomic `TFD_NONBLOCK \| TFD_CLOEXEC`; initially disarmed |
| `Now(clock)` | `clock_gettime` for Monotonic, Realtime, or Boottime |
| `SetAfter(initial, interval)` | Relative `timerfd_settime`, returning previous relative state |
| `SetAt(Instant, interval, flags)` | Absolute `timerfd_settime`; matching clock required; optional Realtime cancellation |
| `State()` | `timerfd_gettime`; relative remaining time and repeat interval without consuming counts |
| `Disarm()` | Set both initial expiration and interval to zero, clearing unread counts |
| `Read` / `ReadExpirations` | One native-endian expiration record, preserving clock cancellation and zero-byte results |
| `SetReadDeadline`, `Close`, `SyscallConn` | Go polling, deterministic local ownership, and borrowed descriptor access |

`Clock` is an exclusive enum, with zero selecting `Monotonic`; values map explicitly to Linux clock IDs. Realtime uses the Unix epoch; Monotonic excludes suspend time and Boottime includes it. Neither monotonic domain is Go's private `time.Time` monotonic origin. `Instant` keeps the Linux clock tag and normalized seconds/nanoseconds rather than suggesting interchangeability with wall-clock time. Clock readings must belong to the timer's time namespace. Initial clocks and flags predate the Linux 7.0 floor and use the maintained amd64/arm64 syscall bindings.

`SetFlags` initially contains only `CancelOnSet`, accepted on Realtime absolute timers. Relative timers have no flag parameter. Unknown flags, incompatible clocks, negative durations, and invalid timestamps fail before the syscall. Absolute timestamps are bounded by signed 64-bit nanoseconds, matching Linux's ktime representation; larger valid timespecs are rejected with `EOVERFLOW` instead of relying on kernel saturation. Zero initial timestamps disarm and nonzero past timestamps expire. Intervals remain relative. State snapshots use `time.Duration`; overflow checks precede multiplication. A setter's old-state conversion happens after the syscall, so a conversion failure does not undo the new setting. No rollback or invented transaction is promised.

Setters reset unread expirations. Zero initial time disarms even with a nonzero interval, which remains visible in state. `Disarm` clears both. Zero remaining time may describe an expired one-shot with an unread record. Inspection retains unread counts, including periodic overruns. Setters return the old state from their own syscall rather than inspecting before the change.

## Polling, exceptional reads, and errors

As in `eventfd`, `os.NewFile` takes ownership of the nonblocking descriptor and the constructor checks a zero read deadline before returning. Go can retain an unpollable file after registration failure; this check closes it on failure rather than silently weakening deadline or close-interruption guarantees. There are no intermediate descriptors. Constructor syscall/poller rejection cleanup is inspected without introducing a private failure-injection API.

Reads validate size before calling `os.File.Read` with eight bytes. Go supplies EINTR retries, EAGAIN polling, deadlines, lifetime synchronization, and closed-error normalization. Its `ZeroReadIsEOF` conversion turns successful zero-byte syscalls into `io.EOF`; a timerfd has no permanent EOF, so `Read` restores `(0, nil)` when that exact error is returned. This avoids a second read/poll loop while preserving the Linux exceptional backward-Realtime-jump result. Typed reads report `io.ErrNoProgress` rather than manufacturing a count. ECANCELED is preserved. The installed Go 1.27.4 poll and file sources support this inference; clock-change execution remains a VM requirement.

Linux may rearm a canceled absolute Realtime timer yet return ECANCELED, leaving the new setting active. This side effect is documented rather than retried. Conversion errors from old-state reporting can also occur after application. `State` conversion failure returns a zero value and the error. Callers can inspect state to determine the current schedule; concurrent calls can change it again.

State and setting syscalls run inside `RawConn.Control`, protecting against descriptor closure/reuse. One private `control` helper shares lifetime protection, contextual syscall errors, and normalization of Control's private closing error. Deadline setters similarly normalize closing errors because the constructor has already established pollability. Zero-owner errors remain `os.ErrInvalid`; high-level syscall failures retain errno through `os.PathError`. No permission error is collapsed into a generic unsupported result.

## Ownership, concurrency, and omissions

Methods are safe concurrently through `os.File` lifetime protection and atomic kernel timer operations. Readers compete; multi-call sequences are not transactions. Close releases the owned descriptor and wakes poll-blocked reads. Repeated close and valid calls after close match `os.ErrClosed`; invalid arguments take precedence on a previously constructed owner. Zero `Timer` methods report `os.ErrInvalid`; nil receivers and copying a used owner are unsupported.

Disarming does not wake a waiting reader. Read deadlines and close do; arming/state operations are independent of those deadlines. `SyscallConn` retains standard raw error semantics. Raw callbacks must return promptly and must not close, retain, or clear nonblocking mode on the borrowed descriptor. A duplicate has independent ownership but shared settings, counts, and file flags. Kernel destruction occurs when the last descriptor closes.

Initial omissions: Realtime/Boottime alarm clocks and `CAP_WAKE_ALARM` policy, host clock changes, suspend/wakeup guarantees, checkpoint/restore tick injection, raw-mode construction, adoption/duplication helpers, contexts, write/seek methods, and wait-many orchestration. No automatic scheduler, goroutine cancellation shim, or future namespace package is introduced. Files use plain names and Linux build tags.

## Verification and prerequisites

The dominant harness is external Go package tests with Testify `require`, launched by `package=timerfd make specs` with race detection and JSON/JUnit/coverage reports. Two executable examples cover relative reads and clock-tagged absolute arming/disarming. Real kernel tests cover all three clock IDs and queries, relative/absolute one-shots and periodic timers, accumulated counts, previous-state returns, state inspection, disarming and clearing unread counts, native-endian byte records, all short buffers, input validation without mutation, timestamp/duration boundaries, nonblocking/close-on-exec flags, installed cancellation flags without clock changes, deadlines/reset, blocked-read wakeups, concurrent state/arming, zero/closed owners, and descriptor cleanup.

Timer tests use tolerant lower bounds and five-second safety deadlines. Periodic assertions allow scheduler delay and compare counts against elapsed time; raw EAGAIN checks verify consumption without confusing it with Go I/O timeouts. `/proc/self/fdinfo` distinguishes clock IDs even when Monotonic and Boottime readings coincide, and confirms Linux retained the absolute/cancellation flags. No fake timer or private test seam stands in for kernel behavior.

Ordinary operations require permitted timerfd/clock_gettime and Go polling, plus procfs for ownership/flag tests; unexpected denials fail rather than skip success coverage. Local Linux 7.3 amd64 tests and required Linux 7.0 arm64 CI execution pass at Go 1.27.0 and 1.27.4; [job 74](https://circleci.com/gh/alexfalkowski/go-linux/74) and [job 73](https://circleci.com/gh/alexfalkowski/go-linux/73) each record 60 timerfd and 136 total test passes without failures/skips. The primary-target ordinary-operation gap is closed. Discontinuous clock changes, ECANCELED read/rearm side effects, and exceptional zero-byte reads need a dedicated clock-controlled VM that cannot affect the shared host. They are source-reviewed, not executed or marked verified. Suspend behavior and exhaustive concurrency schedules are outside executed coverage. Old-state conversion rejection and construction failure cleanup remain source-reviewed paths. Exact commands and results belong to the P03 handoff.

## Primary references

- [timerfd_create(2), timerfd_settime(2), and timerfd_gettime(2)](https://man7.org/linux/man-pages/man2/timerfd_create.2.html): clocks, records, arming, cancellation, and ECANCELED rearm side effects.
- [Linux 7.0 timerfd implementation](https://github.com/torvalds/linux/blob/v7.0/fs/timerfd.c): baseline creation/flags, read/poll, old-state capture, and lifetime.
- [clock_gettime(2)](https://man7.org/linux/man-pages/man2/clock_gettime.2.html) and [time namespaces](https://man7.org/linux/man-pages/man7/time_namespaces.7.html): clock origins and namespace scope.
- [Go os.NewFile](https://pkg.go.dev/os#NewFile), [read deadlines](https://pkg.go.dev/os#File.SetReadDeadline), and [syscall.RawConn](https://pkg.go.dev/syscall#RawConn).
- [Go 1.27.4 os/file.go](https://github.com/golang/go/blob/go1.27.4/src/os/file.go), [os/file_unix.go](https://github.com/golang/go/blob/go1.27.4/src/os/file_unix.go), [os/rawconn.go](https://github.com/golang/go/blob/go1.27.4/src/os/rawconn.go), [internal/poll/fd_unix.go](https://github.com/golang/go/blob/go1.27.4/src/internal/poll/fd_unix.go), and [fd_posix.go](https://github.com/golang/go/blob/go1.27.4/src/internal/poll/fd_posix.go): inspected installed sources for EOF translation, ownership, and polling.
- [x/sys/unix v0.50.0](https://pkg.go.dev/golang.org/x/sys/unix@v0.50.0): pinned syscall wrappers and ABI types.
