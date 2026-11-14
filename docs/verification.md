# Verification

Run commands from the repository root after the submodule bootstrap in [README.md](../README.md). The Go floor is in `go.mod`; CircleCI is the CI owner. [IMPLEMENTATION.md](../IMPLEMENTATION.md) records actual evidence and outstanding requirements. Commands inherit the trusted operator environment, but diagnostics print only selected kernel/resource observations and validated verification settings.

## Contributor commands

```sh
make dep
make test-environment
make vet
make lint
make scripts-lint
make specs
make coverage
make compile
make sec
```

`make dep` uses the shared download/tidy/vendor workflow. Toolchain selection and resolution of the pinned `golang.org/x/sys` dependency may require network access to Go's module proxy/checksum services. Environment discovery requires `uname` and `true` on PATH. `make scripts-lint` requires ShellCheck. `make sec` uses govulncheck and Trivy and may download vulnerability databases. The CI image supplies the quality tools listed in the README; local tool absence is a verification gap, not a pass. `make ci-validate` uses the official CircleCI CLI and its validation API when the CLI/access are available.

`make specs` retains the shared gotestsum runner, race detection, and coverage. It also writes all test events, including output and skips, to `test/reports/specs.json`, alongside `specs.xml` and `profile.cov`. A skipped test is not kernel runtime coverage. Each run replaces those files: archive a run before changing profiles/toolchains if its evidence must be retained. CircleCI stores each job's reports separately. Scope tests with `package=internal/testenv make specs` (or a later implemented package path). The `.gocov` filter excludes the `/test/` directory, retaining coverage of the implemented environment checker and command even though their names contain `test`. `make compile` builds each package and its tests into unique temporary paths for both linux/amd64 and linux/arm64 with cgo disabled, then removes them. It never executes the resulting binaries and is not race/runtime coverage.

## Profiles and prerequisite results

| Setting | Values and behavior |
| --- | --- |
| `profile` | `rootless` (default): ordinary-user checks; `namespaces`: additionally require creation of user, mount, and network namespaces in a disposable subprocess |
| `required` | `false` (default): a known unavailable selected prerequisite skips `TestConfiguredProfile` with a precise reason; `true`: the same result fails the test and command |
| `arch` | Empty (default), `amd64`, or `arm64`; a supplied value must match both the executing Go binary and `uname -m` |
| `kernel` | `supported` (default): Linux 7.0 or later; `7.0`: exactly the baseline major/minor; `newer`: newer than 7.0. A mismatch fails even when the profile is optional. |

Make exports these as `GO_LINUX_TEST_PROFILE`, `GO_LINUX_TEST_REQUIRED`, `GO_LINUX_TEST_ARCH`, and `GO_LINUX_TEST_KERNEL` for the Go command/tests. Invalid settings fail before probing. Set the Make variables at the documented entrypoints rather than relying on inherited values for those four variables.

```sh
make test-environment profile=namespaces
make specs profile=namespaces
make specs profile=namespaces required=true
make specs profile=rootless required=true arch=arm64 kernel=7.0
```

`kernel=newer` remains an optional selector for an available newer-kernel runner or local checks. It is not a required CI lane. The required runtime target is the available Linux 7.0 arm64 CircleCI runner; update the target and recorded evidence when that runner environment changes.

These are requests to verify an environment, not proof that such runners exist. Default rootless tests can pass on UID 1000 without capabilities. The profile selects tests intended for ordinary users; execution on UID 0 does not establish ordinary-user runtime coverage, so retain the actual UID/capability observations with the results. Namespace creation is probed even during rootless discovery, but a known namespace denial does not invalidate rootless coverage. The namespace profile requests an actual `clone` with user/mount/network namespace flags and one-entry UID/GID mappings; the child executes `true` and exits, with a five-second bound and deterministic process reaping. It performs no mount or network mutation. Successful discovery establishes only creation/mapping prerequisites, not that every filesystem, namespace transition, or privileged facility works.

Diagnostics include Go version, kernel, machine and Go architectures, UID, effective/bounding capabilities, no-new-privileges, seccomp mode, namespace identifiers, selected mount types/options, and cgroup v2 controller observations. Read-only mount/controller observations do not establish writable delegation. The namespace probe's observed error remains visible. It classifies `EPERM`, `EACCES`, `ENOSYS`, or `ENOSPC` from that precise creation/mapping attempt as unavailable prerequisites; it does not infer the responsible seccomp rule or policy. Missing executables, timeouts, unexpected child failures, `EINVAL`, and other unexplained errors fail. An expected permission denial is distinct from successful isolation coverage.

Later package tests must perform their own operation-specific prerequisite checks; do not turn any facility `EPERM` into a blanket skip. Add capability or delegated-resource profiles when an implemented operation first needs them, with its real isolated/delegated resource checks. F02 does not create speculative privilege fixtures for the remaining packages. Mount, route mutation, cgroup delegation, and clock-changing success paths remain separate verification obligations.

`package=memfd make specs` exercises creation, ordinary I/O, sealing, mappings affected by seals, executable opt-in, and lifecycle without additional profiles or skips. It requires permitted `memfd_create`/`fcntl`/`mmap` operations and policy allowing `MFD_EXEC`; denial fails rather than claiming that mode is verified. Tests never execute a memfd or modify host security policy. See the [memfd design note](design/memfd.md) and P01 handoffs for per-operation evidence and verified Linux 7.0 arm64 execution at both Go versions.

`package=eventfd make specs` exercises ordinary/semaphore counters, byte records and typed helpers, descriptor flags/raw nonblocking access, blocking reads/writes, overflow backpressure, deadlines/reset, close interruption, and ownership. It requires permitted `eventfd2`, counter I/O, and Go polling, without extra profiles or skips. Local Linux 7.3 amd64 execution passes at Go 1.27.0 and 1.27.4. Required Linux 7.0 arm64 execution is verified by [Go 1.27.0 job 57](https://circleci.com/gh/alexfalkowski/go-linux/57) and [Go 1.27.4 job 58](https://circleci.com/gh/alexfalkowski/go-linux/58): each reports 38 eventfd passes and 76 total passes with no failures/skips, closing P02's primary-target gap. Post-acquisition poller-registration rejection and resource exhaustion remain source-reviewed cleanup paths without injected runtime coverage. See the [eventfd design note](design/eventfd.md) and P02 handoff for evidence and limitations.

## CI and outstanding runners

The existing quality build remains on the Docker executor, image `alexfalkowski/go:5.23`, resource class `arm.large`. The added `verify` matrix requests Go 1.27.0 (minimum) and Go 1.27.4 (latest stable checked on 2026-11-14), using [GOTOOLCHAIN](https://go.dev/doc/toolchain). It runs dependency setup, environment discovery, vet, both compile architectures, and required rootless tests. Update the pins when the Go floor or latest supported release changes; the [Go release feed](https://go.dev/dl/?mode=json) is the reference. Branch synchronization and release jobs require both the quality build and all Go verification jobs.

Job startup prints the configured executor/image/resource class and records actual kernel/architecture/privilege observations in `environment.json` and job output. Tests require arm64 execution and the supported kernel floor. CircleCI manages the Docker executor's host kernel and does not offer per-job or per-organization kernel selection; changing the Docker image does not select another kernel. The required matrix follows that available arm64 runner at both Go versions, currently Linux 7.0; it does not require provisioning another kernel. Continue observing every run and update the documented verification target when CircleCI changes the runner environment. A runner upgrade does not implicitly change the library's minimum Linux version. See [CircleCI's Docker kernel policy](https://discuss.circleci.com/t/circleci-response-to-cve-2026-43499-ghostlock-linux-kernel-vulnerability/54758), the [configuration reference](https://circleci.com/docs/reference/configuration-reference/), and [Arm execution environments](https://circleci.com/docs/guides/execute/using-arm/).

The [F02 workflow](https://app.circleci.com/workflow/29e0f43d-2801-47ba-bdd0-d2c09719de50) passed for commit `156738dd828681ba1375ed7a66c7e8ff1f26c374`. Jobs 14 (Go 1.27.0) and 12 (Go 1.27.4) each ran 15 tests successfully on `7.0.0-1017-aws`, arm64, UID 3434; environment diagnostics report namespace creation/mapping available. Both verification jobs passed vet and amd64/arm64 compilation, and build 13 passed the quality checks. This verifies the foundation on the current target; actual facility operations are verified during their owning package work.

Future package-specific requirements remain outstanding until observed: successful privileged operations, writable cgroup delegation, and clock-controlled VM tests when those package items need them. Namespace probe success alone does not verify mounts, network mutation, delegation, or every namespace operation. No required privileged lane is declared successful by skipping it. Full amd64 runtime support still needs matching baseline runtime evidence; local amd64 execution and compilation are recorded separately.
