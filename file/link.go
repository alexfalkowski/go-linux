//go:build linux

package file

import (
	"os"

	"golang.org/x/sys/unix"
)

// Link creates a hard link to source at name using [linkat(2)] with
// AT_EMPTY_PATH. Use it to publish an inode opened with TempFile without
// Exclusive. An existing destination returns EEXIST. Both files are borrowed
// and remain caller-owned. A successful link survives closing source.
//
// Relative names are resolved from dir, and absolute names ignore the anchor.
// A live dir is required in either case. Resolution follows ordinary Linux
// rules, including traversal of intermediate symlinks and parent directories.
// The source and destination directory must be on the same mount. Linux requires
// the opener's credentials or CAP_DAC_READ_SEARCH in the opener's user namespace.
// Hard-link policy and destination permissions also apply.
//
// Nil or zero files match os.ErrInvalid. Closed files match os.ErrClosed.
// Errors are *os.LinkError values and preserve errno. The operation may block.
//
// [linkat(2)]: https://man7.org/linux/man-pages/man2/link.2.html
func Link(source, dir *os.File, name string) error {
	if source == nil || *source == (os.File{}) {
		return &os.LinkError{Op: "linkat", New: name, Err: os.ErrInvalid}
	}

	var err error
	if dir == nil || *dir == (os.File{}) {
		err = os.ErrInvalid
	} else {
		err = control(source, func(sourceFD int) error {
			return control(dir, func(dirFD int) error {
				return unix.Linkat(sourceFD, "", dirFD, name, unix.AT_EMPTY_PATH)
			})
		})
	}
	if err != nil {
		return &os.LinkError{Op: "linkat", Old: source.Name(), New: name, Err: err}
	}
	return nil
}
