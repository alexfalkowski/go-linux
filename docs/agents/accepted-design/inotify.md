# inotify design decisions

Part of the [accepted-design index](../accepted-design.md).

See the [usage guide](../../inotify.md) for calling contracts and [verification](../../verification.md) for runner evidence.

## Keep watch identity under kernel ownership

Inotify watches belong to an instance, and watching another path to the same inode can return the existing watch descriptor. A Go watch object with an independent Close or stable identity would conceal mask replacement, automatic removal, ID reuse, and queued stale events. The API therefore returns a typed signed ID and keeps explicit removal on the watcher. It mirrors no watch/path map and makes no guarantee that a path still resolves to the watched inode. Callers retain the context needed to interpret their own IDs.

Event conditions and watch options share a typed mask, preserving Linux names and compact combinations such as `Create|OnlyDir`. A separate options struct would repeat these flag choices across fields without simplifying ordinary calls. Input validation rejects unsupported bits, output-only conditions, and contradictory `MaskAdd|MaskCreate` requests before changing a watch; output decoding keeps unknown bits for forward inspection. Output-only conditions remain ordinary records, including overflow with ID -1. Automatic recursive installation, rescan, rename pairing, and reliable delivery would require application policy and cannot restore atomicity; the package deliberately leaves those decisions with callers.

## One queue, raw and typed records

Raw reads are useful for batching or forwarding kernel records, while a standalone decoder makes malformed/truncated input a supported and independently testable boundary. Typed batch reads compose those operations without hidden buffering. `Events()` adds a synchronous `iter.Seq2` for ordinary event loops, avoiding a separate scanner type or background channel producer. Its closure retains the unread batch across an early break, and a terminal error exhausts that sequence. Read-ahead therefore belongs to the saved sequence; abandoning it cannot put consumed records back into Linux's queue. This explicit boundary preserves batches without adding buffering to raw reads. Decoded names own their storage so buffer reuse cannot corrupt an earlier batch. Returning no events for a malformed batch prevents a partial prefix being mistaken for complete success; no recovery is claimed after a destructive read.

The [Linux 7.0 UAPI](https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/inotify.h) fixes the four native-endian header fields. The decoder uses explicit field widths rather than unsafe struct casts and checks the declared length before converting it to an index. The [kernel record writer](https://github.com/torvalds/linux/blob/v7.0/fs/notify/inotify/inotify_user.c) terminates names and zeroes their padding. Decoding validates those bytes without requiring a particular padding alignment, so framing follows the advertised length. No syscall numbers or ABI structs are copied.

## Require polling without another event loop

Atomic nonblocking creation lets the standard Go file poller provide read waiting, deadlines, and concurrent-close interruption. Construction probes read-deadline support because `os.NewFile` can retain a descriptor when poll registration fails. A failure closes the descriptor rather than adding blocking I/O or a background reader. Watch mutations pin the private descriptor only for the syscall using `RawConn.Control`; they never hold that reference across a waiting read. There is no exported descriptor, raw callback, or alternate registration obligation for callers.

The package provides mutable read deadlines because callers can bound a whole session, renew an idle timeout, or interrupt a pending read without destroying the instance. Constructor-only deadlines cannot express those operations. Event iteration reuses this mechanism. Context and channel ownership stay in the application: a session owner can close its watcher on cancellation, whereas temporarily changing a shared deadline would affect unrelated readers. Path-based watch mutations remain synchronous kernel operations; adding a context without cancellation support would not make them interruptible. There is no adoption surface because the initial use cases create their own instances; validating an externally supplied file's facility and shared queue/status semantics would add an ownership contract without a current caller.
