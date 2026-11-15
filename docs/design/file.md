# file

Status: P08 complete at `bff403e`. Required native Linux 7.0 arm64 and amd64 CI jobs each pass all 71 file events without failures/skips using Go 1.27.4; the required mount and clock-change matrices also pass at that revision. Local Linux 7.3.4-3-t2-resolute amd64 results remain separate. The [usage guide](../file.md) owns caller workflows, [API conventions](api.md) own shared contracts, and [verification guidance](../verification.md) owns the linked job evidence and remaining source-reviewed limits.

## Standard-library comparison and caller

The installed Go 1.27.4 `os/root.go`, `os/file_unix.go`, `os/rawconn.go`, and `internal/poll/fd_posix.go` were inspected before implementation. `os.File` already owns byte I/O, metadata, close, sizing, seeking, and standard interface integration. `os.Root` supplies ordinary operations confined beneath a directory, including OpenFile, Stat/Lstat, and Rename. Its documented boundary permits mount crossings and procfs special files; it does not supply all six Linux resolution controls, statx validity/attribute masks and extended identities, or rename exchange/no-replace modes. The package therefore returns and borrows standard files, with no new file owner, filesystem abstraction, general callback, or descriptor accessor. No existing package or dependency changes its interface.

Caller flow: `os.Open` an anchor → `file.Open` with explicit resolution options → ordinary Go I/O and/or `file.Stat` → `Close` the returned standard file. Directory-relative queries and entry changes use `StatAt` and `Rename` directly. Input handles stay caller-owned. The package depends only on the standard library and pinned `x/sys/unix`; it does not depend on the other facility packages.

## Initial operation list and omissions

| Surface | Included Linux contract |
| --- | --- |
| Open | openat2 with ReadOnly/WriteOnly/ReadWrite, Append, Create, Exclusive, Truncate, Directory, NoFollow, and Path; unconditional O_CLOEXEC |
| Resolve | NO_XDEV, NO_MAGICLINKS, NO_SYMLINKS, BENEATH, IN_ROOT, and CACHED, with no weaker fallback |
| Stat | statx with AT_EMPTY_PATH on a borrowed file and default synchronization |
| StatAt | statx relative/absolute/empty-path lookup, final no-follow/no-automount, and default/force/don't synchronization |
| Metadata | basic stat fields, birth time, regular/unique mount IDs, attribute values/support mask, preferred I/O block size, and containing/represented device IDs |
| Rename | renameat2 replace, RENAME_NOREPLACE, and RENAME_EXCHANGE as exclusive typed modes |

The initial scope omits O_TMPFILE, direct I/O and synchronization open flags, general nonblocking special-file configuration, whiteout creation, statx direct-I/O alignment/subvolume/atomic-write fields, attribute mutation, cloning, allocation, locking, and ordinary os operations. These omissions bound this item; they are not claims that those operations require a newer kernel. `OPENAT2_REGULAR`, added after the baseline, is also omitted. No future feature stub or dependency is introduced.

All selected operations, resolution bits, metadata masks, and rename modes appear in the versioned Linux 7.0 UAPI. The pinned x/sys v0.50.0 supplies the maintained syscall wrappers and ABI layouts, but omits RESOLVE_CACHED. Its value is declared locally with the exact versioned header reference; no syscall numbers, copied layouts, or fallback are needed. Future output mask/attribute bits are preserved, while unknown input bits are rejected. STATX_ALL is deliberately not used.

## Ownership and implementation choices

OpenOptions uses typed Linux flags plus Go permission bits, converted explicitly because Go's special-mode bit positions differ from Linux. Unsupported bits and incompatible combinations fail before borrowing inputs. Read-only access is zero; zero Resolve has ordinary Linux lookup semantics. Path-only creation/access combinations are rejected rather than silently ignored. Linux validates remaining filesystem/path constraints without userspace cleaning or existence checks.

A package-private control function uses `os.File.SyscallConn().Control` for the repeated borrowed-file lifetime boundary and normalizes Go's private closing error to os.ErrClosed. It does not duplicate inputs or alter flags. Rename nests two protected references, including when the same file is both anchors. Inspected Go RawControl holds lifetime references rather than an exclusive mutex across the callback, so same-file and opposite-anchor calls do not acquire conflicting callback locks. Open's only acquisition is the successful openat2 result, immediately wrapped in os.NewFile; kernel errors acquire nothing. RawControl cannot return an error after invoking its callback. There is no fallible construction stage after acquisition, retained file owner, or hidden finalizer beyond standard os.File behavior.

Stat and StatAt preserve the Linux validity and attribute support masks rather than claiming every requested value is available. Mode converts to os.FileMode, unsigned values retain their Linux ranges, and valid timestamps convert to UTC time.Time even before the epoch; unavailable timestamps remain zero. A single mount-ID field serves the regular and unique masks with Linux's returned mask identifying the kind. No caching or implicit second query tries to manufacture an atomic snapshot.

Only Open restricts resolution. The anchor requirement does not turn StatAt or Rename into confined operations; their documentation says to use trusted names or restricted Open followed by Stat. Rename invokes one kernel operation with no racy emulation. Operations can block and have no cancellation mechanism. Closing an anchor during an admitted syscall cannot redirect it to a reused descriptor, but cannot interrupt blocking filesystem work either.

## Validation scope

External `file_test` scenarios use the existing require-based Go harness and public typed operations with real temporary files, symlinks, directory anchors, metadata masks, and rename modes. Focused ABI cases independently observe close-on-exec, procfs magic links, mount crossings, special-file type conversion, metadata identifiers/attribute masks, and descriptor release through shared `internal/test` helpers. Descriptor-limit changes stay inside a bounded subprocess. The local required rootless amd64 run passes all 71 events, including two examples, without failures/skips, at 98.8% package statement coverage. Lint, vet, and the permitted security scan pass. The validation and challenge passes checked option boundaries, permission conversion, metadata validity, timestamp identity including pre-epoch access time, same-anchor rename lifetime protection, cleanup, and caller-facing documentation; the sole uncovered statement is block-device type conversion. These are local results, separate from the required CI gates.

Required ordinary-user [arm64 job 338](https://circleci.com/gh/alexfalkowski/go-linux/338) and [amd64 job 342](https://circleci.com/gh/alexfalkowski/go-linux/342) verify all 71 file events at `bff403e`, including both examples, on actual Linux 7.0.0-1017-aws with Go 1.27.4 and UID 3434. Neither file run has failures/skips. The same revision passes both required mount and clock-change jobs; the verification guide records those environments and counts. This closes P08's native baseline and architecture acceptance gap.

Remote-server synchronization, triggering/preventing a real automount, block-device creation, positive immutable/encrypted/verity/DAX attributes, deterministic confinement races returning EAGAIN, and every concurrent-close interleaving are not established by temporary-file tests. Those paths retain direct kernel semantics and source review; no mock, raw public hook, shared-host policy mutation, or performance claim is introduced. Birth-time absence is exercised on procfs; filesystem-dependent presence is checked when returned on temporary files.

## Primary references

- [Go 1.27.4 os.Root](https://pkg.go.dev/os@go1.27.4#Root), [os.NewFile](https://pkg.go.dev/os@go1.27.4#NewFile), and [RawConn.Control](https://pkg.go.dev/syscall@go1.27.4#RawConn): standard overlap, resource wrapping, and descriptor lifetime.
- [Linux 7.0 openat2.h](https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/openat2.h) and [openat2(2)](https://man7.org/linux/man-pages/man2/openat2.2.html): baseline resolution bits, unknown-option rejection, path-only symlink handles, and errno semantics.
- [Linux 7.0 stat.h](https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/stat.h) and [statx(2)](https://man7.org/linux/man-pages/man2/statx.2.html): validity masks, timestamp representation, mount-ID variants, attributes, and synchronization.
- [Linux 7.0 fs.h](https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/fs.h) and [renameat2(2)](https://man7.org/linux/man-pages/man2/rename.2.html): baseline rename modes, entry atomicity, and filesystem support.
