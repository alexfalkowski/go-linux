# fanotify

`github.com/alexfalkowski/go-linux/fanotify` provides descriptor-based Linux filesystem notifications and permission decisions. Use it when an event must identify an already opened object or an application must decide whether an open/read/exec-open may proceed. For ordinary unprivileged directory watches and names, use [inotify](inotify.md).

## Good for

Good for on-access scanners that inspect an opened file and may decide whether access proceeds. [ClamAV's on-access scanner uses fanotify](https://docs.clamav.net/manual/OnAccess.html) for this workflow; supported permission modes require the privileges described below.

## Requirements and supported modes

The package targets Linux 7.0+ with `CONFIG_FANOTIFY`. Both supported classes require `CAP_SYS_ADMIN` in the initial user namespace and permission from the syscall/security policy. Acquiring capabilities in a new user namespace, or using rootless Docker's `--privileged`, does not supply that privilege. Permission events additionally require `CONFIG_FANOTIFY_ACCESS_PERMISSIONS`. Errors retain Linux errno; `EPERM` alone does not establish which prerequisite failed.

| Class | Events | Event file access |
| --- | --- | --- |
| `Notification` (zero) | Access, modify, open, exec-open, writable/nonwritable close | Read-only |
| `Content` | The same notifications plus open, read, and exec-open permission requests | Read-only |

The filesystem and operation must support fanotify generation. Mount and filesystem marks cover their Linux scope, not arbitrary path prefixes. Remote changes, mmap-based access, and several pseudo-filesystem operations do not produce the ordinary file events users might expect. Consult [fanotify(7)](https://man7.org/linux/man-pages/man7/fanotify.7.html), [fanotify_init(2)](https://man7.org/linux/man-pages/man2/fanotify_init.2.html), and [fanotify_mark(2)](https://man7.org/linux/man-pages/man2/fanotify_mark.2.html) for kernel restrictions.

FID/name reporting, unprivileged FID groups, PIDFD reporting, pre-content/range events, filesystem error reporting, audit responses, ignore masks, and unlimited queues/marks are deferred. This package does not deliver create/delete/rename names. Event files are read-only; it does not modify content before access.

## Receive notifications

Run with the stated privilege; `directory` is an existing directory owned by the application:

```go
group, err := fanotify.New(fanotify.Notification)
if err != nil {
    return err
}
defer group.Close()
if err := group.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
    return err
}
if err := group.AddMark(directory,
    fanotify.CloseWrite|fanotify.EventOnChild, fanotify.OnlyDir); err != nil {
    return err
}
for event, err := range group.Events() {
    if err != nil {
        return err
    }
    if event.Mask&fanotify.QueueOverflow != 0 {
        // Reconcile application state: notifications were lost.
        continue
    }
    info, err := event.Stat()
    if err != nil {
        return err
    }
    fmt.Println(info.Size())
    // The event is closed when this iteration ends.
}
```

The complete [executable examples](../fanotify/example_test.go) are invoked by the privileged integration fixture. `Events` lends an event only for the loop body. It closes that event after the body returns, including on break or panic, and closes every unread event from the consumed batch. Reusing the sequence starts reading the next kernel batch; discarded records do not resume. A cleanup error ends iteration; on early exit it cannot be yielded. No background goroutine is created.

For asynchronous processing or independent lifetimes, call `ReadEvents` with scratch space (64 KiB recommended). Close **every** returned event yourself, including overflow events and records you decide to ignore. Returned information bytes do not alias scratch space. Readers compete for batches, and coordinating multiple reads/marks/responses remains the caller's responsibility.

`Event.Stat` and `Event.ReadAt` inspect the event's open file through ordinary Go file operations. The file survives rename, unlink, and group closure until the event closes. It does not provide a stable pathname, an immutable content snapshot, or a stable process identity. `PID` is an observed process ID in the listener's PID namespace and may be zero. Unknown output masks and extension information are retained; input masks and flags reject unknown bits.

## Marks and scope

`AddMark(path, mask, flags)` adds conditions to an existing mark rather than replacing its mask. `RemoveMark` removes selected bits and returns `ENOENT` for a missing mark. `Flush(0)`, `Flush(Mount)`, and `Flush(Filesystem)` remove all marks of the selected scope. Neither removal nor flush cancels already queued events or delivered permission requests.

Zero flags select an inode and follow symlinks. `OnlyDir` requires a directory; `DontFollow` selects the symlink itself. `EventOnChild` covers immediate children of a marked directory, without recursion; `OnDir` includes directory activity. `Mount` covers the containing mount and its descendants excluding submounts. `Filesystem` covers the filesystem across its mount points. The latter two are mutually exclusive. Be deliberate with broad scopes: the listener's own operations can also generate events.

Paths are resolved anew on each call, relative to the current working directory unless absolute. Empty paths match `ENOENT`; embedded NULs, missing conditions, output-only masks, and conflicting/unknown flags match `os.ErrInvalid`. The package does not retain paths or reconstruct a directory tree.

## Permission decisions and shutdown

Create a `Content` group and mark `OpenPermission`, `AccessPermission`, or `OpenExecPermission`. Permission events pause the originating operation while the listener decides:

```text
Originating operation                 Listener
        |
   waits for decision --> permission event
        |                       |
        |                 inspect event file
        |                       |
   receives decision <--- Respond(Allow / Deny)
                                |
                           close event file
```

Call `event.Respond(fanotify.Allow)` or `event.Respond(fanotify.Deny)` once successfully per permission event, then close it. A denied operation receives `EPERM`; another listener or security policy can still reject an allowed operation. Respond leaves the file open, including on failure, and failed responses preserve errno. With `Events`, iteration closes the event for you; with `ReadEvents`, close it yourself.

Install `AccessPermission` marks before opening the files whose reads must request permission. Linux determines read-permission event eligibility at open time; reads through files opened before the mark may not generate permission events.

Closing an unanswered event attempts `Deny`, then closes its file even when the response fails. If that deny fails, the package also closes the group to release requests it can no longer safely service. The response and file errors are joined. An event is consumed on every Close attempt; do not retry descriptor closure after an error.

**Linux allows outstanding permission requests when the group closes.** `Group.Close`, a listener crash, or a malformed batch can therefore permit access that has received no explicit decision. The package is not a fail-closed sandbox. Use `Event.ReadAt` to inspect content: the kernel-supplied event file suppresses fanotify feedback, including recursive permission requests. Reopening paths or accessing other marked files while handling a request can still deadlock the listener; account for the listener's own ordinary I/O when choosing permission scopes.

## Errors, deadlines, and ownership

Constructors return no resource on error and close acquired descriptors. Group and event files are atomically close-on-exec; group reads require nonblocking Go polling and read deadlines. There is no blocking fallback, raw descriptor accessor, or caller-managed poll registration. Event file operations themselves have ordinary filesystem blocking semantics.

`SetReadDeadline` bounds pending and future reads; zero clears it and expiration matches `os.ErrDeadlineExceeded`. `Close` interrupts pending group reads with `os.ErrClosed`. The group does not close files already transferred by `ReadEvents`. Group methods and event methods are safe concurrently, but the exported observations must not be mutated concurrently with their readers. Owners must not be copied after use. Zero owners match `os.ErrInvalid`; repeated Close and valid operations on closed owners match `os.ErrClosed`. For initialized groups, input validation precedes closed-state checks.

Too-small read buffers match `EINVAL` without consuming the next whole record. A malformed batch returns no partial result, closes identifiable files and the group, and matches `os.ErrInvalid`; unsupported metadata ABI versions match `EPROTO`. Corrupted framing cannot identify arbitrary later descriptors, so decoding is deliberately private and accepts only kernel batches from the configured group. No public byte parser may assume ownership of arbitrary process descriptors. Unknown extension payloads remain uninterpreted; unsupported reporting flags cannot be requested.

See [verification](verification.md#fanotify-verification) for the isolated privileged runner and [design decisions](design/fanotify.md) for ownership and decoding rationale.
