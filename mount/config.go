//go:build linux

package mount

import (
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

// Open acquires a close-on-exec filesystem configuration context with fsopen.
// The caller owns the result and must close it. No configuration, creation,
// attachment, or namespace entry occurs implicitly. Filesystem names use kernel
// validation; embedded NUL bytes fail with EINVAL. Errors wrap os.PathError and
// preserve errno identity. On error no descriptor is retained and result is nil.
func Open(filesystem string) (*Config, error) {
	fd, err := unix.Fsopen(filesystem, unix.FSOPEN_CLOEXEC)
	if err != nil {
		return nil, &os.PathError{Op: "fsopen", Path: filesystem, Err: err}
	}
	return &Config{file: os.NewFile(uintptr(fd), filesystem)}, nil
}

// Config owns one filesystem configuration descriptor and implements io.Closer. Do not copy it after first use. Methods, including Close
// and diagnostic reads, are serialized. Each operation's error diagnostics are
// collected while holding the same lock; pending warnings from earlier success
// can also be included. Sequences of methods are not transactions. The kernel
// owns configuration phase validation; a failed Create may render it unusable.
// A zero Config owns nothing and methods returning errors match os.ErrInvalid.
type Config struct {
	file *os.File
	mu   sync.Mutex
}

// SetFlag supplies a parameter with no value using FSCONFIG_SET_FLAG. It is
// distinct from SetString(key, ""). Unknown parameters and phase errors are
// kernel-defined. On syscall failure an Error preserves available diagnostics;
// the existing Config remains caller-owned, including after failure.
func (c *Config) SetFlag(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.configure("fsconfig_set_flag", func(fd int) error {
		return unix.FsconfigSetFlag(fd, key)
	})
}

// SetString supplies a string parameter with FSCONFIG_SET_STRING. Empty values
// are permitted; NUL bytes in key/value match EINVAL. Filesystem validation and
// failure ownership/diagnostics follow SetFlag. No option-list parsing occurs.
func (c *Config) SetString(key, value string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.configure("fsconfig_set_string", func(fd int) error {
		return unix.FsconfigSetString(fd, key, value)
	})
}

// Create prepares the filesystem with FSCONFIG_CMD_CREATE. The kernel may
// reuse an existing filesystem instance; this is not exclusive creation or an
// isolation guarantee. Mount remains a separate step. Errors and ownership
// follow SetFlag; successful configuration is not rolled back on later failure.
func (c *Config) Create() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.configure("fsconfig_create", unix.FsconfigCreate)
}

// Mount acquires a detached close-on-exec mount with fsmount. Create must have
// succeeded. Zero attributes adds no restrictions; unknown bits match
// os.ErrInvalid. The caller owns the new Handle and the existing Config
// independently. Failure returns nil, retains no new descriptor, and leaves
// Config open. A kernel failure can advance configuration state even without a
// returned handle. Available diagnostics are preserved as for SetFlag.
func (c *Config) Mount(attributes Attributes) (*Handle, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if attributes & ^supportedAttributes != 0 {
		return nil, &os.PathError{Op: "fsmount", Path: "mount", Err: os.ErrInvalid}
	}
	var result int
	err := c.configure("fsmount", func(fd int) error {
		var err error
		result, err = unix.Fsmount(fd, unix.FSMOUNT_CLOEXEC, int(attributes))
		return err
	})
	if err != nil {
		return nil, err
	}
	return &Handle{file: os.NewFile(uintptr(result), "mount")}, nil
}

// Diagnostics drains the currently available kernel records. No records returns
// nil, nil. Partial records accompany retrieval errors. Failed configuration
// methods already drain into Error; those messages are not returned again.
// The bounded kernel queue can overwrite earlier records. Reads grow from 4 KiB
// to 1 MiB on EMSGSIZE; larger messages return EMSGSIZE without consuming that
// record on Linux 7.0+. No lossless log or fixed producer size is promised.
func (c *Config) Diagnostics() ([]Diagnostic, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var records []Diagnostic
	err := fileControl(c.file, "fscontext_read", "mount", func(fd int) error {
		var err error
		records, err = readDiagnostics(fd)
		return err
	})
	return records, err
}

// Close releases the configuration context, including its pending diagnostics.
// It leaves previously acquired handles and attached mounts intact. Repeated
// Close matches os.ErrClosed. It waits for in-flight methods to finish.
func (c *Config) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.file.Close()
}

// configure runs with mu held and collects diagnostics before another method
// can change the queue. Reading within Control also pins lifetime throughout.
func (c *Config) configure(op string, operation func(int) error) error {
	return fileControl(c.file, op, "mount", func(fd int) error {
		if err := operation(fd); err != nil {
			records, diagnosticErr := readDiagnostics(fd)
			return &Error{Err: os.NewSyscallError(op, err), Diagnostics: records, DiagnosticError: diagnosticErr}
		}
		return nil
	})
}
