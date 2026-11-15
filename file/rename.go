//go:build linux

package file

import (
	"os"

	"golang.org/x/sys/unix"
)

// RenameMode selects one renameat2 operation. Modes cannot be combined.
type RenameMode uint32

// Replace atomically replaces an existing destination, as in os.Rename.
const Replace RenameMode = 0

// NoReplace fails with EEXIST if the destination exists (RENAME_NOREPLACE).
const NoReplace RenameMode = unix.RENAME_NOREPLACE

// Exchange atomically swaps two existing entries, which may have different
// types (RENAME_EXCHANGE). Both names must exist.
const Exchange RenameMode = unix.RENAME_EXCHANGE

// Rename moves or exchanges directory entries through [renameat2(2)]. Each
// name is relative to its corresponding directory; absolute names ignore that
// anchor. Both anchors must be live and nonnil. Intermediate symlinks and ..
// use ordinary resolution: this operation does not confine untrusted paths.
// Final symlinks are moved or exchanged themselves, without following them.
//
// Unknown or combined modes match os.ErrInvalid before borrowing either file.
// Errors are os.LinkError values preserving errno and both supplied names.
// Filesystem support, permissions, and cross-mount failures remain observable;
// there is no emulation with separate existence checks or multiple renames.
// Success is atomic for the directory entries, not a durable-storage guarantee
// or an atomic snapshot for subsequent lookups. Borrowed files remain open.
//
// [renameat2(2)]: https://man7.org/linux/man-pages/man2/rename.2.html
func Rename(oldDir *os.File, oldName string, newDir *os.File, newName string, mode RenameMode) error {
	var err error
	switch mode {
	case Replace, NoReplace, Exchange:
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
