# Support and compatibility

The library exposes Linux mechanisms through typed Go operations. The [package index](../README.md#-packages) links each facility's usage guide; those guides and GoDoc own detailed prerequisites, ownership, concurrency, errors, and omissions. [Verification](verification.md) records observed environments, and [verification limits](verification-limits.md) records missing execution evidence.

## Supported platforms

| Dimension | Support boundary |
| --- | --- |
| Operating system | Linux 7.0 or newer; repository Go files use the `linux` build constraint |
| Native architectures | amd64 and arm64 |
| Go | Minimum Go 1.27.0, declared in [go.mod](../go.mod); CI uses the configured image's single toolchain |
| Implementation | Pure Go with maintained `golang.org/x/sys/unix` bindings and public runtime integration; no cgo or private runtime hooks |
| Runtime verification | Native architecture lanes and facility-specific isolation; exact Linux 7.0 is required by the baseline lanes specified in [verification](verification.md) |

Newer kernels do not prove the Linux 7.0 baseline. Other architectures and non-Linux operating systems are outside the supported set. Kernel configuration, filesystem support, namespace ownership, seccomp/LSM policy, resource limits, and permissions remain separate requirements. No constructor promises that every operation will be permitted, and errors never silently select a weaker mechanism.

## Feature requirements

All rows assume the supported platform/toolchain and permission for the relevant syscalls. These are selection guidance; follow the linked guide for the operation's exact contract.

| Facility | Additional requirements |
| --- | --- |
| [memfd](memfd.md) | Memory-backed file creation/sealing permitted; sealing state can irreversibly forbid later operations |
| [mmap](mmap.md) | Readable source for file mappings, write permission for shared writable mappings, and coordinated mapping/truncation lifetime |
| [eventfd](eventfd.md), [timerfd](timerfd.md), [epoll](epoll.md) | Descriptor creation, polling, and duplication permitted; ordinary operations need no extra capabilities; timer clocks remain explicit |
| [pidfd](pidfd.md) | Process-handle operations permitted; signaling follows target permissions, and reaping requires an eligible child and one designated reaper |
| [file](file.md) | Path access and filesystem support for the selected operation. Allocation requires a writable handle, and readahead requires a readable handle. |
| [xattr](xattr.md) | Filesystem and attribute-namespace support plus access to the selected object; system/security attributes can require privilege |
| [inotify](inotify.md) | Access to watched objects and available per-user watch/instance capacity; consumers must reconcile reported queue overflow |
| [fanotify](fanotify.md) | CONFIG_FANOTIFY and initial-user-namespace CAP_SYS_ADMIN for the supported classes; permission events also require CONFIG_FANOTIFY_ACCESS_PERMISSIONS |
| [mount](mount.md) | Modern mount API, filesystem support, applicable CAP_SYS_ADMIN, and an explicitly isolated/private mount namespace before mutations |
| [socket](socket.md) | Unix/Netlink message sockets permitted; protocol, multicast, and file-passing permissions apply; use standard `net` for TCP/UDP/streams |
| [netlink](netlink.md), [netlink/route](netlink-route.md) | Selected kernel protocol/family/groups available; routing mutations normally require CAP_NET_ADMIN in the applicable network namespace |
| [namespace](namespace.md) | Namespace kernel support, applicable authority/quotas, and permitted transitions; new user namespaces/mappings use standard Go child startup |
| [cgroup](cgroup.md) | A v2 hierarchy; mutations require an explicit writable delegation with the requested controllers enabled and correct topology |
| [capability](capability.md) | Thread credential operations permitted; bounding-set drops require CAP_SETPCAP, and existing permitted/inheritable sets constrain transitions |
| [seccomp](seccomp.md) | Kernel filter support and either no-new-privileges or applicable CAP_SYS_ADMIN; policies must accommodate the workload and Go runtime |
| [landlock](landlock.md) | Landlock enabled and the ABI required by selected rights/synchronization; enforcement uses no-new-privileges or applicable authority |
| [procfs](procfs.md), [sysfs](sysfs.md) | Explicit trusted readable roots and access to requested entries; visibility need not be container-local or an atomic snapshot |

Use a dedicated locked goroutine and bounded subprocess where the package guide requires it for thread-local namespaces, credentials, or irreversible restrictions. Root or capability observations alone do not prove an operation works. The library never changes the shared host's clocks, creates a cgroup delegation, or supplies a complete sandbox automatically.

## Coverage index

This index describes the 21 shipped facilities against the selected storage, worker, and container scenarios. **Implemented** means the listed contract is callable; it does not mean every Linux interface in that family is wrapped. **Partial** identifies missing mechanisms needed to extend those scenarios. **Absent** means this module has no public operation for the capability. **Externally supplied** and **excluded** boundaries appear below. Detailed operations, accepted flags, returned fields, and omissions belong to the linked usage guides.

The comparison uses the [versioned Linux 7.0 UAPI](https://github.com/torvalds/linux/tree/v7.0/include/uapi/linux), its [memory definitions](https://github.com/torvalds/linux/blob/v7.0/include/uapi/asm-generic/mman-common.h), and the [v2 cgroup contract](https://docs.kernel.org/7.0/admin-guide/cgroup-v2.html), alongside the public APIs. It is a scope comparison; operation evidence and configuration-specific limits remain in the verification guides.

| Facility | Coverage | Current Linux-specific contribution and boundary |
| --- | --- | --- |
| [memfd](memfd.md) | Implemented | Anonymous files, all six named baseline seals, executable opt-in, and independently owned mappings; ordinary I/O uses Go file behavior. Huge-page creation and opaque-file passing are omitted. |
| [eventfd](eventfd.md) | Implemented | Accumulating/semaphore counters, record I/O, deadlines, and typed epoll registration. Go channels and mutexes remain the ordinary in-process choice. |
| [timerfd](timerfd.md) | Implemented | Relative/absolute timers on three explicit clocks, periodic counts, realtime cancellation, deadlines, and epoll registration. Wakeup/alarm clocks and tick injection are omitted. |
| [mmap](mmap.md) | Partial | Owned anonymous/file mappings, shared/private views, protection, and five advice choices. Synchronization, locking/residency, resizing, and further advice are absent. |
| [pidfd](pidfd.md) | Partial | Stable acquired process identity, signals, exit observation, and designated child reaping. Target file duplication, wait-many, orphan-adoption controls, and wait-any reaping are absent. |
| [mount](mount.md) | Partial | Flag/string configuration, detached creation/cloning, four restrictions, attachment, diagnostics, and unmount. Structured discovery, other parameter forms, reconfiguration, propagation/ID mapping, and root activation are absent. |
| [epoll](epoll.md) | Implemented | Owned registrations, identity-preserving readiness, level/edge/one-shot modes, bounded waits, and close interruption. Ordinary Go I/O retains runtime polling; signal-mask waits and power/exclusive controls are omitted. |
| [file](file.md) | Partial | Restricted openat2 lookup, selected statx records, atomic rename modes, range allocation, hole punching, zero-range, collapse, insert, reflink unsharing, write-zeroes, access advice, readahead, file data and filesystem synchronization, and whole-file advisory locks on standard files. Byte-range locks, full metadata, unnamed temporary/direct-I/O opening, and whiteouts are absent. |
| [xattr](xattr.md) | Implemented | Handle/relative-path byte attributes, atomic create/replace, bounded reads, and visible-name enumeration. ACL, capability, and security-label interpretation remain external. |
| [inotify](inotify.md) | Implemented | Native watch conditions/options, complete-record decoding, rename cookies, explicit overflow, and synchronous iteration. Recursive watch management and reconciliation policy remain external. |
| [fanotify](fanotify.md) | Implemented | Descriptor notification/content groups, scoped marks, event-file inspection, allow/deny decisions, and bounded reading. FID/name, PIDFD, pre-content, and other reporting modes are omitted. |
| [socket](socket.md) | Implemented | Unix datagram/seqpacket messages, file transfer, credentials, deadlines, and Netlink transport. Standard `net` owns ordinary stream/Internet networking. |
| [netlink](netlink.md) | Implemented | Framing, attributes, ACK diagnostics, matched requests/dumps, multicast/loss, and Generic Netlink family discovery. Family-specific policy and automatic resynchronization remain external. |
| [netlink/route](netlink-route.md) | Partial | Link/address/route observations, dummy links, selected mutations, and notifications. Veth/bridges, namespace moves, neighbors/rules, lookups, and multipath/nexthop mutation are absent. |
| [namespace](namespace.md) | Implemented | Eight handle types, identity/hierarchy, adoption, and explicit supported thread transitions. New-user-namespace startup/mappings use Go child startup; hostname and time-offset mutation are omitted. |
| [cgroup](cgroup.md) | Partial | Explicit delegated v2 handles, process migration, CPU/memory/PIDs controls, and event snapshots/watchers. Freezer/kill, other controllers, deeper records, and atomic command placement are absent. |
| [capability](capability.md) | Implemented | All 41 baseline capability masks, thread sets, bounding drops, and ambient controls. Go child credentials/ambient setup complement this; securebits and file-capability policy are omitted. |
| [seccomp](seccomp.md) | Partial | Native classic-BPF construction, ABI guards, selected actions, action discovery, and thread/all-thread loading. User-notification listeners, responses, and file injection are absent. |
| [landlock](landlock.md) | Implemented | Baseline ABI 8 filesystem/TCP/scoped IPC policy, logging, and kernel all-thread restriction; explicit optional newer rights. Errata discovery and newer rule interfaces are omitted. |
| [procfs](procfs.md) | Implemented | Selected process, descriptor, mapping, mount, system CPU, and memory observations through a private Prometheus backend. Path views do not pin process identity or provide exhaustive procfs capture. |
| [sysfs](sysfs.md) | Implemented | Block/network discovery, selected typed identity fields, and bounded explicit attribute reads through a private Prometheus backend. No mutation or device-specific control. |

The expansion families below are **absent**, including their proposed package homes. They are not additional shipped packages or API promises. The execution queue owns their implementation sequence; new guides and dependencies arrive with callable capabilities.

| Capability and proposed home | Application contract | Implementation route |
| --- | --- | --- |
| Orphan supervision: `process` | Subreaper state and adopted-child reaping for worker trees/container init | Small project-owned operations using existing Linux bindings; retain standard Go startup and signal handling. |
| Thread placement/policy: `sched` | CPU sets, scheduling attributes, and I/O priority | Existing bindings internally; keep Go goroutine scheduling distinct from Linux thread policy. |
| Resource limits: `rlimit` | Typed soft/hard limits and atomic previous-limit results | Existing prlimit bindings; no implicit runtime-limit changes. |
| Asynchronous I/O: `uring` | Requests/completions, registration, cancellation, and buffer lifetime | Internal queue/ABI implementation with x/sys where available and liburing as reference; no C runtime or giouring dependency. |
| Workload measurements: `perf` | Counters/groups and bounded sample records | Internal perf bindings and ring decoding; application owns profiling products. |
| Workload eBPF: `bpf` | Compiled-program loading, maps, cgroup traffic accounting, tracepoints, and event readers | Cilium eBPF is the selected future internal backend; it is not yet a module dependency. |
| Fault handling: `userfaultfd` | Negotiated registration, missing/write-protect/minor fault resolution, and lifecycle events | Internal typed fault/mapping owners over Linux bindings; full checkpoint orchestration remains external. |
| Workload credentials: `keyring` | Private rings/keys, permissions, requests, and expiry | Internal typed key-management operations; no application secret-management policy. |
| Priority IPC: `mqueue` | POSIX queue identity, message priorities, timing, and readiness | Internal Linux queue operations; no stream abstraction that loses records/priorities. |
| Persistent IPC: `sysvipc` | System V queues, atomic semaphore sets, and shared-memory attachments | Internal typed operations with explicit removal and mapping lifetimes. |

Existing `file`, `mmap`, `mount`, `pidfd`, `seccomp`, and the new scheduling/IPC owners should first reuse `x/sys` operations where they fit. Complex routing extensions will evaluate [vishvananda/netlink](https://github.com/vishvananda/netlink); cgroup extensions will evaluate [containerd/cgroups](https://github.com/containerd/cgroups). Those are evaluation candidates, not current dependencies or decided backend replacements. The [API conventions](agents/accepted-design/api.md) require any adapter to retain project-owned types, ownership, errors, and namespace behavior. Cilium is the selected eBPF backend because the expanded loading/relocation/BTF work is substantial; its types and descriptors must remain private when introduced.

### Compose storage and worker lifecycles

Use `os.File`/`os.Root` for ordinary storage, `ReadAt`/`WriteAt` for positioned I/O, `Truncate` for logical sizing, and `File.Sync` for its standard synchronization contract. Add `file` for restricted lookup, metadata, atomic rename, log preallocation, or sparse-range changes. Use `xattr` for byte metadata and `mmap` for mapped indexes or shared buffers. Shared mapped writes do not supply msync or crash consistency. Explicit data/filesystem synchronization, locks, and mapping controls remain the partial coverage identified above. Asynchronous requests are outside the current API. Database layout, transactions, logging, and recovery policy belong to the application.

Use `os/exec` to start workers, Go pipes/message sockets for coordination, and `pidfd.OpenProcess` for independent stable exit observation/signaling. Keep `Cmd.Wait` as the reaper for a command; use `Process.Wait` from pidfd only with a separately designated eligible-child reaping owner. Counters/timers can join epoll through typed registration; procfs/sysfs supply non-atomic observations. A complete worker-tree supervisor still needs orphan adoption/reaping; CPU deadline scheduling and typed resource limits need the absent `sched`/`rlimit` facilities. Closing handles does not terminate workers, flush mappings, undo restrictions, or remove persistent kernel objects.

### Compose a container lifecycle

| Stage | Existing contribution | Missing mechanism or external owner |
| --- | --- | --- |
| Prepare rootfs | Standard Go file operations, `file`, `xattr`, and detached `mount` trees | Image fetching/unpacking, filesystem formatting, and device policy are externally supplied. Structured mount discovery and full configuration forms are absent. |
| Establish isolation | Standard `exec.Cmd.SysProcAttr` clone/unshare flags and UID/GID mappings; namespace handles/explicit entry | Use Go's pre-runtime child setup for new user namespaces. UTS identity/time-offset configuration is outside the current API; selecting a namespace alone does not configure it. |
| Activate root | Mount attachment and explicit unmount | Propagation controls, ID-mapped mounts, and pivot_root are absent. A standard Go child chroot option does not supply the promised mount-root switch/old-root lifecycle. |
| Place resources | Explicit cgroup delegation, limits, and post-start `AddProcess` | Atomic start-in-group integration is absent. Go has clone-into-cgroup support, but this library must borrow its owned group internally rather than require caller descriptor access. |
| Connect networking | Standard `net` I/O and typed link/address/route operations in a captured network namespace | Private veth/bridge construction and richer routing are absent. IP allocation, DNS, forwarding policy, and NAT/firewalls are external. |
| Set credentials/restrictions and exec | Standard child credentials/ambient setup, explicit capability operations, seccomp, and Landlock | Caller chooses order, executable, allowed resources, and policy. Go offers no general pre-exec Go callback; a reexecuted setup child must apply library operations before starting the workload. |
| Supervise and reap | `pidfd` signals/exit observation and eligible identified-child waits; ordinary `Cmd.Wait` and `os/signal` | PID 1 duties, orphan adoption, and wait-any collection are not implemented. Signal forwarding/restart policy remains with the application even after those primitives arrive. |
| Observe and tear down | procfs/sysfs snapshots, cgroup events, explicit handle close, unmount, and empty-group removal | Workload BPF observation and recursive cgroup kill are absent. Application orders workload termination/reaping, attachment release, unmount, and object removal; close is not rollback. |

NAT/firewall configuration should compose with [google/nftables](https://github.com/google/nftables) or the application's existing networking stack. Full checkpoint/restore should compose with CRIU and [go-criu](https://github.com/checkpoint-restore/go-criu). OCI runtime lifecycle, CNI plugins/IPAM, image distribution, cluster scheduling/reconciliation, security policy compilation, and general init policy remain **externally supplied**, without becoming dependencies of this module. Their integration needs separate actual workload evidence; a package inventory does not establish a working container runtime.

## Compatibility

Version 1 uses [Go module compatibility and semantic versioning](https://go.dev/ref/mod#module-compat). Public package/import paths, exported names and types, documented zero values, flag/mode meanings, ownership, error matching, and blocking/close semantics are compatibility surfaces. New optional facilities may arrive in a minor version. Incompatible changes require a new major module path, subject to the ordinary Go compatibility exceptions for correcting security defects and clearly incorrect behavior.

Linux errno identity is preserved through `errors.Is`/`errors.As`; error text is diagnostic rather than a machine-readable format. Unknown kernel output values are preserved where documented. Successful ownership transfer and `Adopt` failure consumption remain explicit; borrowing leaves the original resource caller-owned. Closing a local handle is distinct from undoing a kernel effect. [API conventions](agents/accepted-design/api.md) define those shared design decisions.

The Linux floor and supported native architectures are part of the v1 support contract. The Go minimum follows maintained Go releases and may advance in a documented minor release; consumers should check `go.mod` before upgrading. Verification evidence applies to its recorded revision and environment, not every kernel configuration or future toolchain.

Internal packages and implementation dependencies are outside the stable library API; callers must not depend on upstream procfs types leaking through project packages.

## Deliberate boundaries

The current release contains the 21 facilities in the README. The absent expansion families in the coverage index are future work. Device-specific APIs, raw futex interfaces, direct signal management, a general profiler, a checkpoint engine, and a secret-management product are **excluded** from this sequence. The selected eBPF scope excludes XDP/TCX, LSM and device policy, scheduler programs, compilation tooling, and persistent pin management. It does not rebuild `os`, `net`, `time`, `sync`, or `os/exec`. Facility-specific omissions remain in their usage guides, and [verification limits](verification-limits.md) records source-reviewed paths without direct execution evidence.

Descriptors and syscall plumbing stay internal to facility owners; use concrete typed operations and standard Go resource interfaces where provided. Namespace, capability, seccomp, and Landlock mechanisms do not together imply a complete container runtime or security policy. Kernel and application guarantees must be evaluated for the actual operation and workload.

## Dependencies and licensing

Repository-owned code is covered by the [MIT license](../LICENSE). After `make dep`, the current dependency license files are available at these checkout paths; `vendor/` is generated and is not committed:

| Module | Version | License files |
| --- | --- | --- |
| `github.com/prometheus/procfs` | v0.23.0 | Apache-2.0: `vendor/github.com/prometheus/procfs/LICENSE` and `NOTICE` |
| `golang.org/x/sys` | v0.50.0 | BSD-3-Clause: `vendor/golang.org/x/sys/LICENSE` |
| `golang.org/x/sync` | v0.24.0 | BSD-3-Clause: `vendor/golang.org/x/sync/LICENSE` |
| `github.com/stretchr/testify` | v1.12.1 | MIT: `vendor/github.com/stretchr/testify/LICENSE`; bundled BSD/ISC terms: `internal/difflib/LICENSE` and `internal/spew/LICENSE` within that directory |
| `go.yaml.in/yaml/v3` | v3.0.5 | File-specific MIT/Apache-2.0: `vendor/go.yaml.in/yaml/v3/LICENSE` and `NOTICE` |

`go.mod`, `go.sum`, and `vendor/modules.txt` own the resolved versions; preserve the applicable dependency licenses/notices when redistributing their source or binaries. The project MIT license does not replace dependency terms. `testify` and YAML serve the test harness; procfs/sysfs use the private Prometheus dependency, with `x/sys` supplying native bindings.
