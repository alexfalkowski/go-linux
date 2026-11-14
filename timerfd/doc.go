//go:build linux

// Package timerfd provides Linux descriptor timers with expiration counts,
// Go polling, and read deadlines. It requires Linux 7.0 or later and permitted
// timerfd, clock_gettime, and polling operations.
//
// New creates an owned, disarmed Timer for Monotonic, Realtime, or Boottime.
// SetAfter arms relatively; SetAt uses a tagged Instant from the same Linux
// clock, not Go's monotonic time origin. State and setters report relative
// remaining time and interval. Close the timer when done.
//
// Read consumes native-endian eight-byte expiration records; ReadExpirations
// decodes their counts. Reads wait through Go's poller, bounded by
// SetReadDeadline or Close. I/O deadlines are independent of timer arming.
// SyscallConn lends the same nonblocking, close-on-exec descriptor.
//
// See [timerfd_create(2)] for clock-change, count, and timer semantics.
//
// [timerfd_create(2)]: https://man7.org/linux/man-pages/man2/timerfd_create.2.html
package timerfd
