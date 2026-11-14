# memfd

Package `memfd` creates Linux memory-backed files that work with ordinary Go file operations. Use it when you need an anonymous file descriptor for sharing data, with optional seals that permanently restrict modification. It requires Linux 7.0+ and the Go version in [go.mod](../go.mod). Runtime verification evidence is recorded in [IMPLEMENTATION.md](../IMPLEMENTATION.md).

## Install and use

```sh
go get github.com/alexfalkowski/go-linux/memfd
```

This example writes a message, seals its contents and size, then reads it using standard Go I/O:

```go
package main

import (
    "fmt"
    "io"

    "github.com/alexfalkowski/go-linux/memfd"
)

func main() {
    file, err := memfd.New("message", memfd.Options{})
    if err != nil {
        panic(err)
    }
    defer file.Close()

    if _, err := file.WriteString("hello"); err != nil {
        panic(err)
    }

    if err := file.AddSeals(memfd.SealWrite | memfd.SealGrow | memfd.SealShrink | memfd.SealSeal); err != nil {
        panic(err)
    }

    if _, err := file.Seek(0, io.SeekStart); err != nil {
        panic(err)
    }

    contents, err := io.ReadAll(file)
    if err != nil {
        panic(err)
    }
    fmt.Println(string(contents)) // hello
}
```

The matching [executable example](../memfd/example_test.go) runs in the test suite. The name is a debugging label, not a path; it may be empty but cannot contain NUL or exceed 249 bytes.

## File ownership and defaults

`New` returns an empty, readable and writable file. Use its ordinary Go methods for reading, writing, seeking, resizing, and metadata, including `io.ReaderFrom` and `io.WriterTo`. The file is opaque: it exposes neither an underlying `*os.File` nor raw descriptor access. Do not copy a `memfd.File` after use. Close explicitly when done.

Descriptors are atomically close-on-exec and allow sealing. By default, execute permission bits are cleared and locked by `SealExec`. `Options{Executable: true}` explicitly requests an executable file; namespace or security policy can reject that request, and the constructor returns the actual error without fallback. Non-executable mode bits do not prohibit executable memory mappings or provide a general execution sandbox.

Methods are safe for concurrent use, including sealing during close, but a sequence of calls is not a transaction. These regular files do not support I/O deadlines. A zero `File` owns no descriptor and error-returning operations match `os.ErrInvalid`; after creation, I/O and sealing after close match `os.ErrClosed`, except that an invalid seal mask is rejected before checking closed state.

## Map file contents

Call `file.Map(offset, length, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read | mmap.Write})` after sizing the file with `Truncate`. Import `github.com/alexfalkowski/go-linux/mmap` for mapping options. The [mapping example](../memfd/example_test.go) shows a shared page without descriptor callbacks. The returned `*mmap.Region` is independently owned: close it separately; closing the file leaves the mapping valid. A failed Map returns nil and leaves the file open. Seals can prohibit shared writable mappings. Offsets, bounds, protection, and borrowed-slice lifetimes follow the [mmap guide](mmap.md), including SIGBUS risk after truncation.

## Applying seals

Seals are permanent and belong to the underlying inode, affecting every descriptor for that file. Combine flags with `|`:

| Seal | Effect |
| --- | --- |
| `SealShrink` | Prevent reducing file size |
| `SealGrow` | Prevent increasing file size, including writes past its end |
| `SealWrite` | Prevent writes and new shared writable mappings; adding it fails with `EBUSY` while a shared writable mapping exists |
| `SealFutureWrite` | Prevent descriptor writes and new shared writable mappings while retaining writes through existing shared writable mappings |
| `SealExec` | Lock execute mode bits; on an executable file, also add shrink, grow, write, and future-write seals |
| `SealSeal` | Prevent all subsequent seal additions, including zero masks and repeated seals |

`SealWrite` alone permits resizing, so combine it with `SealGrow` and `SealShrink` to freeze both contents and size. `SealSeal` then locks the set of seals. Inspect the current mask with `file.Seals()`; other owners may add seals after that snapshot. Unknown input bits match `os.ErrInvalid`; inspection preserves unknown returned bits.

Errors retain the underlying errno for `errors.Is`/`errors.As`, with operation and file-label context for creation and sealing syscalls. Tests require permitted memfd creation, file operations, sealing, mappings, and executable opt-in. A policy denial remains a verification failure rather than being silently skipped. Huge pages, descriptor passing, and execution helpers are outside this package's current scope.

See [GoDoc](https://pkg.go.dev/github.com/alexfalkowski/go-linux/memfd) for the callable API, the [design note](design/memfd.md) for rationale and Linux references, and [verification guidance](verification.md) for `package=memfd make specs` and reports.
