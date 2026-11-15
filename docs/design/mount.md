# mount design decisions

The [usage guide](../mount.md) owns configuration, attachment, and cleanup workflows. [Verification](../verification.md) owns isolated-runner requirements, runtime results, and unexecuted paths.

## Separate configuration, acquisition, and attachment

Config and Handle model distinct Linux resources. Configuration accepts individual flags and strings, preserving the difference between a flag and an empty value instead of parsing a legacy option string. Create is explicit; Mount acquires a detached handle without implying attachment or a unique superblock. The library does not enter namespaces or choose propagation policy for callers.

A handle's descriptor lifetime differs from the mount's namespace lifetime. Closing it cannot mean “undo Attach,” and failed attachment must leave the handle available for cleanup or another attempt. Unmount is therefore explicit. Kernel configuration and attribute failures can follow state changes; a mirrored userspace state machine or automatic retry would falsely imply rollback or safely repeatable transitions.

The four supported mount restrictions use explicit set/clear masks so zero does not silently clear policy. ID mapping, propagation, atime groups, and reconfiguration are omitted because they require further contracts beyond those restrictions. There is no legacy fallback with different semantics.

## Keep failure diagnostics with their operation

The filesystem-context diagnostic queue belongs to the Config, not to an individual syscall. Config methods serialize the command and failure drain so another goroutine cannot steal its diagnostics. Successful calls leave warnings available; later failures may therefore carry earlier records. Automatic draining favors useful errors over a separate racy “read last error” protocol. See [configuration serialization](../../mount/config.go).

A structured Error preserves the primary operation cause separately from diagnostic retrieval failure. Unwrap exposes only the primary cause, preventing `errors.Is` from reporting the retrieval errno as the reason configuration failed. Already read records survive a later retrieval error.

The [Linux 7.0 producer](https://github.com/torvalds/linux/blob/v7.0/fs/fs_context.c) formats messages dynamically; its eight-record queue does not bound message bytes. The [reader](https://github.com/torvalds/linux/blob/v7.0/fs/fsopen.c) retains an oversized record on EMSGSIZE. Reads therefore grow from 4 KiB to a 1 MiB allocation cap, returning partial records and an explicit error at the cap rather than truncating text or allocating without bound. This is bounded diagnostics, not a lossless log. The [decoder](../../mount/diagnostic.go) preserves unknown severity and unframed records for the same reason.
