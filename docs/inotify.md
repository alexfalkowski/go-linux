# inotify

`github.com/alexfalkowski/go-linux/inotify` provides Linux file and directory notifications with Go polling and read deadlines. It requires Linux 7.0+, permitted [inotify_init1(2)](https://man7.org/linux/man-pages/man2/inotify_init.2.html), watch and read syscalls, and Go polling support. It needs no capability for ordinary readable files; filesystem behavior and access checks still apply. See [inotify(7)](https://man7.org/linux/man-pages/man7/inotify.7.html) for Linux event semantics and limitations.

## Good for

Good for rebuild triggers, search indexes, and caches that react to local file changes. [Watchman uses inotify on Linux](https://meta.github.io/watchman/docs/install); its documented watch and queue limits also illustrate why consumers need overflow recovery.

## Watch and iterate

```go
watcher, err := inotify.New()
if err != nil {
    return err
}
defer watcher.Close()
if err := watcher.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
    return err
}
id, err := watcher.AddWatch(directory, inotify.Create|inotify.MoveEvents|inotify.OnlyDir)
if err != nil {
    return err
}
for event, err := range watcher.Events() {
    if err != nil {
        return err
    }
    if event.Mask&inotify.QueueOverflow != 0 {
        // Reconcile application state: notifications have been lost.
        continue
    }
    if event.Watch == id {
        fmt.Println(event.Mask, event.Cookie, event.Name)
    }
}
```

`directory` is an existing readable directory; this snippet waits for external activity. Imports are `fmt`, `time`, and `github.com/alexfalkowski/go-linux/inotify`. The [executable examples](../inotify/example_test.go) create their own events and also show removal, resuming iteration, and forwarding events to a channel with context cancellation.

`New` returns one owned watcher, already nonblocking, close-on-exec, and registered with Go's poller. It returns no resource on failure and closes anything acquired. `Close` releases all watches and interrupts pending reads; arrange it after successful creation. Descriptors and syscall plumbing stay private. There is no adoption API or independent watch resource to close.

## Watch identity and options

[`AddWatch`](https://man7.org/linux/man-pages/man2/inotify_add_watch.2.html) watches the inode resolved by the path. A directory watch covers immediate children, not recursively installed descendants. `Event.Name` is a child name relative to the watched directory, or empty for the watched object itself. Names can contain non-UTF-8 bytes and may no longer exist when processed. The library does not maintain absolute paths or follow a renamed directory in an application path map.

`WatchID` preserves Linux's signed watch descriptor. It belongs to one watcher, owns nothing independently, and can be reused after removal. Adding another path to an already watched inode normally returns the same ID and replaces its mask. `MaskAdd` extends its existing conditions/options; `MaskCreate` fails with EEXIST instead of altering an existing watch. These two flags cannot be combined. `MaskAdd` retains existing `ExclUnlink` and `OneShot` bits; replace the mask without `MaskAdd` to clear them. `OnlyDir` atomically requires a directory (ENOTDIR otherwise), `DontFollow` watches a symlink itself, `ExclUnlink` suppresses later child events after unlink, and `OneShot` removes a watch after its first event.

`AddWatch` requires at least one `AllEvents` bit in its mask. Unknown and output-only bits, contradictory flags, negative removal IDs, and paths containing NUL match `os.ErrInvalid` before making changes. Empty or missing paths preserve Linux ENOENT; permission, filesystem, and resource-limit failures retain their errno. Watch operations can block in kernel path resolution and have no deadline. No weaker watch is substituted on failure.

[`RemoveWatch`](https://man7.org/linux/man-pages/man2/inotify_rm_watch.2.html) queues `Ignored`; earlier queued events remain available. Automatic removal, including deletion of the watched inode and `OneShot`, also reports `Ignored`. Removal is not idempotent: a nonexistent nonnegative ID fails with EINVAL. Do not pass foreign or stale IDs: a reused number can affect a different watch. `Close` discards the instance and queue rather than delivering removal records.

## Records, loss, and ordering

`Events()` returns an `iter.Seq2[Event, error]` that reads batches internally and yields one event at a time. The sequence is single-use: `break` pauses it, and ranging over the same saved sequence resumes with its remaining batch. Creating another sequence does not recover the old sequence's buffered records. An I/O error is yielded once with a zero event, then that sequence is exhausted; after renewing an expired deadline, create a new sequence. Names remain valid after later iterations. The watcher stays caller-owned, and buffered events may still be yielded after it closes.

Iteration starts no goroutine and imposes no channel capacity or event-dropping policy. Do not iterate the same sequence concurrently or recursively. Other readers compete for kernel batches, so coordinate them with iteration if order or delivery of buffered records matters. For `select`, the [channel example](../inotify/example_test.go) shows an application-owned goroutine with cancelable sends and joined shutdown. Its context owns the whole session and closes the watcher on cancellation; it does not claim to cancel one independent read.

`Read` implements `io.Reader` with Linux record constraints: it returns only complete native-endian records. Buffers shorter than the 16-byte header fail with EINVAL immediately, including nil/empty buffers. A buffer that cannot fit the first queued record also fails with EINVAL without consuming it. A 4096-byte buffer accommodates ordinary filesystem names; larger buffers can receive more whole records at once. Generic stream readers that split reads into tiny chunks need not work.

`ReadEvents` uses the caller's byte buffer for one `Read` and returns decoded events. Returned names own their bytes, so the buffer can be reused. `ParseEvents` decodes raw batches independently; pass only the bytes returned by `Read`. Empty input returns nil, nil. Malformed lengths, truncation, missing name terminators, and nonzero name padding match `os.ErrInvalid` and return no partial events. A failed decode after `ReadEvents` has already consumed that batch. Unknown output bits remain intact.

`MovedFrom` and `MovedTo` carry the same nonzero rename cookie when both are observed. Partners can be separated by other events or reads, or absent when the other directory is unwatched or events are lost. The library does not pair or buffer them. `IsDir` identifies directory subjects. `Unmount` and `Ignored` remain explicit events.

The kernel can coalesce identical unread events, and inotify misses some filesystem activity; it is not an operation counter, audit log, or atomic snapshot. A full per-instance queue drops events and reports `QueueOverflow` with watch ID -1. The library returns that record without an automatic rescan or fabricated recovery. Applications choose reconciliation and must also account for races while installing watches. Per-user instance/watch limits and the per-instance queue limit are configured by the host; the library does not modify them.

## Lifetime, concurrency, and errors

Methods are safe concurrently, but multiple readers compete for batches and callers coordinate watch/update/remove/read sequences. Queued and already returned records can outlive removal and ID reuse. A watcher must not be copied after use. Its zero value owns nothing; error-returning operations match `os.ErrInvalid`. Nil receivers are unsupported.

`SetReadDeadline` is optional and applies an absolute deadline to pending and future reads; zero clears it. A deadline set once bounds the whole reading session, not each operation separately. Renew it after activity when implementing an idle timeout. Construction's zero-deadline call only verifies polling support and installs no timeout. Expiry matches `os.ErrDeadlineExceeded`, and an expired deadline remains active until changed. Watch operations remain usable while reads have expired. Valid operations and repeated close after `Close` match `os.ErrClosed`. On an initialized watcher invalid arguments take precedence over closed state. Kernel failures preserve `errors.Is`/`errors.As` errno identity, normally with `os.PathError` operation/path context.

See [design decisions](design/inotify.md) for the record and ownership choices, [verification](verification.md) for checks and runtime evidence, and [verification limits](verification-limits.md) for missing execution evidence.
