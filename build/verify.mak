profile ?= rootless
required ?= false
kernel ?= supported

export GO_LINUX_TEST_PROFILE := $(profile)
export GO_LINUX_TEST_REQUIRED := $(required)
export GO_LINUX_TEST_ARCH := $(arch)
export GO_LINUX_TEST_KERNEL := $(kernel)

# Preserve the shared harness while retaining every test event, including skips.
specs: export GOTESTSUM_JSONFILE := test/reports/specs.json

# Report Linux prerequisites as JSON; profile=namespaces selects isolation checks.
test-environment:
	@go run -mod=vendor ./internal/cmd/testenv

# Compile all packages and tests for linux/amd64 and linux/arm64 without execution.
compile:
	@bash build/compile

# Run go vet over the module, including test code.
vet:
	@go vet -mod=vendor ./...

# Check repository-owned Bash scripts with ShellCheck.
scripts-lint:
	@shellcheck build/compile

# Validate CircleCI configuration with the official CLI (requires CircleCI API access).
ci-validate:
	@circleci config validate .circleci/config.yml
