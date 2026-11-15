//go:build linux

// Package inotify provides Linux file and directory notifications.
// It requires Linux 7.0 or later and permitted inotify and polling operations.
//
// New creates an owned Watcher. AddWatch selects inode events and returns a
// WatchID local to that watcher. Events yields individual events for range
// loops, and ReadEvents decodes complete kernel batches. Read and ParseEvents
// expose the same records for callers managing raw batches.
// Reads wait through Go's poller until an event, a read deadline, or Close.
//
// Watches are not recursive. Events can be coalesced or lost on queue overflow;
// QueueOverflow and Ignored remain visible. Rename cookies correlate events
// without promising adjacent or complete pairs. Watch IDs can be reused, and
// queued events can outlive a watch. See [inotify(7)] for these limitations.
//
// [inotify(7)]: https://man7.org/linux/man-pages/man7/inotify.7.html
package inotify
