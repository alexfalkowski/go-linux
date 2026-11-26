# memfd design decisions

Part of the [accepted-design index](../accepted-design.md).

The [usage guide](../../memfd.md) documents file operations and seals; [verification](../../verification.md) records runtime evidence. This note explains the choices behind the wrapper.

## A file owner with a restricted surface

Ordinary I/O delegates to a private `os.File`, preserving Go's file behavior without embedding its raw descriptor methods. Mapping is a concrete `File.Map` operation returning an independent `mmap.Region`; this permits composition without making descriptor borrowing part of the public API. The dependency points from memfd to mmap, with no reverse dependency or shared descriptor superclass.

Copy methods need the same boundary. An `os.File` copy fallback can invoke the peer's optional `ReaderFrom` or `WriterTo` method with the underlying file. The wrapper narrows the peer to its basic interface before delegation so user callbacks cannot recover that file. This can bypass specialized copying; no zero-copy performance guarantee is made. See [the copy adapters](../../../memfd/file.go).

## Sealable data files by default

Raw zero creation flags would produce a different resource: sealing would be disabled, and execute permission would not express the library's chosen data-file default. Creation therefore enables sealing and requests `MFD_NOEXEC_SEAL`; executable files require an explicit option. Policy rejection is returned rather than retried with weaker flags. Execute-mode locking is deliberately not presented as protection against executable mappings or as a sandbox.

Seals remain explicit Linux operations rather than presets such as “immutable.” Content writes, growth, shrinkage, future writable mappings, and further seal changes are distinct restrictions. In particular, sealing future writes can preserve an existing producer mapping, while sealing all writes cannot. A preset would hide which restrictions the caller actually requested.

Huge-page allocation, descriptor passing, and execution helpers are omitted because they introduce separate resource, ownership, or execution policies beyond file I/O and sealing.

The baseline rationale is grounded in the [Linux 7.0 creation and sealing implementation](https://github.com/torvalds/linux/blob/v7.0/mm/memfd.c).
