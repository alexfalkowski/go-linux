//go:build linux

// Package fanotify provides Linux descriptor-based filesystem notifications and
// access permission decisions. It requires Linux 7.0+, CONFIG_FANOTIFY, and
// CAP_SYS_ADMIN in the initial user namespace. Permission groups additionally
// require CONFIG_FANOTIFY_ACCESS_PERMISSIONS. Security policy can deny either
// syscall independently of capabilities.
//
// New creates a Group. AddMark selects objects and events. ReadEvents transfers
// event ownership to the caller; every returned Event must be closed. Events
// instead lends each event for one loop iteration and closes it afterwards,
// including on early exit. Event files remain private; ReadAt and Stat inspect
// the object without reopening a potentially changed path.
//
// Respond allows or denies a permission event. Closing an unanswered event
// attempts to deny it. Closing the group releases pending kernel permission
// requests as allowed: this mechanism is not a fail-closed security boundary.
// Avoid monitoring the listener's own I/O with permission marks, which can
// deadlock it. See [fanotify(7)] for filesystem and event-generation limitations.
//
// [fanotify(7)]: https://man7.org/linux/man-pages/man7/fanotify.7.html
package fanotify
