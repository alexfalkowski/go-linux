# 🧪 Runnable examples

Five small applications show how the library's Linux facilities compose. They are inspired by familiar tools and deliberately implement a small part of each tool. They add no module dependencies.

Use Linux 7.0+, the Go version in [go.mod](../go.mod), and the [repository setup](../README.md#-setup). Run Make from the repository root:

| Application | Start | Linux facilities |
| --- | --- | --- |
| [Process monitor](top/README.md), inspired by htop | `make example-top` | `procfs` observations and process identity across samples |
| [Directory watcher](watch/README.md), inspired by Watchman | `make example-watch` | `inotify` records, overflow handling, and `pidfd` child lifetime |
| [Process supervisor](supervise/README.md), inspired by systemd | `make example-supervise` | `pidfd` observation/signaling and `timerfd` boottime backoff |
| [Cache](redis/README.md), inspired by Redis | `make example-redis` | `timerfd` expiration sweeps, `mmap` snapshot loading, and `file.Rename` replacement |
| [Container runner](container/README.md), inspired by Docker | `make example-container` | `namespace`, `mount`, `capability`, `seccomp`, `landlock`, `pidfd`, and optional delegated `cgroup` limits |

The first four require no additional capabilities for their ordinary operations. The runner requires permitted user namespaces and mount/security operations; its guide includes the privileged Docker target for environments whose syscall policy refuses them. Ctrl-C stops a running example and initiates its child cleanup.

## 🔧 Arguments

Every target accepts `args` as a JSON array of literal arguments. This preserves spaces and special characters without adding shell interpretation:

```sh
make example-top args='["-once","-limit","5"]'
make example-supervise args='["--","/bin/echo","hello world"]'
make example-watch args='["-dir","/tmp/watched files","--","/bin/echo","changed"]'
make example-redis-cli args='["SET","hello","hello world"]'
```

Place application flags before `--` and the command that follows it. Use `args='["-help"]'` for a command's flags. Shell syntax is interpreted only when you explicitly choose a shell as the supervised command. For example, `"/bin/sh","-c","echo hello"` is an explicit shell invocation; a single argument containing `$(...)` remains literal otherwise.

The launcher builds executables into an owned temporary directory and removes it after the command exits. The container target also builds its static guest and creates a temporary root filesystem. You can also run an application directly, for example `go run ./examples/top -once`.

On cancellation, the Make launcher gives the watcher and supervisor their configured `-grace` period plus five seconds to finish child cleanup before forcing the application to exit. Other applications receive five seconds.

## 🛠️ Builds and coverage

`make examples-build` compiles every example, its launcher, and its guest. The standard rootless CI verification includes this build check. There are intentionally no tests for these applications. The default test package list and coverage instrumentation omit `examples/`; [.gocov](../.gocov) also removes example paths from final coverage reports. The facility packages retain their tested executable GoDoc examples.

These are teaching applications. Their command flags, output, and snapshot format are outside the library's [v1 compatibility promise](../docs/support.md#compatibility).

## 🧭 Reading the applications

Each application's `main.go` handles entrypoint concerns and composes packages under its own `internal/` directory. For example, the [cache entrypoint](redis/main.go) opens the [store](redis/internal/store/cache.go) and selects the [server](redis/internal/server/server.go) or [client](redis/internal/client/client.go). The command handler works through store methods; storage owns synchronization, expiry, and persistence without depending on RESP or networking.

The monitor separates collection, rendering, and scheduling. The watcher separates notification handling from child replacement. The supervisor separates command lifecycle from boottime delays. The container separates parent-side resource ownership, helper initialization/security, and the startup handshake. Child ownership shared across applications stays in [process](internal/process/process.go); application-specific behavior stays within each application's internal packages.
