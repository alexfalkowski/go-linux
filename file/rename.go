//go:build linux

package file

import (
	"os"

	"golang.org/x/sys/unix"
)

// RenameMode selects one of the named renameat2 operations.
type RenameMode uint32

// Replace atomically replaces an existing destination, as in os.Rename.
const Replace RenameMode = 0

// NoReplace fails with EEXIST if the destination exists (RENAME_NOREPLACE).
const NoReplace RenameMode = unix.RENAME_NOREPLACE

// Exchange atomically swaps two existing entries, which may have different
// types (RENAME_EXCHANGE). Both names must exist.
const Exchange RenameMode = unix.RENAME_EXCHANGE

// Whiteout moves the source and atomically creates a whiteout at its old
// name (RENAME_WHITEOUT). The destination can be replaced, as with Replace.
// If both names already refer to the same inode, the operation succeeds as a no-op.
const Whiteout RenameMode = unix.RENAME_WHITEOUT

// WhiteoutNoReplace creates a whiteout as in Whiteout and returns EEXIST if
// the destination exists. Neither entry changes on that conflict.
const WhiteoutNoReplace RenameMode = unix.RENAME_WHITEOUT | unix.RENAME_NOREPLACE

// Rename moves or exchanges directory entries through [renameat2(2)]. Each
// name is relative to its corresponding directory; absolute names ignore that
// anchor. Both anchors must be live and nonnil. Intermediate symlinks and ..
// use ordinary resolution: this operation does not confine untrusted paths.
// Final symlinks are moved or exchanged themselves, without following them.
//
// Values outside the named modes match os.ErrInvalid before borrowing either file.
// Errors are os.LinkError values preserving errno and both supplied names.
// Filesystem support, permissions, and cross-mount failures remain observable;
// there is no emulation with separate existence checks or multiple renames.
// Whiteout modes require filesystem support and permission to create a whiteout.
// Success is atomic for the directory entries, not a durable-storage guarantee
// or an atomic snapshot for subsequent lookups. Borrowed files remain open.
//
// [renameat2(2)]: https://man7.org/linux/man-pages/man2/rename.2.html
func Rename(oldDir *os.File, oldName string, newDir *os.File, newName string, mode RenameMode) error {
	var err error
	switch mode {
	case Replace, NoReplace, Exchange, Whiteout, WhiteoutNoReplace:
		err = control(oldDir, func(oldFD int) error {
			return control(newDir, func(newFD int) error {
				return unix.Renameat2(oldFD, oldName, newFD, newName, uint(mode))
			})
		})
	default:
		err = os.ErrInvalid
	}
	if err != nil {
		return &os.LinkError{Op: "renameat2", Old: oldName, New: newName, Err: err}
	}
	return nil
}
