# file

`github.com/alexfalkowski/go-linux/file` adds Linux path-resolution, metadata, rename, allocation, access advice, and synchronization controls to standard `*os.File` handles. Use `os` and `os.Root` for ordinary file operations. The module supports Linux 7.0+ and the Go floor in `go.mod`. Filesystem support and security policy can vary by operation.

## Good for

Use it for controlled path lookup, extended metadata inspection, atomic file replacement or exchange, and preallocating log segments. [GNU mv's exchange mode](https://www.gnu.org/software/coreutils/manual/coreutils.html#mv-invocation) demonstrates swapping two existing entries atomically on supporting filesystems.

## Restricted opening

Open an anchor with `os.Open`, then borrow it for one restricted lookup:

```go
dir, err := os.Open("/srv/app")
if err != nil {
    return err
}
defer dir.Close()

config, err := file.Open(dir, "config.json", file.OpenOptions{
    Resolve: file.Beneath | file.NoSymlinks | file.NoXDev,
})
if err != nil {
    return err
}
defer config.Close()
```

The result is an ordinary owned `*os.File`, ready for standard I/O or composition with `mmap.Map`. Close it independently of the borrowed directory. This is the deliberate standard-file boundary for this package; descriptor borrowing and Linux calls are handled internally. The [executable examples](../file/example_test.go) include complete setup and error handling.

`OpenOptions{}` opens read-only with ordinary Linux resolution. Zero does not confine the lookup. `Beneath` rejects absolute names and escapes above the anchor; `InRoot` instead interprets absolute names, absolute symlinks, and parent traversal relative to that anchor for the duration of the lookup. They cannot be combined. `NoSymlinks` rejects traversal through any symlink, while the `NoFollow` open flag affects only the last component. `NoMagicLinks` excludes procfs-style magic-link traversal; request it explicitly with `Beneath` or `InRoot` if required. `NoXDev` rejects mount-point crossings, including bind mounts. `Cached` returns EAGAIN when lookup needs uncached work. No restriction is removed on retry, and the package performs no automatic retry.

`Flags` accepts ReadOnly, WriteOnly, ReadWrite, Append, Create, Exclusive, Truncate, Directory, NoFollow, and Path. `Create` uses `Perm` in Go `os.FileMode` representation, including Go's setuid, setgid, and sticky constants; umask and filesystem policy still apply. `Perm` must be zero otherwise. All descriptors are atomically close-on-exec. A `Path` handle permits metadata and directory anchoring without data I/O. Combine `Path|NoFollow` to open a final symlink itself, including under `NoSymlinks` or `NoMagicLinks`.

Names pass to Linux unchanged. Trailing slashes, empty names, symlink expansion, and `..` keep kernel semantics. A live nonnil anchor is required even when an absolute path would ignore it. Blocking opens of special files can occupy an OS thread; closing the anchor does not cancel the call. The returned `os.File` retains standard I/O behavior, including that regular files do not support deadlines and a blocking special-file handle may lack runtime polling.

## Extended metadata

`Stat(opened, file.MaskBasic|file.MaskBirthTime|file.MaskMountID)` observes the open inode, including after unlink. `StatAt(dir, name, file.StatOptions{...})` resolves a name relative to a borrowed directory. Set `NoFollow` for the final symlink itself, `NoAutomount` to avoid triggering a final automount, and `Sync` to SyncDefault, SyncForce, or SyncDont. An empty name observes the handle itself, permitting explicit synchronization of an open file.

`Metadata.Mask` is the validity mask supplied by Linux. A requested field may be unavailable, and unrequested fields may be present. Check the returned bit rather than testing a field for zero. `MaskBasic` includes type, permission mode, link count, UID/GID, access/modification/status-change times, inode, size, and allocated 512-byte blocks. Birth time is filesystem-dependent. Times are UTC `time.Time` values; an unavailable timestamp is the zero Go time. Mode uses Go bits, and Linux unsigned size/identifier ranges are retained.

`MaskMountID` requests the reusable ID used in procfs mountinfo; `MaskUniqueMountID` requests the system-lifetime nonreused identity. They share `Metadata.MountID`, and the returned mask identifies the kind. Linux prefers the unique kind if both are requested. `AttributesMask` separately identifies the meaningful bits of `Attributes`: an unset unsupported bit does not establish a false attribute. Unknown output mask and attribute bits are preserved. BlockSize and the containing DeviceMajor/DeviceMinor are always supplied; RdevMajor/RdevMinor apply to device inodes.

`StatAt` uses ordinary Linux resolution and permits escapes, intermediate symlinks, and mount crossings. For a constrained lookup, call `Open` with the required restrictions and then `Stat` on its result. Metadata fields can reflect different instants during concurrent mutation; neither one query nor multiple queries promise an atomic snapshot.

## Rename modes

`Rename(oldDir, oldName, newDir, newName, mode)` supports `Replace` (zero), `NoReplace`, and `Exchange`. `NoReplace` fails with EEXIST for an existing destination, including a dangling symlink. `Exchange` requires both entries and atomically swaps them, even when their types differ. Final symlinks are moved themselves. Modes cannot be combined. Whiteout creation is outside this initial scope.

Each name is relative to its matching borrowed directory, or absolute with that anchor ignored. Both anchors must remain live for admission to the operation. Rename has ordinary Linux path resolution and does not confine untrusted names. The library does not emulate modes with existence checks or multiple renames. Filesystem support failures and cross-mount errors retain errno. Entry replacement/exchange is atomic, but later lookups are separate operations; success does not promise crash durability or synchronize storage.

## Allocation and ranges

Reserve storage for a log segment before writing the first record. Size preservation keeps the logical end of the file at the last written record.

```go
if err := file.Fallocate(segment, file.Allocate|file.KeepSize, 0, 64*1024); err != nil {
    return err
}
```

Pass a writable open file, a mode, and a byte offset and length. The [executable example](../file/example_test.go) writes a record to a preallocated segment.

| Mode | File effect |
| --- | --- |
| `Allocate` (zero) | Reserves storage and extends the file when the range ends beyond EOF. Existing data is preserved. Add `KeepSize` to preserve EOF. |
| `PunchHole` with `KeepSize` | Frees whole blocks and zeros partial blocks in the range. Size stays the same. |
| `ZeroRange` | Zeros the range and reserves storage for it. Add `KeepSize` to preserve EOF. |
| `CollapseRange` | Removes bytes, shifts later data toward the start, and shrinks the file. The range must end before EOF. |
| `InsertRange` | Inserts a hole, shifts later data toward the end, and grows the file. The insertion offset must be before EOF. |
| `UnshareRange` | Reserves storage and makes shared blocks in the range private while preserving existing data. Add `KeepSize` to preserve EOF. |
| `WriteZeroes` | Zeros the range and prepares its mappings for subsequent overwrites. Extends the file if the range ends beyond EOF. Use it alone. |

Offsets must be nonnegative and lengths positive. Invalid inputs match `os.ErrInvalid`. A range end outside the signed 64-bit range returns EFBIG. The filesystem checks alignment and can reject a mode with EOPNOTSUPP. Collapse and insert commonly require both offset and length to be aligned to the filesystem's block size. KeepSize is required for hole punching and cannot be combined with collapse or insert.

Use `UnshareRange|KeepSize` before overwriting a range in a reflinked working copy when the logical file size must stay fixed. The filesystem determines the allocation granularity. An unaligned range can affect extent sharing at its boundary blocks while preserving their contents. The [working-copy example](../file/example_test.go) treats the reservation as optional. See [mount verification](verification.md#mount-verification) for the test on real reflinked files.

Use `WriteZeroes` to prepare a new or reusable database segment for in-place overwrites. On Linux 7.0, ext4 requires backing storage that advertises write zeroes with unmap and returns EOPNOTSUPP when that capability is absent. Ext4 also supports partial edge blocks and preserves bytes outside the requested range. The [database segment example](../file/example_test.go) treats preparation as optional. [Linux 7.0 ext4 implementation](https://github.com/torvalds/linux/blob/v7.0/fs/ext4/extents.c)

Fallocate borrows the open file and preserves its current offset. Coordinate overlapping reads, writes, mappings, and range mutations at the application level. The call can block in the kernel. Errors use `*os.PathError` and preserve errno. The package passes the operation to Linux without fallback or rollback. Use the usual file synchronization calls for durability. [Linux fallocate(2)](https://man7.org/linux/man-pages/man2/fallocate.2.html)

## Access advice and readahead

Set sequential advice before scanning a segment. Use readahead to request advance reads for the first part of the scan.

```go
if err := file.Fadvise(segment, file.Sequential, 0, 0); err != nil {
    return err
}
if err := file.Readahead(segment, 0, 64*1024); err != nil {
    return err
}
```

`Fadvise` accepts one hint. `Normal` restores defaults, `Random` disables automatic file readahead, `Sequential` increases the readahead window, and `NoReuse` tells Linux that accesses need not protect pages from reclamation. These four hints affect the open file description and are shared by duplicated handles. `WillNeed` requests advance reads for the byte range, and `DontNeed` asks Linux to reclaim cached pages in the range. The kernel and filesystem may limit or ignore every hint. [Linux 7.0 advice implementation](https://github.com/torvalds/linux/blob/v7.0/mm/fadvise.c)

`Readahead` requires a readable handle. Linux can round requests to pages, limit the work, and block while resolving filesystem metadata. On Linux 7.0, a request for an ordinary tmpfs file can succeed without starting advance reads. Cache residency can change at any time. [Linux 7.0 readahead implementation](https://github.com/torvalds/linux/blob/v7.0/mm/readahead.c) · [Linux 7.0 tmpfs implementation](https://github.com/torvalds/linux/blob/v7.0/mm/shmem.c)

Both calls borrow the file and preserve its current offset. Offsets and lengths are nonnegative byte counts. A zero length requests the range through EOF. A negative offset or length matches `os.ErrInvalid`, and a range end above `MaxInt64` returns EFBIG.

## Data and filesystem synchronization

After writing a record to an existing write-ahead log, use `file.Fdatasync(log)` to synchronize its data and the metadata needed to retrieve it, including size changes. For broader file metadata, use the standard `log.Sync()`. If a new or renamed directory entry must be durable, synchronize its containing directory separately with `dir.Sync()`.

Use `file.SyncFS(dir)` for an explicit filesystem-wide checkpoint. A regular file or directory handle identifies the containing filesystem. The operation can synchronize other files and report writeback errors unrelated to the supplied file. Both synchronization calls preserve the input's offset and can block. Coordinate transaction ordering and recovery in the application. [Linux 7.0 synchronization implementation](https://github.com/torvalds/linux/blob/v7.0/fs/sync.c)

## Coverage boundary

The [Linux 7.0 UAPI](https://github.com/torvalds/linux/tree/v7.0/include/uapi/linux) includes more than these operations. `Open` accepts only the flags listed above; `O_TMPFILE`, `O_DIRECT`, `O_SYNC`, and `O_DSYNC` are not exposed. There is no operation to publish an unnamed temporary inode. Ordinary creation, I/O, logical resizing, and `File.Sync` remain standard Go operations.

The allocation API accepts the modes in the table. Whole-file and record locking and `sync_file_range` are outside the current surface. [Linux 7.0 allocation definitions](https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/falloc.h)

The accepted statx request mask is exactly `MaskBasic|MaskBirthTime|MaskMountID|MaskUniqueMountID`, including individual basic bits. Direct-I/O memory/offset/read alignment, subvolume identity, and atomic-write unit/segment limits from [stat.h](https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/stat.h) have no fields or request constants here. Returned unknown mask/attribute bits survive, including an atomic-write attribute bit without a named constant; that does not expose the omitted fields or implement atomic data writes. Whiteout rename is absent. Reflink creation, extent inspection, and device control are outside this surface.

## Ownership, errors, and verification

Every input is borrowed for one call and stays caller-owned on success and failure. Descriptor references are pinned internally against concurrent Close; operations either use the original open description or fail with `os.ErrClosed`. File status flags and offsets are preserved. Separate calls are safe concurrently, while logical sequences require caller coordination. No context or syscall-deadline API is promised.

Invalid supported-surface options match `os.ErrInvalid` before file access. Nil files match `os.ErrInvalid`, and closed files match `os.ErrClosed`. Kernel failures such as EEXIST, ELOOP, EXDEV, EAGAIN, EACCES, and ENOTDIR retain identity through `errors.Is`. Operations use `*os.PathError` for context, with the exception of Rename, which uses `*os.LinkError` with both names. A failed Open returns nil with nothing for the caller to clean up.

Run `package=file make specs` for the real-kernel race/coverage suite. The [verification guide](verification.md#file-verification) describes temporary filesystem prerequisites and the isolated metadata scenario. The [design note](agents/accepted-design/file.md) explains API tradeoffs.
