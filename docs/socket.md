# Message sockets

`socket` provides Unix message sockets and Netlink transport on Linux 7.0+. Use the standard `net` package for TCP, UDP, Unix streams, listeners, and dialing. This package keeps message truncation and received-file ownership visible and requires no public descriptor access. Import `github.com/alexfalkowski/go-linux/socket`.

## Exchange messages

```go
sender, receiver, err := socket.Pair(socket.Datagram)
if err != nil {
    return err
}
defer sender.Close()
defer receiver.Close()
if err := receiver.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
    return err
}
if _, err := sender.Send([]byte("hello"), nil); err != nil {
    return err
}
buffer := make([]byte, 4096)
message, err := receiver.Receive(buffer, 0)
if err != nil {
    return err
}
if message.Flags&socket.Truncated != 0 {
    return fmt.Errorf("message exceeded receive buffer")
}
fmt.Printf("%s\n", buffer[:message.N])
```

`Pair` supports `Datagram` and `SeqPacket`. Both preserve record boundaries; each `Receive` consumes one record and discards any payload tail that does not fit. `N` counts copied bytes. `Truncated` reports lost payload; `ControlTruncated` reports ancillary loss. Empty receive buffers still consume a record. Unix empty messages return zero bytes successfully; Netlink rejects empty sends with `ENODATA`. On seqpacket sockets, peer shutdown also returns zero bytes successfully; protocols needing an EOF convention must exclude empty records. Unix pairs wait for capacity with polling and deadlines. Sends with an explicit Unix or Netlink destination return `EAGAIN` when capacity is unavailable; polling the sender cannot track destination-queue capacity. The caller chooses whether and when to retry. This also applies when the payload carries files: failed attempts close temporary duplicates and leave the borrowed inputs open.

These are message operations, so `Conn` implements `io.Closer` but deliberately does not implement stream or `net.PacketConn` interfaces that hide these results.

## Unix address namespaces

`ListenUnix` creates a bound Unix datagram endpoint. Send to another `UnixAddress`; `Message.From` identifies a named sender. The package follows [Go's `net.UnixAddr` convention](https://pkg.go.dev/net#UnixAddr): a leading `@` is readable shorthand for an abstract socket. Linux receives a leading NUL byte, not the `@` character. Abstract sockets have no filesystem entry.

| Address value | Interpretation |
| --- | --- |
| `socket.UnixAddress{Name: "/run/app.sock"}` | Filesystem socket at `/run/app.sock`. |
| `socket.UnixAddress{Name: "@app"}` | Abstract socket with kernel name `"\x00app"`. |
| `socket.UnixAddress{Name: "\x00app"}` | The same abstract socket, using its native leading NUL. |
| `socket.UnixAddress{Name: "@app", Pathname: true}` | Filesystem socket literally named `@app`, relative to the working directory. |

`Pathname` is the package's literal-path selector, not a Linux socket option. Its zero value keeps the familiar Go shorthand. Set it to true when a leading `@` is part of a filesystem filename. This distinction matters when replying to native peers: a literal filesystem `@app` and abstract `@app` are separate endpoints and can exist at the same time.

Pass the complete `Message.From` value back to `Send`, preserving `Pathname`, so replies remain in the sender's namespace. Do not rebuild the destination from `From.String()` or `UnixAddress.Name` alone: neither string records the literal-path selector. `Receive` and `LocalAddress` automatically set `Pathname` for leading-`@` and full-length filesystem names.

Filesystem names cannot contain NULs. Embedded and trailing NULs are preserved in abstract names. Names using the default interpretation must be shorter than 108 bytes; explicit pathnames may use all 108 bytes supported by Linux. Returned abstract names use `@`; unnamed pairs have an empty `LocalAddress` name, distinct from the abstract name `@`.

The caller removes filesystem socket paths after closing; neither constructor failures nor `Close` unlink existing paths. Abstract names disappear when their last reference closes.

## Pass files and credentials

Pass files with `sender.Send(payload, nil, file1, file2)` on a Unix pair, or supply a Unix destination on a bound datagram socket. At least one payload byte is required with files. The sender borrows the inputs and protects each against concurrent close while duplicating it. Temporary duplicates remain owned by the operation while it waits and close on every return path; the original files remain caller-owned. A concurrent input close can prevent duplication. Successful send queues new references; it does not consume the originals, and does not prove receipt.

`message, err := receiver.Receive(buffer, maxFiles)` accepts a limit from 0 through 253. On success, close every `message.Files` entry, including when `Truncated` is set. Received files are atomically close-on-exec and share file offsets and status flags with the originals. They may outlive either socket. Socket closure discards queued rights after the last reference to that endpoint closes.

On ancillary truncation, excess rights, or decoding failure, the package closes all received rights and returns no files. Ancillary truncation and excess rights match `EMSGSIZE`; `Message` still describes the consumed payload. The control buffer includes credential space, so the explicit file limit can reject rights even without Linux setting `ControlTruncated`. There is no retry for a consumed message. Unknown control types are ignored; timestamps, security labels, explicit credential sending, pidfd ancillary messages, and arbitrary control-message injection are outside this initial API.

`SetPassCredentials(true)` enables kernel-validated per-message credentials on Unix sockets; inspect `Message.Credentials` (nil when absent). `PeerCredentials` reports the connection-time credentials recorded for a pair. Neither observation is a complete authorization policy; account for namespaces, credential changes, and file capability implications in the application.

## Use Netlink transport

`OpenNetlink(socket.Route, socket.NetlinkAddress{})` opens and binds a raw Netlink endpoint. `Route`, `Generic`, and `Usersock` are named protocols; other nonnegative signed-32-bit Linux protocol numbers are accepted and may fail with their kernel errno. A zero local `PortID` requests automatic allocation. Obtain the actual value with `LocalAddress`; it is not necessarily the process ID, especially for multiple endpoints in one process. Send encoded bytes to `NetlinkAddress{}` for the kernel or another explicit port for user messaging. Netlink is message-oriented even though its socket type is `SOCK_RAW`.

`SetExtendedAck`, `SetCapAck`, and `SetStrictCheck` expose `NETLINK_EXT_ACK`, `NETLINK_CAP_ACK`, and `NETLINK_GET_STRICT_CHK`. `JoinGroup` and `LeaveGroup` use positive group IDs, not masks; `NetlinkAddress.Groups` is the older 32-bit bind/send mask. Membership and protocol operations can require privilege. These options do not encode messages, parse acknowledgements, match sequences, recover losses, or coordinate concurrent requests. Netlink protocol encoding belongs in the planned `netlink` package. Kernel `ENOBUFS` remains visible; payload truncation must also be handled before decoding. No suppression of loss notifications is offered.

## Lifetime, options, and errors

Constructors atomically request nonblocking and close-on-exec, establish public Go polling support, and return no owner on error. There is no blocking fallback or background goroutine. Close every successful owner; `Pair` transfers two owners. Methods are safe concurrently, but readers compete and options/operations are not transactions. Do not copy owners after use. The zero value owns nothing and error-returning methods match `os.ErrInvalid`; nil receivers are unsupported. Valid operations after closing match `os.ErrClosed`. Argument validation precedes closed-state checks. Syscall errors are wrapped in `os.PathError` and preserve `errors.Is`/`errors.As` errno matching.

`SetDeadline`, `SetReadDeadline`, and `SetWriteDeadline` affect pending and future I/O; zero clears the selected deadline. Expiration matches `os.ErrDeadlineExceeded`. `Close` interrupts pending I/O. `SetReadBuffer` and `SetWriteBuffer` request positive signed-32-bit byte counts through `SO_RCVBUF`/`SO_SNDBUF`; Linux doubles requests for accounting and applies limits. They do not guarantee a particular usable queue size. No adoption, raw descriptor export, stream connection management, Internet sockets, or half-close operation is provided.

Executable examples are in [socket/example_test.go](../socket/example_test.go). Linux contracts are documented in [socket(7)](https://man7.org/linux/man-pages/man7/socket.7.html), [unix(7)](https://man7.org/linux/man-pages/man7/unix.7.html), [netlink(7)](https://man7.org/linux/man-pages/man7/netlink.7.html), [sendmsg(2)](https://man7.org/linux/man-pages/man2/sendmsg.2.html), and [recvmsg(2)](https://man7.org/linux/man-pages/man2/recvmsg.2.html). See [verification](verification.md) for the repository harness and [design rationale](design/socket.md) for boundary and ownership decisions.
