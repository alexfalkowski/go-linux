//go:build linux

package fanotify

import "golang.org/x/sys/unix"

// Class selects the Linux notification group class. Unknown values are invalid.
type Class uint

// Notification receives events after operations, without permission decisions.
const Notification Class = unix.FAN_CLASS_NOTIF

// Content receives notifications and permission requests before file access.
const Content Class = unix.FAN_CLASS_CONTENT

// Mask contains Linux FAN_* conditions and event selection modifiers.
// Unknown returned bits are preserved; mark operations reject unknown inputs.
type Mask uint64

// Access reports reading file data (FAN_ACCESS).
const Access Mask = unix.FAN_ACCESS

// Modify reports changes to file data (FAN_MODIFY).
const Modify Mask = unix.FAN_MODIFY

// CloseWrite reports closing a writable file (FAN_CLOSE_WRITE).
const CloseWrite Mask = unix.FAN_CLOSE_WRITE

// CloseNoWrite reports closing a file not opened for writing (FAN_CLOSE_NOWRITE).
const CloseNoWrite Mask = unix.FAN_CLOSE_NOWRITE

// Open reports opening a file (FAN_OPEN).
const Open Mask = unix.FAN_OPEN

// OpenExec reports opening a file for execution (FAN_OPEN_EXEC).
const OpenExec Mask = unix.FAN_OPEN_EXEC

// OpenPermission requests an open decision (FAN_OPEN_PERM).
const OpenPermission Mask = unix.FAN_OPEN_PERM

// AccessPermission requests a read decision (FAN_ACCESS_PERM).
// Install the mark before opening files whose reads must request permission.
const AccessPermission Mask = unix.FAN_ACCESS_PERM

// OpenExecPermission requests an execution-open decision (FAN_OPEN_EXEC_PERM).
const OpenExecPermission Mask = unix.FAN_OPEN_EXEC_PERM

// OnDir includes directory events and identifies returned directory events
// (FAN_ONDIR). It does not make inode marks recursive.
const OnDir Mask = unix.FAN_ONDIR

// EventOnChild includes immediate children of a marked directory
// (FAN_EVENT_ON_CHILD). It does not include deeper descendants.
const EventOnChild Mask = unix.FAN_EVENT_ON_CHILD

// QueueOverflow reports dropped notifications (FAN_Q_OVERFLOW), without a file.
// Callers must reconcile their state; this is an event, not a read error.
const QueueOverflow Mask = unix.FAN_Q_OVERFLOW

// MarkFlags controls path resolution and mark scope. Zero selects an inode
// mark following symlinks. Mount and Filesystem are mutually exclusive.
type MarkFlags uint

// DontFollow marks a symlink itself (FAN_MARK_DONT_FOLLOW).
const DontFollow MarkFlags = unix.FAN_MARK_DONT_FOLLOW

// OnlyDir requires a directory (FAN_MARK_ONLYDIR), otherwise Linux returns ENOTDIR.
const OnlyDir MarkFlags = unix.FAN_MARK_ONLYDIR

// Mount selects the mount containing the path (FAN_MARK_MOUNT), including its
// descendants within that mount, but excluding submounts.
const Mount MarkFlags = unix.FAN_MARK_MOUNT

// Filesystem selects the filesystem containing the path from all mount points
// (FAN_MARK_FILESYSTEM). Use only when this scope is intended.
const Filesystem MarkFlags = unix.FAN_MARK_FILESYSTEM

// Decision is a permission response. Only Allow and Deny are supported.
type Decision uint32

// Allow permits the pending operation, subject to other listeners and policy.
const Allow Decision = unix.FAN_ALLOW

// Deny rejects the pending operation with EPERM.
const Deny Decision = unix.FAN_DENY

const (
	permissions = OpenPermission | AccessPermission | OpenExecPermission
	conditions  = Access | Modify | CloseWrite | CloseNoWrite | Open | OpenExec | permissions
)
