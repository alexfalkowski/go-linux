# xattr design decisions

Part of the [accepted-design index](../accepted-design.md).

The [usage guide](../../xattr.md) owns caller workflows and contracts; [verification](../../verification.md) owns environment evidence.

## Borrow standard files

Attributes belong to inodes and do not need another Go resource owner. Standard files already provide identity and lifetime; package functions borrow them through private RawConn lifetime references. This keeps syscall numbers and descriptors out of the public API without duplicating os.File. Calls do not create or duplicate descriptors. A reference stays pinned throughout sizing retries so concurrent Close cannot reuse the handle for a different inode.

Handle calls use empty paths; at-style calls also support directory-relative lookup and explicit final-symlink selection. Ordinary path resolution remains visible instead of pretending that a directory anchor confines a name. Linux rejects O_PATH handles for empty-path attribute operations; reopening through procfs would change access and lifetime semantics, so the package retains EBADF. An O_PATH directory remains useful as the anchor of a nonempty lookup.

## Use the baseline at-style interface

The [Linux 7.0 xattr implementation](https://github.com/torvalds/linux/blob/v7.0/fs/xattr.c) supplies getxattrat, setxattrat, listxattrat, and removexattrat, including AT_EMPTY_PATH and AT_SYMLINK_NOFOLLOW. Older getxattr/fgetxattr/lgetxattr families cannot directly express directory-relative operations. Emulating those with cwd changes or procfs paths adds races and prerequisites, while opening every target adds permission checks and cannot honestly reproduce all symlink semantics. The at-style interface supplies the requested operations directly without raising the module's kernel floor. No post-baseline features are selected.

The pinned x/sys v0.50.0 supplies maintained architecture-specific syscall numbers but no at-style wrappers or xattr_args type. A narrow private binding uses those numbers, unix.Syscall6, and the fixed-width layout from the [versioned UAPI header](https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/xattr.h). This avoids copying syscall tables or adding a dependency. The layout carries a pointer inside a 64-bit integer; runtime.Pinner keeps its backing bytes stable and live for the call. Replace the private wrappers with maintained x/sys bindings when those provide equivalent semantics. Kernel and policy errors stay observable instead of activating an older-syscall fallback.

## Return complete bounded observations

Callers normally need a whole value or a list of names rather than a raw buffer-sizing protocol. Shared private read logic handles both variable results, with four size/read attempts and a 64 KiB allocation limit derived from the Linux VFS contract. Four attempts bound work under continuous growth while allowing ordinary concurrent changes to settle. A one-byte minimum read allocation prevents the zero-size-query trap when an empty result grows before the read. A failed operation returns no partial result. Exhaustion retains ERANGE, allowing the caller to choose whether to retry at a higher level.

Allocating the maximum on every read would avoid sizing races but penalize typical small attributes. Unbounded retries could prevent return under a busy writer. Exposing a retry knob or caller-buffer API is unnecessary for the current whole-attribute workflow. Lists preserve kernel visibility and order, and values remain uninterpreted bytes. Set modes remain mutually exclusive atomic operations; nil writes retain an empty attribute rather than implicitly removing it.
