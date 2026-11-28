# mmap design decisions

Part of the [accepted-design index](../accepted-design.md).

The [usage guide](../../mmap.md) explains access, synchronization, resizing, memory locking, and advice. [Verification](../../verification.md) records kernel and fault-test evidence.

## Own the mapping rather than its source file

Region owns one complete mapping and borrows a file only while creating it. A mapping has a lifetime independent of the source descriptor, so retaining or duplicating that file would add ownership without protecting mapped access. The returned byte slice has exactly the requested length and capacity; page rounding does not enlarge the caller's Go view. Zero protection is NoAccess, requiring callers to choose permissions before using the memory.

The owner retains the current slice registered by the pinned x/sys mapper. Whole-region protection, advice, resizing, and unmapping use that registration.

## Escaped slices prevent automatic lifetime management

A mutex can serialize Region methods but cannot protect memory after Bytes returns. A finalizer could unmap memory while a caller still holds a slice, so cleanup is explicit and callers coordinate all view access. Keeping the Region reachable also cannot make an explicitly closed mapping safe. A failed unmap retains the current mapping for retry, matching x/sys's bookkeeping. See [Region lifetime operations](../../../mmap/region.go).

A file-size check would race with truncation and reject legitimate mappings beyond current contents. The package leaves backing-size coordination to the caller. Fixed addresses and executable mappings need additional ownership and protection contracts.

## Let callers choose when to synchronize

Shared visibility, writeback, and unmapping happen at different points in a mapped writer's lifecycle. An explicit Sync lets the caller pause writers, wait for the filesystem, and handle a writeback error while the region is still usable. Close owns unmapping. Sync uses the pinned x/sys `Msync` binding under the region mutex and relies on the kernel's backing reference, which keeps sync and close safe to call concurrently even after the source file closes. Whole-region scope follows the mapping owner and the kernel's page rounding. The writeback mode is exclusive. A separate typed flag names invalidation and lets callers combine it with either writeback mode.

## Keep locking and residency on the mapping owner

Region already owns the address range and serializes its operations with Close. Linux owns page rounding, lock accounting, and release during unmap, so a separate lock handle or Go lock count would imply lifetime or nesting the kernel does not provide. Use the pinned x/sys operations for mlock and munlock and its syscall numbers for mlock2 and mincore, which lack slice bindings. Preserve errors from mlock2 because an immediate lock cannot provide deferred fault behavior. Return mincore's defined residency bit in a separate snapshot so callers can retain it independently of the mapping. [Linux 7.0 mlock implementation](https://github.com/torvalds/linux/blob/v7.0/mm/mlock.c). [Linux 7.0 mincore implementation](https://github.com/torvalds/linux/blob/v7.0/mm/mincore.c).

## Resize the owned mapping in one operation

A mapped index needs to grow without copying its retained contents or keeping a second mapping owner. Use the pinned x/sys slice-aware `Mremap` with `MREMAP_MAYMOVE`. It updates the same registration used by `Munmap` upon success and preserves the original registration upon error. Region replaces its slice under the owner mutex only after success. An explicit instruction to reacquire the view after every success lets callers use the same lifetime rule whether the kernel moves the range or changes its size in place. Fixed-address replacement can change unrelated mappings and alias-producing modes require additional ownership. [Pinned x/sys remap implementation](https://cs.opensource.google/go/x/sys/+/v0.50.0:unix/mremap.go). [Linux 7.0 remap implementation](https://github.com/torvalds/linux/blob/v7.0/mm/mremap.c).

Linux fixes the backing size of a shared anonymous mapping at creation. `Mremap` can extend the virtual range beyond that backing, where access would fault. The caller has no backing file to extend, so Region rejects growth beyond the original page-rounded size. This still permits regrowth after a shrink. [Linux 7.0 shared anonymous backing](https://github.com/torvalds/linux/blob/v7.0/mm/shmem.c#L5549-L5577).

## Keep advice decisions close to the kernel operation

Whole-region advice uses the same owner lock and the pinned x/sys `Madvise` binding. The typed choices express supported operations. Linux determines eligibility from the current VMA, permissions, lock state, kernel configuration, and backing store, and can report partial application. Caching those properties to predict eligibility would duplicate Linux policy and could miss a kernel-visible change. Guard state changes page access while leaving both the owner and its backing reference intact. [Linux 7.0 advice implementation](https://github.com/torvalds/linux/blob/v7.0/mm/madvise.c).
