# mmap design decisions

Part of the [accepted-design index](../accepted-design.md).

The [usage guide](../../mmap.md) explains mapping access and synchronization; [verification](../../verification.md) records kernel and fault-test evidence.

## Own the mapping rather than its source file

Region owns one complete mapping and borrows a file only while creating it. A mapping has a lifetime independent of the source descriptor, so retaining or duplicating that file would add ownership without protecting mapped access. The returned byte slice has exactly the requested length and capacity; page rounding does not enlarge the caller's Go view. Zero protection is NoAccess, requiring callers to choose permissions before using the memory.

The owner retains the original slice required by the pinned x/sys mapper. Whole-region protection, advice, and unmapping keep that ownership explicit. Partial unmapping or remapping would require a different representation and rules for outstanding views; these operations are not approximated by reslicing.

## Escaped slices prevent automatic lifetime management

A mutex can serialize Region methods but cannot protect memory after Bytes returns. A finalizer could unmap memory while a caller still holds a slice, so cleanup is explicit and callers coordinate all view access. Keeping the Region reachable also cannot make an explicitly closed mapping safe. A failed unmap retains the owner and original mapping for retry, matching x/sys's bookkeeping. See [Region lifetime operations](../../../mmap/region.go).

A file-size check would race with truncation and reject legitimate mappings beyond current contents. The package leaves backing-size coordination to the caller. Fixed addresses and executable mappings need additional ownership and protection contracts.

## Let callers choose when to synchronize

Shared visibility, writeback, and unmapping happen at different points in a mapped writer's lifecycle. An explicit Sync lets the caller pause writers, wait for the filesystem, and handle a writeback error while the region is still usable. Close owns unmapping. Sync uses the pinned x/sys `Msync` binding under the region mutex and relies on the kernel's backing reference, which keeps sync and close safe to call concurrently even after the source file closes. Whole-region scope follows the mapping owner and the kernel's page rounding. The writeback mode is exclusive. A separate typed flag names invalidation and lets callers combine it with either writeback mode.

The [pinned x/sys mapper](https://cs.opensource.google/go/x/sys/+/v0.50.0:unix/syscall_unix.go) supplies mapping registration and successful-unmap bookkeeping.
