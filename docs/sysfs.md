# Sysfs usage

Package `sysfs` provides read-only block-device and network-interface observations from an explicit sysfs root. It uses private Prometheus sysfs discovery behind a project-owned API, following the [procfs wrapper](procfs.md). The module targets Linux 7.0+ and the Go version in `go.mod`.

## Good for

Good for inventory and monitoring tools that inspect block-device or network-interface attributes. [lsblk reads device information from sysfs](https://github.com/util-linux/util-linux/blob/master/lsblk-cmd/lsblk.8.adoc); this package provides read-only observations from an explicit root.

## Discover and inspect

```go
fs, err := sysfs.Open("/sys")
if err != nil {
    return err
}
names, err := fs.NetworkInterfaces()
if err != nil {
    return err
}
for _, name := range names {
    device, err := fs.NetworkInterface(name)
    if err != nil {
        return err
    }
    info, err := device.Info()
    if err != nil {
        return err
    }
    fmt.Println(name, info.Index, info.MTU, info.Flags&sysfs.Up != 0)
}
```

Import `github.com/alexfalkowski/go-linux/sysfs`. `Open` accepts a mount, bind mount, or ordinary fixture directory; relative roots become absolute. Views keep no descriptors and need no `Close`. Nil and zero views return `os.ErrInvalid`. Executable examples are in [sysfs/example_test.go](../sysfs/example_test.go).

`BlockDevices` lists `class/block`, including partitions. `NetworkInterfaces` lists `class/net`. Both return lexical name order without checking each entry's target, so a listed name may already have disappeared. `BlockDevice(name)` and `NetworkInterface(name)` check the selected directory, following normal class symlinks. Names must be single nonempty path components other than `.` and `..`.

`BlockDevice.Info` requires `dev`, `size`, and `ro`. Device major/minor components remain separate integers. Capacity is bytes, using the kernel's fixed 512-byte sectors with checked multiplication, regardless of `queue/logical_block_size` or `queue/physical_block_size`. Zero capacity is valid. `ReadOnly` reports the block layer's `ro` bit; it does not establish filesystem permissions or whether a write would succeed.

`NetworkInterface.Info` requires `ifindex`, `mtu`, `flags`, and `type`. Indexes are positive signed-32-bit Linux indexes; MTU is bytes excluding link headers. Flags retain native Linux `IFF_*` values and unknown bits, unlike `net.Flags`. Administrative `Up` does not establish carrier or operational state. Hardware types retain unknown `ARPHRD_*` values. See the [Linux network ABI](https://github.com/torvalds/linux/blob/v7.0/Documentation/ABI/testing/sysfs-class-net) and [block ABI](https://github.com/torvalds/linux/blob/v7.0/Documentation/ABI/stable/sysfs-block) for the source attributes.

## Read attributes explicitly

`Attributes` lists immediate regular-file names without opening them, including unknown names. It excludes symlinks and subdirectories. `ReadAttribute("queue/logical_block_size")` on a block view or `ReadAttribute("statistics/rx_bytes")` on a network view reads a specific nested file. Clean relative UTF-8 paths are required: empty paths, absolute paths, NULs, and dot/dot-dot components return `os.ErrInvalid`.

Reads preserve bytes exactly, including trailing newlines, whitespace, NULs, multi-line data, and unknown formats. Each result belongs to the caller. No universal text or numeric format is imposed on attributes. For example, network `speed` uses Mbits/sec, can report an unknown value, and may instead return a driver error; it is not an unsigned byte rate. Optional driver properties are excluded from `Info` so an unavailable `speed` or `carrier` does not prevent reading basic identity.

Each attribute read is limited to 1 MiB. Larger files return `ErrLimit` with no partial bytes. Nonregular targets, including FIFOs, return `os.ErrInvalid`; the FIFO open is nonblocking. This is a bounded attribute reader, not a general binary sysfs file or streaming API. It provides no deadline for kernel driver callbacks.

## Errors, lifetime, and scope

Filesystem errors retain native errno through `errors.Is` and `errors.As`, with path context. Missing attributes match `os.ErrNotExist`; permission and driver errors remain distinguishable. Typed malformed values, invalid booleans, and numeric overflow match `sysfs.ErrParse` without including file contents in error messages. Failed operations return no partial result. No attribute error is silently treated as zero or an unsupported feature.

Views are immutable and safe for concurrent reads. Results describe separate filesystem lookups, not an atomic snapshot. Hotplug, permission changes, mount replacement, and name/index reuse can affect any call; a retained view may observe a replacement device. Callers decide how to handle disappearance during enumeration. Sysfs visibility follows the supplied mount and namespace; it is not necessarily a container-local hardware inventory.

Roots and their symlinks are trusted. Making a root absolute does not pin its inode or confine resolution; explicit attribute reads follow symlinks, which may point outside that root. The package never writes sysfs. Broad mutation, driver control, recursive inventory collection, hardware monitoring, and device-specific parsers remain outside this initial surface. [Design rationale](agents/accepted-design/sysfs.md) explains the wrapper and parsing boundary; [verification](verification.md) describes contributor commands and runtime evidence.
