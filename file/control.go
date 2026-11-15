//go:build linux

package file

import "os"

// control borrows a standard file without exporting descriptor plumbing or
// changing shared flags. Keep nested calls in caller order: RawConn references
// pin lifetimes without holding a mutually exclusive lock across callbacks.
func control(file *os.File, operation func(int) error) error {
	conn, err := file.SyscallConn()
	if err != nil {
		return err
	}

	var callErr error
	if err := conn.Control(func(fd uintptr) {
		callErr = operation(int(fd))
	}); err != nil {
		// os.File exposes a private closing error through RawConn.Control.
		return os.ErrClosed
	}
	return callErr
}
