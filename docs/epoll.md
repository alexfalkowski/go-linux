# epoll

`github.com/alexfalkowski/go-linux/epoll` provides explicit Linux readiness sets for standard `*os.File` sources. It requires Linux 7.0+, the Go version in `go.mod`, and permitted epoll, eventfd, and descriptor duplication operations. Ordinary use needs no extra capabilities. Continue using normal Go I/O and its runtime poller for ordinary file and network operations.

P07 is complete on the available Linux 7.0 arm64 target at Go 1.27.0 and 1.27.4; [IMPLEMENTATION.md](../IMPLEMENTATION.md) owns acceptance and exact CI evidence. The [executable example](../epoll/example_test.go) demonstrates pipe readiness and one-shot rearming.

## Register, wait, and remove

Create an owned poller and register an existing `*os.File`. This snippet belongs inside an error-returning function, imports epoll and time, and assumes `reader` is a live pipe reader whose peer may write:

```go
poller, err := epoll.New()
if err != nil {
    return err
}
defer poller.Close()

registration, err := poller.Add(reader, epoll.Readable|epoll.OneShot)
if err != nil {
    return err
}
defer registration.Close()

events := make([]epoll.Event, 16)
n, err := poller.Wait(events, time.Second)
if err != nil {
    return err
}
for _, event := range events[:n] {
    if event.Registration == registration && event.Events&epoll.Readable != 0 {
        // Consume input using bounded I/O, then rearm when ready to receive again.
        if err := registration.Modify(epoll.Readable | epoll.OneShot); err != nil {
            return err
        }
    }
}
```

Wait reports advisory readiness; other I/O can consume it first. Events do not reserve bytes or writable capacity. Bound subsequent file I/O with its own deadlines or known record lengths. The complete pipe example performs a bounded read before rearming. For EOF or source-error handling, inspect the returned Hangup and Error bits as well. Hangup can coexist with buffered readable data. Priority and ReadHangup request Linux exceptional and stream half-close notifications; their exact meaning depends on the source.

Add borrows the file and privately duplicates it with close-on-exec. The original stays caller-owned and usable, including on failure. Close the registration to remove it and release that duplicate, or close the poller to release all remaining registrations. Closing the original file alone leaves its registration alive and can delay EOF or peer teardown. Repeated Add calls for the same file produce independent registrations. Add does not change source status flags or deadlines. Ordinary disk files are not epoll sources and return Linux EPERM; errors preserve errno with `errors.Is` and `errors.As`.

## Trigger modes and identity

The default `epoll.LevelTriggered` reports a condition again while it remains true. `epoll.EdgeTriggered` reports readiness transitions; consume nonblocking input until it would block before expecting another edge. Standard Go file reads may themselves wait, so use deadlines or known lengths rather than an unbounded drain loop. This package does not change a source's I/O mode or consume its data.

`epoll.OneShot` disables the registration after delivery. `registration.Modify(fullMask)` replaces all requested conditions and flags and rearms it, even with an unchanged mask. EdgeTriggered and OneShot can be combined. Unknown input bits match `os.ErrInvalid`. Zero interests request only the automatically reported Error and Hangup conditions. Those conditions appear without requesting them, and are readiness bits rather than Wait errors.

Each Event holds the exact Registration pointer returned by Add. Keep application state keyed by that pointer. Descriptor numbers are never event identities: closing a source and reusing its number cannot retarget an existing registration. Removed entries are filtered before translation, but events already translated or returned can still reference a removed registration. Such an event retains its old identity; Modify on the closed registration matches `os.ErrClosed`. Modification can overlap events from the prior mask. Event order and one result per I/O operation are not guaranteed.

## Timeout, close, and concurrency

Wait requires a nonempty buffer and changes only `events[:n]` on success. Zero timeout polls immediately, any negative timeout waits indefinitely, and a positive duration bounds the wait against a monotonic deadline. Timeout returns `(0, nil)`. Durations round up to kernel milliseconds; scheduling can overrun the deadline. Long durations are split internally without truncation, and signal interruption retries preserve the original deadline. Source deadlines do not affect Wait.

Close interrupts every pending Wait with `os.ErrClosed`, joins outstanding waits, and releases the poller's descriptors and registration duplicates. It leaves original source files open. An already translated result may still succeed during a concurrent close. Calls are safe concurrently, but each concurrent Wait needs its own buffer, and waiters compete for source events. No background goroutine is started; each blocked Wait can occupy an OS thread. Close and timeouts are the supported interruption mechanisms.

Zero owners match `os.ErrInvalid`; operations on closed owners match `os.ErrClosed`. Nil file, unknown-mask, and empty-buffer validation precede closed-state checks. Do not copy owners after first use or use nil receivers. On a registration deletion error, retain the registration and retry or close its poller; a close error after successful deletion leaves it closed. Do not retry Poller.Close to repeat descriptor cleanup.

The API accepts standard files. Opaque eventfd, timerfd, and pidfd owners currently have no epoll registration operation; there is no public raw-access workaround. Context cancellation, signal masks, exclusive/power-management flags, nested pollers, raw adoption, and newer wait facilities are outside this initial scope. See the [package documentation](../epoll/doc.go), [design note](design/epoll.md), and [verification guide](verification.md).
