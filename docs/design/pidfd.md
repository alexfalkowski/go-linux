# pidfd design decisions

The [usage guide](../pidfd.md) owns acquisition, signaling, and reaping workflows. [Verification](../verification.md) records runtime evidence, including the remaining positive core-dump-status gap.

## Independent identity without replacing process management

Go already uses pidfds for `os.Process`. OpenProcess duplicates the stable handle inside `Process.WithHandle`; looking up Process.Pid again would reintroduce a PID-reuse window. The duplicate gives independent local ownership while preserving the original process manager's reaping responsibility. Numeric Open remains useful when no stable handle exists, but cannot establish identity before acquisition.

Adopt consumes the input on both success and failure, eliminating conditional caller cleanup. Validation uses pidfs identity and descriptor flags rather than signal zero: signal permission is unrelated to descriptor type, and an already reaped handle is still a valid pidfd. Thread handles and path-only descriptors are excluded because they do not provide the promised process operations. See [acquisition and validation](../../pidfd/process.go).

## Interrupt waits without changing shared flags

A duplicate shares file status flags with its source. Requiring or enabling nonblocking mode for Go poller reads could interfere with `os.Process` waiting through another duplicate. The package instead polls the pidfd alongside a private eventfd, accepting either blocking mode without changing it. One extra descriptor and explicit closed state are the cost of safe standard-library composition and close interruption.

The wake record stays unread so every pending waiter observes shutdown. Protected file references prevent descriptor reuse while polling. This avoids a background event loop or cancellation goroutine, but each blocked waiter can occupy an OS thread. Contexts, deadlines, and wait-many orchestration are omitted rather than promised without another interruption model.

## Separate observation from reaping

Readiness can be observed repeatedly; child status can be consumed only once. Wait combines readiness with nonblocking waitid checks so non-children fail promptly and competing reapers retain Linux's ECHILD behavior. Caching an Exit would hide whether this call actually reaped the child. Close releases handles without killing or reaping.

The private SIGCHLD decoder reads the maintained x/sys siginfo storage using the supported amd64/arm64 ABI, without depending on Go's private process structures. CoreDump preserves CLD_DUMPED rather than inferring it from a signal or promising that a core file exists.

[Go's stable handle contract](https://pkg.go.dev/os@go1.27.4#Process.WithHandle) and [Linux 7.0 pidfs](https://github.com/torvalds/linux/blob/v7.0/fs/pidfs.c) underpin the ownership and identity choices.
