# procfs

`github.com/alexfalkowski/go-linux/procfs` provides a bounded, read-only process and system API on Linux 7.0+. Callers use this package; `github.com/prometheus/procfs` is an implementation dependency. No upstream types or embedded method sets are exported.

## Inspect a process

```go
fs, err := procfs.Open("/proc")
if err != nil {
    return err
}
process, err := fs.Self()
if err != nil {
    return err
}

stat, err := process.Stat()
if err != nil {
    return err
}
fmt.Println(process.PID(), stat.Name, stat.Threads, stat.VirtualMemory)
```

`Open` requires an explicit directory: `/proc`, an alternate proc mount, or a synthetic fixture root. Relative roots become absolute at construction. Empty paths and nondirectories match `os.ErrInvalid`; inaccessible and missing roots preserve their filesystem errors. Root and child symlinks are followed, so the root must be trusted and does not provide path confinement.

`FS` and `Process` are immutable path-based views. They retain no descriptors and have no `Close` method. Operations are safe concurrently, and returned maps and slices belong to the caller. Nil and zero views report `os.ErrInvalid`; `Process.PID()` returns zero for an uninitialized view.

Use `fs.Process(pid)` for a positive Linux PID or `fs.Self()` to resolve the selected root's `self` symlink. An alternate proc mount can use a different PID namespace, so an application-level PID need not identify the intended process there. `fs.Processes()` lists positive PID entries in numeric order without guaranteeing that they remain present. PID lookup checks only the directory, not permission to read its individual files.

`process.Stat()` reads selected [process-stat fields](https://man7.org/linux/man-pages/man5/proc_pid_stat.5.html). Names retain spaces, parentheses, and newlines; they are kernel comm names, not command lines or executable paths. CPU and start counters remain Linux USER_HZ ticks (100 per second on supported amd64/arm64); `StartTicks` is since boot, not Unix time. `ResidentPages` is in native pages and is approximate, while `VirtualMemory` is in bytes. Unknown one-character process states are retained. Executable [examples](../procfs/example_test.go) show complete calls.

## Inspect descriptors, mappings, and mounts

`process.Descriptors()` returns sorted descriptor numbers from [fd](https://man7.org/linux/man-pages/man5/proc_pid_fd.5.html). These are observations in the target process, not acquired descriptors. When inspecting the current process, the list may include the temporary directory descriptor used to enumerate itself. A listed number can disappear or be reused before inspection.

`process.Descriptor(number)` reads the common numeric [fdinfo](https://man7.org/linux/man-pages/man5/proc_pid_fdinfo.5.html) fields and the exact fd symlink text. It parses position as signed bytes, flags as octal, and mount/inode identifiers as decimal. Missing common fields and numeric overflow match `ErrParse`; inaccessible or disappearing entries keep their filesystem errors. Flags retain unknown bits. Test the access mode with `info.Flags & procfs.AccessMode`; `ReadOnly` is zero. Facility-specific fdinfo extensions are omitted. The fdinfo and symlink reads are separate and can describe different incarnations of a concurrently reused number.

`process.Maps()` returns [memory mappings](https://man7.org/linux/man-pages/man5/proc_pid_maps.5.html) in file order, including address ranges, file offsets, device/inode identifiers, typed permissions, and names. Addresses belong to the inspected process and are not dereferenceable pointers. Names retain internal/trailing whitespace, bracketed labels, and the ` (deleted)` suffix. Linux's textual `\012` for newline remains escaped because it is ambiguous with a literal backslash sequence. Empty maps are valid. Invalid records, scanner limits, and read failures discard the accumulated result; an individual record is limited to 1 MiB.

`process.Mounts()` returns selected [mountinfo](https://man7.org/linux/man-pages/man5/proc_pid_mountinfo.5.html) fields in file order. Linux's pathname escapes are decoded once, device components are typed, and all option names/values remain available. Bare options map to an empty string. This uses the upstream full-file reader, so the entire mountinfo file may exceed 1 MiB, although upstream's per-line scanner limit still applies. Propagation/other optional fields and mountstats are omitted from this initial API. Mount identifiers can be reused and paths are relative to the inspected process's root.

## Read system CPU and memory information

`fs.Stat()` returns basic [system counters](https://man7.org/linux/man-pages/man5/proc_stat.5.html), an aggregate CPU record, and a map keyed by CPU number. CPU values are cumulative floating-point seconds, not percentages or `time.Duration` values. Guest time is already included in user time; guest-nice is already included in nice. Do not add those components twice. I/O-wait accounting is approximate and may decrease between observations. At least one per-CPU entry is required; individual unreported counters remain zero, and absent boot time is the zero `time.Time`. A present boot time is represented in UTC.

`fs.Memory()` converts the seven selected [meminfo](https://man7.org/linux/man-pages/man5/proc_meminfo.5.html) quantities from Linux kB to bytes with checked conversion. All selected fields must exist; a missing value is distinct from a reported zero, including when swap is disabled. Values describe the kernel's system view and may exceed the process's cgroup limits. Neither memory nor CPU results are automatically restricted to cgroups or CPU affinity.

The upstream stat, meminfo, and fdinfo readers inspect at most the first 1 MiB of each file. This is sufficient for ordinary basic records, but unusually large system-stat files can omit trailing CPU/counter information. These readers are not an exhaustive capture-validation interface; upstream accepts some incomplete records and ignores additional fields. Use the package for the documented selected observations, without interpreting a successful read as validation of every byte in an arbitrary fixture.

## Errors and changing processes

Filesystem failures preserve errno through `errors.Is` and `errors.As`. Package validation matches `os.ErrInvalid`; detected malformed/incomplete selected data matches `procfs.ErrParse` inside a path error. Dependency parse errors and panics become that local error without exposing file contents or upstream error types. Operations return no partial result on error. This does not classify permission denials, process disappearance, or unavailable files as generic unsupported features.

Every operation reads afresh. A view cannot pin process identity: after exit and PID reuse it can describe a replacement process. Mapping changes, descriptor reuse, permission changes, and mount changes can race with reads, including within one multi-file operation. Several successful reads never establish an atomic snapshot. Use [pidfd](pidfd.md) for stable process operations; it does not make these path-based reads atomic.

Command lines, environments, smaps accounting, arbitrary proc entries, mutation, descriptor duplication, and polling are omitted. Fixture content should be synthetic rather than copied from a host, since names, paths, arguments, and statistics can disclose host information. The [design note](design/procfs.md) explains the dependency boundary and local parsing exception.

Run `make specs package=procfs profile=rootless required=true` for the fixture, live read-only, concurrency, cleanup, and example coverage. No namespace or host mutation is required. The permission test requires an ordinary user without DAC override; UID 0 skips it in optional runs and fails required coverage. [Verification](verification.md) owns commands and environment evidence; [IMPLEMENTATION.md](../IMPLEMENTATION.md#open-verification-gaps) owns missing native-baseline evidence.
