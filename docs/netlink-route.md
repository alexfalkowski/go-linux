# Routing Netlink

`netlink/route` provides typed interface, IPv4/IPv6 address, and routing-table operations over [netlink](netlink.md). It owns request and notification sockets internally. Use standard `net` for ordinary network I/O; use this package for Linux interface configuration, routing identifiers, and multicast changes. See the [package reference](https://pkg.go.dev/github.com/alexfalkowski/go-linux/netlink/route) and [rtnetlink(7)](https://man7.org/linux/man-pages/man7/rtnetlink.7.html).

## Good for

Good for network inventory, interface configuration, and routing agents that react to kernel changes. [iproute2's ip route implementation](https://github.com/iproute2/iproute2/blob/main/ip/iproute.c) uses the same routing Netlink mechanism for route queries and mutations.

## Read current state

```go
conn, err := route.Open(netlink.Options{StrictCheck: true})
if err != nil {
    return err
}
defer conn.Close()

if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
    return err
}
links, err := conn.Links()
if err != nil {
    return err
}
for _, link := range links {
    fmt.Println(link.Index, link.Name, link.MTU, link.Flags&route.Up != 0)
}
```

Import `github.com/alexfalkowski/go-linux/netlink/route` and `github.com/alexfalkowski/go-linux/netlink`, plus the standard packages used above. `Addresses(route.IPv4)` and `Routes(route.IPv6)` select exactly one IP family; zero and other families are invalid. `Links` lists interfaces independently of IP configuration. The executable examples cover interface and route listing.

Each list is a separate kernel dump, not an atomic snapshot or transaction. A failed, interrupted, oversized, or malformed dump returns a nil list. An empty successful dump returns an empty slice. Results remain valid after subsequent calls and Close. Interface indices and other identifiers can be reused after removal.

`Link.Flags` preserves Linux `IFF_*` values, which differ from `net.Flags`. `Address.Prefix` retains the local host bits: `192.0.2.1/24` is an address, not the masked route prefix `192.0.2.0/24`. Linux `IFA_LOCAL` wins over `IFA_ADDRESS`; a differing `IFA_ADDRESS` appears as `Peer`. The full `IFA_FLAGS` attribute overrides the header's eight-bit fallback. `Route.Source` is a source selector, distinct from `PreferredSource`. `Table` retains full 32-bit IDs, including an `RTA_TABLE` override of the fixed header.

Records retain all original `Attributes`, including unknown IDs, output flags, nested route data, cache lifetimes, and link-kind metadata. The typed fields interpret a bounded subset. In particular, an absent route `Gateway` or zero `Index` does not mean there are no next hops: advanced route forms remain in Attributes. `ParseLink`, `ParseAddress`, `ParseRoute`, and `ParseEvent` also decode messages obtained through the base Netlink package. These parsers borrow attribute bytes from the input; they reject malformed interpreted fields and duplicate interpreted attributes while preserving unknown attributes and repetitions. They do not validate the schemas of opaque attributes.

## Change configuration

Mutations need applicable `CAP_NET_ADMIN` and kernel support in the connection's network namespace. `Open` captures the calling thread's current namespace through socket creation; this package does not change namespaces. Check each returned error. Changes persist after closing the connection, and address/link deletion can remove associated routes.

`AddLink("example0", route.Dummy)` creates a dummy device with exclusive-create semantics. The dummy driver must be available; requesting it may trigger kernel module autoloading. List links to obtain its assigned index. Only dummy creation is supported. `ChangeLink(index, route.LinkChange{Flags: route.Up, Change: route.Up})` sets administrative state without rewriting other flags; `Change: route.Up` with zero Flags clears it. Name and nonzero MTU optionally rename/change the device. Only the documented mutable flag mask is accepted, and Flags must be a subset of Change. Zero Name/MTU leaves those properties untouched. A multi-property change can partially apply before a kernel error; it is not a transaction. `DeleteLink(index)` asks Linux to remove that interface, subject to device restrictions.

For an existing positive interface index, the address request is explicit:

```go
config := route.AddressConfig{
    Index: index,
    Prefix: netip.MustParsePrefix("192.0.2.1/24"),
    Flags: route.NoPrefixRoute,
}
if err := conn.AddAddress(config); err != nil {
    return err
}
// Later, when removal is intended:
if err := conn.DeleteAddress(config); err != nil {
    return err
}
```

`AddressConfig` supports an optional same-family peer, IPv4 broadcast/label, scope, `NoDAD`/`NoPrefixRoute`, and automatic prefix-route priority. An invalid peer means use the local address; invalid optional broadcast means omit it. Zone suffixes are not inputs: Index supplies the interface identity. Host bits in Prefix are preserved. Add/change omit cache lifetimes and request permanent lifetime; finite lifetimes and address generation policies are outside this API.

`AddAddress` uses `CREATE|EXCL`; `ChangeAddress` uses `REPLACE`. Linux can create an absent address on replacement, even without CREATE. IPv4 replacement updates lifetime and prefix-route priority while retaining existing flags, scope, label, peer, and broadcast. IPv6 modification follows its kernel rules, including flag changes. Replacement is not a general rewrite of every config field. Delete uses the local/peer/prefix identity. Zero priority requests the kernel default on creation; on replacement, it resets IPv4 priority to its default but preserves an existing IPv6 priority.

`AddRoute`, `ChangeRoute`, and `DeleteRoute` accept `RouteConfig`. Destination must be a valid, masked IP prefix; default routes are explicitly `0.0.0.0/0` or `::/0`. Optional Gateway and PreferredSource must be unzoned and in the destination family. Index selects an output interface; zero leaves selection to Linux. Table zero selects MainTable, including for deletion. Protocol/Type zero use BootProtocol/Unicast for add/change and remain unspecified selectors on delete. Priority zero omits the metric, which can act as a wildcard on delete. Scope zero is Universe; callers normally choose LinkScope for an IPv4 direct route. Unicast, Local, Blackhole, Unreachable, and Prohibit are the accepted mutation types; device and family restrictions remain kernel errors.

Route add uses `CREATE|EXCL`; change uses `REPLACE` without CREATE. Linux chooses replacement identity, including the route metric. This does not implement a userspace delete/recreate sequence or an existence preflight. Deletion can match unspecified fields as wildcards; provide enough identity to select the intended route. Separate request configs prevent accidentally replaying opaque multipath or encapsulated observations as simpler routes.

## Observe changes

`Listen(netlink.Options{}, route.LinkGroup, route.IPv4AddressGroup, route.IPv6AddressGroup, route.IPv4RouteGroup, route.IPv6RouteGroup)` opens a separate socket with numeric memberships. Supply at least one supported group. SetReadDeadline bounds `ReadEvents`, which returns one datagram's events. Exactly one of Event.Link, Address, or Route is set. `NewLink`, `NewAddress`, and `NewRoute` report both creation and changes; the corresponding `Del*` types report removal. A notification is not a transaction boundary, and one mutation may emit multiple notifications.

Notifications can be lost. `ENOBUFS`, `netlink.ErrTruncated`, and unsupported or malformed records return an error with no partial batch. The listener stays open, including after deadline expiration; rebuild state over a separate request connection when continuity is lost. Returned records remain valid after later reads and Close. There is no atomic subscribe-and-dump handshake or automatic reconciliation. Consumers own scheduling and any channels; concurrent readers compete for datagrams.

## Ownership, errors, and limits

Open/Listen return nil and release all acquired sockets/memberships on failure. Owners must be used through pointers and not copied. Zero owners return `os.ErrInvalid`; repeated Close and valid operations after Close match `os.ErrClosed`. Validation happens before I/O, so invalid arguments still match `os.ErrInvalid` on a closed connection. Constructors accept the base package's explicit receive/accumulation limits; zero options use its defaults. Deadlines default to unbounded, so set them for bounded waits.

Requests serialize per connection. Close and deadline setters can interrupt active I/O; deadlines do not bound waiting for the request lock. An incomplete exchange closes the underlying request connection, preventing unread replies from affecting later requests. A fully drained dump with a typed-decoding error leaves it open. Completed kernel rejections preserve errno and extended error diagnostics through `*netlink.Error`, and leave it usable. Receive timeout matches `os.ErrDeadlineExceeded`. No transport error proves a mutation was not applied, and the package never retries or rolls back mutations.

Traffic control, neighbors/rules, route lookup, every link type, link namespace moves, multipath/nexthop-object mutations, route encapsulation/metrics nests, finite address lifetimes, and transactions are outside this initial operation set. The [design note](agents/accepted-design/netlink-route.md) explains the boundaries. [Verification](verification.md#netlink-isolation) owns runner commands and isolation; [verification limits](verification-limits.md) records missing execution evidence.

The [Linux 7.0 routing UAPI](https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/rtnetlink.h) separates neighbor, rule, and nexthop messages from the link/address/route records interpreted here. Preserving opaque attributes does not supply typed mutation for those families. Veth peer configuration, bridge membership, and namespace transfer are not supported by `AddLink`/`ChangeLink`. NAT/firewall policy belongs to a separate Netfilter integration, such as the external composition described in the [support guide](support.md#compose-a-container-lifecycle); ordinary routes do not provide it.
