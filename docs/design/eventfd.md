# eventfd design decisions

See the [usage guide](../eventfd.md) for counter operations and [verification](../verification.md) for runtime evidence.

## Preserve kernel records through Go I/O

A Counter supports standard byte I/O plus typed helpers, but it is not an arbitrary stream. Buffer checks happen before `os.File` I/O: Go can accept an empty read without reaching Linux, while eventfd must reject an undersized record without consuming anything. Reads pass only eight bytes, and writes require exactly eight, so generic file behavior cannot split or combine counter operations. Typed helpers encode the same records rather than adding another buffering layer.

The initial value remains `uint32` to match creation, while later records carry `uint64`. Default and semaphore behavior are exclusive modes rather than combinable flags. Kernel counter state is not mirrored in Go; that would race with other users of the open description.

## Require runtime polling at construction

Atomic nonblocking creation lets Go's poller implement waiting, deadlines, and close interruption without a goroutine per counter. `os.NewFile` can return a file after poller registration failed, so the constructor probes a zero deadline before publishing the owner. Failure closes the file instead of silently losing the advertised interruption behavior. [The constructor and record methods](../../eventfd/counter.go) contain this boundary.

The file remains private because generic file methods would expose operations outside the record contract. Explicit epoll registration is a typed operation on Counter: it delegates lifetime protection and duplication to Poller.Add while consumption stays with the counter. The independent registration can outlive Counter.Close, so shutdown must release both owners. Kernel-producer registration and raw adoption still require separate concrete ownership decisions.

The [Linux 7.0 implementation](https://github.com/torvalds/linux/blob/v7.0/fs/eventfd.c) defines the exact record-size and counter semantics, including the requirement that writes contain exactly eight bytes.
