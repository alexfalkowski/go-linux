# Mapped regions

Import `github.com/alexfalkowski/go-linux/mmap` for anonymous or file-backed mapped memory. Linux 7.0+ is the baseline; see [verification status](verification.md), [executable examples](../mmap/example_test.go), and [design decisions](agents/accepted-design/mmap.md).

## Good for

Use mapped regions to read file contents, share updates, or grow an in-memory index as entries arrive. [SQLite optionally uses memory-mapped I/O for database reads](https://sqlite.org/mmap.html), showing where mapped access can avoid a separate read buffer.

## Create and use a region

```go
region, err := mmap.Anonymous(4096, mmap.Options{
    Protection: mmap.Read | mmap.Write,
})
if err != nil {
    return err
}
defer region.Close()

copy(region.Bytes(), "hello")
if err := region.Protect(mmap.Read); err != nil {
    return err
}
message := string(region.Bytes()[:5]) // An independent Go copy.
_ = message
```

The caller owns `Region` and must close it. Zero options mean `Private` and `NoAccess`: set protection explicitly before reading/writing. `Protect` accepts `NoAccess`, `Read`, `Write`, or `Read|Write` for the whole region. Requested permissions can be broadened by architecture/process settings; this API is not a sandbox. It does not offer executable mappings.

`Bytes` borrows memory with exactly the requested length and capacity; ordinary Go bounds checks apply. It performs no permission check. All slices, subslices, and pointers derived from it are valid only while the region remains mapped with the necessary protection. Stop every access before `Resize`, `Close`, protection changes, or advice that changes contents or access. Access after unmapping can fault or silently corrupt another mapping. Retaining a slice or Region cannot pin the mapping. Copy bytes into ordinary Go storage when they must outlive the region.

## Map a file

`mmap.Map(file, offset, length, options)` borrows `*os.File` during the call and returns an independent mapping owner. Use a positive length and a nonnegative offset divisible by `os.Getpagesize()`; the length need not be page-aligned. Closing the file after success leaves the mapping valid, and closing the region leaves the file open. For a `memfd.File`, call `file.Map(offset, length, options)` directly; its descriptor stays private.

Select `mmap.Shared` to make changes visible to other shared mappings and ordinary file I/O. The source must be readable. Shared writable mappings also require a writable source. `mmap.Private` (the default) permits private copy-on-write modifications without changing the file, even with a read-only source. Later file modifications have unspecified visibility in private mappings. Use explicit synchronization when mapped writes need to reach storage.

Arrange adequate file contents and coordinate truncation with every mapping user. Mapping does not extend or freeze the file; accessing whole pages beyond EOF can cause fatal SIGBUS, including when another owner truncates a mapped file. There is no race-free automatic file-size check.

## Resize a region

Pause all readers and writers before calling `region.Resize(newLength)`. Linux may relocate the mapping. On every successful call, discard all old slices, subslices, and pointers and obtain a fresh view from `region.Bytes()`. On failure the original mapping and its views remain usable with their existing permissions. If the OS limits a locked mapping's growth, the error preserves Linux's `EAGAIN`. [Executable resize example](../mmap/example_test.go). [mremap(2)](https://man7.org/linux/man-pages/man2/mremap.2.html).

The new view has exactly the requested length and capacity. The retained prefix keeps its contents and protection. Linux operates on whole pages, so shrinking within a page leaves the underlying bytes available to a later grow, and newly exposed bytes within an already mapped page may contain earlier data. Close releases the current mapping. Growth requires one eligible Linux virtual memory area. If prior page operations split the range or the backing cannot expand, Linux can return `EFAULT`.

Shared anonymous mappings can shrink and regrow within their original page-rounded size. Regrowth can expose earlier contents from the same backing object. Larger lengths match `os.ErrInvalid`. To grow shared storage beyond that size, use a file-backed mapping, such as `memfd.File.Map`, and extend the backing file.

Resize leaves the backing file's length unchanged. Grow the file far enough to cover the mapping offset and new length before accessing the added range. When reducing both sizes, end access and shrink the mapping before truncating the file. Coordinate that sequence with other users of the file.

## Synchronize a region

After updating a shared file mapping, use `region.Sync(mmap.SyncOptions{})` to wait for the filesystem to synchronize the mapped data. Sync covers every page intersecting the region, including the entire last page of a partial-page view. Linux uses the mapping's backing reference, so Sync works after the original file handle closes. Close unmaps the region independently of writeback. See the [executable Sync example](../mmap/example_test.go).

The zero options and `Mode: mmap.SyncWait` request `MS_SYNC`. The call can block while the filesystem completes data synchronization for the covered file range. Coordinate writers through the return of Sync to control which changes are included. Filesystem and device guarantees determine durability. Applications still own ordering across updates, directory-entry durability, and crash recovery. Private copy-on-write changes stay in the view, and anonymous mappings have no persistent storage. [Linux 7.0 msync implementation](https://github.com/torvalds/linux/blob/v7.0/mm/msync.c).

`Mode: mmap.SyncAsync` requests `MS_ASYNC`. Linux tracks dirty pages independently, so this mode neither starts nor waits for writeback. Set `Flags: mmap.SyncInvalidate` with either mode to request `MS_INVALIDATE`. Linux returns `EBUSY` for locked pages in the range, and a successful request carries no eviction or reload guarantee. Choose one mode. Unknown flags or invalid modes match `os.ErrInvalid`, after checking the region's lifetime. A kernel error can follow partial writeback, preserves its errno, and leaves the caller owning the region. [msync(2)](https://man7.org/linux/man-pages/man2/msync.2.html).

## Lock pages in memory

Use `region.Lock(mmap.LockImmediate)` before handing a mapped buffer to a latency-sensitive worker. Choose `mmap.LockOnFault` to lock pages already present in the mapping and have Linux lock additional pages as they fault in. A page present only in the file cache can already appear as resident in `Residency` and is locked when it faults into this mapping. Both modes cover every page intersecting the region, including a partial final page, and charge the whole range against the process's `RLIMIT_MEMLOCK` unless it has permission to bypass that limit. The on-fault mode requires the actual `mlock2` operation. A syscall denial or unavailable operation returns its original errno. [mlock(2)](https://man7.org/linux/man-pages/man2/mlock.2.html). [Linux 7.0 lock implementation](https://github.com/torvalds/linux/blob/v7.0/mm/mlock.c#L358-L370).

Call `region.Unlock()` to release the lock while continuing to use the mapping. A successful `Close` unmaps the region and releases any remaining lock. Locking the same pages again adds no reference count. Linux can reclaim unlocked pages later. An error from the kernel may follow a change in lock state. Memory locking addresses paging. Applications handling secrets still own access control, dump policy, and cleanup.

Call `region.Residency()` to inspect the status Linux reports without accessing the mapped bytes. `snapshot.Resident[i]` describes the page starting at byte offset `i * snapshot.PageSize` within the region. The caller owns the snapshot and can keep it after closing the mapping. Residency can change while the call is in progress. File-backed pages may already be in the page cache, and Linux can report every page as resident when access rules hide that cache state. A resident page can still fault when first accessed. [Executable example](../mmap/example_test.go). [Linux 7.0 mincore implementation](https://github.com/torvalds/linux/blob/v7.0/mm/mincore.c).

## Advice, errors, and concurrency

Select one typed `Advice` in `region.Advise`. Linux applies it to every page intersecting the complete region, including the partial last page. Coordinate contents and access changes with all view users. Advice can block and may apply to part of the region before returning an error. [madvise(2)](https://man7.org/linux/man-pages/man2/madvise.2.html).

| Advice | Effect and prerequisites |
| --- | --- |
| `Normal`, `Random`, `Sequential`, `WillNeed` | Set the access pattern or request read-ahead. Residency can change independently. |
| `DontNeed`, `DontNeedLocked` | Discard private anonymous data or private file changes. File-backed and shared regions repopulate from their backing object. Choose `DontNeedLocked` to permit discarding pages even in locked regions while keeping the lock policy. |
| `Free` | Makes unlocked private anonymous pages eligible for lazy discard. |
| `Remove` | Punches a hole in the backing store of an unlocked shared mapping. Linux must permit changing the backing object, and its filesystem must support hole punching. A mapping from a writable file can punch holes even with `mmap.Read` protection. Other users see the holes, and the backing object's size stays the same. |
| `DontFork`, `DoFork`, `WipeOnFork`, `KeepOnFork` | Exclude the mapping from fork inheritance or restore eligibility. Wipe-on-fork gives a child zero-filled pages and requires private anonymous memory. Keep-on-fork clears that setting. |
| `DontDump`, `DoDump` | Omit the region from core dumps or return it to normal system dump policy. |
| `Mergeable`, `Unmergeable` | Opt eligible private anonymous pages into or out of kernel same-page merging. Availability and merging depend on kernel configuration, page eligibility, and system policy. |
| `HugePage`, `NoHugePage`, `Collapse` | Set transparent huge-page eligibility or request synchronous best-effort collapse of eligible huge-page-aligned ranges. Availability and allocation depend on the kernel and backing memory. |
| `Cold`, `PageOut` | Deprioritize eligible unlocked pages or ask Linux to reclaim them. Success carries no eviction, writeback, or residency guarantee. |
| `PopulateRead`, `PopulateWrite` | Prefault with the requested permission. Write population can break copy-on-write. Errors can expose backing or permission problems during the call, and residency can change afterward. |
| `GuardInstall`, `GuardRemove` | Install access-faulting guards on unlocked pages or remove them. Installing guards discards private contents. Removing them restores access according to the mapping's protection and backing. |

Methods serialize their own operations, including concurrent Close, but cannot protect escaped slices. Callers must synchronize shared-memory accesses themselves and ensure all views stop being used before lifecycle changes. Sequences of method calls are not transactions.

Errors preserve errno and standard identities through `errors.Is`/`errors.As`, with `os.PathError` operation context. Invalid length, alignment, mode, or protection matches `os.ErrInvalid`; overflowing signed 64-bit offset+length matches `EOVERFLOW`. Construction validates arguments before source-file state. Valid arguments with a nil source match `os.ErrInvalid`; a closed source matches `os.ErrClosed`. Kernel permission and filesystem errors remain observable.

A zero Region returns nil from Bytes and `os.ErrInvalid` from other methods. After successful Close, Bytes returns nil and other methods, including repeated Close, match `os.ErrClosed` before validating method arguments. A failed Close retains ownership for retry. Kernel protection/advice errors may follow partial application. Do not copy a Region after use; nil receivers are unsupported.

## Coverage boundary

The mapping owner supports `Private` and `Shared` and a single contiguous region. Fixed-address replacement and alias-producing remaps require control of other virtual ranges or a separate owner. Explicit huge-page mappings, executable/protection-key mappings, MAP_SYNC, and process-wide locking are outside the API. Hardware-poison/offline advice affects memory beyond this owner's lifetime. Remote-process advice requires a different process and access contract. Advice values outside the supported set match `os.ErrInvalid`. [Linux 7.0 memory controls](https://github.com/torvalds/linux/blob/v7.0/include/uapi/asm-generic/mman-common.h).

Run `make specs package=mmap profile=rootless required=true` for ordinary-user integration coverage, including bounded subprocess faults and remapping. See the [verification guide](verification.md) for runner requirements and reports.
