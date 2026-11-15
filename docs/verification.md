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

## Profiles and prerequisites

| Setting | Meaning |
| --- | --- |
| `profile=rootless` | Default; tests intended for ordinary users. Mount success tests require the namespace profile. |
| `profile=namespaces` | Also request namespace creation/mapping and isolated mount integration. |
| `required=true` | Fail known unavailable selected prerequisites; the default `false` permits precise skips. Unexpected errors fail in either mode. |
| `arch=amd64` or `arch=arm64` | Require both the Go binary and host machine to match; omit for an unselected native run. |
| `kernel=supported` | Default; Linux 7.0 or later. Use `7.0` for the exact baseline major/minor or `newer` for a later kernel. Mismatches always fail. |

Pass these Make variables explicitly; the Makefile exports the corresponding `GO_LINUX_TEST_*` settings. Invalid values fail before probing. For example:

```sh
make test-environment profile=namespaces required=true arch=arm64 kernel=7.0
package=mount make specs profile=namespaces required=true arch=arm64
```

Environment discovery reports actual Go/kernel/architecture, UID/capabilities, security settings, and namespace prerequisites. Namespace creation alone does not prove mount operations work, and UID 0 does not establish ordinary-user coverage. Rootless runs may skip mount success scenarios; the separate required mount lane covers them. A skip is not runtime verification, and a facility error such as EPERM must not become a blanket skip.

## Mount verification

Use a native Linux Docker host that permits privileged containers. Select the Go image from [.circleci/config.yml](../.circleci/config.yml) and the host architecture; the current amd64 invocation is:

```sh
make verify-mount-container image=alexfalkowski/go:5.23 arch=amd64
```

The launcher runs dependency setup and `make verify-mount`, which records `environment.json` and runs required mount-only tests in the namespace profile. Reports are bind-mounted back to `test/reports/`. Each fixture creates separate user/mount namespaces and makes propagation private before mounting. Required prerequisite or operation failures fail the command. The caller must provide the permitted isolation environment; changing a container image does not change its host kernel.

With tools installed directly, `make verify-mount arch=amd64` runs the same discovery and mount suite.

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

The [workflow for bff403e](https://app.circleci.com/workflow/191849be-75c5-4a97-94fc-4165cfa85ed6) passed the required matrix using Go 1.27.4 from `alexfalkowski/go:5.23`:

| Lane | Native architectures | Observed kernel | CI evidence: arm64 / amd64 |
| --- | --- | --- | --- |
| Ordinary user | arm64, amd64 | 7.0.0-1017-aws | [338](https://circleci.com/gh/alexfalkowski/go-linux/338) / [342](https://circleci.com/gh/alexfalkowski/go-linux/342) |
| Isolated mount | arm64, amd64 | 7.0.0-1004-aws | [340](https://circleci.com/gh/alexfalkowski/go-linux/340) / [341](https://circleci.com/gh/alexfalkowski/go-linux/341) |
| Disposable-VM clock changes | arm64, amd64 | 7.0.0-1004-aws | [343](https://circleci.com/gh/alexfalkowski/go-linux/343) / [339](https://circleci.com/gh/alexfalkowski/go-linux/339) |

These results establish the recorded revision and scenarios, not exhaustive kernel behavior or later revisions. CI runs the configured image toolchain rather than a separate minimum-Go-version matrix. Inspect the retained environment and test reports when accepting a new revision; compilation alone, image names, privileges, or source inspection do not establish runtime coverage. Remaining gaps and source-reviewed paths are tracked in the implementation plan.
