# API decisions

Status: F01 design record with implementation follow-ups, 2026-11-14. This document owns shared conventions and the original provisional sketches for P01–P06; sketches alone do not establish package availability or runtime verification. [IMPLEMENTATION.md](../../IMPLEMENTATION.md) owns scope, execution order, acceptance, and verification status. Each package item must refine its sketch in its own design note and supply executable examples and kernel tests before claiming completion.

Implementation follow-up: [memfd](memfd.md) owns the accepted P01 API; [eventfd](eventfd.md) owns the accepted P02 API and verified primary-target runtime behavior. The original sketches and F01 evidence below remain design history where refined by those notes.

## Standard-library overlap

The caller is a Go program that needs a Linux facility while retaining normal Go I/O and resource management. Ordinary application work should continue through the standard library. The inventory uses the installed Go 1.27.4 documentation and source, including the API history for features available before the module's Go 1.27.0 floor; minimum-version execution remains F02 work.

| Planned packages | Existing Go surface | Boundary retained here |
| --- | --- | --- |
| `memfd`, `file`, `xattr` | `os.File` already supplies ordinary I/O, seeking, sizing, metadata, and close; `os.Root` supplies operations confined beneath a directory | Memory-backed creation/seals, explicit Linux resolution/metadata/rename controls, and extended attributes; return or reuse standard files |
| `eventfd`, `timerfd` | Channels, `sync`, `time.Timer`, and `time.Ticker` cover ordinary Go synchronization and timers | Kernel counters and timers represented by descriptors, including semaphore reads, expiration counts, and Linux clock domains |
| `mmap` | Slices and file reads cover ordinary byte access; `syscall.Mmap` exposes raw mappings | Explicit mapping ownership, protection, advice, and invalidation rules; no replacement allocator |
| `pidfd` | `os.Process` supports signaling and child waiting; `Process.WithHandle` exposes a lifetime-protected pidfd callback on Linux; `syscall.SysProcAttr.PidFD` can obtain a descriptor during process creation | Independently owned/adopted process descriptors and exit readiness without reaping; keep process startup in `os/exec` or `os.StartProcess` |
| `mount`, `namespace`, `cgroup` | Files and `SysProcAttr` cover parts of process setup and pseudo-filesystem access | Explicit mount/configuration handles, namespace mechanisms, and v2 resource control; no container lifecycle manager |
| `epoll` | The Go runtime already polls supported files and network connections | Explicit application readiness sets; no replacement for the runtime poller |
| `inotify`, `fanotify` | Ordinary `os` operations do not supply filesystem event streams | Linux event records, watch identities, overflow, and permission-event ownership |
| `socket`, `netlink`, `netlink/route` | `net.Conn`, `net.PacketConn`, Unix message I/O, `FileConn`, and `SyscallConn` already cover much networking | Linux extensions, ancillary data, and bounded kernel protocols; preserve datagram boundaries and truncation |
| `capability`, `seccomp`, `landlock` | `SysProcAttr` supports selected child setup; it is not a general live-process security API | Explicit kernel state/restriction operations with documented thread and exec scope |
| `procfs`, `sysfs` | `os`, `io/fs`, `bufio`, and parsing packages provide file access and parsing machinery | Bounded typed Linux records with an explicit root, visibility/race handling, and unknown-field policy |

In particular, `os.Root` permits filesystem-boundary traversal and is not equivalent to every `openat2` resolution restriction. P08 must compare its precise operation list with the chosen Go version again. `os.Process.WithHandle` is already public since Go 1.26; P05 must not claim that the standard library hides all process descriptors. References: [Go 1.27.4 os](https://pkg.go.dev/os@go1.27.4), [Root](https://pkg.go.dev/os@go1.27.4#Root), [Process.WithHandle](https://pkg.go.dev/os@go1.27.4#Process.WithHandle), [SysProcAttr](https://pkg.go.dev/syscall@go1.27.4#SysProcAttr), and [net](https://pkg.go.dev/net@go1.27.4).

## Existing Go libraries as references

These are selected API comparisons, not an exhaustive ecosystem survey or dependency endorsements. No dependency is added by F01. Recheck the relevant library and standard-library overlap when implementing each package; do not copy implementation code without a licensing review.

| Reference | Surface inspected | Design consequence |
| --- | --- | --- |
| [golang.org/x/sys/unix](https://pkg.go.dev/golang.org/x/sys/unix) | Syscall wrappers, constants, and ABI types | Use as the normal syscall foundation; select and pin a compatible version when the first implementation needs it, rather than copying syscall tables |
| [iofd](https://pkg.go.dev/code.hybscloud.com/iofd) | MemFD, EventFD, TimerFD, PidFD, mapped regions, and raw nonblocking defaults | Typed helpers are useful; retain separate facility packages and standard Go polling semantics instead of adopting its universal descriptor abstraction |
| [mmap-go](https://pkg.go.dev/github.com/edsrzf/mmap-go@v1.2.0) | Slice-based mappings, MapRegion, Flush, and Unmap | Make lifetime explicit through a region owner; do not infer Linux protection/advice guarantees from a portable mapping API |
| [moby/sys/mount](https://pkg.go.dev/github.com/moby/sys/mount@v0.3.5) | Mount/unmount and propagation helpers | Retain familiar mount vocabulary, but model configuration contexts and detached mounts separately instead of a single device/target/options operation |
| [mdlayher/netlink](https://pkg.go.dev/github.com/mdlayher/netlink@v1.11.2) and [vishvananda/netlink](https://pkg.go.dev/github.com/vishvananda/netlink@v1.3.1) | Message/attribute connection APIs versus typed route operations | Keep protocol framing in P13 and route resources in P14; defer their exact designs until those items |
| [fsnotify](https://pkg.go.dev/github.com/fsnotify/fsnotify@v1.10.1) | Portable watcher/event API | P10 exposes Linux watch/cookie/overflow details; a portable channel watcher is a different boundary |

`x/sys/unix` adds an external module and vendoring/update obligations, but supplies architecture-specific UAPI definitions without cgo or private runtime integration. This is preferable to maintaining copied ABI declarations. The other libraries remain references, avoiding dependencies on overlapping lifecycle, polling, or higher-level policy. Any missing wrapper in the pinned syscall dependency must be established from its Linux-specific source before deciding whether an internal addition is necessary.

## Shared conventions

### Naming, types, and package direction

Use a facility package plus a resource noun: `memfd.File`, `eventfd.Counter`, `timerfd.Timer`, `mmap.Region`, `pidfd.Process`, and `mount.Config`/`mount.Handle`. Use `New` for a newly allocated resource, `Open` for acquisition by identity, `Adopt` for ownership transfer, and `Close` for releasing the owned local resource. Keep Linux terms such as seal, pidfd, and mount where they communicate the mechanism. Use typed flags for independent bits and enums for exclusive modes. Validate unknown bits, invalid enum values, overflow, and contradictory combinations; zero options must have one documented meaning.

Public packages depend on the standard library, `x/sys/unix`, and only demonstrated shared internals. The six pilots must not depend on future `epoll` or `namespace` packages. For example, a caller passes the standard file underlying a memfd to `mmap`; `mmap` need not import `memfd`. Keep byte order, ABI layouts, and syscall plumbing private. Do not add a public descriptor superclass, generic options framework, or speculative helper package. Consolidate only repetition demonstrated by P01–P04.

Put package documentation in `doc.go`. Exported identifier comments describe behavior, ownership, errors, feature requirements, and the relevant Linux interface. Package notes own included and omitted operations. These Go APIs have no HTTP or RPC transport; REST/AIP route conventions do not apply.

### Ownership, lifetime, and concurrency

Constructors return one owner with deterministic `Close`; create descriptors close-on-exec atomically. Close-on-exec has no initial opt-out: explicit child descriptor passing uses standard process-start facilities. Close every acquired intermediate resource when construction fails, preserving the primary error and reporting a cleanup error when material. Do not retry a Linux `close` after an error: its descriptor number may already have been reused.

Passing a file into an operation borrows it only for the operation unless the method explicitly says otherwise. `Adopt` transfers ownership only on success; failure leaves the caller responsible. Duplication creates a separate descriptor owner, but shared open-file-description state remains shared. Never wrap the same live descriptor in two independent `os.File` owners. A borrowed file or descriptor must remain valid for the operation; use `SyscallConn.Control` for descriptor lifetime protection where applicable.

Owned types are used through pointers and must not be copied after first use. Their zero value owns no resource; error-returning operations on it report `os.ErrInvalid`, and a slice view may return nil where documented. For successfully constructed descriptor resources, repeated close and operations after close match `os.ErrClosed` through `errors.Is`. Nil receiver support is not a separate promise. Do not rely on garbage collection for cleanup, and do not attach an automatic unmap finalizer to a mapping whose slice can escape.

`memfd` ordinary file methods retain `os.File` concurrency guarantees. Counter and timer operations are intended to be safe concurrently, including close waking a poll-blocked operation with a closed error; this is a requirement for P02/P03 to verify, not F01 runtime evidence. Multiple readers compete for kernel records. Synchronizing individual calls does not make a sequence transactional. Mapping views require caller synchronization against protection changes and unmapping. Mount configuration sequences require caller serialization. P05 must document the interaction between multiple waiters, reaping, and close.

Expose `SyscallConn() (syscall.RawConn, error)` on descriptor owners where applicable. Within a raw callback, do not close the borrowed descriptor, retain its numeric value for later use, or change nonblocking mode behind the wrapper. A caller requiring independent lifetime must duplicate under the callback and own that duplicate. Raw access can consume counters/events or change resource state; it is not a second independent stream. See [RawConn](https://pkg.go.dev/syscall@go1.27.4#RawConn).

### Byte I/O and typed helpers

Expose standard interfaces only where their behavioral contracts fit. `memfd.File` reuses `os.File` methods rather than reimplementing streams, offsets, or sizing. Counter and timer reads preserve fixed eight-byte records in native byte order; a larger read buffer still consumes one record, and a smaller buffer, including zero length, is invalid. Use `encoding/binary.NativeEndian` for typed helpers, not `unsafe` casts. A byte `Read` count is a byte count; typed helper results are counter values or expiration counts.

For eventfd, writes must contain exactly eight bytes on the Linux 7.0 baseline. Do not split or coalesce records. Invalid buffers and the forbidden all-ones write value fail without changing the counter. Standard `io.Writer` short-write errors must still be honored. Interface assertions supplement behavioral tests; they do not prove stream utilities are appropriate. In particular, `io.Copy` with arbitrary chunk sizes is not the counter protocol. References: [io contracts](https://pkg.go.dev/io@go1.27.4), [eventfd](https://man7.org/linux/man-pages/man2/eventfd.2.html), and the authoritative baseline [eventfd read/write implementation](https://github.com/torvalds/linux/blob/v7.0/fs/eventfd.c). The latter checks write length for equality, which is more precise than the manual's description of undersized writes.

Keep the underlying `os.File` private for record descriptors. Its generic I/O can bypass a zero-length read and its writer can loop after a short write; a timer's exceptional zero-byte read must not accidentally become permanent EOF. Use supported raw callbacks where necessary to preserve one kernel operation and its result while retaining lifetime and polling support. This is a design inference from the inspected Go 1.27.4 `os/file_unix.go`, `os/rawconn.go`, and `internal/poll/fd_unix.go`; no private runtime calls are permitted.

### Blocking, deadlines, and errors

For pollable counter/timer descriptors, create with atomic nonblocking and close-on-exec flags, then use `os.NewFile` and its public polling integration. The intended public byte and typed operations wait like Go I/O, with deadlines and concurrent close; the internal descriptor being nonblocking does not imply public `EAGAIN` behavior. Before returning a constructor that promises deadlines, establish polling support, for example through a zero deadline probe, and clean up on failure. Do not silently fall back to a blocking syscall. See [NewFile](https://pkg.go.dev/os@go1.27.4#NewFile).

Raw callbacks retry `EINTR` where the operation permits, return false only when readiness must be awaited, and preserve terminal syscall errors. `RawConn.Control` is the route for a caller deliberately making one nonblocking syscall and observing `EAGAIN`; the initial sketches do not add a second raw mode or `Try` family. Never hold a wrapper lock across a wait if close needs that lock to wake it. Implement polling/close tests before promising the behavior on a runner.

Use `time.Duration` for relative intervals and `time.Time` for Go I/O deadlines. Those deadlines bound the I/O operation; they do not arm/disarm a kernel timer. A zero I/O deadline clears the deadline. Deadline errors must match `os.ErrDeadlineExceeded`. Do not offer deadlines for ordinary memory-backed files, mappings, mount configuration, or every syscall. Do not accept contexts until there is a tested cancellation mechanism that actually releases the pending operation.

Preserve errno identity through `errors.Is`/`errors.As`. Prefer existing `*os.PathError` for file/path operations and `*os.SyscallError` for contextual syscall failures; reuse errors already supplied by `os.File`. Validation errors match `os.ErrInvalid` unless the documented kernel-shaped check promises `unix.EINVAL`. A new structured error is justified for additional information such as mount diagnostics, with `Unwrap` preserving its cause. Do not classify `EPERM`, `EINVAL`, or `ENOSYS` uniformly as unsupported: permissions, options, filtering, configuration, and kernel support differ. Preserve partial results and irreversible side effects explicitly; successful `Close` is not a promise to reverse prior operations.

### Compatibility and feature policy

Keep the plan's Linux 7.0 and Go 1.27.0 floors, arm64 primary runtime target, and amd64 compilation/available execution distinction. Linux-specific files use appropriate build constraints; do not add non-Linux emulation stubs. A feature being defined in upstream 7.0 is different from it being permitted/configured on a runner. Compare operations against versioned upstream sources and probe actual prerequisites. Optional later flags carry explicit requirements and must never silently raise the floor or weaken a requested guarantee.

The sketches may change during the owning package item, with reasons recorded in its design note and material changes in the plan. Once shipped, exported names/types, defaults, error matching, blocking/close behavior, and ownership are compatibility surfaces. Additive options keep zero-value behavior; do not change a requested flag into a weaker fallback. Before v1, record intentional breaks and migration consequences; v1 follows [Go module versioning](https://go.dev/doc/modules/version-numbers). Reassess the Go floor against the [Go support policy](https://go.dev/doc/devel/release#policy), without treating a policy check as minimum-toolchain execution.

## Provisional pilot sketches

The following tables are signature sketches, not compilable examples or exported declarations. Named types describe intended domain concepts; exact constants, integer backing types, and method sets are finalized and tested in the owning item. Each caller flow assumes checking errors after every operation and releasing each successfully acquired owner. No sketch below constitutes completed package acceptance.

### P01: memfd

P01 refines this provisional sketch in [memfd.md](memfd.md); that note and the package documentation own the implemented API. The plan records completed runtime acceptance on the available Linux 7.0 arm64 target.

Caller flow: `memfd.New` → standard `Truncate`/`WriteAt`/`ReadAt` → `AddSeals` → `Close`. `File` embeds `*os.File` so ordinary interfaces, offsets, and sizing stay standard; its additional responsibility is seal inspection/application. The embedded file is the same owner: callers may pass it to file operations but must not replace it or independently assume ownership.

| Sketch | Contract |
| --- | --- |
| `New(name string, options Options) (*File, error)` | New empty memory-backed file; `Options.Executable` is an explicit opt-in, false by default |
| `(*File).Seals() (Seals, error)` | Inspect the inode's seal bits, including bits not recognized by this version |
| `(*File).AddSeals(seals Seals) error` | Apply a supported mask irreversibly; expose seal-further-sealing, shrink, grow, write, future-write, and execute controls with Go names |
| Embedded file operations | Ordinary byte I/O, random access, seeking, `Stat`, `Truncate`, `SyscallConn`, and `Close`; deadlines are not promised |

Default creation requests `MFD_CLOEXEC | MFD_ALLOW_SEALING | MFD_NOEXEC_SEAL`; executable opt-in replaces `MFD_NOEXEC_SEAL` with `MFD_EXEC`. The default is intentionally sealable and non-executable, differing from raw zero flags; it begins with the execute seal implied by no-exec creation. Ordinary data remains writable until the caller seals it. Names are debugging labels, not paths. Preserve permission/configuration failures without retrying as executable.

Initial omissions: huge-page allocation/size encodings and execution helpers. Huge-page and execute controls are already present in the selected baseline, not post-7.0 requirements; their extra policy/resource requirements are distinct from kernel age. No post-7.0 flag is initially promised. Inspect the exact chosen flags/seals against the [7.0 memfd header](https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/memfd.h) and fcntl definitions during P01. References: [memfd_create](https://man7.org/linux/man-pages/man2/memfd_create.2.html), [seals](https://man7.org/linux/man-pages/man2/F_GET_SEALS.2const.html), and [non-executable memfd policy](https://docs.kernel.org/userspace-api/mfd_noexec.html). P01 must verify size/I/O, each included seal's failure behavior, lifecycle, and constructor cleanup without elevated privileges when permitted.

### P02: eventfd

P02 refines this sketch in [eventfd.md](eventfd.md), with a [package guide](../eventfd.md) and executable kernel tests. `Mode` is a `uint8` enum with `Default = 0` and `Semaphore = 1`. Borrowed access uses the standard `SyscallConn() (syscall.RawConn, error)` interface. Linux 7.0 arm64 CI and local Linux 7.3 amd64 execution verify polling, deadlines, and close interruption at both supported Go versions; the P02 primary-target runtime gap is closed. Deadline setters normalize Go's private closing error; returned descriptors preserve `os.File` raw error behavior.

Caller flow: create `eventfd.Counter`, optionally set a deadline, call `Add`, then consume `ReadValue`, and close. A private `os.File` supplies lifetime/polling; do not expose seek or file-copy methods that imply an arbitrary byte stream.

| Sketch | Contract |
| --- | --- |
| `New(initial uint32, mode Mode) (*Counter, error)` | Zero mode is ordinary counter draining; the other supported mode is semaphore decrementing |
| `(*Counter).Read(p []byte) (int, error)` | Consume one native-endian eight-byte record |
| `(*Counter).Write(p []byte) (int, error)` | Add one encoded value; require exactly eight bytes |
| `(*Counter).ReadValue() (uint64, error)` | Destructive read: drain the counter, or return one in semaphore mode |
| `(*Counter).Add(value uint64) error` | Encode one addition; the all-ones value is invalid, and zero is allowed |
| `Close`, `SyscallConn`, `SetDeadline`, `SetReadDeadline`, `SetWriteDeadline` | Standard signatures and the polling/close requirements above |

The initial value reflects the syscall's unsigned 32-bit input; subsequent values are 64-bit. Reads when the counter is zero and additions that would exceed the kernel's allowed counter range wait subject to deadlines. Do not implement typed reads by buffering partial records. Initial omissions: a raw-mode constructor, contexts, userspace event broadcasting, and wait-many orchestration. Prerequisites are permitted `eventfd2` and runtime polling, with no extra capabilities normally needed. P02 must verify both modes, invalid sizes/values without mutation, accumulation/draining, deadline expiry/reset, close interrupting blocked I/O, and the actual nonblocking raw-access behavior. Reference: [eventfd](https://man7.org/linux/man-pages/man2/eventfd.2.html).

### P03: timerfd

Caller flow: `timerfd.New` for a named clock → `SetAfter` or `Now`/`SetAt` → `ReadExpirations` → `Disarm`/`Close`. `Clock` initially selects monotonic, realtime, or boottime; zero selects monotonic. The owner contains a private pollable file and implements `io.Reader`/`io.Closer`, without `io.Writer` or seek methods.

| Sketch | Contract |
| --- | --- |
| `New(clock Clock) (*Timer, error)` | New disarmed timer using the selected clock |
| `Now(clock Clock) (Instant, error)` | Query the same Linux clock used by absolute arming |
| `Instant{Clock, Seconds, Nanoseconds}` | Tagged absolute clock reading; nanoseconds are normalized and the tag must match the timer |
| `(*Timer).SetAfter(initial, interval time.Duration) (State, error)` | Relative first expiration and repeat interval; zero initial disarms; negative values fail |
| `(*Timer).SetAt(deadline Instant, interval time.Duration, flags SetFlags) (State, error)` | Absolute arming; initial flags are zero or cancel-on-clock-set, the latter restricted to realtime absolute timers |
| `(*Timer).State() (State, error)` and `Disarm() (State, error)` | Remaining duration and repeat interval; setters return the previous kernel state from the same operation |
| `(*Timer).Read(p []byte) (int, error)` and `ReadExpirations() (uint64, error)` | Read an eight-byte native-endian expiration count |
| `Close`, `SyscallConn`, `SetReadDeadline` | Standard signatures; read deadlines bound reads independently of timer arming |

An `Instant` is measured in its named Linux clock, not a Go `time.Time` with an inaccessible monotonic origin. Check conversion overflow and invalid timestamps; an all-zero absolute value follows the kernel's disarm semantics. P03 must define how an unrepresentable remaining duration is reported without wrapping it, including whether a setter has already taken effect before conversion of the old state fails. State inspection is not a transaction with concurrent arming/reads. Byte reads preserve cancellation errors and the exceptional zero-byte realtime clock-change result; typed reads must report that no count was obtained, provisionally with `io.ErrNoProgress`, rather than inventing an expiration or permanent EOF.

Initial omissions: alarm clocks requiring wake capabilities, suspend/wakeup guarantees, clock adjustment, and checkpoint/restore tick injection. All three initial clocks and the proposed cancel flag predate the baseline, but remain subject to allowed syscalls. P03 needs tolerant one-shot/periodic assertions, relative and absolute arming for each clock, counts, disarming, invalid arguments, deadlines, and close behavior. Clock-jump cancellation and exceptional reads require a dedicated clock-controlled VM; keep that evidence pending until such a runner exists. Reference: [timerfd](https://man7.org/linux/man-pages/man2/timerfd_create.2.html).

### P04: mmap

Caller flow: borrow a standard file for `mmap.Map`, or call `Anonymous` → access `Bytes` within its lifetime → optional `Protect`/`Advise` → `Close`. A `Region` owns one mapping, not its source file, and implements `io.Closer`; it does not pretend to be a file or stream.

| Sketch | Contract |
| --- | --- |
| `Map(file *os.File, offset int64, length int, options Options) (*Region, error)` | Borrow the file during mapping; positive explicit length and nonnegative page-aligned offset |
| `Anonymous(length int, options Options) (*Region, error)` | No source descriptor; explicit shared/private mode |
| `Options{Mode, Protection}` | Exclusive shared/private enum, zero meaning private; protection is a bitmask with zero meaning no access |
| `(*Region).Bytes() []byte` | View of exactly the requested length, with capacity limited to that length; nil for zero/closed region |
| `(*Region).Protect(protection Protection) error` | Change protection for the whole owned mapping |
| `(*Region).Advise(advice Advice) error` | Whole-region normal/random/sequential/will-need/don't-need advice; document mode-specific effects |
| `(*Region).Close() error` | Unmap the original complete mapping; a successful unmap invalidates all escaped views |

File closure after a successful map does not invalidate it; file truncation can make access fault. Validate length, offset addition, protection bits, and enum values before mapping. Byte indexing/slicing has normal Go bounds behavior; page-rounded allocation does not grant access past the requested view. Callers must coordinate all view use against `Protect`, advice with content effects, and `Close`; no method can protect an escaped slice after unmap. A failed unmap must not falsely mark the region closed. Close does not promise disk durability or an implicit sync.

Initial omissions: fixed-address mapping, remap, locking, huge pages, executable-memory workflows, and persistence helpers. P04 must verify sharing/private copies, anonymous access, invalid arguments, view bounds, cleanup, and source-file independence; protection/fault cases run in bounded subprocesses. References: [mmap/munmap](https://man7.org/linux/man-pages/man2/mmap.2.html), [mprotect](https://man7.org/linux/man-pages/man2/mprotect.2.html), and [madvise](https://man7.org/linux/man-pages/man2/madvise.2.html).

### P05: pidfd

Caller flow: open a process identity or adopt a descriptor → signal and/or observe readiness → optionally reap a child through its sole wait owner → close the descriptor. Prefer standard `os.Process` for ordinary subprocess management. This package adds a descriptor owner/readiness boundary, not an alternative process launcher.

| Sketch | Contract |
| --- | --- |
| `Open(pid int) (*Process, error)` | Acquire a process-group-leader pidfd; document the numeric-PID race before acquisition |
| `Adopt(file *os.File) (*Process, error)` | Adopt an existing pidfd only on success; validation/poll setup must preserve the caller's ownership on failure |
| `(*Process).Signal(signal syscall.Signal) error` | Signal through the handle; preserve Linux permission and vanished-process errors |
| `(*Process).WaitReady() error` | Observe exit readiness without consuming child status or promising reaping rights |
| `(*Process).Wait() (Exit, error)` | Reap an eligible child with `waitid(P_PIDFD)`; `Exit` distinguishes normal code, terminating signal, and core-dump status |
| `Close`, `SyscallConn` | Close releases the local descriptor, without killing or reaping the process |

For a child already managed by `exec.Cmd`, leave reaping with `Cmd.Wait`; use only readiness observation here. To obtain a separate descriptor from `os.Process.WithHandle`, duplicate close-on-exec inside its protected callback, then wrap/adopt that duplicate; never adopt the borrowed handle. `SysProcAttr.PidFD` is another acquisition path to review for its ownership contract. Once acquired, a pidfd avoids subsequent PID-reuse targeting, but opening a numeric PID does not prove that it is the intended earlier process.

P05 must select and test a readiness wait using public polling/syscall interfaces independently of P07. Do not block in a raw `waitid` while claiming close cancellation; combine readiness and nonblocking child-status checks if offering that guarantee. The intended owned wait is interrupted by close; verify concurrent waiters and external-reaper races before documenting exact results. No context or deadline API is initially promised. A non-child can be observed but cannot generally be reaped; preserve `ECHILD`.

Initial omissions: thread pidfds, process-group signaling modes, descriptor stealing with `pidfd_getfd`, and process creation. Readiness, same-user child signals, lifecycle, supported waits, and expected permission errors need subprocess tests. Cross-user success requires a suitable runner. References: [pidfd_open](https://man7.org/linux/man-pages/man2/pidfd_open.2.html), [pidfd_send_signal](https://man7.org/linux/man-pages/man2/pidfd_send_signal.2.html), [waitid](https://man7.org/linux/man-pages/man2/waitid.2.html), and the [7.0 pidfd header](https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/pidfd.h).

### P06: mount

Caller flow inside an isolated mount environment: `mount.Open("tmpfs")` → explicit configuration → `Create` → `Mount` to get a detached handle → set attributes/attach → close both local descriptors. Cloning an existing tree is a separate acquisition path. The caller owns namespace setup and attachment policy; the library does not enter a namespace implicitly.

| Sketch | Contract |
| --- | --- |
| `Open(filesystem string) (*Config, error)` | Own a close-on-exec `fsopen` configuration context |
| `(*Config).SetFlag(key string) error` and `SetString(key, value string) error` | Initial supported fsconfig parameter kinds; preserve distinctions instead of encoding a comma-separated option string |
| `(*Config).Create() error` | Explicit `FSCONFIG_CMD_CREATE`; the kernel may reuse a filesystem instance |
| `(*Config).Mount(attributes Attributes) (*Handle, error)` | Separate `fsmount` acquisition; the config remains caller-owned |
| `(*Config).Diagnostics() ([]Diagnostic, error)` | Drain available severity/message records; preserve partial records and retrieval errors |
| `Clone(dir *os.File, path string, flags CloneFlags) (*Handle, error)` | `open_tree` clone, with recursive cloning explicit; borrow the directory during the call |
| `(*Handle).SetAttributes(change AttributeChange) error` | `mount_setattr` on the handle; explicit set/clear masks and recursive scope |
| `(*Handle).Attach(dir *os.File, path string) error` | `move_mount` attachment; borrow target directory, retain handle ownership |
| `Close`, `SyscallConn` on both owners | Descriptor lifetime only; no deadline promise or automatic unmount of an attached mount |

Initial mount attributes are read-only, nosuid, nodev, and noexec. Reject overlapping set/clear bits. Initial omissions: binary/path/fd configuration values, reconfiguration, ID mappings, propagation policy, atime field groups, namespace creation flags, exclusive filesystem-instance creation, and unmount orchestration. These omissions bound the initial API; they do not imply that the corresponding facilities are newer than 7.0. `FSCONFIG_CMD_CREATE` is not an isolation guarantee for a unique superblock. Check the exact selections against the [7.0 mount UAPI](https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/mount.h).

Keep the configuration descriptor open after a configuration/create error so callers can obtain filesystem diagnostics before closing. Serialize configuration and diagnostic reads so errors remain attributable. Diagnostics are a bounded kernel queue, not a lossless log. In particular, an undersized read can discard a message: P06 must inspect the producer's message-size bound, allocate accordingly, and surface retrieval failure without replacing the primary errno. A failed attachment leaves the handle owned by the caller; successful attachment is not undone by closing it. References: [fsopen diagnostics/lifecycle](https://man7.org/linux/man-pages/man2/fsopen.2.html), [fsconfig](https://man7.org/linux/man-pages/man2/fsconfig.2.html), [fsmount](https://man7.org/linux/man-pages/man2/fsmount.2.html), [open_tree](https://man7.org/linux/man-pages/man2/open_tree.2.html), [move_mount](https://man7.org/linux/man-pages/man2/move_mount.2.html), and [mount_setattr](https://man7.org/linux/man-pages/man2/mount_setattr.2.html).

Integration requires permitted user/mount namespace isolation with applicable `CAP_SYS_ADMIN`, a supported filesystem, and private mount propagation, or a dedicated isolated runner. Use a subprocess fixture before P15 exists. Verify configuration/creation, clone or attachment, attributes, failure diagnostics, and cleanup there; expected permission-denial coverage does not establish successful mounting.

## External constraints and evidence still required

| Constraint | Evidence as of F01 | Owning work |
| --- | --- | --- |
| Supported Go/kernel/architecture execution matrix | Local Go 1.27.4, Linux 7.3.4 amd64 observed; Go 1.27.0 and CircleCI runtime not executed here | F02 establishes runners and exact versions; R01 reconciles complete runtime coverage |
| CircleCI isolation, capabilities, and kernel floor | Docker image/resource class are configuration, not proof of kernel or syscall availability | F02 probes actual execution; absent required runners stay outstanding |
| Local syscall permission/configuration | UID 1000, empty effective/bounding capabilities, no-new-privileges and seccomp filter observed; no facility probes run | F02 prerequisite discovery and each P-item's operation-specific probes |
| Polling/deadline/close behavior | Public Go integration is documented and local source inspected; no pilot descriptor runtime tests yet | P02/P03/P05 must prove their exact promised method behavior and constructor cleanup |
| Baseline ABI versus current documentation | Selected versioned 7.0 sources inspected for memfd, eventfd, pidfd, and mount; no complete six-package ABI audit | Each P-item checks its exact flags/operations and pinned syscall version; R01 audits all packages |
| Clock changes, wake alarms, huge pages | Clock-changing runner unavailable/uninspected; other special modes initially omitted | P03 records missing clock-jump evidence; later extensions require their own prerequisites |
| Reaping, adoption, and diagnostics details | Ownership choices recorded; real external-reaper races, pidfd validation, and diagnostic-buffer sizing not yet implemented | P05/P06 resolve and verify before accepting those package APIs |
| Dependency reproducibility | `go.mod` currently has no external requirements; references do not establish a usable dependency version | First syscall implementation pins `x/sys`; F02 owns reproducible setup/verification commands |

F01 completion means the decisions, alternatives, provisional caller surfaces, and unresolved constraints are recorded and reviewed. It does not complete any P-series common-checklist requirement or remove any future runtime verification obligation.
