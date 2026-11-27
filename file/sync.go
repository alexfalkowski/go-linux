//go:build linux

package file

import (
	"os"

	"golang.org/x/sys/unix"
)

// Fdatasync synchronizes the file's data and the metadata needed to retrieve
// that data, such as its size. Use [os.File.Sync] to also synchronize other file
// metadata, and synchronize the containing directory separately when a new or
// renamed entry needs to be durable.
//
// The file is borrowed and its offset is preserved. A nil or zero file matches
// os.ErrInvalid, and a closed file matches os.ErrClosed. Errors use *os.PathError
// and preserve errno. The call may block without a deadline.
func Fdatasync(f *os.File) error {
	return syncFile(f, "fdatasync", unix.Fdatasync)
}

// SyncFS synchronizes pending data and metadata across the filesystem
// containing f. A regular file or directory can identify the filesystem.
// Linux can report writeback errors from elsewhere on that filesystem.
// Duplicated handles share the record of which errors Linux has reported.
//
// The file is borrowed and its offset is preserved. A nil or zero file matches
// os.ErrInvalid, and a closed file matches os.ErrClosed. Errors use *os.PathError
// and preserve errno. The call may block without a deadline.
func SyncFS(f *os.File) error {
	return syncFile(f, "syncfs", unix.Syncfs)
}

func syncFile(f *os.File, op string, operation func(int) error) error {
	if f == nil || *f == (os.File{}) {
		return &os.PathError{Op: op, Err: os.ErrInvalid}
	}
	if err := control(f, operation); err != nil {
		return &os.PathError{Op: op, Path: f.Name(), Err: err}
	}
	return nil
}
