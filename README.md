# go-linux

Linux APIs with idiomatic Go interfaces.

`github.com/alexfalkowski/go-linux` provides focused Linux facilities that compose with standard Go interfaces. Packages are added sequentially through the implementation plan.

The library targets Linux 7.0+ and the Go version in `go.mod`. Primary runtime verification uses arm64 on CircleCI; amd64 compilation and available local execution are checked separately. Package verification status is recorded in the [implementation plan](IMPLEMENTATION.md).

## 📦 Packages

- [memfd](docs/memfd.md): memory-backed files, ordinary Go file I/O, and irreversible seals.
- [eventfd](docs/eventfd.md): event counters, semaphore reads, and pollable Go I/O with deadlines.
- [timerfd](docs/timerfd.md): kernel timers, explicit clock domains, and pollable expiration counts.
- [mmap](docs/mmap.md): owned anonymous/file mappings, page protection, and memory advice.
- [pidfd](docs/pidfd.md): process handles, signaling, exit observation, and eligible child reaping.

## 🚀 Setup

Use the Go version declared in [go.mod](go.mod).

Initialize the shared build tooling before using Make targets. Fetching the
submodule requires SSH access to GitHub:

```sh
git submodule sync && git submodule update --init
make dep
make help
```

The shared tooling uses `betteralign` and `golangci-lint` for linting,
`gotestsum` for tests, and `govulncheck` and Trivy for security checks. The CI
image supplies these tools; install them locally to run the corresponding
targets.

See [AGENTS.md](AGENTS.md) for contributor commands, the [implementation plan](IMPLEMENTATION.md) for work order, and [verification guidance](docs/verification.md) for profiles, reports, and runner requirements.
