![Gopher](assets/gopher.png?v=163x174)
[![CircleCI](https://circleci.com/gh/alexfalkowski/go-linux.svg?style=svg)](https://circleci.com/gh/alexfalkowski/go-linux)
[![codecov](https://codecov.io/gh/alexfalkowski/go-linux/graph/badge.svg)](https://codecov.io/gh/alexfalkowski/go-linux)
[![Go Reference](https://pkg.go.dev/badge/github.com/alexfalkowski/go-linux.svg)](https://pkg.go.dev/github.com/alexfalkowski/go-linux)
[![Stability: Active](https://masterminds.github.io/stability/active.svg)](https://masterminds.github.io/stability/active.html)

# go-linux

Linux APIs with idiomatic Go interfaces.

`github.com/alexfalkowski/go-linux` provides focused Linux facilities that compose with standard Go interfaces. Packages are added sequentially through the implementation plan.

The library targets Linux 7.0+ and the Go version in `go.mod`. CircleCI requests native arm64 and amd64 runtime verification, including isolated mount tests. Runtime evidence is in the [verification guide](docs/verification.md); unfinished work and open gaps are tracked in the [implementation plan](IMPLEMENTATION.md).

## 📦 Packages

- [memfd](docs/memfd.md): memory-backed files, ordinary Go file I/O, and irreversible seals.
- [eventfd](docs/eventfd.md): event counters, semaphore reads, and pollable Go I/O with deadlines.
- [timerfd](docs/timerfd.md): kernel timers, explicit clock domains, and pollable expiration counts.
- [mmap](docs/mmap.md): owned anonymous/file mappings, page protection, and memory advice.
- [pidfd](docs/pidfd.md): process handles, signaling, exit observation, and eligible child reaping.
- [mount](docs/mount.md): filesystem configuration, detached mount handles, cloning, attributes, and explicit attachment.
- [epoll](docs/epoll.md): explicit readiness sets, stable registrations, trigger modes, and interruptible waits.
- [file](docs/file.md): explicit path resolution, extended metadata, and atomic rename modes on standard files.
- [xattr](docs/xattr.md): whole extended attributes, atomic set modes, and explicit file/path/symlink selection.
- [inotify](docs/inotify.md): file and directory watches, record decoding, rename cookies, and explicit queue-overflow events.
- [fanotify](docs/fanotify.md): event-owned file inspection and explicit filesystem permission decisions.
- [socket](docs/socket.md): Unix and Netlink message sockets, deadlines, typed file passing, and credentials.

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
