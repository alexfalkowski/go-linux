# mmap design

P04 adds owned anonymous and file-backed mappings. Implementation and verification status are tracked in [the plan](../../IMPLEMENTATION.md); ordinary Linux 7.3 amd64 execution and Linux 7.0 arm64 CI acceptance are distinct. The [usage guide](../mmap.md) owns caller workflows.

## Resource and caller model

The caller borrows a standard file for `mmap.Map(file, offset, length, options)` or creates `mmap.Anonymous(length, options)`, uses `region.Bytes()` within its lifetime, optionally changes `Protect`/`Advise`, then calls `Close`. `Region` owns one complete original mapping and implements `io.Closer`. It owns no descriptor and does not act as a stream. Closing the source file after construction leaves the mapping valid; closing the mapping leaves the file open. No duplication or adoption API, garbage-collection cleanup, raw address accessor, or `SyscallConn` belongs to this resource.

`Map` acquires `file.SyscallConn()` and runs `unix.Mmap` inside `Control`, protecting descriptor lifetime against concurrent close without taking ownership. Control's private closing error is normalized to `os.ErrClosed`, following the descriptor packages' inspected Go behavior. There are no fallible post-mapping setup steps. `Anonymous` calls `unix.Mmap` with descriptor -1, offset zero, and `MAP_ANONYMOUS`. The pinned x/sys mapper registers the original slice and only removes registration after successful unmapping; Region retains that exact slice.

## Included operations and defaults

| Surface | Initial contract |
| --- | --- |
| `Mode` | Exclusive `Private` (zero, `MAP_PRIVATE`) or `Shared` (`MAP_SHARED`); separate anonymous calls create independent backing objects |
| `Protection` | `NoAccess` (zero), `Read`, `Write`, and `Read|Write`; reject every other bit |
| `Options` | Mode and protection only; zero options deliberately create private inaccessible memory |
| `Map` | Explicit positive length, nonnegative page-aligned offset, checked signed 64-bit offset+length; source permissions remain kernel-enforced |
| `Anonymous` | Explicit positive length, initially zero-filled memory, same mode/protection validation |
| `Bytes` | Borrowed slice with requested length and capacity, nil for zero/closed owner; does not access or validate page permissions |
| `Protect` | Whole-region `mprotect`, including the partial final page; validates masks before entering the kernel |
| `Advise` | Whole-region `Normal`, `Random`, `Sequential`, `WillNeed`, and `DontNeed`; exclusive advice values |
| `Close` | Whole-original-region `munmap`; invalidate views only after successful unmap, retain ownership on error |

These options cover the current callers without a generic flag framework. Executable permissions, protection keys, fixed addresses, remapping, partial-range operations, memory locking, explicit huge pages, residency queries, synchronization/persistence helpers, fork controls, and other advice are omitted. Their omission bounds this API and is not an assertion that they need a newer kernel. No extra dependency or new shared mapping framework is needed.

## Bounds, protection, and lifetime

Use `os.Getpagesize()` for offset alignment; do not assume 4096-byte pages on arm64. Kernel page rounding grants no extra Go slice capacity. Invalid length, negative/unaligned offset, unknown mode, or unknown protection matches `os.ErrInvalid`; an offset+length above MaxInt64 matches `EOVERFLOW`. Validation precedes source access. Nil files match `os.ErrInvalid`; closed files match `os.ErrClosed`. Errors use `os.PathError` with the syscall operation and preserve errno identity. The zero Region is invalid; after successful close, repeated close and error-returning operations match `os.ErrClosed`, ahead of method-argument validation. Nil receivers are unsupported.

File mappings neither resize files nor check/freeze their sizes. A size check could race with truncation and would prohibit legitimate Linux mappings; the caller must arrange adequate backing and coordinate size changes. Access beyond EOF can fault. Private writes are copy-on-write and never update the file; subsequent file-change visibility in a private mapping is unspecified. Shared writes are visible to peers and ordinary file I/O, but visibility is neither memory synchronization nor disk durability. Close performs no implicit `msync`.

Protection requests are subject to architecture and process-personality rules: for example write can imply read, and read can imply execute. This package does not promise strict executable exclusion or a sandbox. NoAccess forbids access while preserving the slice's shape. Methods never dereference protected contents. A kernel protection/advice error may follow partial application; no rollback is promised.

A mutex serializes methods and protects the slice header/closed state. It cannot guard slice accesses after `Bytes` returns. Callers must stop and coordinate all view access before `Close`, `Protect`, or content-affecting advice, and synchronize conflicting memory reads/writes themselves. No copy, finalizer, `KeepAlive`, or reachability rule makes an escaped slice safe after explicit unmap. An invalid access may fault or touch a reused address, including silently corrupting unrelated memory. A successfully closed Region drops its view but cannot revoke callers' views.

Advice values normal/random/sequential are access-pattern hints. WillNeed requests preparation without a residency guarantee. DontNeed changes contents: private anonymous pages refault as zero; private file copies are discarded and reload from the file; shared mappings refault from the backing object, whose reclamation may be delayed. DontNeed is not a shared-memory eraser or a persistence operation. No syscall deadline/context or page-fault latency bound is promised.

## Verification and pilot consolidation

External Go tests use the repository's gotestsum/race/require harness. Ordinary-user checks need permitted mmap/munmap/mprotect/madvise, temporary files, and procfs. No capability profile or skip is needed. Tests exercise both anonymous modes, page-derived offsets and partial lengths, shared peers and file I/O, private copies, source/region independence, read-only permission failures, whole-region protection, all five advice values, invalid inputs, standard error identity, zero/closed owners, concurrent methods/source closure, complete unmapping via `/proc/self/maps`, and construction failure cleanup.

Bounded subprocesses use the current test executable and `debug.SetPanicOnFault` to observe actual address faults from default/changed NoAccess, writes to the last page after read-only protection, a retained slice after unmap, and file truncation. Setup errors, unexpected panics, child timeouts, or absence of an address fault fail; arbitrary process failure is not accepted as proof. These negative tests demonstrate misuse consequences without promising every stale address will fault. Pattern hints' performance effects and WillNeed residency are not asserted. Shared anonymous backing mode is additionally inspected through procfs in tests; sharing across raw fork is not exposed by the Go caller workflow.

The original P04 review found no common internals worth extracting. The user-selected opaque-resource migration now uses direct private file fields; pidfd owns its wake file locally, and the temporary shared descriptor holder has been removed. mmap still owns no descriptor and keeps its standard `*os.File` borrowing API; memfd now supplies a typed `File.Map` operation rather than exposing its file. Facility-specific synchronization, error handling, and mapping lifetime remain distinct.

Required Linux 7.0 arm64 execution is verified by [Go 1.27.0 job 92](https://circleci.com/gh/alexfalkowski/go-linux/92) and [Go 1.27.4 job 90](https://circleci.com/gh/alexfalkowski/go-linux/90) for implementation revision 11c177c. Each passes all 36 mmap events, including both examples and five fault scenarios, and 172 total regression test events without failures/skips. The retained environment reports confirm actual Linux 7.0.0-1017-aws arm64 execution at the matching Go version. P04 is complete for the available target; see the plan's CI acceptance handoff. Failed-unmap retention and rare kernel allocation/partial-application errors are source-reviewed rather than fault-injected; no unsupported external unmapping or private seam is introduced to force them.

## Primary references

- [mmap/munmap](https://man7.org/linux/man-pages/man2/mmap.2.html), [mprotect](https://man7.org/linux/man-pages/man2/mprotect.2.html), and [madvise](https://man7.org/linux/man-pages/man2/madvise.2.html): Linux operation contracts, backing-file access, page rounding, advice effects, and fault behavior.
- [Linux 7.0 mapping-mode UAPI](https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/mman.h) and [common protection/advice UAPI](https://github.com/torvalds/linux/blob/v7.0/include/uapi/asm-generic/mman-common.h): selected baseline flags and values; separate architecture-header fetches were unavailable, so pinned per-architecture bindings and cross-compilation supplement these checks.
- [Pinned x/sys mapper](https://cs.opensource.google/go/x/sys/+/v0.50.0:unix/syscall_unix.go) and [Linux syscall wrappers](https://cs.opensource.google/go/x/sys/+/v0.50.0:unix/zsyscall_linux.go): exact slice registration/unmapping and syscall arguments, inspected in vendor.
- [os.File.SyscallConn](https://pkg.go.dev/os#File.SyscallConn), [syscall.RawConn](https://pkg.go.dev/syscall#RawConn), and [Go 1.27.4 raw connection source](https://cs.opensource.google/go/go/+/go1.27.4:src/os/rawconn.go): protected descriptor use.
- [debug.SetPanicOnFault](https://pkg.go.dev/runtime/debug#SetPanicOnFault): subprocess fault observation for non-Go memory.
