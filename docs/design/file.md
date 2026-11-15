# file design decisions

The [usage guide](../file.md) owns operations and caller examples; [verification](../verification.md) owns execution evidence and source-reviewed limits.

## Extend standard files without another owner

`os.File` already supplies ordinary I/O and lifetime management. `os.Root` supplies common confined filesystem operations, but not the complete openat2 resolution controls, statx validity/attribute masks, or rename exchange/no-replace modes. This package adds those operations while borrowing and returning standard files. A new file owner or filesystem abstraction would duplicate existing Go behavior without improving these contracts.

## Preserve the requested kernel operation

Open passes names unchanged. Cleaning a path in userspace can change symlink/parent traversal or trailing-slash semantics; retrying without a restriction could violate the caller's security policy. Cached lookup failure remains observable for the same reason. Rename uses one renameat2 operation instead of racy existence checks or multi-step exchange emulation. Only Open supplies lookup restrictions; directory-relative StatAt and Rename are not presented as confined operations.

Borrowed descriptors are pinned for the syscall rather than duplicated and retained. Nested lifetime references permit Rename to use the same anchor twice without introducing lock-order synchronization. This protects identity against concurrent close but does not interrupt blocking filesystem work. See [the private borrowing boundary](../../file/control.go).

## Keep metadata validity separate from values

Metadata retains Linux's returned field mask and attribute-support mask. An unsupported field cannot be inferred from a zero value, and a single query is not an atomic snapshot. Unsigned Linux sizes and identifiers retain their ranges, while mode bits and valid timestamps convert to Go representations. Unknown output bits survive inspection; unknown input bits are rejected rather than silently ignored.

The maintained x/sys bindings own syscall and ABI layouts. The missing RESOLVE_CACHED constant is defined locally with its [Linux 7.0 header](https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/openat2.h) reference rather than copying broader syscall definitions. Direct I/O, temporary-file creation, whiteouts, and additional statx fields are omitted to keep the operation set bounded; their absence is not a claim that they need a newer kernel.
