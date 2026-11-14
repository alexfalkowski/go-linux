# AGENTS.md

## Shared guidance

Use `bin/AGENTS.md` for shared skills and cross-repository defaults.

## Repository

- Follow `IMPLEMENTATION.md` for execution order, acceptance criteria, verification gaps, and handoffs. API conventions are in `docs/design/api.md`.
- Go library for Linux APIs; currently a scaffold without API implementations.
- Check `go.mod` for the module path and Go version.
- Keep dependencies and packages limited to implemented features.
- Put package documentation in `doc.go` and document exported identifiers.

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
