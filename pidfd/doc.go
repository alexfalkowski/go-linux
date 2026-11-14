//go:build linux

// Package pidfd owns Linux process descriptors for signaling, exit observation,
// and eligible child reaping. It requires Linux 7.0 or later with permitted
// pidfd, pidfs inspection, eventfd, poll, and waitid operations.
//
// OpenProcess borrows an os.Process to acquire an independently owned handle.
// Open acquires a numeric PID; Adopt consumes an existing close-on-exec pidfd
// even on failure. Acquiring a PID can race with PID reuse. Acquired handles
// retain their process identity and must be closed deterministically.
//
// WaitReady observes exit without reaping. Wait consumes an eligible child's
// exit status and preserves ECHILD for non-children and external-reaper races.
// Keep reaping with exec.Cmd.Wait for children managed by os/exec. os.Process
// already supplies ordinary process management; this
// package adds independent descriptor ownership and close-interruptible waits.
// Close does not signal or reap the process. No deadlines or context API are
// exposed; each blocked waiter can occupy an OS thread in poll.
//
// See [pidfd_open(2)], [pidfd_send_signal(2)], and [waitid(2)].
//
// [pidfd_open(2)]: https://man7.org/linux/man-pages/man2/pidfd_open.2.html
// [pidfd_send_signal(2)]: https://man7.org/linux/man-pages/man2/pidfd_send_signal.2.html
// [waitid(2)]: https://man7.org/linux/man-pages/man2/waitid.2.html
package pidfd
