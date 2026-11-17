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
| [file](file.md) | Path access and support for selected resolution/metadata/rename operations on the filesystem |
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

## Compatibility

Version 1 uses [Go module compatibility and semantic versioning](https://go.dev/ref/mod#module-compat). Public package/import paths, exported names and types, documented zero values, flag/mode meanings, ownership, error matching, and blocking/close semantics are compatibility surfaces. New optional facilities may arrive in a minor version. Incompatible changes require a new major module path, subject to the ordinary Go compatibility exceptions for correcting security defects and clearly incorrect behavior.

Linux errno identity is preserved through `errors.Is`/`errors.As`; error text is diagnostic rather than a machine-readable format. Unknown kernel output values are preserved where documented. Successful ownership transfer and `Adopt` failure consumption remain explicit; borrowing leaves the original resource caller-owned. Closing a local handle is distinct from undoing a kernel effect. [API conventions](design/api.md) define those shared design decisions.

The Linux floor and supported native architectures are part of the v1 support contract. The Go minimum follows maintained Go releases and may advance in a documented minor release; consumers should check `go.mod` before upgrading. Verification evidence applies to its recorded revision and environment, not every kernel configuration or future toolchain.

Internal packages and implementation dependencies are outside the stable library API; callers must not depend on upstream procfs types leaking through project packages.

## Deliberate boundaries

The first release contains the 21 facilities in the README. It does not include io_uring, eBPF, perf events, userfaultfd, device-specific APIs, System V IPC, POSIX message queues, raw futex interfaces, or direct signal management. It does not rebuild `os`, `net`, `time`, `sync`, or `os/exec`. Facility-specific omissions remain in their usage guides, and [verification limits](verification-limits.md) records source-reviewed paths without direct execution evidence.

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
