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

## 📦 Packages

Choose a package by what you want to build. The tool links illustrate practical uses of the underlying Linux facilities independently of this module. Each package guide explains the supported operations and prerequisites.

| Package | Good for | Real-world reference |
| --- | --- | --- |
| [memfd](docs/memfd.md) | Preparing memory-backed payloads with optional irreversible seals | [PipeWire media buffers](https://docs.pipewire.org/page_library.html) |
| [eventfd](docs/eventfd.md) | Signaling workers or processes through a readiness loop | [QEMU device notifications](https://www.qemu.org/docs/master/devel/multi-process.html) |
| [timerfd](docs/timerfd.md) | Scheduling readiness events against an explicit Linux clock | [systemd event timers](https://github.com/systemd/systemd/blob/main/src/libsystemd/sd-event/sd-event.c) |
| [mmap](docs/mmap.md) | Reading files through memory mappings, sharing mapped regions, and locking mapped buffers in memory | [SQLite mapped reads](https://sqlite.org/mmap.html) |
| [pidfd](docs/pidfd.md) | Observing and signaling a stable process identity | [systemd child monitoring](https://www.freedesktop.org/software/systemd/man/latest/sd_event_add_child.html) |
| [mount](docs/mount.md) | Preparing and attaching filesystems inside an isolated namespace | [runc filesystem setup](https://github.com/opencontainers/runc/blob/main/THREAT_MODEL.md) |
| [epoll](docs/epoll.md) | Waiting for multiple kernel I/O sources in an explicit event loop | [Redis readiness backend](https://github.com/redis/redis/blob/unstable/src/ae_epoll.c) |
| [file](docs/file.md) | Restricting path lookup, querying metadata, renaming atomically, allocating file ranges, setting access advice, synchronizing storage, and locking files or byte ranges | [GNU mv atomic exchanges](https://www.gnu.org/software/coreutils/manual/coreutils.html#mv-invocation) |
| [xattr](docs/xattr.md) | Preserving file metadata in backups and restores | [GNU tar extended attributes](https://www.gnu.org/software/tar/manual/html_node/Extended-File-Attributes.html) |
| [inotify](docs/inotify.md) | Watching directories for rebuilds, indexing, or cache invalidation | [Watchman file watching](https://meta.github.io/watchman/docs/install) |
| [fanotify](docs/fanotify.md) | Inspecting accessed files and answering permission requests | [ClamAV on-access scanning](https://docs.clamav.net/manual/OnAccess.html) |
| [socket](docs/socket.md) | Exchanging Unix messages, open files, and credentials | [systemd service notifications](https://www.freedesktop.org/software/systemd/man/latest/sd_notify.html) |
| [netlink](docs/netlink.md) | Building kernel requests and discovering Generic Netlink families | [iw wireless configuration](https://wireless.docs.kernel.org/en/latest/en/users/documentation/iw.html) |
| [netlink/route](docs/netlink-route.md) | Inspecting and configuring interfaces, addresses, and routes | [iproute2 routing commands](https://github.com/iproute2/iproute2/blob/main/ip/iproute.c) |
| [namespace](docs/namespace.md) | Inspecting or entering isolated Linux resource views | [runc namespaces](https://github.com/opencontainers/runc/blob/main/THREAT_MODEL.md) |
| [cgroup](docs/cgroup.md) | Applying CPU, memory, and task limits in a delegated v2 hierarchy | [systemd resource control](https://www.freedesktop.org/software/systemd/man/latest/systemd.resource-control.html) |
| [capability](docs/capability.md) | Inspecting and reducing Linux thread privileges | [runc capability policy](https://github.com/opencontainers/runc/blob/main/THREAT_MODEL.md) |
| [seccomp](docs/seccomp.md) | Restricting the syscalls a workload may invoke | [runc syscall filters](https://github.com/opencontainers/runc/blob/main/THREAT_MODEL.md) |
| [landlock](docs/landlock.md) | Restricting filesystem access and TCP ports without a privileged helper | [Landrun sandboxing](https://landlock.io/integrations/) |
| [procfs](docs/procfs.md) | Building process monitors and system statistics collectors | [htop process collection](https://github.com/htop-dev/htop/blob/main/linux/LinuxProcessTable.c) |
| [sysfs](docs/sysfs.md) | Inspecting block devices and network-interface attributes | [lsblk device discovery](https://github.com/util-linux/util-linux/blob/master/lsblk-cmd/lsblk.8.adoc) |

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

For contributions, follow [AGENTS.md](AGENTS.md) and the [API conventions](docs/agents/accepted-design/api.md). The [accepted-design index](docs/agents/accepted-design.md) links to package decisions and rationale. [Verification guidance](docs/verification.md) owns checks, profiles, reports, and runner requirements.
