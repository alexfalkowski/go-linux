//go:build linux

// Package socket provides Linux message sockets, ancillary file/credential
// handling, and selected Netlink options. It requires Linux 7.0 or later.
//
// Pair creates Unix datagram or seqpacket peers; ListenUnix binds datagrams;
// OpenNetlink binds a Netlink endpoint. Each Conn owns its descriptor. Close
// releases it and interrupts pending I/O. Nonblocking, close-on-exec creation
// and Go polling are mandatory; constructors return no resource on failure.
//
// Send and Receive preserve message boundaries. Receive exposes payload and
// control truncation and returns owned *os.File values for received rights.
// Always close successful message.Files, even when payload truncation occurred.
// Files passed to Send remain caller-owned. No public raw descriptors are needed.
// Use net for TCP, UDP, Unix streams, and stream connection management.
//
// See [socket(7)], [unix(7)], [netlink(7)], [sendmsg(2)], and [recvmsg(2)].
//
// [socket(7)]: https://man7.org/linux/man-pages/man7/socket.7.html
// [unix(7)]: https://man7.org/linux/man-pages/man7/unix.7.html
// [netlink(7)]: https://man7.org/linux/man-pages/man7/netlink.7.html
// [sendmsg(2)]: https://man7.org/linux/man-pages/man2/sendmsg.2.html
// [recvmsg(2)]: https://man7.org/linux/man-pages/man2/recvmsg.2.html
package socket
