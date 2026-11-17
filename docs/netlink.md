# Netlink

`netlink` supplies Linux message/attribute framing, kernel request matching, acknowledgements, multipart dumps, and multicast reception. It uses [socket](socket.md) internally; callers perform protocol operations without descriptors or poll registration. See the [package reference](https://pkg.go.dev/github.com/alexfalkowski/go-linux/netlink) and [Linux Netlink introduction](https://docs.kernel.org/userspace-api/netlink/intro.html).

## Discover a Generic Netlink family

```go
conn, err := netlink.Open(socket.Generic, netlink.Options{})
if err != nil {
    return err
}
defer conn.Close()

if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
    return err
}
family, err := conn.LookupFamily("nlctrl")
if err != nil {
    return err
}
fmt.Println(family.Name, family.ID)
```

Import `github.com/alexfalkowski/go-linux/netlink` and `github.com/alexfalkowski/go-linux/socket`, plus the standard packages used above. `Families` dumps registered families. These methods require `socket.Generic`, validate controller attribute widths, names, duplicate known fields, and nested group records, and retain unknown attributes. Failed discovery returns no family/list; use the lower-level request operations when partial wire results are useful. Lookup of an unknown name can trigger the kernel's module autoload path. Family and group IDs are runtime observations and can be reused after unregistering; the package does not cache them.

## Requests and dumps

Build a `Message` with a protocol-specific `Header.Type` and `Data`. `GenericMessage.MarshalBinary` adds `genlmsghdr`, and `UnmarshalBinary` decodes it with an owned copy of the family payload; any family-specific fixed header precedes that family's attributes. `MarshalMessages`/`UnmarshalMessages` and `MarshalAttributes`/`UnmarshalAttributes` provide matching framing operations with native-endian headers and four-byte padding. Controller command/attribute constants and ACK attribute constants are available from `netlink`, so callers need no syscall-package import. Use `Attribute.Data` for binary values; `Uint16`, `Uint32`, and `Uint64` enforce exact widths and respect `NetworkByteOrder`. `String` requires a single final NUL and rejects interior NULs. Framing parsers preserve unknown IDs, duplicate attributes, output flags, and ordering; the consumer owns family-specific policy beyond the bounded controller decoder.

`Execute` adds `Request|Acknowledge`, assigns a nonzero sequence, and waits for the ACK. `Dump` additionally adds `DumpFlags` and waits for `NLMSG_DONE`, including when an ACK arrives first. Input sequence/port fields must be zero. Execute accepts the named NEW modifiers `Replace`, `Exclusive`, `Create`, and `Append`; Dump accepts only request/ACK/dump flags. Modifier bits overlap in Linux, so the method selects their meaning. Echo requests and custom completion protocols are outside this interface. `Response.Messages` excludes control messages; `Acknowledgement` and `Done` retain status and diagnostics, including success warnings. A completed kernel rejection leaves the connection usable. Responses are matched using the kernel socket sender, sequence, local-or-zero header port, and echoed request header. A header port of zero alone never authenticates a sender.

Concurrent requests on one `Conn` serialize. Use separate connections for parallel requests and a separate `Listener` for multicast. Deadlines apply to pending/future I/O, including a request that reaches I/O after waiting for the request lock; they do not bound the lock wait itself. Close interrupts active I/O and queued requests subsequently see closure. No receive goroutine or context adapter is hidden inside the library. Set deadlines explicitly: zero means no I/O deadline.

## Failure and partial results

Always inspect the returned error, even when `Response.Complete` is true. That field means a terminal status was received; it does not mean the operation succeeded or that a dump is consistent. `*netlink.Error` preserves kernel errno through `errors.Is` and exposes diagnostic text, rejected-attribute offset, binary cookies, and every extended ACK attribute. Diagnostic text can change across kernel versions. `ParseAcknowledgement` also decodes capped and uncapped controls supplied by other transports.

`NLM_F_DUMP_INTR` anywhere in a matched dump produces `ErrDumpInterrupted`; the connection drains through completion when possible. A dump is never an atomic snapshot and the package never retries it automatically. `ENOBUFS` or `NLMSG_OVERRUN` means messages were lost. Datagram truncation produces `ErrTruncated`; accumulated response overflow produces `ErrLimit`. `ErrMalformed` identifies invalid framing/control policy; `ErrUnexpectedReply` identifies sender or matching violations. Complete earlier data messages remain in `Response` on an exchange error, but an untrusted/truncated datagram contributes no partial messages. Convenience family discovery discards partial lists.

An incomplete exchange closes its request connection, including after receive timeout, malformed data, loss, or a size bound. Open a fresh connection before retrying. This prevents unread replies or a still-active dump from contaminating another request. A send failure leaves the connection open and retains its errno, including `EAGAIN`; the caller decides whether to retry. No transport error proves a kernel mutation was not applied. Constructors return no resource on failure and release everything acquired, including earlier multicast memberships.

`Options.DatagramSize` defaults to 65536 bytes and `MaxResponseBytes` to 16 MiB of received wire data. These bounds are explicit, not guarantees that every family dump fits. `StrictCheck` requests the kernel's route GET/dump validation; validation support remains operation-specific. All connections enable extended and capped ACKs and retain option failures rather than silently changing behavior.

## Multicast

Use `Listen(protocol, options, groupIDs...)` with positive numeric group IDs; these are not legacy bitmasks. Generic Netlink group IDs come from `Family.Groups`. `JoinGroup` and `LeaveGroup` change individual memberships. A failed initial membership closes the entire listener. Queued messages can survive membership removal.

`ReadMessages` returns one datagram, preserving message boundaries and authenticated kernel messages. Data stays valid after later reads and Close. Multiple readers compete for datagrams. A deadline error leaves the listener open; Close interrupts all blocked readers. Notifications are lossy: handle `ENOBUFS`, overrun controls, and truncation by rebuilding state over a separate request connection. The API does not identify the destination multicast group; use separate listeners when that distinction matters. It does not provide an atomic dump-plus-subscription handshake, automatic resynchronization, or a transaction across messages.

`UnmarshalMessages` and `UnmarshalAttributes` borrow their input and return no partial batch on malformed framing. Lengths include headers and exclude padding. Final padding may be completely absent, but partial padding and trailing fragments are rejected. Nested attributes are decoded one level at a time; parsing does not recurse implicitly. The executable examples cover discovery, a raw controller dump, and attribute conversion.

The [design note](design/netlink.md) explains connection separation and failure handling. [Verification](verification.md) owns contributor commands and runner requirements; [verification limits](verification-limits.md) records missing runtime evidence. [netlink/route](netlink-route.md) owns typed routing operations. Automatic family-policy discovery/enforcement, userspace peer messaging, raw descriptor adoption, and custom Netlink exchange protocols are outside this package's current scope.
