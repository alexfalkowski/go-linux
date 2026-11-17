![Gopher](assets/gopher.png?v=163x174)
[![CircleCI](https://circleci.com/gh/alexfalkowski/go-linux.svg?style=svg)](https://circleci.com/gh/alexfalkowski/go-linux)
[![codecov](https://codecov.io/gh/alexfalkowski/go-linux/graph/badge.svg)](https://codecov.io/gh/alexfalkowski/go-linux)
[![Go Reference](https://pkg.go.dev/badge/github.com/alexfalkowski/go-linux.svg)](https://pkg.go.dev/github.com/alexfalkowski/go-linux)
[![Stability: Active](https://masterminds.github.io/stability/active.svg)](https://masterminds.github.io/stability/active.html)

# go-linux

Linux APIs with idiomatic Go interfaces.

`github.com/alexfalkowski/go-linux` provides 21 focused Linux facilities that compose with standard Go interfaces. Use it for kernel features that ordinary file, network, and process APIs do not express. Continue using the standard library for ordinary Go I/O and process creation.

The library targets Linux 7.0+ and the Go version in `go.mod`. CircleCI requests native arm64 and amd64 runtime verification, including isolated mount tests. The [verification guide](docs/verification.md) records runtime evidence and links to known execution limits.

## 💻 First use

Add the module to a Linux Go application:

```sh
go get github.com/alexfalkowski/go-linux
```

Import the facility you need. For example, create a memory-backed file using ordinary Go I/O:

```go
//go:build linux

package main

import (
    "fmt"
    "os"

    "github.com/alexfalkowski/go-linux/memfd"
)

func main() {
    file, err := memfd.New("hello", memfd.Options{})
    if err != nil {
        panic(err)
    }
    defer file.Close()

    if _, err := file.WriteString("hello, Linux\n"); err != nil {
        panic(err)
    }

    info, err := file.Stat()
    if err != nil {
        panic(err)
    }
    fmt.Fprintln(os.Stdout, info.Size()) // 13
}
```

See [memfd usage](docs/memfd.md) for seals and [executable examples](memfd/example_test.go) for shared mappings. The [support guide](docs/support.md) covers feature prerequisites, compatibility, omissions, and dependency licenses.

## 🧪 Showcase applications

Try small applications inspired by htop, Watchman, systemd, Redis, and Docker:

```sh
make example-top args='["-once"]'
make example-watch
make example-supervise
make example-redis
make example-container
```

Run one long-lived application per terminal. [Example walkthroughs](examples/README.md) cover arguments, the bundled cache client, snapshots, and the container demo's additional prerequisites and Docker target. The applications add no dependencies and are excluded from tests and coverage; `make examples-build` compiles them.

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
- [netlink](docs/netlink.md): kernel requests, extended acknowledgements, bounded dumps, family discovery, and multicast reception.
- [netlink/route](docs/netlink-route.md): typed link, IPv4/IPv6 address, and route configuration with multicast changes.
- [namespace](docs/namespace.md): owned namespace handles, identity and hierarchy inspection, and explicit thread/child transitions.
- [cgroup](docs/cgroup.md): explicit v2 group lifecycle, membership, resource limits, and populated/frozen notifications.
- [capability](docs/capability.md): thread capability sets, irreversible bounding drops, and explicit ambient transitions.
- [seccomp](docs/seccomp.md): bounded syscall filters, mandatory native ABI guards, and explicit thread synchronization.
- [landlock](docs/landlock.md): owned access rulesets, typed filesystem/port rules, IPC scopes, and explicit thread synchronization.
- [procfs](docs/procfs.md): read-only process/system statistics, descriptor observations, maps, and mount information with an explicit root.
- [sysfs](docs/sysfs.md): read-only block-device and network-interface attributes, typed identity and units, and explicit byte reads with an explicit root.

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

For contributions, follow [AGENTS.md](AGENTS.md) and the [API conventions](docs/design/api.md). [Verification guidance](docs/verification.md) owns checks, profiles, reports, and runner requirements. Keep package behavior in GoDoc and its linked usage guide, with rationale in the corresponding design note.
