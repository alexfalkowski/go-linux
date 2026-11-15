# AGENTS.md

## Shared guidance

Use `bin/AGENTS.md` for shared skills and cross-repository defaults.

## Repository

- Follow `IMPLEMENTATION.md` for remaining work, execution order, acceptance criteria, and open verification gaps. API conventions are in `docs/design/api.md`.
- Go library for Linux APIs; README lists implemented packages. `docs/verification.md` is a concise human contributor guide to commands, prerequisites, isolation/safety, reports, and a verified environment matrix. Keep detailed run results in CI reports and unfinished work or verification gaps in `IMPLEMENTATION.md`.
- Check `go.mod` for the module path and Go version.
- Keep dependencies and packages limited to implemented features.
- Design public APIs and examples to read like standard-library Go while preserving Linux names, typed modes/flags, record boundaries, clock domains, errno identity, and ownership. Use familiar Go interfaces and explicit resource operations where their contracts fit.
- Keep descriptor access and syscall plumbing internal. Callers should use typed facility operations without obtaining descriptors, invoking syscalls, or managing duplication/poll registration themselves. Do not expose `SyscallConn`, `Fd`, raw callbacks, or underlying-file fields as a substitute for a concrete operation; avoid embedding that unintentionally exposes them. Migrate existing exports with their replacements, callers, docs, and tests together, preserving documented ownership and compatibility decisions.
- Creation and adoption APIs own failure cleanup: return no resource on error, close everything acquired or consumed, and never require caller error-path cleanup. `Adopt` consumes its input even on failure; borrowing operations leave caller-owned inputs open. See `docs/design/api.md` for ownership contracts.
- Use `github.com/stretchr/testify/require` for every Go test assertion and explicit failure, including timeout paths; do not use `t.Fatal`, `t.Fatalf`, `t.Error`, `t.Errorf`, `t.Fail`, or `t.FailNow`. Keep `t.Skip` for unavailable optional prerequisites.
- Put reusable test setup, resource cleanup, descriptor inspection, subprocess, and synchronization helpers in `internal/test`. Keep assertions that explain a package's behavior beside its scenarios; do not hide a scenario in a generic test framework.
- Write ordinary behavior tests as clients of the typed public API. Repeated `syscall`/`unix` calls or inspection of implementation state should trigger a review of the API/test boundary. Verify effects, returned values, errors, lifetime, and concurrency; do not add public raw access, inspection methods, or test-only production hooks merely to make assertions convenient.
- Preserve focused real-kernel/ABI checks when they establish a documented Linux contract. Linux constants, errno comparisons, and independent syscall observations are appropriate there; put reusable OS setup and observations in `internal/test`. Keep these checks distinct from ordinary caller scenarios, and do not replace kernel evidence with mocks or weaken ownership, cleanup, or prerequisite coverage to avoid low-level code.
- Chosen Linux-only convention: use plain Go filenames without a `_linux` suffix and start every repository-owned Go file, including tests and `doc.go`, with `//go:build linux`. Preserve the standard `_test.go` suffix; leave dependency and submodule files unchanged.
- Link each implemented public package from README to its usage guide in `docs/<package>.md`. Keep design notes in `docs/design/<package>.md` focused on decisions, alternatives, rationale, and consequences that add value beyond usage guides and GoDoc; omit API inventories, implementation diaries, CI histories, and work-status lists.
- Before adding documentation, identify the reader's concrete need and check existing coverage. Update the authoritative document and link to it instead of duplicating it. Verify prose against current code and remove obsolete sketches and stale work references; keep unique runtime evidence in `docs/verification.md` and unfinished work in `IMPLEMENTATION.md`.

## Setup and commands

- Discover commands with `make help`; refresh dependencies with `make dep`.
- Quality checks: `make lint`, `make format`, `make specs`, `make sec`.
- Verification: `make test-environment`, `make vet`, and `make scripts-lint`; details and profile requirements are in `docs/verification.md`.
- Use `make specs profile=namespaces` for optional namespace coverage or add `required=true` to fail when its prerequisites are unavailable. JSON and JUnit results are written under `test/reports/`.
- Use `make verify-mount-container image=<tag> arch=<amd64|arm64>` for the same mount verification locally and in CI. Use the Go image reference from `.circleci/config.yml` on a native Linux Docker host that permits privileged containers. The container runs dependency setup and `make verify-mount` to record prerequisites and run required mount-only coverage.
- Scope tests with `package=<path> make specs` once feature packages exist.
- Generate coverage with `make coverage` after `make specs`.
- CircleCI configuration is in `.circleci/config.yml`.
