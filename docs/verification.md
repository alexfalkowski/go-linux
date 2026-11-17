# Verification

This guide helps contributors run the library's checks and understand the environments they require. Use Linux 7.0+ and the Go version declared in [go.mod](../go.mod), with the [README setup](../README.md#-setup) completed. [IMPLEMENTATION.md](../IMPLEMENTATION.md#open-verification-gaps) owns unresolved verification gaps; detailed results belong in CI reports.

## Run local checks

Discover commands with `make help`. The standard checks are:

```sh
make dep
make test-environment
make vet
make lint
make scripts-lint
make specs
make coverage
make sec
```

Run from the repository root. Install the quality tools listed in the README and ShellCheck for `scripts-lint`; missing tools are uncovered validation. Dependency setup and security scanners may download modules or databases. `make ci-validate` additionally requires the CircleCI CLI and API access.

Scope tests to the changed package, for example:

```sh
package=file make specs profile=rootless required=true arch=amd64
```

The gotestsum harness runs with race detection and coverage. Reports are written under `test/reports/`: `specs.json` retains test events and skips, `specs.xml` is JUnit, and `profile.cov` is the coverage input. `make coverage` creates `final.cov` and `coverage.html` from that run. A later test run replaces the reports, so retain any results you need before changing package or profile. CI stores each job's reports separately. Reexecuted fixtures assert operation results in the child; the parent coverage profile does not aggregate child statement counters.

Use the [shared thread helpers](../internal/test/thread.go) for thread-affine tests: `LockThread` pins the current goroutine for its remaining lifetime, and `LockedThread` runs a bounded worker. Executable examples show the public runtime calls. Irreversible mutation fixtures still require bounded subprocesses when using these helpers.

CI stages each raw profile with `make coverage-workspace-stage partition=<facility>-<arch>`, selecting `rootless`, `netlink`, `namespace`, `mount`, `fanotify`, or `timerfd` and `arm64` or `amd64`. Each partition has its own directory under `/tmp/go-linux-reports`. After all twelve jobs pass, the separate coverage job attaches that workspace and runs `make coverage-workspace-merge` followed by `make coverage`. Merging requires all twelve nonempty atomic profiles from the same revision and uses `gocovmerge`, included in the Go image; missing inputs or incompatible profiles fail the job. The Netlink partition contains the single raw profile produced by testing both packages together. Only raw inputs are merged, and `make coverage` applies `.gocov` exclusions.

The coverage job retains the combined reports and uploads `final.cov` once through `make codecov-upload`; sync and release wait for this job, including the required Netlink matrix. The rootless and Netlink jobs explicitly request `kernel=7.0`, so a newer host kernel fails their baseline checks. The combined report records statements exercised across the matrix; individual job reports remain the evidence for each architecture and environment.

The inotify overflow fixture fills only its own instance queue and never changes host limits. It bounds generation at `max_queued_events <= 65536`; a larger existing host limit skips the fixture in optional mode and fails with `required=true`. Decoder fixtures still cover the overflow record independently.

The socket backpressure fixture fills only a test-owned Unix datagram endpoint, using the existing `/proc/sys/net/unix/max_dgram_qlen` value without changing it. Limits above 65536 skip optional coverage and fail with `required=true`; bounded native producers distinguish destination-queue exhaustion from a single sender's buffer limit.

The eventfd, timerfd, inotify, and socket constructor suites use bounded subprocesses to reject EPOLL_CTL_ADD with a thread-local seccomp filter after Go's poller is initialized. They verify os.ErrNoDeadline, nil owners, and descriptor cleanup. The same filter loader supports Netlink's setup-failure fixtures; no parent thread receives these restrictions.

Netlink's ordinary-user tests use read-only controller/route queries, parser fixtures, and test-owned sockets. Kernel socket filters on those sockets exercise missing ACKs and malformed framing; independently queued kernel replies exercise sequence/echo mismatches. Bounded subprocesses confine descriptor exhaustion and thread-local seccomp denial of individual setup operations, checking errno and cleanup without changing parent or host policy.

Run `make specs package=netlink profile=namespaces required=true` for kernel multicast, dump ordering/interruption, and receive-queue overflow fixtures, using the [isolated container command](#netlink-isolation) when the agent shell denies namespace creation. Each fixture reexecutes in mapped user/network namespaces; only the child changes loopback state or adds private addresses. The dump fixtures pause between sending and receiving while a separate connection changes the address generation. The overflow fixture reduces only its own socket receive buffer and generates 32 private loopback changes; it verifies ENOBUFS and a separate recovery dump without changing host limits. Rootless profile runs skip these unselected fixtures; required namespace runs fail if child creation/mapping is unavailable.

Select `package=netlink/route` for typed routing coverage. The rootless suite performs read-only listing, public decoder fixtures/fuzz seeds, validation, deadlines, and lifetime checks. Its selected namespace fixtures use the same bounded child harness for dummy-link lifecycle, IPv4/IPv6 addresses, extended routing tables and gateway replacement, and multicast changes. Only those private network namespaces receive mutations. Native constants, wire construction, errno aliases, and independent kernel observations live in `internal/test`; assertions remain in the package scenarios.

## Profiles and prerequisites

| Setting | Meaning |
| --- | --- |
| `profile=rootless` | Default; tests intended for ordinary users. Namespace transitions, mount, and descriptor-based fanotify success tests require the namespace profile. |
| `profile=namespaces` | Also request namespace creation/mapping and isolated integration. Fanotify additionally requires initial-user-namespace CAP_SYS_ADMIN. |
| `profile=cgroup` | Select live cgroup mutations inside an explicitly supplied delegated subtree; namespace creation is not required. |
| `delegation=/absolute/path` | Supply a disposable cgroup v2 subtree for `profile=cgroup`. Exported as `GO_LINUX_TEST_CGROUP`; discovery only reads its identity/controller files. |
| `required=true` | Fail known unavailable selected prerequisites; the default `false` permits precise skips. Unexpected errors fail in either mode. |
| `arch=amd64` or `arch=arm64` | Require both the Go binary and host machine to match; omit for an unselected native run. |
| `kernel=supported` | Default; Linux 7.0 or later. Use `7.0` for the exact baseline major/minor or `newer` for a later kernel. Mismatches always fail. |

Pass these Make variables explicitly; the Makefile exports the corresponding `GO_LINUX_TEST_*` settings. Invalid values fail before probing. For example:

```sh
make test-environment profile=namespaces required=true arch=arm64 kernel=7.0
package=mount make specs profile=namespaces required=true arch=arm64
```

Environment discovery reports actual Go/kernel/architecture, UID/capabilities, security settings, and namespace prerequisites. Namespace creation alone does not prove mount operations work, and UID 0 does not establish ordinary-user coverage. Rootless runs may skip mount success scenarios; the separate required mount lane covers them. A skip is not runtime verification, and a facility error such as EPERM must not become a blanket skip.

## Rootless container verification

The native rootless CI jobs use machine executors to control the container syscall profile. A managed Docker executor can return ENOSYS for baseline-supported syscalls, including the xattr-at family, even when its host runs Linux 7.0. Changing the Go image does not change that outer filter.

Use a native Linux Docker host, a nonroot invoking user, and the Go image from [.circleci/config.yml](../.circleci/config.yml):

```sh
make verify-rootless-container image=alexfalkowski/go:5.23 arch=amd64 package=xattr
```

Omit `package` for the complete rootless suite used by CI. The launcher downloads the [Moby default seccomp profile at a pinned revision](https://github.com/moby/profiles/blob/f862455628f78f84c937e967e4a03902fb1dee11/seccomp/default.json), checks its SHA-256 digest, and passes it explicitly to Docker. This allowlist includes getxattrat, setxattrat, listxattrat, and removexattrat. The host Docker runtime/libseccomp must recognize those syscalls. Profile download or checksum failures stop verification; the launcher does not fall back to an unconfined container.

A root setup process remaps the image's `circleci` user and home to the invoking UID. Docker's init remains root at PID 1, providing the different-UID target required by signal-permission tests; its KILL capability permits forwarding signals to the nonroot child. All checks run with the invoking UID/GID, no supplementary groups, and empty capability sets; no-new-privileges is enabled from startup. The checkout is mounted separately at `/work`, so account setup does not change its ownership. The Docker bind mount must preserve the test UID/GID's write access; rootless-daemon UID mappings require corresponding checkout ownership. `make verify-rootless` refreshes dependencies, records the environment, and runs lint, script lint, security checks, vet, tests with race detection, and coverage. Modules, build artifacts, and tool caches are disposable container state. Reports are bind-mounted to the checked-in `test/reports/` directory. The local verification command does not upload coverage; `make codecov-upload-container image=<tag>` remains available for an explicit upload of an existing `final.cov` from the Go image.

## Netlink isolation

Netlink's isolated fixtures need permitted mapped user/network namespaces and network administration inside those private namespaces. They do not require fanotify's initial-user-namespace CAP_SYS_ADMIN. Use a native Linux Docker host and the Go image from CI:

```sh
make verify-netlink-container image=alexfalkowski/go:5.23 arch=amd64
```

The same launcher runs locally and in `build-netlink` on native amd64/arm64. It uses the existing privileged-container setup, refreshes dependencies, records `environment.json`, and runs `make verify-netlink`. Dependency setup and image acquisition may require network/registry access and a writable checkout/vendor directory. Use a writable disposable source copy if existing vendor ownership prevents refresh. Rootless Docker works in the observed local configuration; prerequisite and operation failures remain failures on other hosts.

`verify-netlink` records prerequisites and selects `profile=namespaces required=true` for `netlink` and `netlink/route` in one invocation of the shared `specs` harness. Each fixture creates further private namespaces before changing loopback, dummy links, addresses, or routes. Both packages appear in the standard `specs.json`, `specs.xml`, and `profile.cov` files directly under `test/reports/`, alongside `environment.json`. The test target leaves coverage merging to the separate CI coverage job.

With the configured tools and permitted namespaces available directly, use `make verify-netlink arch=amd64`. To require exact-baseline evidence, add `kernel=7.0` to either invocation. Both the rootless `build` jobs and `build-netlink` jobs require this setting in CI. A newer host fails rather than being accepted as Linux 7.0 evidence; a container image does not select the host kernel. Use native amd64 and arm64 runners rather than interpreting emulation as native coverage.

The dump fixtures verify the actual kernel interruption marker independently, then use controlled kernel reply queues and socket filtering to verify ACK-before-DONE ordering, interruption draining, and subsequent connection reuse through Conn.Dump. These controlled conditions do not establish naturally occurring malformed kernel replies. Malformed routing records and controls remain covered through public parsers/fuzzing or explicit source review where the kernel does not emit them. Sequence wrap would require roughly 2^32 exchanges. No production injection hooks or fake-kernel fixtures substitute for missing live evidence; the remaining limits stay tracked under R01.

## Namespace verification

The namespace package's ordinary-user suite covers all eight handle types, identity, adoption, ownership, lifetime, concurrency, errors, and examples. Thread transitions run only in bounded subprocesses selected by the namespace profile. Run the same isolated suite locally and in CI with the configured Go image:

```sh
make verify-namespace-container image=alexfalkowski/go:5.23 arch=amd64
```

The launcher requires a native Linux Docker host that permits privileged containers and mapped user namespaces. It refreshes dependencies and runs `make verify-namespace`, recording prerequisites and the standard reports directly in `test/reports/`. A writable disposable checkout can be used when dependency ownership prevents refresh. Inside the container each scenario creates a private mapped user namespace before creating or entering any further namespaces. Tests verify mount, cgroup, IPC, network, UTS, PID, and time creation; permitted entry; user hierarchy/owner inspection; standard child creation; single-threaded user/time restrictions; and worker-thread cleanup after success, failure, and recovered panic. The startup thread is reserved while tests run so thread-destruction observations target workers rather than Go's permanently parked startup-thread special case. No host namespace is entered or mutated.

The `build-namespace` matrix selects `kernel=7.0` on native amd64 and arm64, and its profiles participate in coverage merging. The launcher runs both `namespace` and `capability` in one standard test invocation. With suitable tools and privileges directly available, use `make verify-namespace arch=amd64`; add `kernel=7.0` to require exact-baseline evidence. Rootless runs skip the unselected transition fixtures; `profile=namespaces required=true` fails on missing creation/mapping prerequisites. Once a child starts, failed facility operations always fail the test. Thread creation or capability bits alone are not evidence of entry/creation success. Child assertions execute real operations but their statement counters are not merged into the parent coverage profile.

## Capability verification

Run `make specs package=capability` for ordinary-user inspection, invalid masks, empty-set transitions, and denied operations through the standard race/coverage harness. Every credential mutation runs in a bounded subprocess and stays on a locked goroutine. The test runner's credentials remain unchanged. Procfs thread status provides independent kernel observations alongside typed API assertions.

Positive permitted/effective/inheritable transitions, bounding drops, ambient changes, thread isolation, securebits refusal, and exec transformations run in private mapped user-namespace subprocesses. They share the existing namespace lane and launcher:

```sh
make verify-namespace-container image=alexfalkowski/go:5.23 arch=amd64
```

This runs both packages with `profile=namespaces required=true`. With the configured tools and permitted namespaces directly available, use `make verify-namespace arch=amd64`. The disposable children need a bounding set containing the tested capabilities, including CAP_CHOWN, CAP_MAC_OVERRIDE, and CAP_BPF, and effective CAP_SETPCAP for bounding/securebits changes. The privileged container provides the intended fixture environment; root or a successful namespace probe alone is not operation evidence. Exec tests change securebits only in children to distinguish ambient inheritance from UID-0 restoration, and execute only the test binary without file capabilities. Rootless runs skip the unselected privileged scenarios; selected required prerequisites and all failures after child startup fail.

Add `kernel=7.0` when collecting exact-baseline evidence and use each native architecture. The existing `build-namespace` matrix requires both, and its coverage partitions include the capability package. Subprocess assertions establish runtime behavior but do not merge their statement counters into the parent coverage profile. Reports stay directly under `test/reports/`; unfinished platform evidence remains in [IMPLEMENTATION.md](../IMPLEMENTATION.md#open-verification-gaps).

## Cgroup verification

The rootless `cgroup` suite exercises captured native-format fixtures, read-only current-group operations, ownership/errors, descriptor exhaustion, watcher deadlines/concurrent close, and executable examples. Event-dependent current-group tests skip only when `cgroup.events` is absent at the real hierarchy root; other inspection tests remain enabled. Selecting `profile=cgroup` makes event-dependent tests use a test-owned child of the supplied delegation, so that profile requires event coverage regardless of the runner's current membership. Mutation tests require an explicit disposable delegation; neither directory ownership, UID 0, nor a namespace probe establishes that it is usable.

```sh
make verify-cgroup delegation=/absolute/delegated/subtree arch=amd64
```

Use the configured Go image's toolchain on a native Linux 7.0+ runner with a writable v2 mount. The delegator must grant directory creation/removal and the documented membership/subtree-control permissions and enable `cpu memory pids` for children. Run the test process in a leaf below the same delegation (or with the necessary common-ancestor migration permission), so its spawned children can move into test groups. Do not supply a shared production workload group. The test suite creates uniquely named children and changes only those children; it never changes the supplied parent's controllers, limits, permissions, or membership, and never migrates the test runner. Test child processes are bounded and reaped before directory cleanup.

`verify-cgroup` records `environment.json` and runs `package=cgroup profile=cgroup required=true` through the standard race/coverage harness. It covers child lifecycle, independently owned handles, CPU/memory/PIDs limits, unavailable controllers, atomic controller changes and refusal, whole-process migration, populated notifications, watched-group removal while its handle remains open, and cleanup after descriptor exhaustion during creation. Missing explicit delegation fails required coverage; invalid paths, read-only mounts, and facility failures fail once a subtree is supplied. Optional rootless runs skip the unselected live scenarios. Read-only discovery is not proof of mutation success. Add `kernel=7.0` and the native architecture when collecting baseline evidence; retain the standard reports directly under `test/reports/`.

The agent sandbox exposes a read-only cgroup mount; the host terminal can supply a writable systemd delegation. The environment matrix below distinguishes those runs, and `IMPLEMENTATION.md` tracks the remaining CI and native-baseline follow-ups. No dedicated delegated CI runner/coverage partition is configured yet; the existing rootless CI lane runs the read-only and fixture subset.

## Mount verification

Use a native Linux Docker host that permits privileged containers. Select the Go image from [.circleci/config.yml](../.circleci/config.yml) and the host architecture; the current amd64 invocation is:

```sh
make verify-mount-container image=alexfalkowski/go:5.23 arch=amd64
```

The launcher runs dependency setup and `make verify-mount`, which records `environment.json` and runs the mount package's required namespace suite. This includes an inotify observer that verifies Unmount followed by Ignored when a private tmpfs filesystem is unmounted. Reports are bind-mounted back to `test/reports/`. Each fixture creates separate user/mount namespaces and makes propagation private before mounting. Required prerequisite or operation failures fail the command. The caller must provide the permitted isolation environment; changing a container image does not change its host kernel. Dependency setup requires a writable checkout and vendor directory; use a writable disposable source copy when existing dependency ownership prevents refresh.

With tools installed directly, `make verify-mount arch=amd64` runs the same discovery and mount suite.

## Fanotify verification

Use a native Linux host with a rootful Docker daemon and initial-user-namespace `CAP_SYS_ADMIN`. The host kernel must provide `CONFIG_FANOTIFY` and `CONFIG_FANOTIFY_ACCESS_PERMISSIONS`. Rootless Docker cannot supply the required initial-namespace privilege, including with `--privileged`; the launcher rejects that environment before running setup.

```sh
make verify-fanotify-container image=alexfalkowski/go:5.23 arch=amd64
```

The launcher uses the configured Go image and runs dependency setup followed by `make verify-fanotify`. That target records the environment and runs `package=fanotify profile=namespaces required=true` through the ordinary race/coverage harness. Each live fixture reexecutes in a private mount namespace without creating a user namespace, preserving the required initial-namespace privilege. It makes mount propagation private and uses only test-owned files and private tmpfs instances; no host files or mounts receive marks. Tests verify notification files, inode/mount/filesystem marks, permission responses and shutdown, iterator cleanup, deadlines, and concurrent close. Examples run in the same isolated fixtures. The `build-fanotify` CI matrix requires these operations on native amd64 and arm64; capabilities and successful namespace creation alone do not count as fanotify evidence.

With the configured tools installed on a suitable privileged Linux runner, use `make verify-fanotify arch=amd64` directly. From a shell with sudo access, `sudo env "PATH=$PATH" make verify-fanotify arch=amd64` retains access to user-installed Go, Ruby, and gotestsum tools. Ordinary rootless package tests execute parser, cleanup, and invalid-input scenarios while skipping the unselected live fixtures. Selecting `profile=namespaces required=true` fails on missing privilege; errors after entering the fixture always fail rather than becoming blanket skips. A failed required run is not runtime coverage. Remaining evidence is tracked in [IMPLEMENTATION.md](../IMPLEMENTATION.md#open-verification-gaps).

## Clock-changing tests

> [!WARNING]
> Run these tests only in a dedicated disposable VM with time synchronization disabled. They change the Docker host VM's CLOCK_REALTIME; containers and time namespaces do not isolate that clock. Never run them on a shared host.

Inside that VM, using its native architecture and the configured Go image:

```sh
sudo timedatectl set-ntp false
make verify-timerfd-container image=alexfalkowski/go:5.23 arch=amd64 clock_change=true
```

The explicit acknowledgment is required before Docker starts. The launcher runs dependency setup and `make verify-timerfd`, selecting only timerfd with the `clockchange` build tag, required prerequisites, vet, race detection, and reports. Missing clock-setting permission fails. Cleanup restores wall time using monotonic elapsed time, but abrupt termination can prevent restoration; discard the VM afterwards.

With tools installed directly, use `make verify-timerfd arch=amd64 clock_change=true`. Ordinary `make specs` does not enable clock-changing tests, including when run as root. Both privileged verification targets retain `environment.json` with their test reports.

## Verified environments

The [workflow for e1d6c4f](https://app.circleci.com/workflow/eb175e2b-377b-480f-a8b8-ae4d60a36f3c) verified the ordinary-user, mount, and clock-change lanes using Go 1.27.4 from `alexfalkowski/go:5.23`. The [workflow for 6fc658c](https://app.circleci.com/workflow/033edb8c-dcc2-4c21-a8ec-62670776b8ef) additionally verified fanotify with the same image and toolchain:

| Lane | Native architectures | Observed kernel | CI evidence: arm64 / amd64 |
| --- | --- | --- | --- |
| Ordinary user | arm64, amd64 | 7.0.0-1004-aws | [408](https://circleci.com/gh/alexfalkowski/go-linux/408) / [409](https://circleci.com/gh/alexfalkowski/go-linux/409) |
| Isolated mount | arm64, amd64 | 7.0.0-1004-aws | [406](https://circleci.com/gh/alexfalkowski/go-linux/406) / [410](https://circleci.com/gh/alexfalkowski/go-linux/410) |
| Isolated fanotify | arm64, amd64 | 7.0.0-1004-aws | [525](https://circleci.com/gh/alexfalkowski/go-linux/525) / [523](https://circleci.com/gh/alexfalkowski/go-linux/523) |
| Disposable-VM clock changes | arm64, amd64 | 7.0.0-1004-aws | [411](https://circleci.com/gh/alexfalkowski/go-linux/411) / [407](https://circleci.com/gh/alexfalkowski/go-linux/407) |

Both ordinary-user jobs ran as UID 1001 with zero effective/bounding capabilities, NoNewPrivs=1, and seccomp mode 2. All 27 xattr tests/examples passed without skips on each architecture, including real-filesystem attribute operations; the pidfd cross-user signal-permission test also passed on both.

Both fanotify jobs ran as UID 0 with initial-user-namespace CAP_SYS_ADMIN and passed all 36 tests without skips, including the isolated notification, permission, lifetime, deadline, concurrent-close, and iterator scenarios and examples.

These results establish the recorded revision and scenarios, not exhaustive kernel behavior or later revisions. CI runs the configured image toolchain rather than a separate minimum-Go-version matrix. Inspect the retained environment and test reports when accepting a new revision; compilation alone, image names, privileges, or source inspection do not establish runtime coverage. Remaining gaps and source-reviewed paths are tracked in the implementation plan.

Additional local evidence covers a newer kernel:

| Scope | Go / kernel / architecture | Observed prerequisites | Evidence and limits |
| --- | --- | --- | --- |
| `capability` sets, thread isolation, and exec inheritance | Go 1.27.4 / 7.3.4-3-t2-resolute / native amd64 | Ordinary suite: UID 1000, no effective/bounding capabilities, NoNewPrivs=1, seccomp mode 2. Isolated suite: UID 0 in privileged rootless Docker with full baseline capability sets, NoNewPrivs=0, seccomp mode 0; private mapped user-namespace children. | All 31 capability cases pass with race detection and no skips through the configured Go image's namespace launcher, which passes 90 cases across both packages. Tests verify both ABI words, permitted/effective/inheritable changes, bounding/ambient operations, refused transitions, actual policy-denial errno, thread isolation, and ordinary exec transformations. The disposable copy matches source and dependency declarations; original vendor ownership prevents dependency refresh. The required rootless suite passes with seventeen unselected privileged scenarios. Exact Linux 7.0 and native arm64 probes reject this host; those results remain pending. |
| `cgroup` formats, read-only inspection, and watcher lifetime | Go 1.27.4 / 7.3.4-3-t2-resolute / native amd64 | UID 1000, no effective/bounding capabilities, NoNewPrivs=1, seccomp mode 2; cgroup v2 mounted read-only, procfs and inotify available | Fixture/read-only tests pass with race detection; delegated scenarios are unselected. Fixtures cover limit/bandwidth formats and malformed/unknown event fields; live reads preserve invisible-PID zero records. Watcher deadlines and concurrent close pass. This sandbox cannot execute delegated mutations; host evidence is recorded separately. |
| `cgroup` host delegation | Go 1.27.4 / 7.3.4-3-t2-resolute / native amd64 | UID 1000, no effective capabilities, NoNewPrivs=0, seccomp mode 0; writable v2 mount and systemd-delegated CPU, memory, and PIDs controllers | The complete required delegated suite passes with race detection and no skips, including lifecycle, controller enable/disable and refusal, CPU/memory/PIDs limit round trips, child migration, populated notifications, and creation-failure cleanup under descriptor exhaustion. This establishes local delegated acceptance; exact Linux 7.0 and native arm64 evidence remain pending. |
| `cgroup` isolated container delegation and watcher removal | Go 1.27.4 / 7.3.4-3-t2-resolute / native amd64 | UID 0 inside privileged rootless Docker with a private cgroup namespace; writable v2 subtree and explicitly enabled CPU, memory, and PIDs controllers; runner in a separate leaf | The required delegated suite passes all 126 cases with race detection and no skips. Removing an empty watched group while its handle remains open interrupts the pending read with `os.ErrNotExist`; the regression timed out before the fix. Caller deadlines, deadline changes/clearing, independent watcher ownership, and concurrent close pass. Only the disposable container's own groups are changed. Exact Linux 7.0 and native arm64 evidence remain pending. |
| `xattr` real-filesystem behavior, examples, and rootless container launcher | Go 1.27.4 / 7.3.4-3-t2-resolute / native amd64 | UID 1000, no effective/bounding capabilities, NoNewPrivs=1, seccomp mode 2 | Required rootless package suite passes with race detection directly and through the launcher with the pinned Moby profile; this is not Linux 7.0 or arm64 evidence. Namespace creation fails with EPERM and is not needed by these tests. |
| `eventfd`, `timerfd`, `inotify`, and `socket` poll-registration failure cleanup | Go 1.27.4 / 7.3.4-3-t2-resolute / native amd64 | UID 1000, no effective/bounding capabilities, NoNewPrivs=1, seccomp mode 2; child-local seccomp loading permitted | Required rootless package suites pass with race detection and no skips, including kernel-refused registration, nil-owner errors, and descriptor cleanup. Native Linux 7.0 amd64/arm64 evidence remains CI-owned. |
| `mount` namespace operations and inotify unmount observation | Go 1.27.4 / 7.3.4-3-t2-resolute / native amd64 | UID 0 inside a privileged rootless-Docker container; mapped user/mount namespaces and private tmpfs operations permitted | All 40 cases pass with race detection and no skips through the configured container launcher, including Unmount then Ignored for the watched filesystem. The launcher ran from a disposable source copy because dependency refresh could not write the original vendor tree; source and dependency declarations matched. Native Linux 7.0 amd64/arm64 evidence remains CI-owned. |
| `inotify` lifecycle, decoding, examples, and actual queue overflow | Go 1.27.4 / 7.3.4-3-t2-resolute / native amd64 | UID 1000, no effective/bounding capabilities, NoNewPrivs=1, seccomp mode 2 | Required rootless package suite passes directly with race detection and no skips; decoder fuzzing also passes. Native Linux 7.0 amd64/arm64 evidence remains pending. Namespace creation fails with EPERM and is not needed by these tests. |
| `fanotify` decoder and resource cleanup | Go 1.27.4 / 7.3.4-3-t2-resolute / native amd64 | UID 1000, no effective/bounding capabilities, NoNewPrivs=1, seccomp mode 2; Docker daemon is rootless | Parser/ABI cleanup and invalid-input tests run with race detection, and decoder fuzzing passes; selected live fixtures fail for missing CAP_SYS_ADMIN. This is not notification or permission-operation evidence. |
| `fanotify` notifications, permissions, and iterator cleanup | Go 1.27.4 / 7.3.4-3-t2-resolute / native amd64 | Direct host run as UID 0 with initial-user-namespace CAP_SYS_ADMIN, NoNewPrivs=0, seccomp mode 0; private mount namespaces available | The contributor-run `sudo env "PATH=$PATH" make verify-fanotify arch=amd64` passes all 37 cases with race detection and no skips, confirmed in retained reports. Both iterator break and panic scenarios pass, verifying borrowed-event closure, descriptor cleanup, unread-event draining, and exactly one terminal deadline error. This verifies the current scenarios locally; it does not establish Linux 7.0 or arm64 runtime coverage for this revision. |
| `socket` message I/O, ancillary ownership, and polling | Go 1.27.4 / 7.3.4-3-t2-resolute / native amd64 | UID 1000, no effective/bounding capabilities, NoNewPrivs=1, seccomp mode 2 | Required rootless package suite passes with race detection and no skips, including binary abstract Unix names/replies, Unix datagram/seqpacket pairs, Netlink transport/options/membership, descriptor passing and truncation, credentials, deadlines, concurrent close, and descriptor-exhaustion cleanup. Linux 7.0 amd64/arm64 runtime evidence remains pending. |
| `netlink` framing, discovery, request exchanges, and isolated kernel fixtures | Go 1.27.4 / 7.3.4-3-t2-resolute / native amd64 | Ordinary suite: UID 1000, no effective/bounding capabilities, NoNewPrivs=1, seccomp mode 2. Isolated suite: UID 0 inside a privileged rootless-Docker container, private user/network namespaces available. | The required isolated suite passes all 99 cases with race detection and no skips through `verify-netlink-container` using the configured Go image and a writable disposable source copy. Coverage includes multicast, ACK-before-DONE ordering, interrupted-dump draining/reuse, receive-queue ENOBUFS, malformed framing, response mismatches, timeouts, and setup failure cleanup. Ordinary-user runs skip the six unselected isolated fixtures. All four parser fuzz targets pass. The direct agent-shell namespace probe still fails with EPERM; Linux 7.0 amd64/arm64 evidence belongs to CI, with remaining source-reviewed paths tracked under R01. |
| `netlink/route` listing, typed mutations, and notifications | Go 1.27.4 / 7.3.4-3-t2-resolute / native amd64 | Ordinary suite: UID 1000, no effective/bounding capabilities, NoNewPrivs=1, seccomp mode 2. Isolated suite: UID 0 inside a privileged rootless-Docker container with private user/network namespace children. | The configured Go image’s required isolated suite passes all 167 cases with race detection and no skips through `verify-netlink-container` from a writable disposable source copy; the original vendor ownership prevented dependency refresh. It verifies dummy-link lifecycle, masked flag changes, IPv4/IPv6 local/peer addresses and replacement semantics, full table IDs, route types/gateway replacement, errno, notifications, truncation, deadlines, and cleanup. Decoder fuzzing passes. Ordinary-user runs skip nine unselected mutation fixtures; Linux 7.0 amd64/arm64 evidence remains pending. |
| `namespace` handles, thread transitions, and child inheritance | Go 1.27.4 / 7.3.4-3-t2-resolute / native amd64 | Ordinary suite: UID 1000, no effective/bounding capabilities, NoNewPrivs=1, seccomp mode 2. Isolated suite: UID 0 inside privileged rootless Docker with mapped user-namespace children. | The configured Go image’s `verify-namespace-container` suite passes all 57 cases with race detection and no skips from a writable disposable source copy; the original vendor ownership prevents dependency refresh. Coverage includes all eight handle types, supported creation/entry, user/PID hierarchy, ownership, child membership, first-child PID 1 startup, thread restrictions/teardown, refused transitions, FIFO rejection, descriptor exhaustion, and panic cleanup. Rootless tests pass with thirteen unselected isolated fixtures. Exact Linux 7.0 and native arm64 checks reject this host; those runtime results remain pending. |
