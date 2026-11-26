# Sysfs design

Part of the [accepted-design index](../accepted-design.md).

## Reuse discovery behind a bounded API

Like [procfs](procfs.md), this package owns its public types and keeps the [Prometheus dependency](https://github.com/prometheus/procfs/tree/v0.23.0/sysfs) private. Its sysfs filesystem and network-class enumeration support the explicit-root design without adding another dependency family. Importing that package also requires its existing `golang.org/x/sync` dependency. Exposing or embedding the upstream FS would commit this library to unrelated hardware collectors and future upstream methods.

The block inventory uses `class/block` because it includes partitions as well as whole devices. The upstream blockdevice reader enumerates `block`, and its constructor accepts both proc and sys roots. Using it for this operation would introduce a proc-root requirement and omit the intended partition inventory. Direct class enumeration is smaller than adapting those semantics.

## Interpret only selected, validated formats

The initial typed summaries cover basic device numbers, capacity, read-only state, interface index, MTU, flags, and hardware type. Their source attributes are defined by [Linux 7.0](https://github.com/torvalds/linux/tree/v7.0/Documentation/ABI). All selected fields are required, so unavailable input cannot be confused with a valid zero. Byte conversion is checked and unknown flag bits and hardware types survive inspection.

Prometheus v0.23.0's network attribute reader scans every immediate file, trims text, and reads at most 128 bytes. It omits some read failures and does not check ValueParser.Err after parsing numbers, allowing malformed values to appear as zero. Its block size reader multiplies sectors by 512 without an overflow check. Those contracts cannot provide exact raw bytes, distinct filesystem errors, and strict selected-field validation. The local parser therefore reads a narrow set of explicit scalar attributes instead of copying a general sysfs collector. Reassess this boundary if an upstream public reader preserves these contracts.

Attribute discovery lists names without opening files. Explicit byte reads preserve unknown attributes and nested formats without forcing a complete eager snapshot, silently skipping driver failures, or inventing a shared format for text, arrays, and binary files. Optional properties such as speed and carrier retain their individual read errors instead of making basic identity depend on driver support. A 1-MiB limit bounds allocation and rejects truncation; nonblocking opens and regular-file checks avoid waiting on fixture FIFOs. Kernel driver callbacks themselves have no general cancellation guarantee.

## Preserve observation semantics

Directory handles could pin an inode, but would add ownership and still would not make multi-file kernel attributes atomic or keep a physical device present. Descriptor-free path views match the existing procfs design and make name reuse explicit. Absolute roots avoid working-directory drift while normal symlink resolution supports actual class layouts and fixture trees. They are intentionally trusted paths, not security boundaries.

The package has no mutation, raw descriptors, driver callbacks, or background collection. Callers choose sampling policy, error handling, and any interpretation of optional attributes. Typed Netlink operations remain the appropriate interface for network configuration; sysfs observations do not create a second configuration API.
