profile ?= rootless
required ?= false
kernel ?= supported

export GO_LINUX_TEST_PROFILE := $(profile)
export GO_LINUX_TEST_REQUIRED := $(required)
export GO_LINUX_TEST_ARCH := $(arch)
export GO_LINUX_TEST_KERNEL := $(kernel)

include bin/build/make/help.mak
include bin/build/make/go.mak
include bin/build/make/git.mak
include bin/build/make/claude.mak
include bin/build/make/codex.mak

# Preserve the shared harness while retaining every test event, including skips.
specs: export GOTESTSUM_JSONFILE := test/reports/specs.json

# Report Linux prerequisites as JSON; profile=namespaces selects isolation checks.
test-environment:
	@go run -mod=vendor ./internal/cmd/testenv

# Record namespace prerequisites and run required isolated mount tests (arch optional).
verify-mount:
	@mkdir -p test/reports
	@bash -eo pipefail -c '$(MAKE) --no-print-directory test-environment profile=namespaces required=true | tee test/reports/environment.json'
	@$(MAKE) --no-print-directory specs package=mount profile=namespaces required=true

# Run required mount verification in privileged Docker (set image=<tag> and arch=amd64 or arm64).
verify-mount-container: export image := $(value image)
verify-mount-container:
	@bash scripts/mount "$$image" "$$GO_LINUX_TEST_ARCH"

# Run clock-changing timerfd tests only on a disposable VM (set clock_change=true).
verify-timerfd: export GO_LINUX_TEST_CLOCK_CHANGE := $(clock_change)
verify-timerfd: override export GOFLAGS := $(GOFLAGS) -tags=clockchange
verify-timerfd:
	@test "$$GO_LINUX_TEST_CLOCK_CHANGE" = true || { printf '%s\n' 'clock-changing tests require clock_change=true on a disposable VM' >&2; exit 2; }
	@mkdir -p test/reports
	@bash -eo pipefail -c '$(MAKE) --no-print-directory test-environment GOFLAGS="$$GOFLAGS" | tee test/reports/environment.json'
	@$(MAKE) --no-print-directory vet GOFLAGS="$$GOFLAGS"
	@$(MAKE) --no-print-directory specs package=timerfd profile=rootless required=true GOFLAGS="$$GOFLAGS"

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
	@shellcheck scripts/mount scripts/timerfd

# Validate CircleCI configuration with the official CLI (requires CircleCI API access).
ci-validate:
	@circleci config validate .circleci/config.yml
