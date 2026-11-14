# AGENTS.md

## Shared guidance

Use `bin/AGENTS.md` for shared skills and cross-repository defaults.

## Repository

- Follow `IMPLEMENTATION.md` for execution order, acceptance criteria, verification gaps, and handoffs. API conventions are in `docs/design/api.md`.
- Go library for Linux APIs; implemented packages and verification status are tracked in `IMPLEMENTATION.md`.
- Check `go.mod` for the module path and Go version.
- Keep dependencies and packages limited to implemented features.
- Design public APIs and examples to read like standard-library Go while preserving Linux names, typed modes/flags, record boundaries, clock domains, errno identity, and ownership. Use familiar Go interfaces and explicit resource operations where their contracts fit.
- Keep descriptor access and syscall plumbing internal. Callers should use typed facility operations without obtaining descriptors, invoking syscalls, or managing duplication/poll registration themselves. Do not expose `SyscallConn`, `Fd`, raw callbacks, or underlying-file fields as a substitute for a concrete operation; avoid embedding that unintentionally exposes them. Migrate existing exports with their replacements, callers, docs, and tests together, preserving documented ownership and compatibility decisions.
- Put package documentation in `doc.go` and document exported identifiers.
- Creation and adoption APIs own failure cleanup: return no resource on error, close everything acquired or consumed, and never require caller error-path cleanup. `Adopt` consumes its input even on failure; borrowing operations leave caller-owned inputs open. See `docs/design/api.md` for ownership contracts.
- Use `github.com/stretchr/testify/require` for every Go test assertion and explicit failure, including timeout paths; do not use `t.Fatal`, `t.Fatalf`, `t.Error`, `t.Errorf`, `t.Fail`, or `t.FailNow`. Keep `t.Skip` for unavailable optional prerequisites.
- Put reusable test setup, resource cleanup, descriptor inspection, subprocess, and synchronization helpers in `internal/test`. Keep assertions that explain a package's behavior beside its scenarios; do not hide a scenario in a generic test framework.
- Write ordinary behavior tests as clients of the typed public API. Repeated `syscall`/`unix` calls or inspection of implementation state should trigger a review of the API/test boundary. Verify effects, returned values, errors, lifetime, and concurrency; do not add public raw access, inspection methods, or test-only production hooks merely to make assertions convenient.
- Preserve focused real-kernel/ABI checks when they establish a documented Linux contract. Linux constants, errno comparisons, and independent syscall observations are appropriate there; put reusable OS setup and observations in `internal/test`. Keep these checks distinct from ordinary caller scenarios, and do not replace kernel evidence with mocks or weaken ownership, cleanup, or prerequisite coverage to avoid low-level code.
- Prefer tables with descriptive case names for related inputs, modes, and error cases. Keep table rows as data where practical, and avoid switches on case names or positional/numeric labels that obscure the behavior.
- Give each test one coherent behavior to verify. Split unrelated contracts into focused tests or named subtests, and compact repeated assertions without removing meaningful coverage, ownership checks, or prerequisite distinctions.
- Keep assertion blocks short: compare complete results instead of checking each field separately, use small shared helpers for repeated success/error shapes, and keep the expected behavior visible in their arguments. Avoid repeating evidence already established by the same scenario.
- Name tests and subtests as readable descriptions of expected behavior. Check the names printed by `make specs`; a failure should identify the behavior and case without requiring the reader to inspect the implementation.
- Chosen Linux-only convention: use plain Go filenames without a `_linux` suffix and start every repository-owned Go file, including tests and `doc.go`, with `//go:build linux`. Preserve the standard `_test.go` suffix; leave dependency and submodule files unchanged.
- Group only related code, and separate distinct steps with blank lines for readability. In tests and test helpers, separate setup, actions, and later observations into visual paragraphs. Keep an operation beside its error/result checks, related assertions together, and short cleanup sequences together. Do not insert a blank line after every call or assertion.
- Write named functions, anonymous functions, and callbacks as multiline blocks, even when short. Keep each statement on its own line; do not compress test cases, cleanup, or error handling into one-line functions.
- Keep README concise: link each implemented public package to its usage guide in `docs/<package>.md`. Keep design rationale in `docs/design/<package>.md` and executable examples in package tests.

## Setup and commands

- Before Make targets can load in a fresh clone, run
  `git submodule sync && git submodule update --init` (requires SSH access).
- Discover commands with `make help`; refresh dependencies with `make dep`.
- Quality checks: `make lint`, `make format`, `make specs`, `make sec`.
- Verification: `make test-environment`, `make vet`, `make compile`, and `make scripts-lint`; details and profile requirements are in `docs/verification.md`.
- Use `make specs profile=namespaces` for optional namespace coverage or add `required=true` to fail when its prerequisites are unavailable. JSON and JUnit results are written under `test/reports/`.
- Use `make verify-mount` after `make dep` on a capable Linux host to record namespace prerequisites and run required mount-only coverage. CI adds `arch=arm64`; local checks can omit it.
- Tests use the shared `gotestsum` harness and write to `test/reports/`.
- Scope tests with `package=<path> make specs` once feature packages exist.
- Generate coverage with `make coverage` after `make specs`.
- CircleCI configuration is in `.circleci/config.yml`.
