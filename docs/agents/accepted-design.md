# Accepted design

This index records go-linux's accepted design choices, tradeoffs, and support
boundaries. The area files carry the same weight as [AGENTS.md](../../AGENTS.md).

Read the relevant area files before proposing a change or recording a finding
or audit entry. Revisit a decision when the owner asks or new evidence changes
its documented contract or tradeoff.

- [API conventions](accepted-design/api.md) covers the standard-library boundary,
  descriptor ownership, record boundaries, clocks, errors, and compatibility.
- [capability](accepted-design/capability.md) covers typed masks, thread scope,
  and exec policy.
- [cgroup](accepted-design/cgroup.md) covers hierarchy ownership, controller
  operations, and event notification.
- [epoll](accepted-design/epoll.md) covers registration ownership, event
  identity, and poller shutdown.
- [eventfd](accepted-design/eventfd.md) covers counter records and Go polling.
- [fanotify](accepted-design/fanotify.md) covers reporting modes, event file
  ownership, decoding, and iteration.
- [file](accepted-design/file.md) covers operations on standard files, kernel
  operation fidelity, and metadata validity.
- [inotify](accepted-design/inotify.md) covers watch identity, queue records,
  and polling.
- [landlock](accepted-design/landlock.md) covers policy validation, ruleset
  lifetime, and thread scope.
- [memfd](accepted-design/memfd.md) covers the file wrapper and sealing defaults.
- [mmap](accepted-design/mmap.md) covers mapping ownership and slice lifetime.
- [mount](accepted-design/mount.md) covers configuration, attachment, cleanup,
  and filesystem diagnostics.
- [namespace](accepted-design/namespace.md) covers reference ownership, Go
  process creation, and thread retirement.
- [netlink](accepted-design/netlink.md) covers transport, request exchange,
  bounded reception, and recovery.
- [netlink/route](accepted-design/netlink-route.md) covers routing operations,
  observations, and request application.
- [pidfd](accepted-design/pidfd.md) covers process identity, interruptible waits,
  and reaping.
- [procfs](accepted-design/procfs.md) covers dependency reuse, observation
  semantics, and data translation.
- [seccomp](accepted-design/seccomp.md) covers filter scope, ABI guards, and
  partial application.
- [socket](accepted-design/socket.md) covers message I/O, polling, and ancillary
  resource ownership.
- [sysfs](accepted-design/sysfs.md) covers bounded discovery, attribute parsing,
  and observation semantics.
- [timerfd](accepted-design/timerfd.md) covers clock domains, exceptional reads,
  and readiness lifetime.
- [xattr](accepted-design/xattr.md) covers borrowed files, the Linux baseline
  interface, and bounded observations.
