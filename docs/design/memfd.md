# memfd

Status: P01 complete with Linux 7.0 arm64 CI execution and local Linux 7.3 amd64 verification at Go 1.27.0 and 1.27.4. [IMPLEMENTATION.md](../../IMPLEMENTATION.md) owns acceptance and verification evidence. The shared [API conventions](api.md) apply.

The [package guide](../memfd.md) owns installation and user workflows; this note records design decisions and verification boundaries.

## Resource model and caller

`memfd.New(name, memfd.Options{})` returns an owned `*memfd.File` embedding `*os.File`. A caller writes or resizes through standard file methods, applies seals with `AddSeals`, inspects them with `Seals`, and closes the owner. Borrow `file.File` when another Go API requires `*os.File`; it is the same owner, not a duplicate. Do not replace the embedded pointer, wrap an unrelated file manually, or copy the wrapper after use. Dependencies point only to `os` and the pinned `golang.org/x/sys/unix`; no shared descriptor abstraction is needed.

The wrapper preserves `io.Reader`, `io.Writer`, `io.ReaderAt`, `io.WriterAt`, `io.Seeker`, and `io.Closer`. The [executable example](../../memfd/example_test.go) freezes a message's contents and size, then reads it with ordinary Go I/O. Offsets, partial I/O, EOF, `Stat`, `Truncate`, and `SyscallConn` retain `os.File` semantics. There is no separate sizing API. Files use plain names and explicit Linux build tags.

## Included operations and defaults

| Surface | Linux operation and behavior |
| --- | --- |
| `New(name, Options{})` | `memfd_create(MFD_CLOEXEC \| MFD_ALLOW_SEALING \| MFD_NOEXEC_SEAL)`: empty read/write file, no execute permission bits, initially sealed with `SealExec` |
| `Options{Executable: true}` | Replaces `MFD_NOEXEC_SEAL` with `MFD_EXEC`; initially executable and sealable with no seals; policy failures are returned without fallback |
| `(*File).Seals()` | `fcntl(F_GET_SEALS)` returns a snapshot, retaining unknown bits |
| `(*File).AddSeals(mask)` | `fcntl(F_ADD_SEALS)` irreversibly adds the supported mask; unknown input bits match `os.ErrInvalid` before the syscall |
| Embedded file methods | Standard Go I/O, size management, metadata, close, and raw descriptor access |

Names are debugging labels rather than paths. Empty names are permitted; names longer than 249 bytes or containing NUL fail with `EINVAL` through `*os.PathError`. The constructor has one acquisition step: after `MemfdCreate` succeeds, `os.NewFile` accepts the valid descriptor with no fallible setup operation. There is no intermediate descriptor or post-acquisition failure requiring cleanup. Failure tests verify invalid names and repeated kernel rejection without descriptor growth.

Default creation intentionally differs from raw zero flags: sealing is enabled and execute mode bits are cleared and locked. Data remains writable until another seal is applied. Close-on-exec is atomic and has no opt-out; use standard child-process descriptor passing when needed. Neither non-executable creation nor `SealExec` prohibits executable memory mappings or supplies a general execution sandbox.

| Seal | Contract |
| --- | --- |
| `SealSeal` | Prevent further `AddSeals` calls, including zero masks and already-present seals, with `EPERM` |
| `SealShrink` | Prevent reducing file size; growth and in-place writes remain allowed |
| `SealGrow` | Prevent increasing file size through resizing or extending writes; shrink and in-place writes remain allowed |
| `SealWrite` | Prevent writes and new shared writable mappings; resizing remains allowed; an existing shared writable mapping prevents adding this seal with `EBUSY` |
| `SealFutureWrite` | Prevent descriptor writes and new shared writable mappings while existing shared writable mappings continue to work |
| `SealExec` | Lock execute permission bits; when execute bits are set, adding this also adds shrink, grow, write, and future-write seals |

Seals belong to the inode and affect all its descriptors. They are cumulative and cannot be removed. A zero mask still invokes the kernel. Reapplying a seal is allowed until `SealSeal` is applied. No cache or emulation changes these semantics.

## Lifetime, concurrency, errors, and blocking

Callers explicitly close the file. Closing the embedded `os.File` closes the same owner; repeated close and subsequent sealing/I/O match `os.ErrClosed`. The zero value owns no file; error-returning methods match `os.ErrInvalid`. Nil receiver use is not a separate contract. Validation takes precedence for an unknown seal mask, including after close.

Sealing uses `SyscallConn.Control` to protect descriptor lifetime against concurrent close. Go's `os.File` raw control can return a private closing error before invoking the callback; the wrapper normalizes that control failure to `os.ErrClosed`, preserving errors from the actual `fcntl` syscall. This follows source inspection of `os/rawconn.go` and `internal/poll/fd_posix.go` and is covered by closed-file and concurrent sealing/close tests. No extra lock, mutable lifecycle state, private runtime hook, or error-string matching is needed.

Ordinary methods preserve `os.File` concurrency guarantees. Operations are individually safe concurrently; inspect-then-seal and write-then-seal sequences are not transactions. No deadlines or contexts are offered. Kernel allocation and sealing can take time, and close does not cancel an in-flight control callback. Raw callbacks must not close or retain the borrowed descriptor; duplicate within the callback for independent ownership. Raw access changes the same resource.

Creation and syscall failures use `os.PathError` with the operation and debugging label, preserving `errors.Is`/`errors.As` and errno identity. Permission, configuration, and unsupported-operation failures are not collapsed into one sentinel. No fallback weakens requested flags or seals.

## Feature requirements and omissions

The selected flags and all six seals are in the upstream Linux 7.0 UAPI. No post-7.0 flag is promised. Ordinary operations need permitted `memfd_create`, `fcntl`, and file I/O and normally need no capabilities. `MFD_EXEC` can be denied by hierarchical `vm.memfd_noexec` policy or other security controls. Tests require its success; a policy denial must remain an explicit verification gap. Tests never change the sysctl, host policy, or namespaces and do not execute a memory-backed program.

Initial omissions: huge pages and page-size encodings, execution helpers, adoption, duplication helpers, mapping APIs, sealing presets, and file-I/O wrappers. Huge-page flags predate the baseline but carry extra resource/privilege requirements. Mapping is used privately in tests to verify sealing; the future `mmap` package is independent.

`golang.org/x/sys v0.50.0` supplies maintained syscall bindings and architecture constants without copied UAPI tables or cgo; its Go floor is 1.26.0, below this module's 1.27.0. External Go tests use the user-requested `github.com/stretchr/testify/require` assertions (Testify v1.12.1) through the shared gotestsum harness. Dependencies add module/checksum resolution, generated vendoring via `make dep`, and future updates/security scans; Testify and its transitive dependencies are used only by tests.

## Verification

`package=memfd make specs` runs real kernel operations with race detection, JSON/JUnit reports, and coverage. It covers stream/random I/O, boundary labels, resizing, descriptor flags, execute-mode locking, each seal's effects and failures, invalid masks without mutation, mapped-file write/future-write distinctions, executable opt-in and implied seals, zero/closed state, borrowed-owner close, constructor failure cleanup, and concurrent sealing/close. Tests have no skips or extra profile. Mapping tests release their owned mappings without accessing unmapped memory. Descriptor-count checks run sequentially within the package.

There are no injected constructor failure seams. Unknown returned seal bits cannot be generated on this kernel through the supported API; retention is checked by reviewing the direct mask conversion. No fuzzing or benchmark is warranted for this small syscall boundary with no parser or performance promise.

Local commands/results and primary-target CI evidence are recorded in the P01 handoffs. [Go 1.27.0 job 35](https://circleci.com/gh/alexfalkowski/go-linux/35) and [Go 1.27.4 job 36](https://circleci.com/gh/alexfalkowski/go-linux/36) each execute all 23 memfd tests with zero failures or skips on actual Linux 7.0.0-1017-aws arm64 for implementation revision `3b9ae31`. Both jobs also pass vet and package/test compilation for amd64 and arm64; compilation remains separate from runtime acceptance. This closes the required P01 primary-target verification gap. Full amd64 baseline support remains a separate project limitation.

## Primary references

- [Linux 7.0 memfd flags](https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/memfd.h) and [seal constants](https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/fcntl.h).
- [Linux 7.0 creation and sealing](https://github.com/torvalds/linux/blob/v7.0/mm/memfd.c): name limit, initial seals, inode scope, mapping checks, execute seals, and hierarchical policy.
- [memfd_create(2)](https://man7.org/linux/man-pages/man2/memfd_create.2.html) and [F_GET_SEALS/F_ADD_SEALS](https://man7.org/linux/man-pages/man2/F_GET_SEALS.2const.html).
- [Non-executable memfd policy](https://docs.kernel.org/userspace-api/mfd_noexec.html).
- [Go os.File and NewFile](https://pkg.go.dev/os#File), [RawConn](https://pkg.go.dev/syscall#RawConn), and [x/sys/unix v0.50.0](https://pkg.go.dev/golang.org/x/sys/unix@v0.50.0).
