# Cgroup v2

`cgroup` opens an explicitly chosen v2 hierarchy root or delegated subtree. It provides owned group handles, child creation/removal, process membership, controller enablement, CPU bandwidth/weight, memory boundaries, task limits, and populated/frozen event snapshots. It targets Linux 7.0+; mounting, delegating permissions, v1, systemd D-Bus, threaded subtree setup, freezer/kill operations, and controllers beyond CPU, memory, and PIDs are outside this surface.

## Good for

Good for workload managers that apply CPU, memory, and task limits within an explicitly delegated cgroup v2 subtree. [systemd resource control](https://www.freedesktop.org/software/systemd/man/latest/systemd.resource-control.html) uses cgroups to organize and constrain services.

## Open and inspect

```go
group, err := cgroup.Open("/sys/fs/cgroup")
if err != nil {
    return err
}
defer group.Close()

controllers, err := group.Controllers()
if err != nil {
    return err
}
fmt.Println(controllers)
```

`Open` verifies the cgroup v2 filesystem. Its initial path uses ordinary symlink resolution. Child `Open`, `New`, and `Remove` take exactly one component and reject empty names, `.`, `..`, slashes, and NULs; child opens reject symlinks. A handle anchors the acquired directory even if its original path changes. It is not a security boundary against a privileged actor replacing mounts or changing the hierarchy concurrently.

`Close` releases only the directory reference. `Remove(name)` removes an empty child with Linux rmdir semantics; it never recursively deletes, kills processes, or moves them. Closing a parent leaves independent child handles and watchers open. Creation returns no owner on error and cleans up a newly created child if opening it fails; a cleanup failure is joined with the primary error. Coordinate lifecycle operations with other hierarchy owners rather than assuming exclusive access from permissions alone.

## Use a delegation

The delegator must grant the directory and the appropriate `cgroup.procs`, `cgroup.threads`, and `cgroup.subtree_control` permissions, and enable the desired controllers in its own subtree control. A systemd-managed path or UID 0 alone is insufficient evidence of delegation. The library never changes ancestors, creates a mount, grants permissions, or moves itself automatically. See the upstream [delegation contract](https://docs.kernel.org/admin-guide/cgroup-v2.html#delegation).

For a caller-owned empty domain group, explicitly enable controllers before creating leaf workloads:

```go
if err := group.SetControllers([]cgroup.Controller{cgroup.CPU, cgroup.Memory, cgroup.PIDs}, nil); err != nil {
    return err
}
worker, err := group.New("worker")
if err != nil {
    return err
}
defer worker.Close()

if err := worker.SetCPUMax(cgroup.CPUBandwidth{
    Quota: 50 * time.Millisecond,
    Period: 100 * time.Millisecond,
}); err != nil {
    return err
}
if err := worker.SetMemoryMax(cgroup.Limit(256 << 20)); err != nil {
    return err
}
if err := worker.SetPIDsMax(64); err != nil {
    return err
}
```

The caller still removes `worker` after all its processes and descendants have gone. An error after some settings were applied does not roll them back. `SetControllers(enable, disable)` sends one command, so Linux accepts or rejects its changes together; unmentioned controllers remain unchanged. Names not supported for mutation and duplicate/overlapping requests match `os.ErrInvalid`. Inspection retains unknown controller names. Enabling domain controllers in a populated non-root group violates Linux's no-internal-process constraint, and disabling a controller needed by descendants fails. Read `Controllers` for availability and `SubtreeControl` for what is enabled for immediate children; neither establishes write permission. See [controller configuration](https://docs.kernel.org/admin-guide/cgroup-v2.html#controlling-controllers).

`AddProcess(pid)` migrates a positive numeric PID's whole thread group through `cgroup.procs`; use `os/exec` and a child handshake to keep application work from starting before migration. No atomic create-into-group startup guarantee is provided. Linux also requires write access to the common ancestor of source and destination. PID exit/reuse can race with the call; a returned membership list is unordered, can contain duplicates, and can include zero for processes invisible in the reader's PID namespace. Existing memory charges do not migrate with the process. See [process organization](https://docs.kernel.org/admin-guide/cgroup-v2.html#organizing-processes-and-threads).

## Limits and counters

| Surface | Meaning |
| --- | --- |
| `CPUWeight` / `SetCPUWeight` | `cpu.weight`, 1–10000; Linux's default is 100. |
| `CPUMax` / `SetCPUMax` | Complete `cpu.max` record; quota and period use `time.Duration` with exact microsecond precision. `UnlimitedQuota` is the explicit unlimited quota; period must be 1 ms–1 s. CPU controls apply to fair-class tasks. |
| `CPUStat` | `cpu.stat`, preserving unsigned keys and Linux units such as `usage_usec`. |
| `MemoryHigh` / `SetMemoryHigh` | `memory.high`, byte-valued reclaim/throttling boundary. |
| `MemoryMax` / `SetMemoryMax` | `memory.max`, hard byte boundary that can lead to cgroup OOM kills. |
| `MemoryCurrent` / `MemoryEvents` | Current hierarchical bytes and `memory.events` counters. `memory_localevents` can change event scope. |
| `PIDsMax` / `SetPIDsMax` | `pids.max`, counting tasks/threads. Zero prevents new task creation; migration can exceed the limit. |
| `PIDsCurrent` / `PIDsEvents` | Hierarchical task count and `pids.events` counters; `pids_localevents` can change event scope. |

`Limit` is a finite nonnegative int64 byte/task count or `Max`, encoded as `max`. Zero always means zero. Negative values other than `Max` are invalid. Ancestor limits continue to apply. Memory setters open with `O_NONBLOCK`, bypassing synchronous reclaim/OOM work in the writer; enforcement occurs on subsequent charges and usage can remain above the requested boundary indefinitely without new charges. The kernel can round memory boundaries to page size. Multiple reads do not form an atomic snapshot. See the [CPU](https://docs.kernel.org/admin-guide/cgroup-v2.html#cpu), [memory](https://docs.kernel.org/admin-guide/cgroup-v2.html#memory), and [PID](https://docs.kernel.org/admin-guide/cgroup-v2.html#pid) controller specifications.

`Limit` and `CPUBandwidth` implement text marshaling/unmarshaling for persisted native records. `ParseCounters` and `ParseEvents` decode captured snapshots without opening a hierarchy; malformed input returns no partial result, and failed text unmarshaling leaves its receiver unchanged. Unknown unsigned counter/event fields are preserved. Decimal counts outside the supported type's range are rejected.

## Observe membership changes

`Events()` reads `cgroup.events` once. For notifications, call `WatchEvents()` before taking the initial snapshot, set a read deadline, then call `Read()` or iterate `Events()` on the returned watcher. Watch registration uses the event file's pinned identity through `/proc/self/fd`; procfs at `/proc` and inotify support are prerequisites. There is no background goroutine.

An `Event` reports current `Populated` and `Frozen` state and retains unknown unsigned fields. Notifications coalesce and may report the same state again; they are hints to reconcile a snapshot, not a lossless transition log. `Read` waits for a notification rather than returning an initial snapshot. Queue overflow matches `ENOBUFS`; reread the group's state before resuming. Removal/unmount matches `os.ErrNotExist`. While waiting, `Read` checks the pinned event file every 100 milliseconds to detect removal even when Linux emits no inode notification; scheduling can delay detection. These checks neither return snapshots nor expire the caller's deadline. The real hierarchy root lacks `cgroup.events` and normally returns `ENOENT`; namespace-root mounts can expose a non-root group's file. See [populated notification](https://docs.kernel.org/admin-guide/cgroup-v2.html#un-populated-notification).

Use one watcher reader/iterator at a time; `Close` and `SetReadDeadline` can run concurrently. Close interrupts a pending read with `os.ErrClosed`; deadlines match `os.ErrDeadlineExceeded`. Iteration yields a terminal error once, then ends. Breaking performs no further I/O, and reusing the same sequence resumes until a terminal error. Group operations are safe concurrently, but sequences of configuration and lifecycle calls require caller coordination. Zero owners match `os.ErrInvalid`; repeated close and valid operations after closure match `os.ErrClosed`. Invalid arguments precede closed state. Kernel errors preserve errno; missing controllers, wrong topology, permission failures, and read-only mounts remain distinguishable.

## Coverage boundary

The named mutation controllers are exactly CPU, memory, and PIDs. [Linux 7.0 cgroup v2](https://docs.kernel.org/7.0/admin-guide/cgroup-v2.html) also defines I/O, cpuset, hugetlb, RDMA, misc, and device-memory controls; those are not typed operations here. Unknown controller names can be observed but cannot be enabled by casting them into `Controller`. Configured/effective CPU and memory-node sets, per-device I/O limits/statistics, and controller-specific resource identities are absent.

`cpu.stat` and the represented event maps retain unknown unsigned counters, without supplying missing file operations. CPU burst/idle/utilization controls and pressure, memory min/low/swap/reclaim/stat/pressure and local-event files, and additional PIDs observations are outside the current set. Frozen snapshots do not provide freeze/thaw commands; populated events do not provide recursive kill. There is no typed command-start integration, so post-start migration cannot establish first-instruction resource placement/accounting. Go's cgroup startup fields require an internal bridge to this owned handle before callers can use that guarantee without raw access.

Threaded hierarchies, ancestor/systemd management, and BPF device-access policy remain separate from the supported delegated domain-group operations. Closing a group neither kills descendants nor removes their groups.

See [executable examples](../cgroup/example_test.go), the [design rationale](agents/accepted-design/cgroup.md), and [verification requirements and delegated runtime evidence](verification.md#cgroup-verification). Known defensive execution limits are recorded in [verification limits](verification-limits.md).
