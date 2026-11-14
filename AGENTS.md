# AGENTS.md

## Shared guidance

Use `bin/AGENTS.md` for shared skills and cross-repository defaults.

## Repository

- Follow `IMPLEMENTATION.md` for execution order, acceptance criteria, verification gaps, and handoffs. API conventions are in `docs/design/api.md`.
- Go library for Linux APIs; implemented packages and verification status are tracked in `IMPLEMENTATION.md`.
- Check `go.mod` for the module path and Go version.
- Keep dependencies and packages limited to implemented features.
- Put package documentation in `doc.go` and document exported identifiers.
- Use `github.com/stretchr/testify/require` for Go test assertions.
- Chosen Linux-only convention: use plain Go filenames without a `_linux` suffix and start every repository-owned Go file, including tests and `doc.go`, with `//go:build linux`. Preserve the standard `_test.go` suffix; leave dependency and submodule files unchanged.
- Group only related code, and separate distinct steps with blank lines for readability.
- Keep README concise: link each implemented public package to its usage guide in `docs/<package>.md`. Keep design rationale in `docs/design/<package>.md` and executable examples in package tests.

## Setup and commands

- Before Make targets can load in a fresh clone, run
  `git submodule sync && git submodule update --init` (requires SSH access).
- Discover commands with `make help`; refresh dependencies with `make dep`.
- Quality checks: `make lint`, `make format`, `make specs`, `make sec`.
- Verification: `make test-environment`, `make vet`, `make compile`, and `make scripts-lint`; details and profile requirements are in `docs/verification.md`.
- Use `make specs profile=namespaces` for optional namespace coverage or add `required=true` to fail when its prerequisites are unavailable. JSON and JUnit results are written under `test/reports/`.
- Tests use the shared `gotestsum` harness and write to `test/reports/`.
- Scope tests with `package=<path> make specs` once feature packages exist.
- Generate coverage with `make coverage` after `make specs`.
- CircleCI configuration is in `.circleci/config.yml`.
