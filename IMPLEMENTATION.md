# Linux APIs for Go: implementation sequence

Next: **E06 — Complete extended file metadata**.

Resume note: E05 passed the required rootless file suite directly on ext4 and in the pinned-profile container on native amd64 Linux 7.3. Confirm the final E05 revision's required CircleCI jobs, including successful POSIX and OFD record-lock scenarios on native Linux 7.0 amd64 and arm64 and merged coverage, before starting E06.

## Objective and scope

Build idiomatic, detailed Linux building blocks for servers, databases, storage engines, container runtimes, and orchestration/node agents. A contributor should be able to assemble the Linux side of a Docker-like runtime or Kubernetes-like node agent from these facilities and suitable existing libraries. The library supplies kernel mechanisms; application protocols, database algorithms, image distribution, cluster scheduling, and reconciliation policy belong to their respective applications.

Completeness means faithfully covering the selected application and container mechanisms. Each addition needs a concrete storage, memory, IPC, process, isolation, resource-control, or workload-observation scenario; the existence of another Linux API alone does not expand the scope. Consumer-demand evidence is not required. Extend existing facilities before adding the subsystems below, preserve current public packages, and keep technical exclusions explicit.

Own the public APIs and prefer implementing the small facility operations that fit the existing code. Reuse maintained libraries when they can supply substantial behavior without compromising the public contract; this is an implementation choice, not a requirement to rebuild their internals. Keep `golang.org/x/sys/unix` as the syscall/ABI foundation and the existing Prometheus procfs dependency behind the project-owned API. A binding in `syscall` or `x/sys/unix` is welcome groundwork for an idiomatic resource API. Reuse ordinary `os`, `net`, `time`, `sync`, and `os/exec` behavior rather than adding equivalent application APIs.

Include focused eBPF support for workload network accounting and process-event tracing, backed by the [Cilium eBPF library](https://github.com/cilium/ebpf) as a long-term internal dependency. Own the public loading, map, attachment, and event-reader contracts and their integration with existing facilities; reuse Cilium's loader, relocations, BTF handling, and kernel plumbing without planning a replacement implementation. Applications supply compiled BPF programs, data formats, and policy. Keep compilation tools, a general tracing product, XDP/TCX networking, LSM policy, and persistent pin management outside the initial scope. Classic BPF used by the existing seccomp facility remains separate.

Low-level perf counters, userfaultfd, and keyrings below have specific workload-measurement, checkpoint/paging, and credential-isolation roles; they do not authorize building a complete profiler, checkpoint engine, or secret-management product.

This file is the temporary execution queue for contributors and coding agents. It follows the unfinished-work-only pattern of the former `IMPLEMENTATION.md`, visible in Git history through `59ae631`; that file was removed in `e61eefa` after its final work shipped. Keep `IMPLEMENTATION.md` as the single current queue. The [support coverage index](docs/support.md#coverage-index) describes shipped capabilities and composition boundaries; this queue owns future deliverables.

## How to execute one item

1. Read [AGENTS.md](AGENTS.md), `bin/AGENTS.md`, the applicable skills, and the [accepted-design index](docs/agents/accepted-design.md), including the API conventions and relevant area files. Inspect the affected package's code, examples, tests, and usage guide, and preserve unrelated working-tree changes. The module path and Go floor come from [go.mod](go.mod); retain Linux 7.0+ and native amd64/arm64 support.
2. When asked to continue this plan, implement the first remaining item, subject to an unresolved resume note. Work sequentially and keep surviving item IDs stable after deletions. Do not start later items while the current item's required implementation or verification is unresolved. If an item needs smaller deliverables, split its remaining scope into adjacent, clearly named subitems before continuing; retain every promised operation and acceptance requirement.
3. Recheck the item against current code and Linux 7.0 UAPI. Sketch a short Go caller and the package dependency direction before choosing exported names or tests. New package names below are proposed homes, to be settled from that caller before the package's first release. Keep production package names lowercase without underscores. Preserve existing v1 APIs, defaults, ownership, errno identity, and blocking behavior; a necessary incompatible change needs an explicit migration decision.
4. State the concrete application/container scenario, the operations being added, the validation scenarios, and the build-versus-reuse decision described below. Routine choices within this plan do not need a new consumer-demand discussion. Record significant implementation/dependency choices in the facility's design note. Do not add future dependencies or package stubs, replace an existing backend wholesale merely for consistency, or leak dependency types and raw descriptors through the public API.
5. Deliver the item's public operations, tests, executable examples, usage documentation, and rationale together. Follow the existing Go test harness: external test packages, `require` assertions, readable behavior scenarios, and reusable/native setup in `internal/test`. Keep Linux constants, errno aliases, native fixtures, and independent syscall observations out of package test files. Keep real kernel/ABI evidence distinct from parser fixtures and source review.
6. Run the narrowest supported local checks and the appropriate isolated live suite. Fix scoped failures. Classify missing tools, runner prerequisites, and policy denials before changing the implementation or skipping tests. Update the required CI coverage in the same item when current jobs do not exercise the new behavior.
7. Delete a finished item's entire section in the implementation change and update `Next`. That deletion is provisional until all applicable required CI checks pass for the final implementation revision. While CI is pending or failing, keep a short resume note identifying the item and missing evidence; the next agent must resolve it before starting the next item. Never describe pending CI, compilation, or skipped tests as completed runtime verification.
8. Once the final revision is verified, remove the resume note. Keep no completed checklist, historical handoff log, or archive here; Git history preserves those details. Put durable behavior in package docs, concise environment evidence in [verification](docs/verification.md), known execution limits in [verification limits](docs/verification-limits.md), and detailed run output in CI artifacts. Publishing, tagging, remote writes, and sub-agent use follow the user's actual authorization and repository instructions.

## Capability and reuse map

These scenarios set the scope; they do not require this module to implement an entire runtime, orchestrator, or database. Existing namespaces, capabilities, Landlock, pidfd lifecycle, sockets, counters, timers, and filesystem observers remain useful foundations. Reuse standard Go networking and process startup when assembling the examples.

| Items | Application or container outcome and home | Required verification route |
| --- | --- | --- |
| E06–E07 | Extend `file` for metadata and temporary/direct-I/O files. | Rootless file suite and isolated filesystem fixtures for filesystem-specific operations. |
| E08–E10 | Extend `mmap` for synchronized mapped indexes, locked buffers, residency, resizing, and advice. | Rootless mmap suite with bounded fault/limit children; filesystem fixtures where required. |
| E11–E13 | Extend `mount` for discovery, configuration forms, propagation/ID mapping, and private root activation. | Existing isolated mount/native lane, including actual UID/GID translation and root switch. |
| E14 | Extend `pidfd` to recover a supervised child's open file. | Rootless child suite where permitted; extend namespace isolation when ptrace/LSM policy requires it. |
| E14a | Add orphan adoption and reaping for worker trees and container init; proposed `process` home, retaining `pidfd` for identified targets. | Rootless bounded subreaper children plus PID-namespace init scenarios in the namespace lane. |
| E15–E16 | Extend `seccomp` for syscall mediation and target file injection. | New required notification lane with real supervisor/target exchange and isolated container/native paths. |
| E17–E19 | Extend `netlink/route` for private veth/bridge networks, neighbor/rule policy routing, lookups, and multiple next hops. Evaluate vishvananda/netlink internally. | Existing combined Netlink/native lane with independent connectivity and routing observations. |
| E20–E23 | Extend `cgroup` for workload stop/kill, I/O/cpuset, deeper CPU/memory/PIDs observations, and configured hugetlb/RDMA/misc/dmem controllers. Evaluate containerd/cgroups internally. | Existing delegated cgroup/native lane; disposable storage and configured hardware fixtures for enforcement. |
| E23a | Extend `cgroup` to start a standard Go command directly in its owned delegation. Reuse Go's clone-into-cgroup startup internally. | Existing delegated lane, observing membership and accounting from the child's first action. |
| E24 | Proposed `sched`: place worker threads on permitted CPUs. | Rootless bounded affinity children and native architecture lanes. |
| E25 | Extend `sched` for worker policy, deadline budgets, and I/O priority. | Rootless permitted transitions plus a new explicitly selected required scheduling lane for privileged policies. |
| E26 | Proposed `rlimit`: constrain worker resources and inspect atomic prior limits. | Rootless bounded children; explicitly isolated privileged transitions. |
| E27–E29 | Proposed `uring`: asynchronous storage requests, registered resources, cancellation, linked operations, and readiness. Implement with existing bindings and liburing as reference. | New required uring/native lane exercising actual queues and GC/lifetime stress. |
| E30–E31 | Proposed `perf`: bounded workload counters/groups and sampling records. | New required perf/native lane with software events; hardware PMUs only on configured runners. |
| E32–E33 | Proposed `bpf`: Cilium-backed cgroup network accounting and process-event tracing composed with `cgroup`, `procfs`, and `pidfd`. | New required eBPF/native lane with private networking/delegation and actual compiled program fixtures. |
| E36–E37 | Proposed `userfaultfd`: lazy paging and selected checkpoint integration primitives. | New required fault-handling/native lane with bounded faulting children and provisioned memory-type variants. |
| E38–E39 | Proposed `keyring`: private worker keys, requests, expiry, and selected key controls. | New required keyring/native lane with private session rings and controlled request helpers when selected. |
| E40–E41 | Proposed `mqueue`: cross-process priority messages, timing, and readiness. | New required IPC/native lane inside private IPC namespaces. |
| E42–E44 | Proposed `sysvipc`: typed messages, atomic semaphore batches, and shared-memory attachment lifetimes. | Extend the required IPC/native lane with independent processes and bounded waits/faults. |
| E45 | Compose storage/worker/container lifecycles; retain standard Go startup, external NAT/firewall policy, and optional CRIU composition. | Combined explicitly isolated lifecycle scenario, then all applicable native baseline CI lanes and coverage merge. |

Every new lane above requires the local/container/CI wiring and reports specified under Local verification and CI before its first item can complete. These are verification routes to implement, not commands already available. IDs E34–E35 remain unused; surviving IDs are stable.

Before implementing an item, inspect the maintained upstream APIs/source and choose one of these routes:

1. **Implement here using existing bindings** when the operation is small, its Linux semantics are clear, and an additional dependency would add more cost than implementation work. Do not recopy syscall tables or ABI structures already maintained by `x/sys`.
2. **Use a dependency internally** when it supplies substantial implementation work and fits the required contract. Keep the project-owned API, validation, lifetime, and error guarantees; do not retest upstream internals merely to raise coverage.
3. **Compose with an external library/tool** when it already provides the complete capability and another project-owned API would add no meaningful resource or semantic contract. Explain the composition and any genuinely missing integration rather than creating a competing product. NAT/firewall policy and complete checkpoint/restore are examples for this sequence.

For a proposed dependency, check current maintenance/release status, license, transitive dependencies, Go/Linux/architecture floors, cgo/native-tool requirements, descriptor ownership, namespace/thread behavior, cancellation, buffer lifetime, and errno fidelity. Inspect the actual operations required by the active item; a similar package name or README is insufficient. Declare the selected module version in `go.mod`, refresh dependencies through `make dep`, update dependency/license documentation, and test the project-owned integration on native architectures. The table distinguishes evaluation candidates, selected backends, and external composition; no entry authorizes adding an unused dependency.

| Area | Implementation or reference | Integration boundary |
| --- | --- | --- |
| Linux bindings | [golang.org/x/sys/unix](https://pkg.go.dev/golang.org/x/sys/unix) | Existing foundation; keep descriptor plumbing internal and preserve Linux errors. |
| procfs/sysfs parsing | [prometheus/procfs](https://github.com/prometheus/procfs) | Existing private dependency; retain project-owned records and the current read-only/ownership contracts. |
| Routing and virtual links | [vishvananda/netlink](https://github.com/vishvananda/netlink) | Evaluate complex link, neighbor, rule, and nexthop support for E17–E19 against the existing transport, namespace capture, deadline, and ACK/loss contracts. Reuse suitable implementation rather than rebuilding it solely to own the code. |
| Cgroup v2 | [containerd/cgroups](https://github.com/containerd/cgroups) | Evaluate v2 controller/resource operations for E20–E23. Preserve explicit delegation and prevent implicit systemd/ancestor/host-policy changes. |
| Packet filtering and NAT | [google/nftables](https://github.com/google/nftables) | Compose for container/service networking beyond routing. A route API does not provide NAT/firewall policy; do not claim otherwise or silently build a policy engine into `netlink/route`. |
| io_uring | Existing `x/sys/unix` bindings where available and upstream [liburing](https://github.com/axboe/liburing) as a reference | Implement queue/ABI work internally for E27–E29; do not use `giouring`. Establish Go buffer pinning, completion, concurrency, and close guarantees. liburing is an implementation/reference source, not permission to introduce a C runtime dependency. |
| Seccomp | Existing project filter code; [libseccomp-golang](https://github.com/seccomp/libseccomp-golang) for comparison | Compare notification support for E15–E16, including native-library/cgo requirements. Preserve the current pure-Go support contract and existing filter semantics; an external binding is not automatically a suitable backend. |
| Container lifecycle | [OCI Linux configuration](https://github.com/opencontainers/runtime-spec/blob/main/config-linux.md) and [runc](https://github.com/opencontainers/runc) | Use as contract/implementation references for isolation, resources, credentials, and root filesystems. Do not add a whole container runtime as a dependency merely to expose an individual kernel operation. |
| Complete checkpoint/restore | [go-criu](https://github.com/checkpoint-restore/go-criu) and CRIU | Compose through the supported checkpoint interface if needed. E36–E37 own fault-handling primitives; they do not implement full process checkpoint/restore or make an external CRIU installation a default library requirement. |
| eBPF | [cilium/ebpf](https://github.com/cilium/ebpf) | Selected long-term internal backend for E32–E33. Keep dependency types and descriptors private; own resource lifetimes, cgroup integration, verifier diagnostics, and bounded event reading. Reuse upstream loading/BTF/relocation support. |

If a candidate cannot preserve a required contract, state the concrete mismatch and implement only the missing portion using the existing architecture. Do not silently weaken ownership, namespace isolation, cancellation, or support policy in order to reuse it. Conversely, do not reject a suitable library merely because a direct implementation is possible. Reuse decisions are complete only when the selected adapter/composition is concrete and its project-owned behavior is verified.

## Shared acceptance requirements

- Every implementation item owns a coherent callable capability; E01 and E45 instead own the inventory and final reconciliation. A constructor, constants-only package, thin dependency re-export, or untested syscall wrapper is insufficient. Identify supported operations, flags, returned fields, and omissions; preserve unknown kernel output where the facility permits it. Do not silently emulate atomic operations, weaken restrictions, retry unsafe mutations, or raise the kernel floor.
- Descriptors and syscall plumbing remain internal. Use concrete facility methods and appropriate standard Go interfaces. Creation/adoption failure returns no resource and cleans up everything acquired or consumed; borrowing leaves inputs open. Keep explicit close, persistent kernel effects, and object removal distinct. Document concurrency, partial application, record boundaries, clocks, and zero/closed behavior.
- Do not promise contexts or deadlines without a mechanism that interrupts the actual operation. Long-lived kernel access to buffers requires explicit lifetime and Go memory-pinning rules. Retaining a Go value alone does not prevent an explicitly closed mapping from being unmapped. Closing a local handle does not automatically cancel every kernel effect or release every independently retained reference.
- Extend the authoritative usage guide and GoDoc. New packages require `doc.go`, a README package-guide link, executable examples, and a focused area file under `docs/agents/accepted-design/` linked from the index. Update the support guide and package count as capabilities actually ship. Do not rewrite procfs/sysfs or rebuild ordinary `os`, `net`, `time`, `sync`, or `os/exec` functionality as part of this expansion.
- Exercise success, invalid input, meaningful errno, ownership/cleanup, close/lifetime, relevant concurrency, and partial failures through public operations. Use bounded subprocesses for irreversible changes, resource-limit changes, thread-affine mutations, faults, and potentially stuck waits. Parser/fuzz fixtures should target genuine record-decoding risks; benchmarks need a specific performance question. Assess test readability and boundary fit before finishing.
- Each item inherits the local/CI procedure below. A feature-specific unsupported filesystem, kernel configuration, or runner policy remains visible in prerequisites and reports. An unavailable selected required scenario must fail; a genuinely newer optional feature may skip only under its documented version/feature rule. No broad `EPERM`, `EINVAL`, or `ENOSYS` skip is acceptable.

## Local verification and CI

[Verification](docs/verification.md), the root [Makefile](Makefile), and [.circleci/config.yml](.circleci/config.yml) own the current commands and environment policy. Rediscover them with `make help` for each item. Run from the repository root using the configured toolchain. Use `make dep` when dependency/vendor state needs refreshing; dependency setup, image acquisition, and security scanners can require network or registry access.

Start with the affected package, for example:

```sh
make test-environment
make specs package=file profile=rootless required=true arch=amd64
make lint package=file
make vet
```

Use the actual native architecture. Inspect target scope: `package` scopes the specs harness, while some lint components and `make vet` inspect the module. Run `make sec` for security-sensitive code/dependency changes, `make scripts-lint` for launcher changes, and `make ci-validate` for CI changes when the configured CLI/API access is available. Report unavailable checks explicitly. There is no requirement to rerun the entire local test suite for each item; CI owns the complete matrix.

Local development may have more useful privileges through Docker than through the agent shell. A shell namespace denial is not proof that local isolated verification is unavailable. Check daemon access and use the existing facility launchers before leaving a local privilege blocker. Do not assume that a rootless Docker daemon grants initial-user-namespace capabilities; prove the selected operation inside the actual container. Keep host resources unchanged: use private namespaces, test-owned files/devices/IPC objects, bounded children, and explicitly disposable cgroup delegations.

Use the Go image from [CircleCI](.circleci/config.yml), currently `alexfalkowski/go:5.27`. These commands already exist:

```sh
make verify-rootless-container image=alexfalkowski/go:5.27 arch=amd64 package=file
make verify-mount-container image=alexfalkowski/go:5.27 arch=amd64
make verify-netlink-container image=alexfalkowski/go:5.27 arch=amd64
make verify-namespace-container image=alexfalkowski/go:5.27 arch=amd64
make verify-cgroup-container image=alexfalkowski/go:5.27 arch=amd64
```

Select the matching launcher; these are alternatives for different scopes, not a checklist to run for every item. The existing fanotify and clock-change launchers remain available for changes affecting those packages. Clock changes require an explicitly disposable VM. Container launchers perform dependency setup and can write vendor/report files; use a writable disposable checkout if necessary, without changing ownership of unrelated work.

Use `kernel=supported` for local runs on any supported newer kernel. Add `kernel=7.0` on an actual Linux 7.0 host to collect exact-baseline evidence. A container image does not supply a different kernel. The rootless, Netlink, namespace, cgroup, and mount CI matrices require exact Linux 7.0. All required native amd64 and arm64 matrices remain gates. New core facility lanes must also establish the Linux 7.0 baseline on both native architectures. Newer optional-feature runs supplement those results.

For a new facility whose live operations do not fit the existing lanes, its first implementation item must add a repository-defined verification target and matching local/container/CI path. Future names such as `verify-uring-container` are proposals, not currently available commands. Extend `internal/testenv`, explicit profile selection, container prerequisites, `.circleci/config.yml`, script lint inputs, and `scripts/coverage-workspace` together where needed. Keep new live suites selected deliberately so ordinary rootless runs do not acquire privileges or mutate shared state. Use narrowly justified syscall permissions; do not weaken the pinned rootless profile globally to obtain a pass.

New required native jobs must retain environment JSON, test events, JUnit, and raw atomic coverage, participate in coverage staging/merging, and gate the existing downstream workflow. Reports stay directly in `test/reports/`; use filename prefixes when retaining multiple local runs. Do not leave newly added jobs out of the coverage job's dependencies or merger facility list. A representative software/kernel scenario must run even where optional hardware or configuration-specific variants are unavailable. If no available runner can establish a promised core operation, keep that item pending and identify the required runner.

## Remaining work

### Existing file and memory facilities

#### E06 — Complete extended file metadata

- **Implement:** Extend `file.Metadata` and its masks with remaining baseline [statx](https://man7.org/linux/man-pages/man2/statx.2.html) fields: direct-I/O memory/offset/read alignment, subvolume identity, atomic-write minimum/maximum/optimized-maximum units and segment limits, and the named atomic-write attribute. Check the versioned structure and pinned `x/sys` binding before choosing representation. Observing limits does not itself add atomic data-write operations.
- **Contracts:** Preserve validity masks independently of zero values, Linux integer widths, units, and unknown output bits. Do not reinterpret unavailable fields as supported zero limits or silently change existing mask/default behavior.
- **Evidence:** Extend focused native comparisons and public metadata scenarios. Supply suitable filesystem fixtures for positive fields where possible; distinguish a supported query returning an unavailable field from a positive observation of that field's semantics.

#### E07 — Extend Linux-specific open and rename controls

- **Implement:** Extend `file` for `O_TMPFILE`, direct-I/O opening, explicit `O_SYNC`/`O_DSYNC` opening, and rename whiteouts where the baseline/filesystem supports them. Add the concrete operation needed to link an unnamed temporary inode into a directory; retain the standard-library boundary for ordinary path operations.
- **Contracts:** Preserve permission/umask behavior, unnamed-inode ownership, direct-I/O alignment requirements, atomic rename modes, and whiteout privilege/filesystem restrictions. Keep current zero options and path-resolution guarantees unchanged. Do not expose descriptor plumbing to complete the workflow.
- **Evidence:** Demonstrate temporary creation/write/link/cleanup, permitted aligned direct I/O, invalid alignment behavior, and whiteout identity through isolated filesystem/namespace fixtures. A normal rename is not positive whiteout evidence.

#### E08 — Add mapped-file synchronization

- **Implement:** Add explicit whole-region [msync](https://man7.org/linux/man-pages/man2/msync.2.html) operations to `mmap`, with named synchronization/invalidation choices and a documented view-lifetime contract. Keep `Close` independent from flushing.
- **Contracts:** Distinguish synchronous writeback, asynchronous semantics, invalidation, shared versus private mappings, and invalid combinations. State precisely what successful synchronization guarantees without claiming transactions or universal crash consistency.
- **Evidence:** Exercise shared mapped writes and actual synchronization, invalid options, private/anonymous behavior, close races, and preserved source-file independence. Verify applicable errno and cleanup without adding raw address access.

#### E09 — Add mapped-memory locking and residency inspection

- **Implement:** Add region-scoped [mlock/mlock2/munlock](https://man7.org/linux/man-pages/man2/mlock.2.html) and [mincore](https://man7.org/linux/man-pages/man2/mincore.2.html) operations. Represent page residency as a snapshot with explicit page units; support on-fault locking only when actually available.
- **Contracts:** Keep mapping lifetime, locked-page accounting, alignment, memory limits, and on-fault semantics distinct. Memory locking is not a complete secret-storage policy. Do not introduce process-wide `mlockall` effects through a region method.
- **Evidence:** Use bounded regions and child-local memory limits to verify successful locking, unlock, residency shape, limits/errors, and close cleanup. Avoid global memory pressure, host swap changes, and assumptions that an unlocked page must immediately become nonresident.

#### E10 — Add mapped-region resizing and further advice

- **Implement:** Add a deliberate [mremap](https://man7.org/linux/man-pages/man2/mremap.2.html) operation and expand typed `madvise` choices against the baseline: lazy free/removal, fork/dump controls, merge/huge-page advice, cold/pageout, read/write population, locked-page discard, and guard installation/removal where their mapping requirements can be represented. Preserve the original mapping if remapping fails and maintain the pinned `x/sys` mapper's bookkeeping correctly. Explain concrete unsupported combinations; hardware poison/offline and remote-process advice remain excluded because they exceed this region owner's scope.
- **Contracts:** Define when all existing slices/pointers become invalid and how callers obtain the new view. Do not approximate remapping by reslicing. Fixed-address or alias-producing modes require a separately sound ownership contract; retain explicit exclusions for modes the region owner cannot represent safely.
- **Evidence:** Verify grow/shrink, retained contents, failed-remap ownership, view lengths, and final unmapping in bounded children where stale access can fault. Exercise added advice without promising kernel heuristics or testing undefined stale-slice use in the main test process.

### Existing mount, process, and security facilities

#### E11 — Add structured mount discovery

- **Implement:** Extend `mount` with [listmount](https://man7.org/linux/man-pages/man2/listmount.2.html) traversal and [statmount](https://man7.org/linux/man-pages/man2/statmount.2.html) records. Include bounded variable-sized results, pagination, unique mount identities, available masks, and namespace selectors supported by the baseline.
- **Contracts:** Keep mount observations distinct from handle ownership and procfs mountinfo identifiers. Preserve unknown fields/bits where possible and document races during traversal; separate calls are not an atomic mount-tree snapshot.
- **Evidence:** Create a private mount tree through the existing mount fixture and independently compare identity, parentage, paths, attributes, pagination, and disappearance. Include malformed record/parser fixtures and bounded retry behavior without replacing live discovery evidence.

#### E12 — Extend filesystem configuration contexts

- **Implement:** Complete the concrete modern mount-configuration parameter forms needed beyond strings/flags: binary, path, descriptor parameters, and reconfiguration through the baseline `fsconfig`/`fspick` interfaces. Reuse the existing diagnostic queue/error ownership.
- **Contracts:** Define copied versus borrowed parameter lifetimes, context phase transitions, permitted reuse, partial state changes, and diagnostic retrieval. Do not silently translate arbitrary legacy comma-separated option strings or fall back to legacy mounting.
- **Evidence:** Use supported filesystems in private namespaces to demonstrate the actual selected parameter forms and reconfiguration. Preserve primary errno when diagnostics fail, reject invalid inputs before mutation, and prove parameter/context cleanup on failure.

#### E13 — Add mount attributes, ID mapping, and root activation

- **Implement:** Extend typed mount attribute changes for supported atime policies, propagation, and ID-mapped mounts. Add a concrete [pivot_root](https://man7.org/linux/man-pages/man2/pivot_root.2.html) operation for activating a prepared container root and composing with existing unmount cleanup. Borrow typed namespace owners through concrete operations; keep descriptors private and settle dependency direction before adding integrations. Split attribute work and root activation into adjacent subitems if they exceed one reviewable change.
- **Contracts:** Preserve recursive-operation semantics, incompatible attributes, namespace ownership, filesystem support, and possible partial application. Distinguish root-mount switching from standard Go child chroot configuration. Document root/cwd and process-wide effects, old-root lifetime, namespace prerequisites, and failure state; this is not a temporary goroutine-local filesystem scope. Use standard Go child/reexec startup and do not add raw fork/clone wrappers.
- **Evidence:** Verify attributes, propagation, and actual UID/GID translations on test-owned files, then switch/detach the old root inside a bounded private-mount-namespace child and demonstrate that parent lookups remain unchanged. Root/capability observations alone are insufficient. Extend the existing required mount/native lanes and keep fixture/report access working after the child root switch.

#### E14 — Duplicate files through process handles

- **Implement:** Add a concrete [pidfd_getfd](https://man7.org/linux/man-pages/man2/pidfd_getfd.2.html) operation on `pidfd.Process`. A target process's file number is target identity input; return an independently owned standard `*os.File` without exposing the local pidfd.
- **Contracts:** Preserve ptrace/LSM permission checks, target exit races, close-on-exec, shared offsets/status flags, and unconditional failure cleanup. Duplication must not steal or close the target's original file. Do not reopen through procfs as a fallback.
- **Evidence:** Coordinate a test-owned child to demonstrate reading the same open description, shared offsets, independent closure, invalid target numbers, denied access, process exit, and local close races. Add required isolated verification if the ordinary rootless policy rejects the syscall.

#### E14a — Add orphan adoption and reaping controls

- **Implement:** Add the Linux-specific supervision primitives missing from identified-target `pidfd` waits: inspect/set child-subreaper state and reap adopted children with returned PID and exit status. The proposed `process` package must complement standard `os/exec` and `os/signal`; settle its caller and dependency direction before export. Use the baseline [prctl controls](https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/prctl.h) and [wait selectors](https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/wait.h) through maintained bindings. Keep signal-forwarding and restart policy with the application.
- **Contracts:** Subreaper state is process-wide; namespace PID 1 already has orphan-adoption duties. Designate one reaper and prevent examples from racing wait-any operations with `Cmd.Wait` or `pidfd.Process.Wait`. Preserve no-ready-child versus no-child results, reparenting, interruption, and PID reuse. Do not claim a cancelable blocking wait without an actual interruption mechanism or install raw signal handlers.
- **Evidence:** Use bounded process trees to prove orphan adoption, actual zombie collection, subreaper state/inheritance, and parent isolation. Extend the required namespace lane to demonstrate PID 1 descendant reaping and signal forwarding through standard Go signal handling. No test may reap unrelated runner children or alter the parent runner's subreaper state.

#### E15 — Add seccomp notification listeners and responses

- **Implement:** Extend `seccomp` with USER_NOTIF filters, new-listener loading, notification-size negotiation, owned listeners, receive/respond, and ID validation using the [notification ABI](https://man7.org/linux/man-pages/man2/seccomp_unotify.2.html). Preserve existing filter-loading behavior and its synchronization rules.
- **Contracts:** Keep notification IDs, target TIDs, scalar arguments, target exit, response errno/value encoding, and interruption explicit. Establish genuine close/interruption behavior before adding deadlines. Continuing a syscall does not provide a race-free pointer/path authorization mechanism.
- **Evidence:** Use bounded supervisor/target processes for actual interception, synthetic returns/errors, continuation, stale IDs, target exit, and listener cleanup. Required CI must exercise live notification exchange, not only filter construction; use isolated security verification without filtering the parent test runner.

#### E16 — Add seccomp notification file injection

- **Implement:** Add typed notification `ADDFD` operations, including baseline-supported atomic add-and-respond behavior and explicit target-file placement choices. Borrow the source file and preserve listener/target ownership.
- **Contracts:** Handle vanished notifications, occupied target positions, close-on-exec, partial success, and shared open descriptions. Keep injection and response atomic only when Linux supplies that operation. Do not promise rollback after successful injection.
- **Evidence:** Have the intercepted target use the injected file, observe resulting flags/offset behavior independently, and verify stale-ID/error cleanup. Exercise the atomic path and ordinary separate response path in the same required notification lane.

### Existing network and cgroup facilities

#### E17 — Add virtual links, bridges, and namespace moves

- **Implement:** Extend `netlink/route` beyond dummy links with veth pair creation, bridge creation and membership, and link moves to a typed network namespace. Preserve original/unknown attributes when observing links and use typed mutation configurations.
- **Contracts:** Express paired-link ownership, namespace-relative indices, rename/move side effects, acknowledgements, and partial failures. Keep concrete namespace integration from creating package cycles or raw descriptor accessors.
- **Evidence:** Extend the existing private-network-namespace lane: create a pair, move an endpoint, configure a bridge, verify membership and connectivity using test-owned endpoints, and clean up from each lifetime boundary. Do not modify the host network or infer link movement from a successful ACK alone.

#### E18 — Add neighbor tables and policy-routing rules

- **Implement:** Add typed list/mutate/listen operations for IPv4/IPv6 neighbors and routing rules, preserving Linux states, masks, selectors, actions, and unknown attributes. Keep namespace selection consistent with the existing connection-creation contract.
- **Contracts:** Define add/replace/delete matching and wildcard behavior, dump interruption, notification loss, and field validity. Do not replay opaque observations as complete mutation requests or hide lossy notifications.
- **Evidence:** Use the existing Netlink container lane for private-interface neighbor lifecycle and rule selection between private tables. Add public parser/fuzz fixtures for variable/nested attributes, and prove state through independent kernel observations.

#### E19 — Add route lookup and multipath/nexthop operations

- **Implement:** Add typed route lookup requests/results, multipath route configuration, and baseline nexthop-object lifecycle. Preserve next-hop weights, families, route/table identity, and nested metadata rather than flattening advanced routes into one gateway/index.
- **Contracts:** Separate a lookup result from an installed route record; keep kernel-selected routes and mutation selectors explicit. Define replacement/delete identity and unsupported encapsulation/metrics fields without silently dropping them.
- **Evidence:** Verify selected paths across private interfaces/tables, multipath and nexthop references, replacement, deletion, and notifications. Run both base Netlink and routing packages through their existing combined required suite; retain malformed-nest coverage separately.

#### E20 — Add cgroup freezing and recursive kill

- **Implement:** Extend `cgroup` with explicit freezer and `cgroup.kill` operations, using the existing delegated-hierarchy ownership and event snapshots. Expose requested versus observed frozen state where the kernel distinguishes them.
- **Contracts:** Preserve asynchronous completion, populated descendants, delegation/topology permissions, and kill scope. Closing a group does not thaw or kill it. A state write alone does not establish that every target task has reached the resulting state.
- **Evidence:** Use disposable delegated groups and test-owned children to observe execution stop/resume and descendant termination. Bound reconciliation/waits, retain cleanup after failures, and run in the existing required cgroup/native lane.

#### E21 — Add cgroup I/O and cpuset controllers

- **Implement:** Add typed I/O limits/weights/statistics and cpuset CPU/memory-node configuration, effective sets, and partition controls present in the baseline. Represent device major/minor identities and sparse CPU/node sets without lossy string shortcuts.
- **Contracts:** Preserve per-device record updates, max/unlimited values, configured versus effective sets, controller delegation, and parent/topology constraints. Do not assume every runner has multiple CPUs/NUMA nodes or an accountably writable test block device.
- **Evidence:** Exercise live configuration/readback in disposable delegations and independent workload effects where the runner supports them. Provision a disposable storage fixture for I/O enforcement; preserve explicit missing evidence if only parsing/readback runs. Use the existing cgroup lane, extended prerequisites, and suitable native hosts.

#### E22 — Deepen existing cgroup controller observations

- **Implement:** Extend CPU, memory, and PIDs coverage with the baseline's remaining useful limits/counters, local versus hierarchical events, memory swap/protection/reclaim controls, and pressure records where exposed. Keep each operation faithful to its native file and units.
- **Contracts:** Preserve optional-field availability, counter scope, kernel rounding, nonblocking/reclaim semantics, and partial multi-file changes. Reading pressure or an event snapshot does not imply lossless monitoring or an atomic hierarchy snapshot.
- **Evidence:** Combine public record parsers with bounded test-owned CPU/memory/PID workloads in delegated groups. Exercise actual effects without exhausting host resources or changing ancestor policy. State precise limits for nondeterministic reclaim/pressure behavior.

#### E23 — Cover additional cgroup v2 controllers

- **Implement:** Add typed operations for remaining baseline controllers identified by E01, including hugetlb, RDMA, and misc/device-memory resources where configured. Keep device/resource identities and unknown observation fields intact; do not create a generic arbitrary-path write escape hatch.
- **Contracts:** Separate kernel/configuration absence, delegation denial, and unsupported controller operations. Discover available resources without changing global pools, hardware configuration, or ancestor delegation. Configuration-specific omissions need technical explanations in the usage guide.
- **Evidence:** Provide record fixtures and actual delegated read/write/enforcement evidence for selected required controllers. Hardware-backed variants need appropriate isolated runners; parser coverage is insufficient to claim those live operations verified. Keep unavailable promised core variants pending rather than declaring this item complete through broad skips.

#### E23a — Start commands atomically in a delegated cgroup

- **Implement:** Add a concrete operation on `cgroup.Group` that starts a caller-configured standard Go command in the captured group using Go's `SysProcAttr.UseCgroupFD`/`CgroupFD` support internally. Borrow the group's identity under lifetime protection; callers must not obtain its descriptor. Keep command I/O, namespace/credential configuration, and ordinary waiting with `os/exec`; do not introduce another process launcher or raw clone wrapper.
- **Contracts:** Preserve command-start failure and pipe cleanup, caller ownership, concurrent group close, explicit delegation, and incompatible preexisting startup settings. A successful child begins in the selected group; do not emulate that guarantee with `Start` followed by `AddProcess`, which permits early execution and charges elsewhere. Document that later migration and hierarchy mutation remain possible.
- **Evidence:** In the existing required cgroup lane, observe membership and a bounded resource charge from the child's first action, before any parent migration could occur. Cover failed startup, removed/inaccessible groups, command reuse rejection, caller-owned pipes, and concurrent close without leaking children or descriptors. Retain ordinary `AddProcess` migration semantics and demonstrate composition with namespace/credential child startup.

### New scheduling and resource facilities

#### E24 — Add typed CPU affinity

- **Implement:** Introduce the proposed `sched` package with typed [CPU sets and thread affinity](https://man7.org/linux/man-pages/man2/sched_setaffinity.2.html). Handle masks larger than a fixed small CPU count and distinguish current-thread operations from operations targeting an explicit TID.
- **Contracts:** Affinity belongs to Linux threads, not Go goroutines. Preserve online/cpuset restrictions, intersection behavior, target lifetime, and errno. Use a dedicated locked goroutine when mutating the current thread and document that contract.
- **Evidence:** In bounded children, choose CPUs from the permitted set, set/query affinity, verify thread isolation, and cover sparse/large masks and invalid targets. Ordinary tests must not change the parent runner's affinity. Establish the package's documentation and required runtime path.

#### E25 — Add scheduling policies and I/O priorities

- **Implement:** Extend `sched` with [scheduling attributes](https://man7.org/linux/man-pages/man2/sched_setattr.2.html), policy/priority inspection and mutation, and explicit I/O-priority operations. Preserve normal, batch, idle, FIFO, RR, and deadline domains where supported; expose size/version negotiation. Preserve observed `SCHED_EXT` policy identity, but compose with an external scheduler for its BPF program/policy rather than expanding E32's network/tracing scope.
- **Contracts:** Keep nice values, real-time priority, deadline runtime/deadline/period, utilization clamps, reset-on-fork, and target thread/group scopes distinct. Do not treat a successful unprivileged policy change as proof of privileged real-time coverage.
- **Evidence:** Use locked, bounded children for permitted transitions, inheritance, rejection, and independently observed policy. Privileged real-time/deadline tests require a disposable runner with appropriate limits/capabilities and conservative bounds; add a matching required lane rather than changing shared host policy.

#### E26 — Add typed resource limits

- **Implement:** Introduce the proposed `rlimit` package with typed resources, soft/hard limits, exact unlimited representation, and [prlimit64](https://man7.org/linux/man-pages/man2/getrlimit.2.html) query/update operations. Preserve the atomic previous-limit result supplied by the kernel.
- **Contracts:** Retain unsigned widths, infinity, cross-process permission checks, irreversible hard-limit reductions, and Linux-specific resources. Do not silently alter Go runtime policy or globally raise limits as package initialization.
- **Evidence:** Use bounded children to lower/inspect limits and demonstrate an actual constrained operation, then verify that the parent is unaffected. Cover invalid resources, soft/hard relationships, inaccessible/exited targets, and standard errno. Ordinary coverage can join rootless CI; privileged transitions need explicit isolated selection.

### New asynchronous I/O facility

#### E27 — Add owned io_uring queues and basic file I/O

- **Implement:** Introduce the proposed `uring` package around `io_uring_setup`, `io_uring_enter`, and negotiated shared queues. Deliver typed asynchronous file reads/writes and completion records with stable request identities, bounded queue capacity, and an explicit submit/wait interface. Any internal draining/cancellation necessary for safe close belongs here; E28 adds the caller-facing cancellation/registration operations.
- **Contracts:** Specify SQ/CQ memory ordering, completion errno, short I/O, queue overflow, file duplication/borrowing, buffer pinning, and concurrent use. Keep kernel pointers, raw SQEs, and local descriptor integers internal. Define how close quiesces in-flight access before releasing buffers or mappings.
- **Evidence:** Prove actual I/O, short/error completions, multiple outstanding requests, queue boundaries, descriptor lifetime, concurrent close, and cleanup. Stress GC while operations are pending. Add required native verification that performs the real syscalls and identifies seccomp/sysctl prerequisites without changing host policy. Use the [io_uring interface](https://man7.org/linux/man-pages/man7/io_uring.7.html) and upstream liburing as primary references.

#### E28 — Add io_uring registration and cancellation

- **Implement:** Add negotiated fixed-file and fixed-buffer registration/update/unregister operations and typed request cancellation. Use `io_uring_register` through owned registration objects or an equally explicit queue-owned lifecycle.
- **Contracts:** Keep registration references distinct from caller files/buffers, account for partially applied registration results, and retain resources until the target operation's terminal completion. Completion of a cancellation request does not by itself prove the target no longer accesses memory.
- **Evidence:** Verify registered I/O, updates/unregister, caller closure, duplicate/error cases, cancellation races, and final quiescence before unpin/unmap. Exercise real kernel cancellation and memory safety under GC/race runs; preserve denial and resource-limit errno.

#### E29 — Extend io_uring operations and composition

- **Implement:** Add typed fsync, vectored I/O, linked-operation/timeout semantics, and readiness/eventfd integration supported by the baseline. Extend network or multishot operations only through concrete typed owners and operation-specific lifetime contracts; inventory remaining opcodes explicitly in the usage guide.
- **Contracts:** Preserve linked failure/cancellation, timeout clock domains, multishot terminal markers when exposed, and completion ordering without inventing ordering across unrelated requests. Avoid a generic opcode interface that makes callers manage unsafe buffers/descriptors.
- **Evidence:** Demonstrate linked success/failure, timeout and cancellation outcomes, bounded waits, nonconsuming readiness, and independent registration lifetimes. Add a benchmark only for a concrete batching/overhead question; do not claim speedups from API shape alone. Use the established required uring lane.

### New workload performance measurements

#### E30 — Add performance counters and groups

- **Implement:** Introduce the proposed `perf` package with owned [perf_event_open](https://man7.org/linux/man-pages/man2/perf_event_open.2.html) counters, typed event configuration, enable/disable/reset, IDs, groups, and read-format decoding for bounded workload measurements. Cover software events as the required portable live scenario; expose hardware/raw events explicitly. Keep symbolization, profile storage/UI, distributed profiling, and BPF program loading outside the package.
- **Contracts:** Preserve task/CPU targeting, inheritance, enabled/running time, multiplexing, group scheduling, and kernel attribute-size negotiation. Do not silently scale counts or equate an unavailable hardware PMU with an unsupported perf facility.
- **Evidence:** Measure a bounded test-owned workload, query/reset/group counters, and compare independently observable group/ID/lifetime behavior. Add a required native perf lane with actual operation probes; do not change host `perf_event_paranoid` or assume a VM exposes every PMU.

#### E31 — Add performance sampling and record streams

- **Implement:** Add owned perf ring mappings, sample selection, ordered record decoding, loss/throttle records, and bounded read/wait behavior. Preserve record headers and unknown output where safe; keep sample layouts tied to the configuration that produced them.
- **Contracts:** Define producer/consumer memory barriers, wraparound, overrun, variable sample fields, timestamp domains, borrowed versus copied records, and close interruption. AUX/hardware tracing needs a separate represented contract if included; otherwise record the omission explicitly.
- **Evidence:** Produce actual software-event samples, verify record/loss handling where reproducible, and fuzz public decoders with independent native fixtures. Bound sampling rates and buffers. Retain source-reviewed limits for kernel records a safe fixture cannot produce and use the required perf lane.

### New eBPF workload mechanisms

#### E32 — Add Cilium-backed loading, maps, and cgroup network accounting

- **Implement:** Introduce the proposed `bpf` package using Cilium internally. Load caller-supplied compiled ELF from `io.ReaderAt`, acquire named programs/maps, and support lookup/update/delete/iteration for the hash, array, and per-CPU variants needed by the examples. Add typed cgroup ingress/egress attachments to account for test-workload traffic through an existing `cgroup.Group`. Deliver the complete load/select/attach/read-counters/close workflow; introduce the dependency with this implementation.
- **Contracts:** Resolve package dependency direction from that caller before exporting names. Keep Cilium types and raw descriptors private; preserve verifier diagnostics and errno. Define collection, acquired program/map, and link lifetimes, including failure cleanup, concurrent close, and detachment. Borrow the group's captured identity without reopening its pathname. Validate map layouts and per-CPU values, preserve update modes and map-type operation limits, and document iteration under concurrent mutation. Load without automatic attachment, pinning, or host-policy changes; compilation remains a build-time application concern and runtime use stays pure Go.
- **Evidence:** Use public operations to account for actual traffic from a bounded child in a private network namespace and test-owned cgroup. Demonstrate counter changes, map mutations, independent owner lifetimes, detachment, rejected loading/attachment cleanup, and retained verifier diagnostics. Keep native observations and reproducible ELF-fixture generation in the existing test support layout. Establish the required native amd64/arm64 Linux 7.0 eBPF lane with explicit syscall, capability, cgroup, and kernel-feature probes, isolated container/local commands, CI coverage merge, and standard reports. Never change host BPF sysctls or weaken rootless policy.

#### E33 — Add tracepoint attachments and bounded BPF event readers

- **Implement:** Extend `bpf` with typed tracepoint attachments and ring-buffer readers for caller-supplied event records. Demonstrate exec/exit tracing for a bounded child workload alongside `procfs` inspection and `pidfd` supervision. Provide deadlines and close interruption through Cilium's reader support; applications own event decoding, filtering programs, and scheduling.
- **Contracts:** Preserve whole records, explicit buffer ownership, and timestamp domains. Define reader/map/program/link lifetimes and concurrent close behavior. Explain filtering scope, PID reuse, observation windows, and non-atomic correlation with procfs; tracing does not provide a stable process handle. Document bounded-buffer loss and expose the example program's failed-output counter rather than implying the reader reports every lost event. Keep BPF event transport distinct from E31's perf sampling API.
- **Evidence:** Observe the selected child's real exec/exit records and identity fields, bounded waits, deadline expiry, close waking blocked reads, and attachment cleanup. Exercise overflow accounting with a controlled producer and bounded buffer. Probe tracing/BTF prerequisites where required by the selected programs, keep attachment scope explicit, and filter test-owned workloads without changing shared tracing configuration. Extend the required eBPF lane from E32 and document both composition examples and the remaining hook/map/persistence exclusions.

### New userspace fault handling

#### E36 — Add userfaultfd registration and missing-page resolution

- **Implement:** Introduce `userfaultfd` with owned creation, API/feature negotiation, registration tokens, event records, unregister, and missing-page copy/zero/wake resolution for controlled lazy paging/checkpoint integration. Start with user-mode-only faults and document any later privileged mode explicitly. Evaluate complete checkpoint/restore integration through existing CRIU interfaces separately; do not recreate that engine in this package.
- **Contracts:** Keep registered mapping lifetimes and permitted memory regions explicit; descriptor closure alone must not release memory still accessed by faulting threads. Prevent resolver self-deadlock by separating faulted memory from event/resolution buffers. Preserve range/alignment, copy progress, and event identity.
- **Evidence:** In bounded subprocesses, fault on test-owned mapped pages, service the actual event, and observe resumed execution and contents. Verify negotiated rejection, unregister/close behavior, target exit, and failed-resolution cleanup. Add required native verification using actual [userfaultfd operations](https://docs.kernel.org/admin-guide/mm/userfaultfd.html), without changing host sysctls.

#### E37 — Add userfaultfd write protection and further events

- **Implement:** Extend negotiated support for write-protect faults, minor faults, and fork/remap/remove/unmap events available for the selected memory types on the baseline. Handle child fault descriptors through typed owners with failure cleanup.
- **Contracts:** Preserve per-memory-type feature support, event ordering, write-protect state, unpopulated-page rules, continued/woken ranges, and concurrent unregister/remap behavior. No requested feature may disappear silently during negotiation.
- **Evidence:** Use isolated mappings and bounded faulting workers to prove actual protection/minor-fault resolution and each selected lifecycle event. Huge-page/shmem variants need suitable explicitly provisioned fixtures; document and retain missing execution evidence rather than inferring it from anonymous-memory tests.

### New key management and IPC facilities

#### E38 — Add kernel keys and keyring lifecycle

- **Implement:** Introduce the proposed `keyring` package with typed key/ring IDs, creation, description, payload read/update, search, link/unlink, permissions, ownership, revoke, and invalidate operations for private workload keyrings and explicit kernel credential storage. Preserve a mechanism-level API; do not implement a general secret manager.
- **Contracts:** Linux key IDs are kernel identities rather than closable descriptors; do not invent `Close` semantics that revoke shared keys. Preserve possession/search permissions, thread/process/session keyring scope, expiration/revocation errors, and opaque payloads. Never log real key payloads.
- **Evidence:** Use synthetic data in a private session keyring inside a bounded child to verify search, permissions, linkage, updates, revocation, and cleanup. Avoid mutating the invoking user's session/user keyrings. Establish a required native path with actual [key-management operations](https://man7.org/linux/man-pages/man7/keyrings.7.html).

#### E39 — Add key requests, expiry, and remaining key controls

- **Implement:** Add typed `request_key` operations, timeout/expiry control, supported request authorization/destination choices, ring clearing, atomic key moves, link restrictions, capability discovery, and security-label inspection. Account explicitly for instantiate/reject/negate and authority-assumption controls when implementing the controlled request-helper workflow. Distinguish key types the kernel handles from cryptographic algorithms the library implements.
- **Boundary:** Keep shared user-persistent rings and session-to-parent credential replacement outside private workload-ring management. Cryptographic DH/public-key operations and watch-queue notifications require separate algorithm/transport contracts and are excluded from this sequence; do not expose a generic keyctl escape hatch for them. Record these omissions in the first keyring usage guide.
- **Contracts:** Document possible external request-key helper execution, credential/thread scope, interruption, negative keys, and asynchronous expiry. Notifications or specialized key types need explicit safe contracts and configuration coverage; retain technical exclusions rather than accepting arbitrary untyped `keyctl` commands.
- **Evidence:** Verify requests satisfied from the private ring without invoking uncontrolled host helpers, bounded expiry and negative/error behavior, and delegated controls on test-owned keys. Helper-backed success requires a dedicated controlled fixture, not assumptions about the developer's host configuration.

#### E40 — Add POSIX message-queue resources

- **Implement:** Introduce the proposed `mqueue` package with named create/open, attributes, close/unlink, and priority-preserving send/receive operations. Use the Linux syscall interface underneath the [POSIX API](https://man7.org/linux/man-pages/man7/mq_overview.7.html); do not assume every libc function maps to a syscall with the same name.
- **Contracts:** Preserve whole-message boundaries, priorities, queue/message size limits, nonblocking behavior, permissions, and persistence after close. Unlink is distinct from closing an open owner. Avoid stream interfaces that discard priority or truncate records silently.
- **Evidence:** Use unique test-owned queues inside a private IPC namespace, demonstrate cross-process priority ordering and unlink/open-owner lifetime, and verify full/empty errors and failure cleanup. Add a required native IPC verification path; do not change global mqueue limits or leak named queues.

#### E41 — Add message-queue timing and readiness

- **Implement:** Add explicit timed send/receive and typed readiness integration. Assess `mq_notify` modes individually and implement modes that can preserve Go runtime and notification ownership contracts; document any concrete unsupported modes in the coverage inventory.
- **Contracts:** Separate native absolute realtime timeouts from Go I/O deadlines, preserve one-shot/single-registrant notification semantics when exposed, and prove how waits are interrupted. Do not install raw signal handlers or silently replace requested notification semantics with level readiness.
- **Evidence:** Verify blocked send/receive, native timeout behavior, readiness without consuming a message, close/unlink during waits, and notification rearming/contention for exposed modes. Use the required IPC lane and bounded child processes for waits that cannot be canceled safely.

#### E42 — Add System V message queues

- **Implement:** Introduce the proposed `sysvipc` package's queue resource with `msgget`, `msgsnd`, `msgrcv`, and typed `msgctl` inspection/configuration/removal. Preserve key versus queue-ID identity and typed message selection.
- **Contracts:** Retain positive/negative/zero message-type selection, truncation/no-truncation choices, nonblocking/interruption/removal errors, persistent object lifetime, permissions, and Linux widths. Removal is explicit; a local wrapper's lifetime is not a kernel reference count.
- **Evidence:** In private IPC namespaces, verify cross-process selection, zero-length and oversized messages, limits, removal waking blocked operations, and complete cleanup. The common [System V IPC overview](https://man7.org/linux/man-pages/man7/sysvipc.7.html) links the operation ABIs. Extend the required IPC/native lane.

#### E43 — Add System V semaphore sets

- **Implement:** Extend `sysvipc` with semaphore-set creation, atomic `semop` batches, `semtimedop`, typed `semctl` values/metadata, and explicit removal. Preserve per-operation indices, adjustments, zero waits, flags, and undo behavior.
- **Contracts:** Explain process/shared undo state, set versus semaphore identity, atomic batch application, timeout/interruption, and removal. Do not present these objects as ordinary Go mutexes or release persistent kernel state automatically on a wrapper close.
- **Evidence:** Use bounded independent processes to prove atomic batches, contention, zero waits, timeout, `SEM_UNDO` on exit, error nonapplication, and removal of blocked waiters. Use private IPC namespaces and the required native IPC suite.

#### E44 — Add System V shared memory

- **Implement:** Extend `sysvipc` with segment create/open/inspect/remove and owned attach/detach mappings through `shmget`, `shmat`, `shmdt`, and `shmctl`. Return a bounded byte view with the same explicit escaped-slice discipline as `mmap`.
- **Contracts:** Keep segment identity, attachment lifetime, remove marking, final detach, permissions, and read-only mappings distinct. Forbid Go pointers in cross-process payloads; callers supply synchronization. Fixed-address, huge-page, and locking variants need their own represented ownership/prerequisite contracts or explicit technical exclusions.
- **Evidence:** Prove independent-process visibility, read-only protection, mark-for-removal lifetime, detach invalidation in bounded children, malformed sizes/IDs, and construction cleanup. Reuse existing mapping/process helpers where they fit and run the required IPC/native lane.

### Integration and completion

#### E45 — Reconcile coverage, compatibility, and verification

- **Review:** Reconcile the support index and every affected usage guide against current exported operations and Linux 7.0 semantics. Account for remaining flags, ioctls, opcodes, record fields, configuration-specific variants, and genuine technical exclusions. Do not equate a package's existence with complete facility coverage or silently waive an unfinished earlier item.
- **Integrate:** Exercise allocated/synchronized mapped database files, uring cancellation/registered-resource cleanup, bounded workload measurement, and shared-memory/IPC lifetimes. Add a container-style lifecycle using standard Go child startup and UID/GID mappings, existing namespaces/capabilities/security APIs, a private prepared/activated rootfs, atomic delegated cgroup startup from E23a, and private virtual networking, followed by PID 1/orphan reaping from E14a and cleanup. Include eBPF network counters and process-event tracing with cgroup/procfs/pidfd operations, keeping application control policy separate from observation and preserving attachment cleanup. Run any combined privileged scenario only through an explicit isolated verification route. Document externally supplied pieces such as image/rootfs contents, device policy, NAT/firewall policy, DNS/IP allocation, and optional checkpoint engines; do not imply that the example is a complete secure container runtime.
- **Verify:** Observe all applicable required native baseline and isolated CI jobs for the actual final revision, including each new facility lane and coverage merge. Reconcile local newer-kernel evidence with exact-baseline evidence, preserve unresolved defensive/configuration limits in the existing verification-limit owner, and verify that no core scenario is covered only by a skip or parser fixture.
- **Acceptance:** Another agent can use and verify the shipped APIs from their documentation, all agreed items have their required evidence, and remaining technical exclusions are explicit. Remove this file when its last item is complete. Release/tag/publish only under the user's actual authorization.
