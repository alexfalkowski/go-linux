# Linux APIs for Go: implementation plan

Status: repository scaffold established; Linux API implementations have not started.

This document is the shared execution plan for Codex, Claude, and human contributors. It is self-contained: an agent should be able to implement the next work item without access to the conversation that produced it.

## Project objective

Build a Linux-only Go library that presents Linux facilities through the naming, types, resource management, and interfaces expected of Go's standard library. Keep Linux's capabilities and semantics explicit. Use recognizable Linux names where they help; replace C conventions and unclear names with natural Go APIs.

The library wraps facilities that already exist. It does not need to invent a higher-level workflow to justify a package. Consistency, discoverability, and composition with existing Go code are part of its value.

The initial scope is **21 public packages in one module, across nine domains**. Implement them sequentially. The first four establish the design conventions; the next two test those conventions against more involved resources.

## Current state and execution order

| Field | Value |
| --- | --- |
| Current work item | None |
| Next work item | F01 |
| Completed work items | None |
| Implementation repository | `github.com/alexfalkowski/go-linux`; this plan lives in `IMPLEMENTATION.md` |
| Module path | `github.com/alexfalkowski/go-linux` |
| Minimum Linux version | 7.0 |
| Primary runtime target | `linux/arm64` on CircleCI |
| Secondary architecture | `linux/amd64`: compile checks and available local runtime checks; full support requires matching runtime evidence |
| Minimum Go version | 1.27.0, declared in `go.mod` |
| Observed development kernel | `7.3.4-2-t2-resolute`, `x86_64` |
| Observed userspace | Ubuntu 26.04.1 LTS |
| User-reported CircleCI kernel | `7.0.0-1017-aws` |
| User-reported CircleCI architecture | `arm64` |
| CircleCI executor/image/resource class | Docker / `alexfalkowski/go:5.23` / `arm.large`, configured in `.circleci/config.yml`; runtime observations pending F02 |
| Observed local execution privileges | UID 1000; effective and bounding capability sets are empty |
| Observed local execution restrictions | `NoNewPrivs: 1`; `Seccomp: 2` (filter mode); allowed syscalls not yet probed |
| CircleCI execution privileges and restrictions | Unknown; probe independently in F02 |
| Environment observation date | 2026-11-13 |

The observed kernel came from `uname -srmo`; userspace came from `/etc/os-release`. The CircleCI kernel and architecture were supplied by the user and must be confirmed by CI startup diagnostics. These observations are not proof that every syscall is permitted or every optional feature is configured. Linux 6.x compatibility is outside the selected scope.

| Phase | Work items, in order | Result |
| --- | --- | --- |
| 0 | F01, F02 | Scope, API conventions, repository, and verification foundation |
| 1 | P01, P02, P03, P04 | `memfd`, `eventfd`, `timerfd`, `mmap` |
| 2 | P05, P06 | `pidfd`, `mount` |
| 3 | P07, P08, P09 | `epoll`, `file`, `xattr` |
| 4 | P10, P11 | `inotify`, `fanotify` |
| 5 | P12, P13, P14 | `socket`, `netlink`, `netlink/route` |
| 6 | P15 | `namespace` |
| 7 | P16 | `cgroup` |
| 8 | P17, P18, P19 | `capability`, `seccomp`, `landlock` |
| 9 | P20, P21 | `procfs`, `sysfs` |
| 10 | R01, R02 | Cross-package verification and v1 release preparation |

Work-item checkboxes below are the authoritative completion record. Update this table's state fields and the handoff log when work advances. A phase is complete only when all its work items meet their acceptance criteria.

## Instructions for sequential agents

Formatting convention: keep each prose paragraph and list item on one source line; let the editor wrap visually. Do not hard-wrap prose at a fixed column. Preserve intentional line breaks in code blocks, tables, headings, and handoff fields.

1. Read this file and applicable repository instructions. Codex should read applicable `AGENTS.md` files; Claude should read applicable `CLAUDE.md` files. Observe the instruction precedence of the tool being used.
2. Inspect the working tree and previous handoff. Preserve unrelated changes. Confirm the current module, toolchain, kernel, and available test facilities.
3. Work on the requested scope. For a request to implement the next item, choose the first unchecked item whose prerequisites have the necessary evidence. Visit items in the listed order. An earlier item with only an environment-bound verification gap may be deferred under the procedure below; keep its checkbox open. Do not implement later packages speculatively. A request for several sequential items may cover several items, with a handoff record for each.
4. Mark the active item in the state table. Read the relevant primary Linux and Go documentation, existing code, and package design notes before editing.
5. Deliver the package or foundation change, its documented API decisions, meaningful tests, and examples. Complete the common acceptance checklist.
6. Run the applicable checks. Record exact commands, environment, results, and limitations. Fix failures within the assigned scope.
7. Check the item only when its required evidence exists. If an essential test cannot run, record the gap and leave the affected acceptance criterion open. Skipped tests and cross-compilation do not count as runtime verification.
8. Update the state table, package design note, and handoff log. State the exact next item. A different agent should be able to resume from these files.

### Continuing when verification is restricted

Track implementation readiness separately from verification. If code, docs, and available checks are finished but an external privilege/configuration requirement prevents an essential integration test, record **implemented; verification pending**. Leave the work-item checkbox open and add an entry to the verification gap register below. This is not package completion or release readiness.

Continue sequentially to the next independent item when the deferred evidence is not needed for its correctness. For example, missing mount-test privileges need not prevent implementing `file` or `xattr`. Record the dependency assessment in the handoff; do not build further guarantees on unverified behavior. Revisit deferred verification when an appropriate runner becomes available.

Ordinary test failures, incomplete implementation, and unexplained permission errors do not qualify for this exception. Fix or investigate them. R01 and v1 readiness still require all promised behavior to have the required evidence; otherwise the affected public scope must be explicitly reduced and documented.

Keep work sequential by default. Do not start concurrent implementations of shared internals. An agent may review its own change; a separate review agent is optional and must not be assumed to have run.

Do not add approval checkpoints for routine implementation choices. Resolve choices from this plan, repository context, and evidence. Ask only when missing information materially prevents a correct decision. A scope request from the user takes precedence over this sequencing guidance.

### Reusable prompt for either Codex or Claude

```text
Read IMPLEMENTATION.md and the repository instructions applicable to your tool.
Implement the next eligible work item from IMPLEMENTATION.md, completing its acceptance criteria and the common package checklist. Keep changes within that item and necessary shared internals. Verify the change, update IMPLEMENTATION.md and its handoff log, and report what changed, check results, remaining gaps, and the next item.
Do not mark unverified requirements complete.
```

Keep `IMPLEMENTATION.md` as the single execution plan. After F02, any `AGENTS.md` and `CLAUDE.md` entry points should be brief, consistent pointers to the plan and repository commands. Do not maintain separate vendor-specific copies of the roadmap. Existing repository instructions must be preserved.

## Package boundaries

Domains organize documentation. Public packages own coherent facilities and resources; avoid large umbrella packages and one package per syscall.

| Domain | Public packages | Initial boundary |
| --- | --- | --- |
| Files and directories | `file`, `memfd`, `xattr` | Linux file extensions, memory-backed files, extended attributes |
| Memory | `mmap` | Mapped regions, protection, advice, lifetime |
| Events and timers | `eventfd`, `timerfd`, `epoll` | Counters, kernel timers, explicit readiness polling |
| Filesystem monitoring | `inotify`, `fanotify` | File events and access notifications |
| Processes and isolation | `pidfd`, `namespace`, `cgroup` | Process handles, namespace operations, resource control |
| Mounts | `mount` | Filesystem configuration and mount objects |
| Networking | `socket`, `netlink`, `netlink/route` | Linux socket operations, kernel messages, routes and interfaces |
| Security | `landlock`, `seccomp`, `capability` | Access rules, syscall filtering, capability sets |
| System information | `procfs`, `sysfs` | Bounded, structured access to kernel pseudo-filesystems |

Keep public packages near the module root; `netlink/route` is a deliberate sub-package. Keep shared descriptor and ABI helpers under `internal/` only when actual implementations need them. Do not create a public universal descriptor type or a general-purpose options framework in advance.

Use `golang.org/x/sys/unix` as the normal syscall foundation. Prefer pure Go and public runtime integration points. Avoid cgo, copied syscall tables, private runtime hooks, and `go:linkname` as the default implementation strategy.

The package list is a bounded first release, not a promise to expose every operation in every subsystem. Each package must record its initial operation list and explicit omissions before implementation.

## API design rules

### Naming and composition

- Keep useful terms such as mount, namespace, seal, memfd, and pidfd.
- Remove redundant C prefixes, output parameters, and names already supplied by the package or receiver. Prefer a clearer operation name where appropriate.
- Start with existing Go interfaces. Implement `io.Reader`, `io.Writer`, `io.Closer`, `io.ReaderAt`, `io.WriterAt`, `io.Seeker`, and `fs.FS` where their contracts fit; do not add interfaces unsupported by the resource's behavior.
- Add typed convenience methods alongside standard methods when they improve Linux-specific operations. A counter can support byte I/O and typed addition.
- Reuse `*os.File`, `time.Duration`, `time.Time`, slices, and standard errors where they accurately represent the operation. Wrap an existing type when additional resource behavior justifies it.
- Prefer typed flags or small option structs. Preserve distinct Linux modes; avoid boolean combinations that permit contradictory configurations.
- Document intentional Go defaults that differ from raw Linux defaults.

Package `io` is specifically intended to supply shared interfaces for I/O implementations. See the [Go I/O documentation](https://pkg.go.dev/io).

Fixed-size and record-oriented I/O needs explicit documentation. For example, eventfd reads produce an eight-byte value in native byte order and reject smaller buffers. Preserve that behavior in direct byte I/O; do not silently buffer parts of a counter read to imitate a stream. Typed helpers can encode or decode the value. See [eventfd documentation](https://man7.org/linux/man-pages/man2/eventfd.2.html).

### Ownership and concurrency

- Constructors must document ownership. Any adoption, borrowing, or duplication of an existing descriptor must have explicit semantics.
- Owned resources have deterministic `Close` behavior. Do not depend on garbage collection for correctness. Define repeated close and use-after-close results.
- Create descriptors with close-on-exec atomically where supported. Any opt-out must be explicit. Handle partial-construction failures without leaks.
- Document concurrency guarantees per type and operation. In particular, decide what happens when `Close` overlaps a blocking operation.
- Expose low-level access using supported mechanisms such as `SyscallConn` where applicable. Document lifetimes and ownership; a raw descriptor number alone does not provide lifetime synchronization.
- Use standard Go polling/deadline integration where the descriptor supports it. Distinguish Go-style blocking calls on a nonblocking descriptor from a raw interface that returns `EAGAIN`. Do not promise deadlines for every descriptor.
- Accept contexts only when cancellation is implemented and documented. A goroutine left blocked on a syscall is not a cancellation implementation.

Consult [os.NewFile](https://pkg.go.dev/os#NewFile) and [syscall.Conn](https://pkg.go.dev/syscall#Conn) when choosing integration points.

### Errors and Linux semantics

- Add useful operation and resource context while preserving the underlying errno for `errors.Is` and `errors.As`.
- Document unsupported features and retain evidence of the actual failure. Do not turn every `EPERM`, `EINVAL`, or `ENOSYS` into the same unsupported error; configuration and sandbox policies can affect observations.
- Define zero-value behavior and validate invalid option combinations.
- Document side effects, partial success, blocking, record boundaries, clock domains, and thread scope where relevant.
- Keep architecture-sensitive layout and byte-order handling internal and tested.
- Link each exported operation's documentation to the relevant Linux interface.

## Modern Linux policy

The selection rule is **prefer supported, extensible interfaces with the required semantics; omit superseded interfaces when they add no needed capability**. Age alone does not establish obsolescence.

- Baseline: Linux **7.0+**, with **arm64 as the primary runtime target**. The user's CircleCI runner reports `7.0.0-1017-aws` on arm64; local development reports a newer 7.3 kernel on amd64. Keep amd64 compilation working and record available local execution separately. Advertise full amd64 runtime support only after the required baseline checks exist. Record changes to this policy explicitly; do not raise the floor implicitly. This choice excludes Linux 6.x and does not claim that every 7.x series is a long-term release. See the [kernel release policy](https://www.kernel.org/category/releases.html).
- Keep the minimum Go version declared in `go.mod` aligned with the [Go support policy](https://go.dev/doc/devel/release#policy). Pin the syscall dependency when an implementation first needs it.
- Newer optional features may require newer kernels. Document requirements per operation or flag and use feature/ABI queries where available. Version strings alone are insufficient; kernel configuration and distribution backports matter.
- Check baseline operations against the upstream Linux 7.0 UAPI as well as the actual runners. Success on an AWS kernel with backports or the local 7.3 kernel must not silently turn a vendor-specific or newer feature into a baseline requirement. Revisit API choices for this floor instead of carrying forward compatibility code needed only for older kernels.
- No silent fallback that weakens requested guarantees. A fallback is acceptable only when its observable semantics match and it has dedicated verification.

| Facility | Initial choice | Initial omission or distinction |
| --- | --- | --- |
| cgroups | v2 unified hierarchy | No v1 compatibility layer |
| Mounts | Descriptor-based mount API | No legacy `mount(2)` compatibility backend |
| File resolution | `openat2` for its resolution controls | No emulation that loses requested controls |
| Process references | pidfds | Numeric PID acquisition still needs documented race boundaries |
| Readiness | `epoll_create1` and epoll operations | No separate select/poll wrapper packages |
| Notifications | `inotify_init1` and fanotify in separate packages | Fanotify is not treated as a universal inotify replacement |
| Networking | Appropriate Netlink families | Routing still needs route Netlink; Generic Netlink does not replace every family |
| Ordinary I/O and mappings | Existing read/write/mmap interfaces as appropriate | Their age does not exclude them |

Relevant primary documentation: [cgroup v2](https://docs.kernel.org/admin-guide/cgroup-v2.html), [descriptor-based mounts](https://man7.org/linux/man-pages/man2/fsopen.2.html), [openat2](https://man7.org/linux/man-pages/man2/openat2.2.html), [pidfd_open](https://man7.org/linux/man-pages/man2/pidfd_open.2.html), [epoll](https://man7.org/linux/man-pages/man7/epoll.7.html), [inotify](https://man7.org/linux/man-pages/man7/inotify.7.html), [fanotify](https://man7.org/linux/man-pages/man7/fanotify.7.html), and [Netlink](https://docs.kernel.org/userspace-api/netlink/intro.html).

Deferred beyond the 21-package scope: io_uring, eBPF, perf events, userfaultfd, device-specific APIs, System V IPC, POSIX message queues, raw futex interfaces, and direct signal-management APIs. This is a scope decision, not a claim that every deferred interface is obsolete. Do not introduce raw fork/clone wrappers that bypass Go's runtime process-start machinery. Do not rebuild `os`, `net`, `time`, `sync`, or `os/exec` wholesale.

Io_uring has a distinct asynchronous submission/completion model. Assess it as its own extension; it is not a required backend for the initial packages. See [io_uring documentation](https://man7.org/linux/man-pages/man7/io_uring.7.html).

## Common acceptance checklist

Every P-series work item must satisfy this checklist before completion:

- A short `docs/design/<package>.md` note records the resource model, included operations, omitted operations, naming, standard-interface fit, ownership, concurrency, blocking, feature requirements, test prerequisites, and primary references. Use `docs/design/netlink-route.md` for the nested package.
- Implementation follows the shared API rules and relevant predecessors.
- Package documentation and at least one useful example demonstrate real use. Examples must compile; privileged examples must identify their requirements.
- Meaningful tests verify resource lifecycle and kernel behavior, applicable standard interfaces, invalid input, errors, and relevant blocking/concurrency behavior. Do not add tests that merely reproduce implementation details.
- Verify partial-failure cleanup and descriptor ownership where applicable.
- Separate ordinary-user coverage from tests requiring capabilities, a delegated resource, namespace support, particular filesystem behavior, or kernel settings. Record verification status per operation or mode, not just per package.
- Applicable formatting, vet, package tests, regression tests, and targeted race checks pass. Record unavailable checks without representing them as passes.
- The handoff records API decisions, changed files, checks and outcomes, environment limitations, and the next item.

### Verification strategy

- Default tests run without elevated privileges and isolate their own resources. Use subprocesses for irreversible restrictions, thread-sensitive operations, and process lifecycle tests. Always bound waits and clean up child processes.
- Use temporary files and explicitly delegated resources. Mount, namespace, networking mutation, and cgroup tests need suitable isolated test environments. Do not modify the host's ordinary routes, mounts, cgroups, or security policy.
- Use fixture tests for parser edge cases and live tests for actual UAPI behavior. Fixture or mock success alone does not verify a syscall wrapper.
- Privileged/feature-specific tests use explicit tags or opt-in configuration. Record exact prerequisites and skip reasons. Required CI lanes must fail or remain outstanding when their prerequisites are missing; they cannot pass by skipping the entire facility.
- For code changes, run `gofmt` on touched Go files, `go vet ./...`, relevant package tests, and `go test ./...` where the runner supports the default suite. Run targeted `go test -race` for meaningful concurrency behavior and after changing shared synchronization. Broaden checks when the change warrants it.
- F02 must add a reproducible compile-only check for amd64 and arm64 that covers both package code and test code. Do not try to execute foreign-architecture test binaries directly. Cross-compilation is recorded separately from execution.
- Required primary CI matrix: execute on Linux 7.0.x arm64 with the declared minimum and latest supported Go versions; also execute on a newer kernel on arm64. Confirm the CircleCI kernel, architecture, executor, image, and resource class in job output. A container image cannot by itself pin the host kernel.
- Keep compile checks for both amd64 and arm64. Record local Linux 7.3 amd64 execution when available. To advertise full amd64 support, add the corresponding 7.0.x and newer-kernel runtime matrix; compile success alone is insufficient.
- Run required privileged lanes on capable isolated runners. Inspect the CircleCI executor's actual capabilities and restrictions instead of inferring them from `uname`. If another runner is needed, leave that lane outstanding until it exists. Document the executed matrix and newer-feature requirements.

### Test prerequisites and restricted environments

Root is not a sufficient test-environment description. Capabilities are scoped, containers may expose read-only resources, and syscall or security policies can block operations even for UID 0. Conversely, several facilities can be exercised without host root, using user namespaces or explicitly delegated resources. See [user namespaces](https://man7.org/linux/man-pages/man7/user_namespaces.7.html) and [cgroup delegation](https://docs.kernel.org/admin-guide/cgroup-v2.html#delegation).

The table describes expected prerequisites, not results of successful probes. Ordinary-user tests still require the relevant syscalls to be allowed. Each package's design note must refine these expectations for its actual operations.

| Packages | Normally testable without host root | Additional requirements / gaps to flag |
| --- | --- | --- |
| `memfd`, `eventfd`, `timerfd`, `mmap`, `epoll` | Ordinary creation, I/O, lifecycle, and readiness | Special flags may need capabilities or configuration. Clock-changing tests need a dedicated isolated VM. |
| `file`, `xattr`, `inotify` | Operations on owned temporary files and watches | Filesystem support, attribute namespace permissions, quotas, and security policy vary. |
| `pidfd` | Handles, signaling, and supported waits for a test-owned child | Cross-user or descriptor-access operations have additional permission/ptrace checks. |
| `socket`, `netlink`, `netlink/route` | Local sockets, permitted discovery, and read-only dumps | Route/link mutations normally need `CAP_NET_ADMIN` in the applicable network namespace. Raw sockets need their applicable capability. |
| `mount`, `namespace` | Handle inspection and some operations in an allowed user namespace | Probe user-namespace creation and policy first. Mount/entry operations need the applicable namespace capabilities; not every filesystem or namespace can be exercised this way. |
| `cgroup` | Read-only inspection and mutation inside a suitably delegated subtree | Need writable delegation, enabled controllers, and containment-compatible membership. Root alone does not provide those in a restricted container. |
| `capability` | Inspect own capability sets and permitted self-reductions in a child | Tests exercising capabilities must start with the necessary sets; do not assume they can be acquired or restored. |
| `seccomp`, `landlock` | Supported self-restriction tests in child processes using `no_new_privs` | Kernel support and policy must allow the calls. Test actual enforcement and thread scope; no blanket root requirement. |
| `fanotify` | Restricted notification modes on a suitable filesystem, when allowed | Permission events and broader modes require `CAP_SYS_ADMIN`; unprivileged modes have explicit restrictions. |
| `procfs`, `sysfs` | Fixtures and permitted read-only access | Visibility, permissions, available devices, and proc/sys mount options vary. |

In particular, filtering with `seccomp` can be unprivileged with `no_new_privs`, and Landlock is designed for unprivileged self-restriction. Do not skip these packages merely because the runner is not root. See [seccomp prerequisites](https://man7.org/linux/man-pages/man2/seccomp.2.html), [Landlock prerequisites](https://docs.kernel.org/userspace-api/landlock.html), and [fanotify privilege modes](https://man7.org/linux/man-pages/man2/fanotify_init.2.html).

F02 must establish the following test-runner behavior:

- Record kernel, architecture, UID, effective/bounding capabilities, `NoNewPrivs`, seccomp mode, and relevant namespace/filesystem/controller observations. Probe the actual operation in an isolated helper when necessary; privilege bits alone do not establish that it works. Report observations, not guessed filter rules. Do not change host policy to make a probe pass.
- Keep the default profile usable without root. Separate profiles select namespace-isolated, delegated-resource, or capability-requiring tests as needed. A profile is a request to test, not evidence that its prerequisites are present.
- Skip only after an explicit prerequisite check establishes an environment limitation. Include the test, operation/mode, observed error, unmet prerequisite, and profile needed in the result. Unexpected errors in the operation under test must fail; do not turn every `EPERM` or `EINVAL` into a skip.
- A required CI profile must fail its prerequisite check when its requested coverage cannot run. An optional local profile may report precise skips. Emit machine-readable test results, including skips, for later verification review.
- Keep permission-denial tests: an expected permission error can be a successful behavioral test, while the corresponding success path still needs another environment. Distinguish these in coverage records.
- Do not assume `sudo`, a privileged container, or an alternate executor exists. Implement available behavior, record missing evidence, and continue independent work under the deferral procedure. Never label mocks or fixtures as privileged kernel integration coverage.

### Verification gap register

No operation-level probes have run yet. The observed local UID/capability/seccomp state is recorded above; CircleCI privileges remain unknown. Add rows when a specific required check is attempted and cannot run. Reassess these rows in R01.

| Work item / operation | Implementation status | Verification missing | Evidence / unmet prerequisite | Required runner or profile | Resolution |
| --- | --- | --- | --- | --- | --- |
| None recorded yet | Not started | Prerequisite discovery planned in F02 | Local privilege inspection only; no facility probes | Local and CircleCI discovery | Pending F02 |

## Phase 0: foundation

- [ ] **F01 — Record API decisions.** Inventory standard-library overlap and existing Go libraries as design references. Write `docs/design/api.md` with naming, ownership, byte-I/O, typed-helper, errors, deadlines, and compatibility conventions. **Acceptance:** decisions and unresolved external constraints are explicit; the six pilot packages have documented provisional API sketches. API review is part of implementation; this item does not require a new user approval gate.

- [ ] **F02 — Extend verification commands.** Extend the existing CircleCI pipeline with supported Go version coverage, architecture compilation, and the kernel/privilege matrix where runners are available. Inspect the configured arm64 executor's actual kernel and available isolation; adjust job settings when evidence requires it. Build a minimal subprocess test helper only when immediately needed. Implement prerequisite discovery, test profiles, precise skip reporting, and required-profile failure behavior described above. Record rootless and capability-requiring coverage separately; do not assume CircleCI grants root. List missing runners as outstanding work; do not claim they exist. **Acceptance:** a minimal Go example/test runs, dependency resolution is reproducible, contributor commands work, and both agent entry points refer to this plan. No empty public-package stubs or speculative shared framework.

## Phase 1: four packages that establish the conventions

- [ ] **P01 — `memfd`: memory-backed files.** Implement creation, ownership, size management using standard file operations, and seal inspection/application. Prefer existing file methods for ordinary I/O; define how the wrapper exposes the additional sealing behavior. Start with baseline-supported flags and identify newer optional flags separately. **Acceptance:** ordinary Go file I/O works; tests cover read/write, resizing, sealing behavior and failures, close behavior, and constructor cleanup.

- [ ] **P02 — `eventfd`: event counters.** Implement ordinary and semaphore modes, byte-oriented `Read`/`Write`, typed counter helpers, and `Close`. Preserve fixed-size/native-endian semantics. Establish Go polling/deadline integration for this descriptor and document any explicitly exposed raw nonblocking behavior. **Acceptance:** verify increments, reads and draining semantics, semaphore behavior, invalid buffers/values, blocking or deadlines as offered, and close during a blocked operation. Interface assertions supplement behavioral tests.

- [ ] **P03 — `timerfd`: kernel timers.** Implement creation, arming/disarming, state inspection, and expiration reads. Represent relative versus absolute deadlines and clock choice explicitly; a monotonic absolute deadline is not an ordinary wall-clock `time.Time`. Support byte I/O and typed expiration counts where appropriate. **Acceptance:** verify one-shot and periodic behavior with tolerant timing assertions, disarming, expiration counts, supported clock modes, and resource shutdown. Clock-change cancellation tests need a dedicated VM or equivalent environment where adjusting the clock cannot affect the shared host.

- [ ] **P04 — `mmap`: mapped regions.** Implement anonymous and file-backed mappings, shared/private modes, protection, selected advice operations, and unmapping through a resource type. Document alignment, bounds, slice lifetime, and ownership of the source file. Clearly state that retaining a byte slice past unmap is invalid; a wrapper cannot make an escaped slice safe after the underlying mapping is removed. **Acceptance:** verify file sharing/private behavior, protection arguments, bounds, cleanup, and lifetime rules. Fault-inducing tests run in subprocesses. Review all four packages and consolidate only demonstrated common internals.

## Phase 2: process handles and mount objects

- [ ] **P05 — `pidfd`: process references.** Implement opening/adopting a pidfd, signaling, exit readiness, and waiting where Linux permits it. Distinguish observing exit from reaping a child. Document numeric-PID acquisition races and the overlap with `os.Process`. Descriptor duplication from another process is an optional extension, not required here. **Acceptance:** subprocess tests verify signaling, exit observation, supported child waiting, ownership, permission failures, and cancellation if offered. Do not promise that arbitrary non-child processes can be reaped.

- [ ] **P06 — `mount`: descriptor-based mount operations.** Model filesystem configuration contexts and mount handles. Implement the baseline workflow using `fsopen`, `fsconfig`, `fsmount`, `open_tree`, `move_mount`, and `mount_setattr` as applicable. Keep configuration and attachment explicit. Preserve diagnostics from filesystem configuration failures. **Acceptance:** isolated integration tests verify creation/configuration, cloning or attachment, attributes, failure cleanup, and handle lifetime. Closing a mount handle must not be documented as universally undoing an attached mount. Use an internal subprocess fixture until P15 exists; do not implement the future namespace package early. No legacy mount backend.

## Phase 3: readiness and Linux file operations

- [ ] **P07 — `epoll`: explicit readiness polling.** Implement instance creation, registration/modification/removal, event results, and waiting. Expose level/edge-triggered and one-shot behavior explicitly. Define event identity and descriptor-reuse handling, concurrent close behavior, and how waits are interrupted. Do not replace Go's internal network poller. **Acceptance:** real-descriptor tests cover readiness, registration lifecycle, trigger modes, wait interruption, and descriptor reuse. Optional newer wait facilities must not silently raise the kernel floor.

- [ ] **P08 — `file`: bounded Linux file extensions.** Compare against the chosen Go version's `os.File` and `os.Root` first. Implement explicit `openat2` resolution options and selected Linux metadata/rename extensions, starting with `statx` and `renameat2` capabilities that add value. Reuse standard file handles; record the exact included operations. **Acceptance:** temporary-directory tests cover resolution restrictions, symlinks, relevant rename modes, metadata masks, and errno preservation. No broad replacement of ordinary `os` file operations.

- [ ] **P09 — `xattr`: extended attributes.** Implement get/set/list/remove with explicit descriptor/path/symlink behavior. Handle variable-sized results and concurrent size changes with bounded, documented retry behavior. Keep filesystem support failures observable. Compare baseline-supported at-style attribute APIs against older variants; choose the modern interface where it supplies the required semantics. **Acceptance:** real-filesystem tests cover round trips, create/replace modes, missing attributes, sizing races where reproducible, and supported symlink behavior. Features introduced after Linux 7.0 remain explicitly optional.

## Phase 4: filesystem notifications

- [ ] **P10 — `inotify`: file and directory events.** Implement watch lifecycle, raw reads where useful, and typed event decoding. Preserve watch identifiers, rename cookies, ignored events, and queue-overflow reporting. Document that watches are not recursively installed by the kernel. **Acceptance:** verify watch addition/removal, event decoding, rename pairing information, shutdown, and malformed/truncated input handling. Do not promise a lossless event stream or hide overflow behind an automatic rescan.

- [ ] **P11 — `fanotify`: filesystem notifications and permission events.** Define the initial supported modes and event record types. Expose event-owned descriptors and permission responses with explicit lifetime rules. Document capabilities, configuration, and filesystem requirements by mode. **Acceptance:** isolated integration tests exercise the promised notification and permission paths; parser tests handle variable records and unknown types. Verify descriptor cleanup on abandoned events, malformed records, and errors.

## Phase 5: socket and Netlink facilities

- [ ] **P12 — `socket`: Linux socket extensions.** Keep the initial API focused on socket operations needed by Netlink and useful Linux extensions beyond `net`: lifecycle, message I/O, selected options, and ancillary data handling. Establish polling/deadlines and standard interface fit per socket mode. Preserve datagram boundaries and expose truncation. **Acceptance:** socket-pair or local-socket tests cover lifecycle, message boundaries, descriptor passing, deadlines, concurrent close, and cleanup. Reuse supported `net`/`os` integration rather than building a second TCP stack.

- [ ] **P13 — `netlink`: kernel messaging.** Implement message/attribute encoding and decoding, request sequencing, acknowledgements, extended errors, multipart dumps, and multicast reception. Include bounded Generic Netlink family discovery and attribute policies as needed. State whether requests can share a connection concurrently. **Acceptance:** parser/fuzz tests handle malformed lengths/alignment and unknown attributes; live read-only tests verify discovery, response matching, dump completion/interruption, and errors. Report truncated or interrupted results.

- [ ] **P14 — `netlink/route`: links, addresses, routes.** Build typed listing and add/change/delete operations over P13. Keep the first operation set bounded; advanced traffic control and every link type are outside this item. Preserve Linux indices, flags, and identifiers where useful. **Acceptance:** live read-only listing plus isolated network-namespace tests verify mutation and notifications. Use private test fixtures before P15 exists. Do not imply atomic snapshots or multi-operation transactions.

## Phase 6: namespaces

- [ ] **P15 — `namespace`: handles and explicit namespace operations.** Implement handle acquisition, inspection, comparison where supported, and the supported entry/creation operations. Explicitly distinguish calling-thread effects, child-process effects, and restrictions in a multithreaded process. Any scoped callback API needs a proved restoration/failure model and documented restrictions on goroutines. Do not require such an API merely for convenience. **Acceptance:** subprocess integration tests verify the supported namespace types, thread affinity, restoration if offered, and cleanup on failure/panic. Verify that no changed thread state leaks into unrelated Go work. Update prior test helpers to reuse this package only when that actually simplifies them.

`setns` is thread-scoped and has namespace-specific restrictions; locking a Go thread does not make every namespace transition legal. Consult [setns](https://man7.org/linux/man-pages/man2/setns.2.html) and [runtime.LockOSThread](https://pkg.go.dev/runtime#LockOSThread).

## Phase 7: resource control

- [ ] **P16 — `cgroup`: unified resource control.** Implement v2 hierarchy/group handles, create/remove, membership, events, and a bounded initial controller set: CPU, memory, and PIDs. Keep delegation and controller enablement explicit. Do not assume the library owns the hierarchy root or that systemd-created groups can be changed arbitrarily. **Acceptance:** fixture tests verify file formats, and live tests in a delegated subtree verify lifecycle, moving a child, limits, events, and cleanup. Test unavailable controllers and invalid delegation. No v1 or systemd D-Bus backend.

## Phase 8: security facilities

- [ ] **P17 — `capability`: Linux capability sets.** Implement inspection and explicit supported set/clear operations, including the chosen permitted/effective/inheritable, bounding, and ambient operations. Separate thread state from exec transitions and any process-wide claims. **Acceptance:** subprocess tests verify allowed transitions, expected failures, and exec behavior. Document Go runtime/thread constraints; a locked goroutine alone does not apply a capability change to every thread.

- [ ] **P18 — `seccomp`: syscall filters.** Implement filter loading, the supported actions, and explicit synchronization policy. Start with a bounded representation of kernel filter programs; a policy language/compiler and user-notification broker are later extensions. Keep architecture validation and Go runtime syscall needs visible. **Acceptance:** subprocess tests verify allowed/denied operations, architecture checks, error propagation, and thread synchronization where promised. Never apply irreversible test restrictions to the main test runner.

- [ ] **P19 — `landlock`: access rulesets.** Implement ABI discovery, ruleset lifecycle, baseline-supported filesystem and network rules, and restriction application. Represent newer rights explicitly. Preserve the caller's requested policy; do not silently discard unsupported restrictions. Document exactly which threads/process descendants are covered. **Acceptance:** subprocess tests verify allowed/denied access, ABI negotiation, rule ownership, unsupported rights, and enforcement scope. Do not claim to restrict an entire multithreaded Go program without verified kernel support.

Reference [capabilities](https://man7.org/linux/man-pages/man7/capabilities.7.html), [seccomp](https://man7.org/linux/man-pages/man2/seccomp.2.html), and [Landlock](https://docs.kernel.org/userspace-api/landlock.html). These packages expose mechanisms; they do not constitute a complete sandbox or container runtime.

## Phase 9: system information

- [ ] **P20 — `procfs`: process and system information.** Accept an explicit filesystem root for fixtures and alternate proc mounts. Start with process identity/statistics, descriptor information, memory maps, mount information, and basic CPU/memory statistics. Avoid a catch-all parser for every proc entry. Document races, permission failures, and disappearing processes; multiple reads do not form an atomic snapshot. **Acceptance:** fixture tests cover awkward names, malformed/truncated input, changing fields, and absent entries. Live read-only tests cover current-process and basic system information. Avoid exposing host-sensitive fixture data.

- [ ] **P21 — `sysfs`: bounded device and kernel attributes.** Accept an explicit root. Start with read-only block-device and network-interface attributes and discoverable identifiers. Keep values typed where semantics are stable; retain unknown attributes without pretending all files share one format. Broad mutation and driver-specific control remain deferred. **Acceptance:** fixtures cover symlinks, missing attributes, units, and device disappearance. Live read-only tests verify available attributes without assuming a particular host device inventory.

## Phase 10: integration and release preparation

- [ ] **R01 — Verify the complete supported surface.** Audit all 21 packages for consistent naming, standard-interface behavior, ownership, errors, concurrency, feature requirements, and actual kernel-floor compatibility. Review public APIs against standard-library overlap and actual usage. Exercise cross-package examples such as memfd plus mmap and eventfd or timerfd plus epoll. Finish the required kernel/architecture/Go and isolated privileged test matrix. Add benchmarks only for specific performance claims or unresolved design choices. **Acceptance:** required checks have recorded results, known defects are fixed, and no skipped or compile-only run is represented as runtime coverage. Gaps remain explicitly outstanding; they do not disappear at the release boundary. Reconcile every verification-gap entry and implemented-but-unverified item.

- [ ] **R02 — Prepare v1 documentation and release materials.** Write the package index, getting-started examples, support matrix, compatibility policy, feature requirements, and contributor guidance. Record intentional omissions, deferred extensions, and unresolved limitations. Review exported API stability and dependency/licensing requirements. **Acceptance:** another contributor can use and verify the library from the repository documentation. Prepare release notes and a concrete release candidate. Publishing or tagging follows the user's actual authorization.

## Decisions and scope changes

Record material changes here with the reason and affected work items. Routine implementation details belong in the relevant package design note.

| Date | Decision | Reason / affected items |
| --- | --- | --- |
| 2026-11-13 | Use this repository, the module identity and Go floor in `go.mod`, and the MIT license in `LICENSE` | Scaffold setup is established; remove completed setup tasks while keeping F01/F02 API design and verification work open |
| 2026-11-13 | One module, 21 focused public packages | Agreed initial package map; excludes internal helpers and deferred facilities |
| 2026-11-13 | Prefer standard Go interfaces and add Linux-specific helpers | Preserve Go composition while retaining Linux semantics |
| 2026-11-13 | Linux 7.0 minimum; newer features remain optional | User selected 7.x based on CircleCI's reported `7.0.0-1017-aws`; excludes Linux 6.x |
| 2026-11-13 | Primary runtime verification on CircleCI arm64 | User reported arm64; amd64 gets compilation and available local checks until full runtime support is verified |
| 2026-11-13 | Execute packages sequentially with shared handoff records | Support continuation by Codex, Claude, or human contributors |
| 2026-11-13 | Track implementation and verification separately | Root/capability/configuration restrictions may defer integration checks; independent work may continue without claiming completion |

## Handoff log

Append one entry per completed or interrupted work item. Keep details concrete; do not replace failed or unavailable checks with a general statement that tests passed. Update the current state and next-item pointer above at the same time.

```text
Work item:
Status: complete / in progress / blocked
Implementation: not started / in progress / ready
Verification: not run / partial / verified / environment-blocked
Agent and date:
Repository and revision, if available:
Changes and relevant files:
API decisions and primary references:
Checks: exact command -> result
Environment: Go version, kernel, architecture, relevant capabilities
Required profile, namespace/delegation/configuration prerequisites:
Skipped/unavailable checks and remaining acceptance criteria:
Verification gap register entries added or resolved:
Unresolved issues or scope changes:
If continuing past a deferred item: dependency assessment and reason
Next work item and concrete starting point:
```

No implementation handoffs yet. Start with F01.
