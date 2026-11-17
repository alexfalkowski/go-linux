# file

`github.com/alexfalkowski/go-linux/file` adds explicit Linux path-resolution, metadata, and rename controls to standard `*os.File` handles. Use `os` and `os.Root` for ordinary file operations. This package follows the module's Linux 7.0+ and Go floor in `go.mod`; filesystem support and security policy remain observable requirements.

## Good for

Good for controlled path lookup, extended metadata inspection, and atomic file replacement or exchange. [GNU mv's exchange mode](https://www.gnu.org/software/coreutils/manual/coreutils.html#mv-invocation) demonstrates swapping two existing files or directories atomically on supporting filesystems.

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

## Ownership, errors, and verification

Every input is borrowed for one call and stays caller-owned on success and failure. Descriptor references are pinned internally against concurrent Close; operations either use the original open description or fail with `os.ErrClosed`. The package does not alter input flags or offsets. Separate calls are safe concurrently, while logical sequences require caller coordination. No context or syscall-deadline API is promised.

Invalid supported-surface options match `os.ErrInvalid` before file access. Nil files match `os.ErrInvalid`, and closed files match `os.ErrClosed`. Kernel failures such as EEXIST, ELOOP, EXDEV, EAGAIN, EACCES, and ENOTDIR retain identity through `errors.Is`. Open/metadata errors use `*os.PathError`; rename errors use `*os.LinkError` with both names. A failed Open returns nil with nothing for the caller to clean up.

Run `package=file make specs` for the real-kernel race/coverage suite. No capability or isolation profile is needed: tests use temporary directories and read-only observations of procfs, `/dev/null`, and existing mount points. Descriptor exhaustion runs only in a bounded subprocess. The [verification guide](verification.md) records actual runner evidence and gaps; the [design note](design/file.md) explains scope and API tradeoffs.
