//go:build linux

// Package netlink implements Linux Netlink framing, kernel requests, dumps,
// acknowledgements, and multicast reception over the socket package.
//
// Open creates a request connection; concurrent Execute and Dump calls serialize.
// Listen creates a separate multicast listener. Close and deadlines interrupt
// blocked I/O. Owners must not be copied after use; their zero values own nothing
// and return os.ErrInvalid. Nil receivers are unsupported. Invalid input takes
// precedence over closed state. No descriptor access is exposed.
//
// Framing decoders borrow input, preserve unknown attributes, and reject malformed
// framing without partial results. Requests retain partial replies on failure,
// explicitly report truncation and interrupted dumps, and never retry a dump or
// imply an atomic snapshot. See https://docs.kernel.org/userspace-api/netlink/intro.html.
package netlink
