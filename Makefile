# Command-line args are auto-exported; freeze them before any shell invocation.
override args := $(value args)

profile ?= rootless
required ?= false
kernel ?= supported
delegation ?=

export GO_LINUX_TEST_PROFILE := $(profile)
export GO_LINUX_TEST_REQUIRED := $(required)
export GO_LINUX_TEST_ARCH := $(arch)
export GO_LINUX_TEST_KERNEL := $(kernel)
export GO_LINUX_TEST_CGROUP := $(delegation)

include bin/build/make/help.mak
include bin/build/make/go.mak
include bin/build/make/git.mak
include bin/build/make/claude.mak
include bin/build/make/codex.mak

PACKAGES = $(shell go list $(filter-out ./examples/%,$(sort $(dir $(SOURCE)))))

example-top example-watch example-supervise example-redis example-redis-cli example-container example-container-docker: export GO_LINUX_EXAMPLE_ARGS := $(value args)

.PHONY: examples-build example-top example-watch example-supervise example-redis example-redis-cli example-container example-container-docker

# Compile all showcase applications; no executables are left in the checkout.
examples-build:
	@go build -mod=vendor ./examples/...

# Run the process monitor (args is a JSON argv array, e.g. '["-once"]').
example-top:
	@go run -mod=vendor ./examples/run top

# Watch the current directory and rerun a command (args is a JSON argv array).
example-watch:
	@go run -mod=vendor ./examples/run watch

# Supervise a child with bounded restarts (args is a JSON argv array).
example-supervise:
	@go run -mod=vendor ./examples/run supervise

# Run the Redis-inspired cache on 127.0.0.1:6380 (args is a JSON argv array).
example-redis:
	@go run -mod=vendor ./examples/run redis

# Send one cache command (args is a JSON argv array, e.g. '["GET","hello"]').
example-redis-cli:
	@go run -mod=vendor ./examples/run redis-cli

# Run the container demo with a temporary rootfs and static guest (args is a JSON argv array).
example-container:
	@go run -mod=vendor ./examples/run container

# Run the container showcase in privileged Docker (image required; limits=true adds a private delegation).
example-container-docker: export image := $(value image)
example-container-docker: export GO_LINUX_EXAMPLE_LIMITS := $(value limits)
example-container-docker:
	@bash scripts/example-container "$$image" "$$GO_LINUX_EXAMPLE_LIMITS"

# Preserve the shared harness while retaining every test event, including skips.
specs: export GOTESTSUM_JSONFILE := test/reports/specs.json

# Report Linux prerequisites as JSON; profile=namespaces selects isolation checks.
test-environment:
	@go run -mod=vendor ./internal/cmd/testenv

# Record prerequisites and run required rootless quality checks, tests, and coverage (package optional).
verify-rootless: dep
	@bash -eo pipefail -c '$(MAKE) test-environment profile=rootless required=true | tee test/reports/environment.json'
	@$(MAKE) lint scripts-lint sec vet examples-build
	@$(MAKE) specs profile=rootless required=true
	@$(MAKE) coverage

# Run rootless verification in a nonroot Docker container with the pinned syscall profile (image, arch required).
verify-rootless-container: export image := $(value image)
verify-rootless-container:
	@bash scripts/rootless "$$image" "$$GO_LINUX_TEST_ARCH"

# Upload coverage from a nonroot Docker container using the Go image (image required).
codecov-upload-container: export image := $(value image)
codecov-upload-container:
	@bash scripts/coverage "$$image"

# Stage the raw CI coverage profile in its facility/architecture workspace partition.
coverage-workspace-stage: export partition := $(value partition)
coverage-workspace-stage:
	@bash scripts/coverage-workspace stage "$$partition"

# Merge all fourteen CI workspace profiles into profile.cov before running coverage.
coverage-workspace-merge:
	@bash scripts/coverage-workspace merge

# Record prerequisites and run both required isolated Netlink suites.
verify-netlink:
	@bash -eo pipefail -c '$(MAKE) test-environment profile=namespaces required=true | tee test/reports/environment.json'
	@$(MAKE) specs package= PACKAGES="$(MODULE)/netlink $(MODULE)/netlink/route" profile=namespaces required=true

# Run required Netlink verification in privileged Docker (image and native arch required).
verify-netlink-container: export image := $(value image)
verify-netlink-container:
	@bash scripts/netlink "$$image" "$$GO_LINUX_TEST_ARCH"

# Record prerequisites and run required namespace, capability, and security privilege scenarios.
verify-namespace:
	@bash -eo pipefail -c '$(MAKE) test-environment profile=namespaces required=true | tee test/reports/environment.json'
	@$(MAKE) specs package= PACKAGES="$(MODULE)/namespace $(MODULE)/capability $(MODULE)/seccomp $(MODULE)/landlock" profile=namespaces required=true

# Run namespace, capability, and security privilege verification in privileged Docker (image and native arch required).
verify-namespace-container: export image := $(value image)
verify-namespace-container:
	@bash scripts/namespace "$$image" "$$GO_LINUX_TEST_ARCH"

# Record prerequisites and run required cgroup tests in an explicitly delegated subtree.
verify-cgroup:
	@bash -eo pipefail -c '$(MAKE) test-environment profile=cgroup required=true | tee test/reports/environment.json'
	@$(MAKE) specs package=cgroup profile=cgroup required=true

# Run required cgroup verification in a private Docker subtree (image and native arch required).
verify-cgroup-container: export image := $(value image)
verify-cgroup-container:
	@bash scripts/cgroup "$$image" "$$GO_LINUX_TEST_ARCH"

# Record namespace prerequisites and run required isolated mount tests (arch optional).
verify-mount:
	@bash -eo pipefail -c '$(MAKE) test-environment profile=namespaces required=true | tee test/reports/environment.json'
	@$(MAKE) specs package=mount profile=namespaces required=true

# Run required mount verification in privileged Docker (set image=<tag> and arch=amd64 or arm64).
verify-mount-container: export image := $(value image)
verify-mount-container:
	@bash scripts/mount "$$image" "$$GO_LINUX_TEST_ARCH"

# Record prerequisites and run required isolated descriptor/permission fanotify tests (arch optional).
verify-fanotify:
	@bash -eo pipefail -c '$(MAKE) test-environment profile=namespaces required=true | tee test/reports/environment.json'
	@$(MAKE) specs package=fanotify profile=namespaces required=true

# Run fanotify verification in privileged Docker (image and arch required).
verify-fanotify-container: export image := $(value image)
verify-fanotify-container:
	@bash scripts/fanotify "$$image" "$$GO_LINUX_TEST_ARCH"

# Run clock-changing timerfd tests only on a disposable VM (set clock_change=true).
verify-timerfd: export GO_LINUX_TEST_CLOCK_CHANGE := $(clock_change)
verify-timerfd: override export GOFLAGS := $(GOFLAGS) -tags=clockchange
verify-timerfd:
	@test "$$GO_LINUX_TEST_CLOCK_CHANGE" = true || { printf '%s\n' 'clock-changing tests require clock_change=true on a disposable VM' >&2; exit 2; }
	@bash -eo pipefail -c '$(MAKE) test-environment GOFLAGS="$$GOFLAGS" | tee test/reports/environment.json'
	@$(MAKE) vet GOFLAGS="$$GOFLAGS"
	@$(MAKE) specs package=timerfd profile=rootless required=true GOFLAGS="$$GOFLAGS"

# Run timerfd clock changes in privileged Docker on a disposable VM (image, arch, clock_change=true required).
verify-timerfd-container: export image := $(value image)
verify-timerfd-container: export GO_LINUX_TEST_CLOCK_CHANGE := $(clock_change)
verify-timerfd-container:
	@bash scripts/timerfd "$$image" "$$GO_LINUX_TEST_ARCH"

# Run go vet over the module, including test code.
vet:
	@go vet -mod=vendor ./...

# Check repository-owned Bash scripts with ShellCheck.
scripts-lint:
	@shellcheck -x scripts/coverage scripts/coverage-workspace scripts/rootless scripts/netlink scripts/namespace scripts/cgroup scripts/mount scripts/timerfd scripts/fanotify scripts/container.sh scripts/example-container

# Validate CircleCI configuration with the official CLI (requires CircleCI API access).
ci-validate:
	@circleci config validate .circleci/config.yml
