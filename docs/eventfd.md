# eventfd

`github.com/alexfalkowski/go-linux/eventfd` creates Linux event counters for descriptor-based notifications. It requires Linux 7.0+, the Go version in `go.mod`, and permitted `eventfd2` and polling operations. Ordinary use needs no extra capabilities. Use Go channels or synchronization primitives when no kernel descriptor is needed.

The [verification guide](verification.md) describes runner requirements and verified environments; [verification limits](verification-limits.md) records missing execution evidence.

## Good for

Good for waking an explicit readiness loop when a worker or another process has work available. [QEMU uses eventfd for device notifications](https://www.qemu.org/docs/master/devel/multi-process.html), illustrating signaling that integrates with kernel I/O.

## Accumulate and consume notifications

Import `github.com/alexfalkowski/go-linux/eventfd`. Create an owned counter with `eventfd.New(0, eventfd.Default)`, close it when done, and use `Add` and `ReadValue` for typed I/O. The [executable example](../eventfd/example_test.go) demonstrates this flow:

```go
counter, err := eventfd.New(0, eventfd.Default)
if err != nil {
    return err
}
defer counter.Close()

if err := counter.SetDeadline(time.Now().Add(time.Second)); err != nil {
    return err
}
if err := counter.Add(3); err != nil {
    return err
}
completed, err := counter.ReadValue() // 3; the counter is now zero.
```

The snippet belongs inside an error-returning function and also imports `time`. The initial value is a `uint32`; later additions and reads are `uint64`. Default mode accumulates additions and drains the whole count on each successful read. `eventfd.Semaphore` instead makes each successful read return one and decrement the count by one. Modes are exclusive; unknown modes match `os.ErrInvalid`.

`ReadValue` waits while the count is zero. `Add` waits if the entire addition would exceed `2^64-2`; a waiting or timed-out addition never commits part of its value. Adding zero is valid. Adding `math.MaxUint64` is invalid and matches `unix.EINVAL` when the write reaches the kernel.

## Deadlines, close, and concurrency

The descriptor is atomically nonblocking and close-on-exec. Counter methods wait using Go's poller; normal I/O does not expose `EAGAIN`. `SetDeadline` affects reads and writes, while `SetReadDeadline` and `SetWriteDeadline` affect only their respective direction. Deadlines affect pending and future operations. Expired I/O matches `os.ErrDeadlineExceeded`; use a zero `time.Time{}` to clear a deadline. Deadline errors preserve timeout semantics through `os.IsTimeout`.

Always call `Close`. It interrupts poll-blocked reads and writes with errors matching `os.ErrClosed`. Repeated close, valid I/O after close, and deadline setters after close also match `os.ErrClosed`. The zero `Counter` owns nothing; its error-returning methods match `os.ErrInvalid`. Do not copy a counter after first use or use a nil receiver.

Methods are safe concurrently. Readers compete for records rather than receiving broadcasts. Individual operations are atomic at the kernel counter boundary, but sequences such as add-then-read are not transactions.

## Byte records

`Counter` implements `io.Reader`, `io.Writer`, and `io.Closer`. Reads require at least eight bytes and return exactly one native-endian eight-byte record, leaving a larger buffer's tail untouched. Writes require exactly eight bytes containing one native-endian value. Smaller reads and non-eight-byte writes, including empty buffers, match `unix.EINVAL` without changing the count. Buffer validation precedes closed-state checks; valid records use Go's deadline/closed-state checks before kernel value validation. Counter I/O is a record protocol; arbitrary stream utilities such as `io.Copy` are unsuitable.

## Explicit readiness

Call `counter.Register(poller, epoll.Readable)` to join an [epoll readiness set](epoll.md). The [registration example](../eventfd/example_test.go) creates both owners, adds a count, waits for the matching registration, and consumes it through `ReadValue`. `epoll.Readable` reports a nonzero count; `epoll.Writable` reports capacity to add at least one, which does not guarantee capacity for an arbitrary `Add` value. Trigger modes and complete interest masks follow `epoll.Poller.Add`.

Register privately duplicates the counter with close-on-exec without changing its flags or deadlines. Close the returned registration or its poller separately; closing either leaves the counter usable. Closing the counter interrupts its I/O but leaves the registration alive, retaining the kernel counter until that reference is also released. Failure returns nil, releases any acquired duplicate, and leaves the counter open. A nil poller, zero counter, or unknown interest bits match `os.ErrInvalid`; valid registration on a closed counter or poller matches `os.ErrClosed`. Other errors retain Poller.Add's behavior.

Readiness is advisory and does not reserve records or capacity against competing I/O. Keep I/O bounded with counter deadlines; a Wait timeout does not set those deadlines, and counter deadlines do not affect Wait. The descriptor stays private. Kernel-producer integration remains outside the current API.

Kernel errors retain their errno through `errors.Is` and `errors.As`. Policy denials are returned without a fallback or blanket unsupported classification. See the [package documentation](../eventfd/doc.go), [design note](design/eventfd.md), and [verification commands](verification.md).
