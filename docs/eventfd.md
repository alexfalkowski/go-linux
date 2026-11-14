# eventfd

`github.com/alexfalkowski/go-linux/eventfd` creates Linux event counters for descriptor-based notifications. It requires Linux 7.0+, the Go version in `go.mod`, and permitted `eventfd2` and polling operations. Ordinary use needs no extra capabilities. Use Go channels or synchronization primitives when no kernel descriptor is needed.

P02 is complete for the available Linux 7.0 arm64 target, verified at Go 1.27.0 and 1.27.4, with additional local Linux 7.3 amd64 checks. [IMPLEMENTATION.md](../IMPLEMENTATION.md) records the CI evidence and remaining project-wide verification limits.

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

Methods are safe concurrently. Readers compete for records rather than receiving broadcasts. Individual operations are atomic at the kernel counter boundary, but sequences such as add-then-read are not transactions. Close cannot cancel an active raw control callback, so callbacks must return promptly.

## Byte records and raw access

`Counter` implements `io.Reader`, `io.Writer`, `io.Closer`, and `syscall.Conn`. Reads require at least eight bytes and return exactly one native-endian eight-byte record, leaving a larger buffer's tail untouched. Writes require exactly eight bytes containing one native-endian value. Smaller reads and non-eight-byte writes, including empty buffers, match `unix.EINVAL` without changing the count. Buffer validation precedes closed-state checks; valid records use Go's deadline/closed-state checks before kernel value validation. Counter I/O is a record protocol; arbitrary stream utilities such as `io.Copy` are unsuitable.

Use `counter.SyscallConn()` for borrowed access to the same descriptor through Go's standard `syscall.RawConn`. The [raw-access example](../eventfd/example_test.go) shows a lifetime-protected flag query. Borrowing does not create a new owner.

A direct syscall inside `RawConn.Control` sees nonblocking behavior: an empty read or an overflowing addition returns `EAGAIN`. Capture syscall errors separately from the callback method's own error. `RawConn.Read` and `Write` can wait through Go polling when their callbacks return false on `EAGAIN`. Raw connection methods preserve `os.File` raw errors; their internal closing errors need not match `os.ErrClosed`.

Never close, retain, or clear nonblocking mode on a borrowed descriptor. Duplicate within a `Control` callback if independent ownership is needed, then close the duplicate separately. Duplicates still share the count and file status flags; changing those flags can break Go polling for the original owner. There is no raw-mode constructor, exported `*os.File`, adoption API, context API, or seek interface.

Kernel errors retain their errno through `errors.Is` and `errors.As`. Policy denials are returned without a fallback or blanket unsupported classification. See the [package documentation](../eventfd/doc.go), [design note](design/eventfd.md), and [verification commands](verification.md).
