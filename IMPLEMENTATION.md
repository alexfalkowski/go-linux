# Linux APIs for Go: remaining implementation

Next: **P12 — `socket`: Linux socket extensions**. Implement the remaining items below in order; open verification gaps may proceed independently when their missing evidence does not affect the next item's correctness.

This file contains unfinished work only. Delete finished items and resolved gaps instead of marking them done. Keep a short resume note only while work is incomplete; replace it when circumstances change and remove it when finished. Do not append completed handoffs, historical command logs, resolved findings, or an archive of deleted entries.

## Working guidance

- Follow [AGENTS.md](AGENTS.md), the shared [API conventions](docs/design/api.md), and each package's usage/design guides. [README.md](README.md) lists implemented packages; [verification guidance](docs/verification.md) owns commands, runner requirements, and current evidence. Keep durable API decisions in `docs/design/<package>.md`.
- Inspect the working tree before editing and preserve unrelated changes. Read the relevant existing implementation and primary Linux/Go documentation. Work on the requested scope; implement the first eligible remaining item when asked to continue the plan. Do not create future package stubs, dependencies, or speculative shared frameworks.
- Keep work sequential by default. If essential verification is blocked by a demonstrated environment prerequisite, retain the item as implemented with verification pending and record the missing operation, observed failure, and required runner below. Continue only to independent work; ordinary failures or unexplained permission errors must be investigated. User-accepted partial scopes may leave explicit R01 follow-ups.
- Before removing an item, satisfy its acceptance criteria and the checklist below. Keep a compact verified-environment matrix in the verification guide and unresolved verification gaps below; design notes explain decisions and tradeoffs. Update the next-item pointer and any open gaps; do not retain a completion entry here.
- Keep prose paragraphs and list items on single source lines. Use the same plan for human contributors and coding agents; do not maintain separate copies of the roadmap.

## Scope and constraints

The bounded first release comprises 21 facility-focused public packages in one module. Keep public packages near the module root; `netlink/route` is the deliberate subpackage. The remaining package work is listed below. Libraries may expose Linux mechanisms directly without inventing a higher-level workflow.

- Target Linux **7.0+** and the Go floor in [go.mod](go.mod), aligned with the [Go support policy](https://go.dev/doc/devel/release#policy). Prefer maintained `golang.org/x/sys/unix` bindings, pure Go, and public runtime integration. Avoid cgo, private runtime hooks, `go:linkname`, and copied syscall tables as the default implementation strategy. Do not add raw fork/clone wrappers that bypass Go process creation.
- Prefer supported extensible interfaces with the required semantics. Check baseline operations against upstream Linux 7.0 UAPI and actual runners; vendor backports or newer local kernels do not establish baseline availability. Keep newer features explicitly optional, probe actual prerequisites, and never silently weaken requested guarantees or raise the kernel floor.
- Deferred beyond this scope: io_uring, eBPF, perf events, userfaultfd, device-specific APIs, System V IPC, POSIX message queues, raw futex interfaces, and direct signal-management APIs. Do not rebuild `os`, `net`, `time`, `sync`, or `os/exec` wholesale.

## Acceptance and verification

- Provide the typed public API, Linux semantics, ownership/error/concurrency contracts, and resource cleanup required by [API conventions](docs/design/api.md). Document operations and omissions in the package guide and GoDoc, with their rationale in the package design note.
- Add GoDoc, a usage guide linked from README, a focused design note, and useful executable examples. Link public operation documentation to the relevant Linux interfaces. Use `docs/design/netlink-route.md` for the nested package. Examples and ordinary behavior tests use the typed public API; focused ABI tests retain independent kernel observations.
- Verify real resource behavior, errors, invalid inputs, partial-failure cleanup, ownership, concurrency, and Linux-specific contracts with the existing harness. Use bounded subprocesses for irreversible restrictions, thread-sensitive changes, faults, and child-process tests. Do not mutate the shared host's clocks, mounts, routes, cgroups, or security policy.
- Run the narrowest supported checks for the changed scope; CI owns the full native arm64/amd64 suites. Use the one Go toolchain from the configured standard image; separate Go-version and compile-only/no-cgo matrices are not required. Re-run affected checks after relevant changes and report actual commands, outcomes, and limitations.
- The `build`, `build-mount`, `build-fanotify`, and `build-timerfd` architecture matrices are required gates. Use the same facility container launchers locally and in CI. Follow [verification guidance](docs/verification.md) for profiles, native architecture checks, image selection, reports, and privilege/isolation requirements. A container image does not select the host kernel.
- Missing optional prerequisites may produce precise skips; selected required prerequisites must fail when absent. Root, capability bits, namespace creation, mocks, or compilation alone do not establish operation success. Record actual Go/kernel/architecture and relevant privilege observations. Reassess runner evidence when the environment changes without implicitly changing support policy.

## Open verification gaps

| Scope | Missing evidence | Required environment or next action |
| --- | --- | --- |
| R01 / `inotify` native baseline coverage | Runtime evidence for inotify on Linux 7.0 amd64 and arm64. Local ordinary-user coverage is native amd64 on 7.3.4-3-t2-resolute; requesting `kernel=7.0` fails the runner check. | Run the required rootless lane with the configured Go image on native Linux 7.0 amd64 and arm64, retaining environment and test reports. No container image can supply the missing host kernel or native architecture. |
| R01 / `xattr` defensive read paths | Concurrent value and list mutation runs do not deterministically establish ERANGE retry/exhaustion, the oversized query guard, or malformed kernel-list rejection. | Reconcile using supported public/kernel scenarios where reproducible; retain source-reviewed limits rather than adding production injection hooks. |
| R01 / source-reviewed paths | Direct execution remains absent for defensive timerfd zero-byte conversion, file block-device conversion/remote synchronization/automount/positive attributes, eventfd/inotify/fanotify poller-registration rejection, inotify unmount notifications, oversized/malformed mount diagnostics, and rare acquisition, response, cleanup, or partial-application failures. | Reconcile during R01 using supported public/kernel scenarios and suitable isolated runners; retain explicit limits where execution is not feasible. |
| R01 / `pidfd` positive core-dump status | Actual CLD_DUMPED and `CoreDump=true` on the Linux 7.0 arm64 target | A runner whose existing core policy produces CLD_DUMPED for the test-owned child. The current test compares with nonconsuming kernel status; CLD_KILLED verifies only the false flag. Do not change host core policy to force this case. |

## Remaining work

### Socket and Netlink facilities

- **P12 — `socket`: Linux socket extensions.** Keep the initial API focused on socket operations needed by Netlink and useful Linux extensions beyond `net`: lifecycle, message I/O, selected options, and ancillary data handling. Establish polling/deadlines and standard interface fit per socket mode. Preserve datagram boundaries and expose truncation. **Acceptance:** socket-pair or local-socket tests cover lifecycle, message boundaries, descriptor passing, deadlines, concurrent close, and cleanup. Reuse supported `net`/`os` integration rather than building a second TCP stack.

- **P13 — `netlink`: kernel messaging.** Implement message/attribute encoding and decoding, request sequencing, acknowledgements, extended errors, multipart dumps, and multicast reception. Include bounded Generic Netlink family discovery and attribute policies as needed. State whether requests can share a connection concurrently. **Acceptance:** parser/fuzz tests handle malformed lengths/alignment and unknown attributes; live read-only tests verify discovery, response matching, dump completion/interruption, and errors. Report truncated or interrupted results.

- **P14 — `netlink/route`: links, addresses, routes.** Build typed listing and add/change/delete operations over P13. Keep the first operation set bounded; advanced traffic control and every link type are outside this item. Preserve Linux indices, flags, and identifiers where useful. **Acceptance:** live read-only listing plus isolated network-namespace tests verify mutation and notifications. Use private test fixtures before P15 exists. Do not imply atomic snapshots or multi-operation transactions.

### Namespaces

- **P15 — `namespace`: handles and explicit namespace operations.** Implement handle acquisition, inspection, comparison where supported, and the supported entry/creation operations. Explicitly distinguish calling-thread effects, child-process effects, and restrictions in a multithreaded process. Any scoped callback API needs a proved restoration/failure model and documented restrictions on goroutines. Do not require such an API merely for convenience. **Acceptance:** subprocess integration tests verify the supported namespace types, thread affinity, restoration if offered, and cleanup on failure/panic. Verify that no changed thread state leaks into unrelated Go work. Update prior test helpers to reuse this package only when that actually simplifies them.

`setns` is thread-scoped and has namespace-specific restrictions; locking a Go thread does not make every namespace transition legal. Consult [setns](https://man7.org/linux/man-pages/man2/setns.2.html) and [runtime.LockOSThread](https://pkg.go.dev/runtime#LockOSThread).

### Resource control

- **P16 — `cgroup`: unified resource control.** Implement v2 hierarchy/group handles, create/remove, membership, events, and a bounded initial controller set: CPU, memory, and PIDs. Keep delegation and controller enablement explicit. Do not assume the library owns the hierarchy root or that systemd-created groups can be changed arbitrarily. **Acceptance:** fixture tests verify file formats, and live tests in a delegated subtree verify lifecycle, moving a child, limits, events, and cleanup. Test unavailable controllers and invalid delegation. No v1 or systemd D-Bus backend.

### Security facilities

- **P17 — `capability`: Linux capability sets.** Implement inspection and explicit supported set/clear operations, including the chosen permitted/effective/inheritable, bounding, and ambient operations. Separate thread state from exec transitions and any process-wide claims. **Acceptance:** subprocess tests verify allowed transitions, expected failures, and exec behavior. Document Go runtime/thread constraints; a locked goroutine alone does not apply a capability change to every thread.

- **P18 — `seccomp`: syscall filters.** Implement filter loading, the supported actions, and explicit synchronization policy. Start with a bounded representation of kernel filter programs; a policy language/compiler and user-notification broker are later extensions. Keep architecture validation and Go runtime syscall needs visible. **Acceptance:** subprocess tests verify allowed/denied operations, architecture checks, error propagation, and thread synchronization where promised. Never apply irreversible test restrictions to the main test runner.

- **P19 — `landlock`: access rulesets.** Implement ABI discovery, ruleset lifecycle, baseline-supported filesystem and network rules, and restriction application. Represent newer rights explicitly. Preserve the caller's requested policy; do not silently discard unsupported restrictions. Document exactly which threads/process descendants are covered. **Acceptance:** subprocess tests verify allowed/denied access, ABI negotiation, rule ownership, unsupported rights, and enforcement scope. Do not claim to restrict an entire multithreaded Go program without verified kernel support.

Reference [capabilities](https://man7.org/linux/man-pages/man7/capabilities.7.html), [seccomp](https://man7.org/linux/man-pages/man2/seccomp.2.html), and [Landlock](https://docs.kernel.org/userspace-api/landlock.html). These packages expose mechanisms; they do not constitute a complete sandbox or container runtime.

### System information

- **P20 — `procfs`: process and system information.** Accept an explicit filesystem root for fixtures and alternate proc mounts. Start with process identity/statistics, descriptor information, memory maps, mount information, and basic CPU/memory statistics. Avoid a catch-all parser for every proc entry. Document races, permission failures, and disappearing processes; multiple reads do not form an atomic snapshot. **Acceptance:** fixture tests cover awkward names, malformed/truncated input, changing fields, and absent entries. Live read-only tests cover current-process and basic system information. Avoid exposing host-sensitive fixture data.

- **P21 — `sysfs`: bounded device and kernel attributes.** Accept an explicit root. Start with read-only block-device and network-interface attributes and discoverable identifiers. Keep values typed where semantics are stable; retain unknown attributes without pretending all files share one format. Broad mutation and driver-specific control remain deferred. **Acceptance:** fixtures cover symlinks, missing attributes, units, and device disappearance. Live read-only tests verify available attributes without assuming a particular host device inventory.

### Integration and release preparation

- **R01 — Verify the complete supported surface.** Audit all 21 packages for consistent naming, standard-interface behavior, ownership, errors, concurrency, feature requirements, and actual kernel-floor compatibility. Review public APIs against standard-library overlap and actual usage. Exercise cross-package examples such as memfd plus mmap and eventfd or timerfd plus epoll. Finish required kernel/architecture coverage with the selected Go image toolchain and required package-specific isolated privileged tests. Add benchmarks only for specific performance claims or unresolved design choices. **Acceptance:** required checks have recorded results, known defects are fixed, and no skipped or compile-only run is represented as runtime coverage. Gaps remain explicitly outstanding; they do not disappear at the release boundary. Reconcile every verification-gap entry and implemented-but-unverified item.

- **R02 — Prepare v1 documentation and release materials.** Write the package index, getting-started examples, support matrix, compatibility policy, feature requirements, and contributor guidance. Record intentional omissions, deferred extensions, and unresolved limitations. Review exported API stability and dependency/licensing requirements. **Acceptance:** another contributor can use and verify the library from the repository documentation. Prepare release notes and a concrete release candidate. Publishing or tagging follows the user's actual authorization.
