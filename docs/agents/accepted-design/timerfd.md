# timerfd design decisions

Part of the [accepted-design index](../accepted-design.md).

The [usage guide](../../timerfd.md) owns arming, reading, and clock-change recovery; [verification](../../verification.md) owns execution evidence and coverage limits.

## Keep clock domains explicit

Absolute arming uses an Instant tagged with its Linux clock. A `time.Time` would suggest that Unix wall time or Go's private monotonic value could represent Monotonic and Boottime deadlines interchangeably. Relative intervals and remaining durations use `time.Duration`, which does fit those values.

The API rejects absolute timestamps beyond signed 64-bit nanoseconds instead of relying on kernel saturation. Returned kernel durations are checked for overflow rather than wrapped. A setter may already have applied its new schedule before conversion of the old state fails, so the error cannot imply rollback. Returning the previous state from the same setting syscall avoids an additional racy inspection.

## Preserve exceptional reads

The private nonblocking file supplies Go polling and read deadlines, with construction proving pollability before returning. Ordinary `os.File.Read` translates a successful zero-byte syscall into EOF. Timerfd has no permanent EOF, so the adapter restores that result; the typed helper reports `io.ErrNoProgress` rather than inventing an expiration. ECANCELED likewise remains observable instead of triggering an automatic rearm that would choose scheduling policy for the caller.

The [Linux 7.0 read path](https://github.com/torvalds/linux/blob/v7.0/fs/timerfd.c) retains EAGAIN for the nonblocking zero-tick backward-jump case, allowing Go to poll again. The defensive zero-byte conversion must therefore remain distinct from claims about what the clock-change tests executed. See [the read adapter](../../../timerfd/timer.go) and verification guide.

Clock adjustment, wake-alarm policy, suspend guarantees, and scheduling orchestration are outside the resource owner. A read deadline bounds consumption; it does not change the armed kernel timer.

## Readiness retains the timer's lifetime

Register delegates to epoll.Poller.Add using the private file. The registration owns an independent duplicate, so a caller can remove readiness without destroying the timer, while closing the timer owner alone leaves its kernel schedule alive. A generic descriptor interface would expose lifetime coordination and syscall plumbing unnecessarily. Readiness consumption remains with Timer so clock cancellation, exceptional reads, and expiration records keep their documented meaning. See the [Linux 7.0 poll implementation](https://github.com/torvalds/linux/blob/v7.0/fs/timerfd.c) for readiness on pending ticks or cancellation.
