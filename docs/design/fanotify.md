# fanotify design decisions

The [usage guide](../fanotify.md) owns calling contracts and requirements. [Verification](../verification.md#fanotify-verification) owns runner setup; unfinished evidence belongs in [IMPLEMENTATION.md](../../IMPLEMENTATION.md).

## Bound the first reporting mode

Descriptor-based notifications and content permission requests share one concrete event-file model. Restricting creation to these classes allows useful file inspection and decisions without exposing file handles, descriptor adoption, raw callbacks, or a future package dependency. Read-only event access supports inspection while avoiding implicit content modification. Linux's unprivileged mode requires FID reporting, so supporting ordinary users by silently changing modes would discard the promised file/permission semantics. FID/name reporting, pre-content modification, and extra reporting modes need their own typed operations and ownership decisions before they can be enabled.

The [Linux 7.0 UAPI](https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/fanotify.h) defines the selected classes, marks, masks, response values, and version 3 metadata. The [kernel implementation](https://github.com/torvalds/linux/blob/v7.0/fs/notify/fanotify/fanotify_user.c) controls file acquisition and permission queue transitions. The implementation uses x/sys bindings and explicit native-endian fields rather than unsafe casts or copied syscall tables. Group creation and delivered event files both request close-on-exec atomically. Standard Go polling supplies group deadlines and concurrent-close interruption; event-file inspection uses ordinary file semantics.

## Separate queue lifetime from event lifetime

Reading a batch allocates file descriptors in the receiving process. A metadata-only event with a raw integer would move ownership, response matching, and descriptor reuse races into every caller. Events instead own private files with ReadAt and Stat operations. The kernel opens these files with notification suppression, allowing inspection during a read-permission request without recursive fanotify feedback. A per-event mutex serializes inspection, response, and Close; the response uses the same still-owned descriptor even if callers mutate exported observations. Group methods do not retain that mutex or hold descriptor references while waiting for reads. Closing the group remains able to release pending requests without waiting for event processing.

Linux permits outstanding requests when the final group reference closes. Duplicating the group for each event would keep requests and blocked tasks alive after Group.Close, so events retain a Go reference to the group owner without duplicating its descriptor. Closing an unanswered event attempts an explicit deny; failed cleanup releases the group rather than leaving a task blocked indefinitely. This choice can allow other outstanding requests and must remain visible to security-sensitive callers. No fail-closed guarantee or whole-program sandbox is implied.

## Keep destructive decoding private

Unlike inotify bytes, a fanotify batch contains newly installed live file descriptors. A public parser accepting arbitrary bytes could close unrelated resources or create competing owners for a reused descriptor. The decoder therefore operates only behind group reads. It acquires identifiable event owners before validating variable metadata, continues through framed records after a bad extension, and releases the whole batch and group on failure. Once framing is corrupted, later file locations cannot be recovered safely; no scan for plausible descriptor-shaped integers is attempted. Version changes are rejected because interpreting resource fields from an unknown ABI would be unsafe. Extra output bits and ordered variable information payloads are retained without enabling unrequested reporting modes.

## Make iteration own its read-ahead

Explicit batch reads transfer every event to the caller and support asynchronous processing without a hidden worker. Synchronous iteration instead lends events and closes its entire batch on return, including early exit and panic. Retaining unread event files in an abandoned closure would leak both descriptors and unanswered requests; closing the batch makes lifetime deterministic. This deliberately differs from inotify's resumable value-only sequence. Callers needing retained events choose ReadEvents before consuming the batch. Cleanup cannot yield after an early break, so a failed deny closes the group under the documented shutdown policy.
