//go:build linux

package procfs

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/prometheus/procfs"
)

// ErrParse identifies malformed or incomplete selected procfs information.
// Errors include the source path without copying file contents into messages.
var ErrParse = errors.New("invalid procfs data")

// Open checks root and returns an immutable view. root must name a directory;
// an empty path or a nondirectory matches os.ErrInvalid. Relative roots are
// made absolute at construction. Ordinary directories support synthetic fixtures;
// the filesystem need not be a proc mount. No descriptors remain open.
func Open(root string) (*FS, error) {
	if root == "" {
		return nil, &os.PathError{Op: "open", Path: root, Err: os.ErrInvalid}
	}

	path, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrInvalid}
	}

	fs, err := procfs.NewFS(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return &FS{root: path, fs: fs}, nil
}

// FS wraps the upstream filesystem to keep callers on this package's bounded
// public surface. It owns no persistent resources and is safe for concurrent use.
type FS struct {
	root string
	fs   procfs.FS
}

// Process checks that pid's directory exists and returns a path-based view.
// pid must be positive and fit a Linux signed 32-bit PID, otherwise the error
// matches os.ErrInvalid. The lookup may race with exit, replacement, and reuse.
func (fs *FS) Process(pid int) (*Process, error) {
	if fs == nil || fs.root == "" || pid <= 0 || pid > math.MaxInt32 {
		return nil, os.ErrInvalid
	}

	path := filepath.Join(fs.root, strconv.Itoa(pid))
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrInvalid}
	}

	p, err := fs.fs.Proc(pid)
	if err != nil {
		return nil, readError(path, err)
	}
	return &Process{fs: fs, process: p}, nil
}

// Process wraps an upstream process without exposing its types or method set.
// It is a concurrent-safe numeric-PID view, not an owned process handle; use
// package pidfd for stable process operations. It requires no Close.
type Process struct {
	fs      *FS
	process procfs.Proc
}

// Self resolves the self symlink in the selected root, so it respects that
// proc mount's PID namespace. Fixtures must supply a positive decimal PID link.
func (fs *FS) Self() (*Process, error) {
	if fs == nil || fs.root == "" {
		return nil, os.ErrInvalid
	}

	path := filepath.Join(fs.root, "self")
	target, err := os.Readlink(path)
	if err != nil {
		return nil, err
	}

	pid, err := strconv.ParseInt(target, 10, 32)
	if err != nil {
		return nil, parseError(path)
	}
	if pid <= 0 {
		return nil, parseError(path)
	}
	return fs.Process(int(pid))
}

// Processes lists positive numeric process entries in increasing order.
// Enumeration does not check their contents or keep the processes alive.
// Nonnumeric entries and numbers outside Linux's PID range are ignored.
func (fs *FS) Processes() ([]int, error) {
	if fs == nil || fs.root == "" {
		return nil, os.ErrInvalid
	}

	processes, err := fs.fs.AllProcs()
	if err != nil {
		return nil, readError(fs.root, err)
	}

	pids := make([]int, 0, len(processes))
	for _, process := range processes {
		if process.PID > 0 && process.PID <= math.MaxInt32 {
			pids = append(pids, process.PID)
		}
	}
	slices.Sort(pids)
	return pids, nil
}

// PID returns the number selected at construction, or zero for a nil or zero
// Process. It does not check whether that number still names the same process.
func (p *Process) PID() int {
	if p == nil {
		return 0
	}
	return p.process.PID
}

func (p *Process) path(entry string) (string, error) {
	if p == nil || p.fs == nil {
		return "", os.ErrInvalid
	}
	return filepath.Join(p.fs.root, strconv.Itoa(p.process.PID), entry), nil
}

// read confines parser panics to the dependency boundary. Upstream v0.23.0's
// process-stat parser can slice outside malformed comm delimiters. Returning a
// local error keeps fixture and kernel data from causing caller panics, without
// adding a second implementation of the upstream parsers.
func read[T any](path string, operation func() (T, error)) (value T, err error) {
	defer func() {
		if recover() != nil {
			var zero T
			value, err = zero, parseError(path)
		}
	}()

	value, err = operation()
	if err != nil {
		var zero T
		return zero, readError(path, err)
	}
	return value, nil
}

func readError(path string, err error) error {
	if pathErr, ok := errors.AsType[*os.PathError](err); ok {
		return pathErr
	}
	return parseError(path)
}

func parseError(path string) error {
	return &os.PathError{Op: "parse", Path: path, Err: ErrParse}
}
