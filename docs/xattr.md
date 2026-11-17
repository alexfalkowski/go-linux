# xattr

`github.com/alexfalkowski/go-linux/xattr` reads, sets, lists, and removes Linux extended attributes while borrowing standard `*os.File` handles. It uses the at-style attribute calls present in the Linux 7.0 baseline. The filesystem, attribute namespace, permissions, and security policy still determine which operations succeed.

## Store and read an attribute

```go
opened, err := os.Open("report.txt")
if err != nil {
    return err
}
defer opened.Close()

if err := xattr.Set(opened, "user.content_type", []byte("text/plain"), xattr.Upsert); err != nil {
    return err
}
value, err := xattr.Get(opened, "user.content_type")
if err != nil {
    return err
}
fmt.Println(string(value))
```

The [executable examples](../xattr/example_test.go) include complete temporary-file setup and directory-relative calls. A descriptor opened read-only can still modify attributes when inode permissions allow it; opening a file is not a blanket authorization to modify every namespace.

Names retain their fully qualified namespace, such as `user.content_type`. Values are arbitrary bytes. Nil and empty values both store a present empty attribute; Get returns a nonnil empty slice for it. `Remove` deletes an attribute. A missing attribute normally returns ENODATA, which can also mean the caller cannot access it. `List` returns visible, fully qualified names in unspecified kernel order, with a nonnil empty slice when none are visible. Hidden names need not appear, and listed names can disappear before Get.

`Set` and `SetAt` take exactly one `SetMode`: `Upsert` (zero) creates or replaces, `Create` fails with EEXIST if already present, and `Replace` fails with ENODATA if absent. These conditions are checked atomically by Linux. The package does not emulate them with separate existence checks. A write replaces the entire value; success does not imply crash durability. Do not modify the input bytes concurrently with a set operation.

## Select an inode or a path

`Get`, `Set`, `List`, and `Remove` operate on the borrowed open inode, including after rename or unlink. These operations require an ordinary file handle; Linux returns EBADF for an `O_PATH` handle. The package does not reopen a descriptor through procfs.

`GetAt(dir, path, name, options)`, `SetAt(dir, path, name, value, mode, options)`, `ListAt(dir, path, options)`, and `RemoveAt(dir, path, name, options)` resolve relative paths from a borrowed directory. Absolute paths ignore the directory identity, but every call still requires a live nonnil handle. Empty paths select the handle itself. An `O_PATH` directory can anchor a nonempty path.

`PathOptions{}` follows symlinks. `PathOptions{NoFollow: true}` selects the final symlink itself; intermediate links are still followed. An empty path always selects the handle itself. Linux disallows `user.*` attributes on symlinks: reads return ENODATA and mutation returns EPERM; listing can still succeed. Other namespaces follow filesystem and security-module policy. There is no fallback that silently accesses the link target.

Paths pass unchanged to Linux, including trailing slashes and `..`. Directory-relative lookup does not confine an untrusted path. For restricted lookup, use `file.Open` with suitable resolution restrictions and an ordinary file access mode, then pass that file to the handle operations. Separate sizing/read lookups may select different inodes if directory entries change.

## Bounded reads and concurrency

Get and List query size, allocate, and read. Each operation makes at most four such attempts. A read that reports ERANGE due to concurrent growth starts the next attempt; after exhaustion, the operation returns ERANGE without a partial result. A nonzero buffer is used even after a zero-size query, so growth from empty cannot be mistaken for a successful empty read. Shrinking results are sliced to the actual returned size. Other errors return immediately.

Each buffer is bounded to the Linux VFS limit of 64 KiB; larger sizes return E2BIG. Filesystems can impose smaller value or aggregate limits and return errors such as ENOSPC, EDQUOT, or E2BIG. A complete returned value reflects the final read; multiple operations do not form an atomic snapshot or transaction. Calls do not retry interrupted mutations.

Every input file stays caller-owned on success and failure. Internal lifetime references protect descriptor identity against concurrent Close; they do not cancel a blocked kernel call. The package acquires no descriptors and changes no file offsets or flags. Calls are safe concurrently when callers do not mutate input buffers, but sequences still need coordination. No context or deadline cancellation is promised.

Errors use `*os.PathError` with the supplied path (empty for handle operations), preserving errno for `errors.Is`. Nil files match `os.ErrInvalid`; closed files match `os.ErrClosed`. Invalid or combined set modes match `os.ErrInvalid` before file access. Embedded NULs preserve EINVAL from the syscall binding, and Linux validates namespace, name length, and remaining constraints. Filesystem support errors, ENOSYS, and security denials remain distinguishable; no older-syscall fallback is attempted. Malformed kernel list records return EIO without partial names.

## Scope and verification

The package exposes byte attributes and visible names, without interpreting ACLs, file capabilities, or security labels. It does not copy attributes automatically, normalize namespaces, provide transactions, or add features beyond the Linux 7.0 interface. The [design note](design/xattr.md) explains the interface choice and private ABI binding.

Run `package=xattr make specs profile=rootless required=true` for real-filesystem tests, examples, race detection, and coverage. Tests use test-owned files and directories, ordinary user attributes, and supported symlink error behavior. No privilege or namespace profile is needed. [Verification guidance](verification.md) owns environment evidence; [verification limits](verification-limits.md) records missing execution evidence.
