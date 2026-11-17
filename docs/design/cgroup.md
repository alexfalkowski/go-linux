# Cgroup design

## Explicit hierarchy ownership

Cgroup directories are shared kernel objects, often managed by a service manager. An owned directory reference provides stable identity without claiming ownership of the hierarchy. Closing a local reference therefore never removes a cgroup or changes membership, controllers, or limits. Explicit child removal prevents an error path or deferred close from unexpectedly destroying a workload. Creation owns rollback when acquisition fails; concurrent external modifications can prevent removal and their cleanup error remains visible.

Directory-relative operations keep the acquired parent identity across rename, use atomic close-on-exec descriptors, and reject child symlinks and path traversal. They avoid exposing control-file streams or raw descriptors as a substitute for typed operations. The package validates v2 filesystem identity rather than treating ordinary temporary directories as real cgroups. Public native text codecs permit captured-format fixtures without introducing a fake live backend. This does not claim confinement against a privileged hierarchy/mount owner.

## Bounded mechanisms

The first controller surface uses individual operations instead of a combined resource configuration object. Separate files do not provide a transaction: a bulk API would need a partial-application contract or misleading rollback. The kernel's actual atomic subtree-control command remains one operation. Unknown inspection values survive, while mutation accepts the selected controller vocabulary and rejects ambiguous requests before writing.

CPU intervals use durations with exact microsecond conversion; memory and task limits retain the decimal/max distinction, with zero a real limit. CPU quota is scoped to fair-class scheduling, avoiding a process-wide reservation claim. Memory boundaries use Linux 7.0's documented `O_NONBLOCK` behavior so reducing a workload's limit does not require synchronous reclaim/OOM in the management caller. This deliberately allows delayed enforcement until new charges. No controller availability, ancestor setting, migration permission, or systemd ownership is inferred from an open handle.

Membership is a whole-process operation through `cgroup.procs`, distinct from threaded hierarchy setup. A numeric PID is an explicit Linux identity with exit/reuse races. A standard process-startup handshake can coordinate migration but cannot provide atomic startup inside the destination. Reimplementing os/exec or exposing its cgroup descriptor field would put syscall plumbing back on callers. Atomic startup integration, additional controllers, pressure monitoring, and freezer/kill operations can be designed when required rather than included speculatively.

## Event ownership and notification

An event watcher owns a separate event file and inotify instance. It survives closure of the source group and integrates with the established Go poller/deadline behavior without a permanent goroutine. Registration uses `/proc/self/fd` while the file is pinned, so an old group path cannot select a replacement inode. This adds an explicit procfs prerequisite instead of a racy path-based fallback. Snapshots preserve the kernel's coalescing semantics rather than claiming every transition was delivered; overflow and removal remain errors requiring reconciliation. Memory/PID event counters are independent snapshots and are not multiplexed into the membership watcher.

Kernfs can remove a pinned control file without notifying its inode. Directory deletion watches can also be delayed by open handles, and parent watches cannot observe the hidden parent of a namespace or bind-mount root. A waiting read therefore uses private 100-millisecond poller deadlines to check the pinned file's active state with a seek. Inotify still supplies state-change notifications; an internal timeout yields no snapshot or caller deadline error. Caller deadline changes wake the read and are synchronized with its next bounded wait. This costs periodic wakeups only while a caller is reading and avoids a background goroutine, additional descriptors, and parent-path prerequisites. Linux's removal error is normalized to `os.ErrNotExist`.

## Baseline sources

The [Linux 7.0 cgroup v2 specification](https://github.com/torvalds/linux/blob/v7.0/Documentation/admin-guide/cgroup-v2.rst) defines controller distribution, delegation containment, resource formats, memory nonblocking writes, and file notification semantics. The [Linux 7.0 membership implementation](https://github.com/torvalds/linux/blob/v7.0/kernel/cgroup/cgroup.c) emits namespace-relative PIDs, including zero when the task is invisible. Runtime runner evidence belongs to the [verification guide](../verification.md), and missing execution evidence is recorded in [verification limits](../verification-limits.md).
