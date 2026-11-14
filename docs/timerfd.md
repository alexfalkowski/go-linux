# timerfd

`github.com/alexfalkowski/go-linux/timerfd` creates Linux timers whose descriptors deliver expiration counts. It requires Linux 7.0+, the Go version in `go.mod`, and permitted timerfd, clock-query, and polling operations. Ordinary use needs no extra capabilities. Use `time.Timer` or `time.Ticker` when no kernel descriptor or Linux clock selection is needed.

P03 is complete for the available Linux 7.0 arm64 target, with ordinary operations verified at Go 1.27.0 and 1.27.4 and local Linux 7.3 amd64 verification. Isolated clock-change verification remains open for R01; [IMPLEMENTATION.md](../IMPLEMENTATION.md) owns acceptance and the verification gaps.

## Arm and consume a timer

Import `github.com/alexfalkowski/go-linux/timerfd`. Create an owned timer, close it when done, set a read deadline to bound waiting, and arm it. The [executable examples](../timerfd/example_test.go) demonstrate relative arming and absolute clock selection:

```go
timer, err := timerfd.New(timerfd.Monotonic)
if err != nil {
    return err
}
defer timer.Close()

if err := timer.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
    return err
}
if _, err := timer.SetAfter(10*time.Millisecond, 0); err != nil {
    return err
}
expirations, err := timer.ReadExpirations() // 1 for this one-shot.
```

This snippet belongs inside an error-returning function and also imports `time`. The timer starts disarmed. `SetAfter(initial, interval)` arms relative to the selected clock. A zero interval selects a one-shot; a positive interval repeats after the first expiration. A delayed reader receives accumulated expirations, rather than one notification per read. Reading consumes the count; competing readers do not each receive a copy.

Setters return the previous `State` from the same kernel operation. `State()` inspects the current `Remaining` and `Interval`, both `time.Duration` values. Remaining time is always relative, even for an absolute timer. An expired one-shot has zero remaining time while it may still have an unread expiration. Inspection does not consume counts.

`Disarm()` stops the timer and clears its interval and unread counts. Any rearming also resets unread counts. Passing a zero initial duration to `SetAfter`, or a zero timestamp to `SetAt`, disarms even with a nonzero interval; that supplied interval remains visible in `State`. Disarming does not interrupt a waiting read.

## Choose a clock and absolute deadline

| Clock | Origin and behavior |
| --- | --- |
| `Monotonic` (zero value) | Linux monotonic origin; excludes suspend time and ignores discontinuous wall-clock changes |
| `Realtime` | Unix epoch; absolute timers follow wall-clock changes |
| `Boottime` | Linux boot-time origin; includes suspend time but does not wake a suspended system |

Call `timerfd.Now(clock)` to obtain an `Instant` in the same clock domain as the timer. `Instant` has a `Clock` tag and normalized `Seconds`/`Nanoseconds` fields. For example, obtain `deadline, err := timerfd.Now(timerfd.Boottime)`, handle the error, increment `deadline.Seconds`, then call `timer.SetAt(deadline, 0, 0)` on a Boottime timer. The absolute-arming example shows the complete flow. When doing timestamp arithmetic, normalize nanoseconds into `[0, 1e9)` and check overflow. For Realtime only, a `time.Time` can be represented as `timerfd.Instant{Clock: timerfd.Realtime, Seconds: when.Unix(), Nanoseconds: int64(when.Nanosecond())}`. Do not use Unix timestamps for Monotonic or Boottime, or Go's private monotonic origin for Linux absolute timers. Obtain and use clock readings in the same time namespace.

`SetAt(deadline, interval, flags)` rejects a mismatched clock. Its interval is relative. A nonzero past deadline expires immediately; the all-zero timestamp disarms. Negative durations/timestamps, unnormalized nanoseconds, unknown clocks/flags, and invalid cancellation combinations match `os.ErrInvalid`. Absolute timestamps above signed 64-bit nanoseconds match `unix.EOVERFLOW`; the wrapper rejects them rather than silently accepting Linux's saturation. These input errors do not modify the timer.

Use `timerfd.CancelOnSet` only with `SetAt` on a Realtime timer. It requests `unix.ECANCELED` after a discontinuous clock change. Without this flag, a backward clock change can instead cause a successful zero-byte read. `Read` preserves `(0, nil)` in that case; `ReadExpirations` returns zero and an error matching `io.ErrNoProgress`. Neither result means permanent EOF. Reevaluate the schedule after clock-change notification. Gradual clock adjustments do not trigger this cancellation mode.

Linux can apply a new absolute setting and still return `ECANCELED` when acknowledging a pending clock cancellation. Do not assume an error means the timer was unchanged: inspect `State()` before deciding whether to retry. Likewise, setters can return `EOVERFLOW` after applying a change if the previous kernel state cannot fit `time.Duration`; state inspection reports conversion errors without wrapping a value. These unusual paths are source-reviewed; clock-change execution needs an isolated clock-controlled VM. The package never adjusts the host clock.

## Reads, deadlines, and ownership

`Timer` implements `io.Reader` and `io.Closer`. `Read` requires at least eight bytes and normally returns one native-endian eight-byte count, leaving a larger buffer's tail untouched. Smaller buffers, including nil, match `unix.EINVAL` without consumption. `ReadExpirations` handles decoding. This is a record protocol; arbitrary stream copying is unsuitable. There is no write or seek interface.

Descriptors are atomically nonblocking and close-on-exec, while high-level reads wait through Go's poller. `SetReadDeadline` affects pending and future reads; zero `time.Time{}` clears it. Expired reads match `os.ErrDeadlineExceeded` and retain timeout semantics. Read deadlines do not change arming or prevent state inspection. To interrupt a reader on a disarmed timer, set a deadline or close it.

Always call `Close`. It releases the descriptor and wakes poll-blocked reads with `os.ErrClosed`; repeated close and valid operations after close also match `os.ErrClosed`. The zero `Timer` owns no resource and reports `os.ErrInvalid`. Invalid arguments take precedence after close; the zero value reports `os.ErrInvalid` first. Nil receivers are unsupported. Do not copy a timer after first use. Methods are safe concurrently, but sequences of calls are not transactions. A successful operation can win a race with close. The kernel destroys the timer when its last descriptor closes.

The timer keeps its descriptor private; use arming, state, reads, and read deadlines directly. External poll registration is outside the current API.

Kernel failures preserve errno identity through `errors.Is` and `errors.As`; policy denials remain observable. Initial omissions include alarm/wakeup clocks, clock adjustment, tick injection, adoption helpers, contexts, and exported ordinary files. See [GoDoc](../timerfd/doc.go), the [design note](design/timerfd.md), and [verification guidance](verification.md).
