# mount

`github.com/alexfalkowski/go-linux/mount` provides filesystem configuration contexts and owned mount handles through the modern Linux mount API. It requires Linux 7.0+ and the Go version in `go.mod`. Implementation and verification are tracked separately in [the plan](../IMPLEMENTATION.md); P06 is complete for the observed Linux 7.0 arm64 target. Required isolated integration passes at Go 1.27.0 and Go 1.27.4; see the [verification evidence](verification.md#ci-and-outstanding-runners).

## Configure, create, and acquire

Call `mount.Open("tmpfs")` to acquire a `*mount.Config`. Supply individual parameters with `SetFlag("rw")` or `SetString("size", "1m")`, call `Create`, and then call `Mount(attributes)` to acquire a detached `*mount.Handle`. Close both owners independently. `SetString(key, "")` passes an empty string; it is distinct from a flag with no value. Strings are not parsed as comma-separated option lists. Names, keys, values, filesystem support, and configuration phase transitions retain kernel validation. On Linux 7.0, fsconfig keys and string values are limited to 255 bytes excluding the terminating NUL; embedded NUL bytes fail with EINVAL.

`Create` can reuse a filesystem instance; it does not promise a unique superblock. `Mount` never calls `Create` implicitly. A failed acquisition returns nil and leaves no newly acquired resource to close. A failed `Mount` leaves the existing Config caller-owned; a kernel failure can still change its phase. A failed setter or `Create` likewise leaves the existing Config open, but the kernel may make a failed context unusable for later configuration. There is no rollback or legacy mount backend.

See the [configuration example](../mount/example_test.go). It is compiled by the normal harness and executed inside the namespace integration fixture when its prerequisites are available.

## Attributes and attachment

`Attributes` supports `ReadOnly`, `NoSUID`, `NoDev`, and `NoExec`. Combine restrictions with `|`. Zero adds no restrictions; existing filesystem and kernel policy still apply. `SetAttributes(mount.AttributeChange{Set: ..., Clear: ..., Recursive: ...})` changes explicit masks on a detached or attached handle. Set and Clear must not overlap or contain unknown bits; invalid masks match `os.ErrInvalid`. Recursive is false by default. Atime, propagation, and ID mapping fields are untouched. Kernel policy can forbid clearing restrictions, and errors may follow partial application.

`handle.Attach(dir, path)` uses `move_mount` to attach or move the mount. A nil directory selects the current working directory; an absolute path uses ordinary absolute lookup. A nonnil directory is borrowed for the duration of the call, protected against concurrent Close. An empty path targets that directory itself and requires a nonnil directory. Closing the directory after attachment does not invalidate the mount. Target symlinks and automounts are not followed by move_mount; component traversal remains the caller's responsibility. These operations do not offer confined path resolution.

The caller retains the handle after either attachment success or failure. **Closing an attached handle does not unmount it.** An unattached tree is released when its last reference disappears. Attached mounts remain until explicitly unmounted or their namespace is destroyed. Use `mount.Unmount(path, 0)` for a normal unmount, which fails with `EBUSY` while references keep the mount busy. `mount.Unmount(path, mount.Detach)` disconnects the mount immediately and defers release until references disappear. Open files and handles remain caller-owned. Unknown flags match `os.ErrInvalid`; other errors preserve Linux errno. Unmount follows target symlinks and kernel propagation rules, so path and namespace policy remain with the caller. The [attachment example](../mount/example_test.go) shows explicit typed cleanup.

`handle.Stat()` returns standard file metadata for the mount root, including while detached. `handle.Attributes()` queries the current four supported restrictions; other kernel flags are omitted. These are snapshots and can change through other references.

## Clone an existing tree

`mount.Clone(dir, path, flags)` uses `open_tree` to acquire a detached copy. Zero flags clones one mount; `mount.Recursive` includes child mounts. The directory/nil/empty-path rules are the same as for Attach. Clone uses open_tree's default symlink following and automount behavior. It borrows the directory on success and failure, and returns no new owner on failure. A successful clone survives closing the source directory and must be closed independently. Clones share filesystem contents while permitting independent mount attributes; they are not data snapshots. Attach the resulting Handle explicitly when a visible mount point is needed.

## Diagnostics and errors

When a Config command reaches its valid descriptor and fails, it drains the kernel filesystem-context queue before another Config method can interleave, returning a `*mount.Error` inside normal error wrapping. Mask validation and failures before descriptor access return ordinary wrapping without draining. Use `errors.As` to inspect `Diagnostics` and `DiagnosticError`. `errors.Is` and `errors.As` traverse the primary operation error only; a diagnostic read failure never replaces its errno. Errors may include warnings left by earlier successful operations. Configuration failures do not always produce diagnostic records.

Each `Diagnostic` contains a Severity byte, normally `e`, `w`, or `i`, and a Message without its framing separator or final newline. Unknown severity bytes are preserved. Unframed kernel records retain all bytes with zero Severity. Error text includes diagnostic messages for ordinary logging; use fields for structured inspection. Kernel text may contain configuration inputs, so apply caller logging policy.

`config.Diagnostics()` drains remaining records, returning nil/nil for an empty queue and partial records alongside retrieval errors. Messages already collected in an Error are not returned again. The kernel queue holds only the newest eight records on Linux 7.0 and can lose earlier messages. Reads grow from 4 KiB to 1 MiB on EMSGSIZE; a larger record reports EMSGSIZE and stays queued on the supported baseline. This bound controls allocation and is not a producer-size guarantee. The queue is not a lossless log.

Other operations wrap `*os.PathError` and preserve kernel errno identity. `EPERM`, `EACCES`, `EINVAL`, and `ENOSYS` are not blanket unsupported-feature signals: capabilities, filters, filesystem support, configuration, and kernel policy differ. Zero owners match `os.ErrInvalid`; repeated Close and ordinary methods on closed owners match `os.ErrClosed` for otherwise valid arguments.

## Isolation, concurrency, and verification

The caller supplies namespace isolation, applicable `CAP_SYS_ADMIN`, filesystem support, attachment/propagation policy, and unmount cleanup. The library never enters a namespace. Do not run attachment examples against host mounts. A container's UID 0 alone does not establish the required capabilities or syscall permissions.

Both owners implement `io.Closer`; descriptors are atomically close-on-exec. Do not copy owners after first use. Config methods, Close, and diagnostic collection are serialized; Config.Close waits for in-flight Config methods. Handle methods pin descriptor lifetime against concurrent Close; operation sequences and path lookups are not transactions. Handle.Close can return while a protected operation is still running, with final kernel descriptor release deferred until its reference is dropped. Close provides no cancellation. Both owners keep descriptors private. There is no adoption API, I/O stream, deadline, or context support.

Run `package=mount make specs` for ordinary-user validation/error tests. Run `package=mount make specs profile=namespaces` to request isolated integration, or add `required=true` to fail when isolation or the explicit private-propagation/fsopen prerequisites are unavailable. After these prerequisites pass, all creation, configuration, mount, attribute, clone, attachment, and cleanup failures fail the test rather than becoming skips. The test binary launches a bounded subprocess with new user/mount namespaces and UID/GID mappings; it makes propagation private before mounting. Examples execute only inside that fixture. See [verification guidance](verification.md) and the [design note](design/mount.md) for evidence and remaining gaps.
