# Landlock design decisions

Part of the [accepted-design index](../accepted-design.md).

See the [usage guide](../../landlock.md) for the public workflow and [verification](../../verification.md#landlock-verification) for contributor commands.

## Preserve the requested policy

Handled filesystem/network masks and IPC scopes express separate kernel mechanisms. Explicit named rights make a policy stable across kernel upgrades; an automatic "all rights" mask would unexpectedly change behavior when the library grows. The builder probes the kernel ABI and refuses unsupported named restrictions rather than following upstream sample code's best-effort masking. Applications can consciously choose a smaller policy after discovery, but the library cannot decide which guarantee is expendable. Unknown inputs are invalid; a known optional restriction unavailable on the running ABI is unsupported. Neither outcome returns a partially configured owner.

The baseline contract comes from [Linux 7.0 UAPI](https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/landlock.h) and its [syscall implementation](https://github.com/torvalds/linux/blob/v7.0/security/landlock/syscalls.c). Maintained x/sys supplies syscall numbers, constants, the ruleset structure, and path rule binding. Creation passes the three-field baseline prefix of the extensible ruleset structure, leaving newer quiet fields unused. The missing port-rule binding is the UAPI's two uint64 fields; this small local layout avoids a new dependency or copied syscall tables. Path rule packing is handled by the maintained binding and Linux's fixed-size copy.

## Separate a builder from applied domains

Linux owns a mutable ruleset descriptor and copies its current rules into an immutable applied domain. New/Close accurately describe the builder's lifetime, while Restrict is a separate irreversible effect. Closing a builder cannot imply policy removal. The public surface therefore exposes neither the underlying descriptor nor an adoption API whose external mutation and ownership would complicate the builder contract. A mutex serializes mutations, application, and close on one owner; independent owners still require policy coordination.

Path strings are useful for ordinary policy construction, so AddPath opens O_PATH with atomic close-on-exec internally and always releases the temporary reference. AddFile supports explicit resolution with the existing file package and standard files, pins borrowing against close/reuse, and lets the kernel retain object identity. Duplication is unnecessary because addition is synchronous and Linux acquires its own object reference. No saved pathname, mutable file field, or raw callback becomes part of the public policy model.

## Make Go thread scope explicit

Linux 7.0's ABI 8 includes [kernel TSYNC](https://github.com/torvalds/linux/blob/v7.0/security/landlock/tsync.c). CallingThread and AllThreads are mandatory choices, following the neighboring seccomp API; an implicit local default could leave most of a Go program unrestricted, while implicit global application would unexpectedly affect unrelated work. Kernel synchronization avoids runtime-private hooks and racy thread enumeration. There is no weaker fallback on failure.

Landlock TSYNC replaces a sibling's divergent domain with the applying thread's resulting domain. This differs from seccomp filter-tree conflict handling and means caller coordination is a substantive security requirement. The wrapper preserves the kernel operation and documents that requirement rather than inventing an unverifiable intersection of inaccessible sibling policies. No-new-privileges is also explicit: callers with existing authorization can avoid prctl, while callers requesting it accept the irreversible partial state if enforcement fails.

The library does not run user work through a restoration callback, manufacture a fresh Go process, or claim that an unlocked goroutine retains thread-local restrictions. Caller-owned runtime thread locking and ordinary Go process creation preserve clear boundaries. Real enforcement, domain replacement, inherited execution, and deliberate syscall failures belong in bounded subprocess fixtures; nothing changes the test runner's security policy or a host-wide setting.
