//go:build linux

// Package route provides typed Linux rtnetlink operations for links, IPv4/IPv6
// addresses, and routes. Request connections and multicast listeners own separate
// sockets; no descriptor or syscall management is required by callers.
//
// Dumps are observations, not atomic snapshots. Notifications can be lost; use a
// separate Conn to rebuild state after loss. Mutations require applicable network
// administration privilege and persist independently of Close. An error after
// sending does not prove that a change was not applied. No operation retries a
// mutation or rolls it back.
//
// See [rtnetlink(7)] and the [usage guide].
//
// [rtnetlink(7)]: https://man7.org/linux/man-pages/man7/rtnetlink.7.html
// [usage guide]: https://github.com/alexfalkowski/go-linux/blob/master/docs/netlink-route.md
package route
