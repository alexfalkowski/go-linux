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

The gotestsum harness runs with race detection and coverage. Reports are written under `test/reports/`: `specs.json` retains test events and skips, `specs.xml` is JUnit, and `profile.cov` is the coverage input. `make coverage` creates `final.cov` and `coverage.html` from that run. A later test run replaces the reports, so retain any results you need before changing package or profile. CI stores each job's reports separately.

The inotify overflow fixture fills only its own instance queue and never changes host limits. It bounds generation at `max_queued_events <= 65536`; a larger existing host limit skips the fixture in optional mode and fails with `required=true`. Decoder fixtures still cover the overflow record independently.

## Profiles and prerequisites

| Setting | Meaning |
| --- | --- |
| `profile=rootless` | Default; tests intended for ordinary users. Mount and descriptor-based fanotify success tests require the namespace profile. |
| `profile=namespaces` | Also request namespace creation/mapping and isolated integration. Fanotify additionally requires initial-user-namespace CAP_SYS_ADMIN. |
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

A root setup process remaps the image's `circleci` user and home to the invoking UID. Docker's init remains root at PID 1, providing the different-UID target required by signal-permission tests; its KILL capability permits forwarding signals to the nonroot child. All checks run with the invoking UID/GID, no supplementary groups, and empty capability sets; no-new-privileges is enabled from startup. The checkout is mounted separately at `/work`, so account setup does not change its ownership. The Docker bind mount must preserve the test UID/GID's write access; rootless-daemon UID mappings require corresponding checkout ownership. `make verify-rootless` refreshes dependencies, records the environment, and runs lint, script lint, security checks, vet, tests with race detection, and coverage. Modules, build artifacts, and tool caches are disposable container state. Reports are bind-mounted to the checked-in `test/reports/` directory. CI uploads coverage separately through `make codecov-upload-container image=<tag>`; the local verification command does not upload it.

## Mount verification

Use a native Linux Docker host that permits privileged containers. Select the Go image from [.circleci/config.yml](../.circleci/config.yml) and the host architecture; the current amd64 invocation is:

```sh
make verify-mount-container image=alexfalkowski/go:5.23 arch=amd64
```

The launcher runs dependency setup and `make verify-mount`, which records `environment.json` and runs required mount-only tests in the namespace profile. Reports are bind-mounted back to `test/reports/`. Each fixture creates separate user/mount namespaces and makes propagation private before mounting. Required prerequisite or operation failures fail the command. The caller must provide the permitted isolation environment; changing a container image does not change its host kernel.

With tools installed directly, `make verify-mount arch=amd64` runs the same discovery and mount suite.

## Fanotify verification

Use a native Linux host with a rootful Docker daemon and initial-user-namespace `CAP_SYS_ADMIN`. The host kernel must provide `CONFIG_FANOTIFY` and `CONFIG_FANOTIFY_ACCESS_PERMISSIONS`. Rootless Docker cannot supply the required initial-namespace privilege, including with `--privileged`; the launcher rejects that environment before running setup.

```sh
make verify-fanotify-container image=alexfalkowski/go:5.23 arch=amd64
```

The launcher uses the configured Go image and runs dependency setup followed by `make verify-fanotify`. That target records the environment and runs `package=fanotify profile=namespaces required=true` through the ordinary race/coverage harness. Each live fixture reexecutes in a private mount namespace without creating a user namespace, preserving the required initial-namespace privilege. It makes mount propagation private and uses only test-owned files and private tmpfs instances; no host files or mounts receive marks. Tests verify notification files, inode/mount/filesystem marks, permission responses and shutdown, iterator cleanup, deadlines, and concurrent close. Examples run in the same isolated fixtures. The `build-fanotify` CI matrix requires these operations on native amd64 and arm64; capabilities and successful namespace creation alone do not count as fanotify evidence.

With the configured tools installed on a suitable privileged Linux runner, use `make verify-fanotify arch=amd64`. Ordinary rootless package tests execute parser, cleanup, and invalid-input scenarios while skipping the unselected live fixtures. Selecting `profile=namespaces required=true` fails on missing privilege; errors after entering the fixture always fail rather than becoming blanket skips. A failed required run is not runtime coverage. Remaining evidence is tracked in [IMPLEMENTATION.md](../IMPLEMENTATION.md#open-verification-gaps).

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

Additional local ordinary-user evidence covers a newer kernel:

| Scope | Go / kernel / architecture | Observed prerequisites | Evidence and limits |
| --- | --- | --- | --- |
| `xattr` real-filesystem behavior, examples, and rootless container launcher | Go 1.27.4 / 7.3.4-3-t2-resolute / native amd64 | UID 1000, no effective/bounding capabilities, NoNewPrivs=1, seccomp mode 2 | Required rootless package suite passes with race detection directly and through the launcher with the pinned Moby profile; this is not Linux 7.0 or arm64 evidence. Namespace creation fails with EPERM and is not needed by these tests. |
| `inotify` lifecycle, decoding, examples, and actual queue overflow | Go 1.27.4 / 7.3.4-3-t2-resolute / native amd64 | UID 1000, no effective/bounding capabilities, NoNewPrivs=1, seccomp mode 2 | Required rootless package suite passes directly with race detection and no skips; decoder fuzzing also passes. Native Linux 7.0 amd64/arm64 evidence remains pending. Namespace creation fails with EPERM and is not needed by these tests. |
| `fanotify` decoder and resource cleanup | Go 1.27.4 / 7.3.4-3-t2-resolute / native amd64 | UID 1000, no effective/bounding capabilities, NoNewPrivs=1, seccomp mode 2; Docker daemon is rootless | Parser/ABI cleanup and invalid-input tests run with race detection, and decoder fuzzing passes; selected live fixtures fail for missing CAP_SYS_ADMIN. This is not notification or permission-operation evidence. |
