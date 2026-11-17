//go:build linux

package sysfs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/prometheus/procfs/sysfs"
)

// ErrParse identifies malformed selected sysfs values, including range overflow.
// Errors include the source path without copying attribute contents.
var ErrParse = errors.New("invalid sysfs data")

// ErrLimit identifies an attribute larger than the 1-MiB read limit.
var ErrLimit = errors.New("sysfs attribute exceeds read limit")

// Open checks root and returns a descriptor-free filesystem view. root must
// name a directory; empty and nondirectory roots match os.ErrInvalid. Relative
// roots become absolute, and ordinary directories support synthetic fixtures.
func Open(root string) (*FS, error) {
	if root == "" {
		return nil, &os.PathError{Op: "open", Path: root, Err: os.ErrInvalid}
	}

	path, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	if err := directory(path); err != nil {
		return nil, err
	}

	native, err := sysfs.NewFS(path)
	if err != nil {
		return nil, err
	}
	return &FS{root: path, fs: native}, nil
}

// FS bounds the upstream filesystem's public surface. It owns no persistent
// resources and can be used concurrently without synchronization.
type FS struct {
	root string
	fs   sysfs.FS
}

// BlockDevices lists class/block names, including partitions, in lexical order.
// It follows no entries and does not check whether listed devices still exist.
// Ordinary files are ignored; directories and symlinks are included.
func (fs *FS) BlockDevices() ([]string, error) {
	if fs == nil || fs.root == "" {
		return nil, os.ErrInvalid
	}
	return directories(filepath.Join(fs.root, "class/block"))
}

// NetworkInterfaces lists class/net names in lexical order, using the upstream
// sysfs reader. Enumeration does not inspect attributes or pin device identity.
// Ordinary files are ignored; directories and symlinks are included.
func (fs *FS) NetworkInterfaces() ([]string, error) {
	if fs == nil || fs.root == "" {
		return nil, os.ErrInvalid
	}

	names, err := fs.fs.NetClassDevices()
	if err != nil {
		return nil, err
	}
	return names, nil
}

// BlockDevice checks a class/block entry and selects its name. name must be one
// nonempty path component other than dot or dot-dot, or the error matches
// os.ErrInvalid. Class symlinks are followed; a missing target preserves errno.
func (fs *FS) BlockDevice(name string) (*BlockDevice, error) {
	path, err := fs.devicePath("block", name)
	if err != nil {
		return nil, err
	}
	return &BlockDevice{path: path}, nil
}

// NetworkInterface checks a class/net entry and selects its name, with the same
// validation and symlink behavior as BlockDevice. It does not pin an ifindex or
// a network namespace; the selected mount determines the visible interfaces.
func (fs *FS) NetworkInterface(name string) (*NetworkInterface, error) {
	path, err := fs.devicePath("net", name)
	if err != nil {
		return nil, err
	}
	return &NetworkInterface{path: path}, nil
}

func (fs *FS) devicePath(class, name string) (string, error) {
	if fs == nil || fs.root == "" || !validName(name) {
		return "", os.ErrInvalid
	}
	path := filepath.Join(fs.root, "class", class, name)
	if err := directory(path); err != nil {
		return "", err
	}
	return path, nil
}

func validName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\x00")
}

func directory(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return &os.PathError{Op: "open", Path: path, Err: os.ErrInvalid}
	}
	return nil
}

func directories(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}
