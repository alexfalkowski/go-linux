//go:build linux

package sysfs

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func readAttribute(path, name string) ([]byte, error) {
	validPath := name != "." && fs.ValidPath(name) && !strings.ContainsRune(name, 0)
	if path == "" || !validPath {
		return nil, os.ErrInvalid
	}

	path = filepath.Join(path, name)
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, &os.PathError{Op: "read", Path: path, Err: os.ErrInvalid}
	}

	const limit = 1 << 20
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, &os.PathError{Op: "read", Path: path, Err: ErrLimit}
	}
	return data, nil
}

func attributes(path string) ([]string, error) {
	if path == "" {
		return nil, os.ErrInvalid
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

func unsignedAttribute(path, name string, base, bits int) (uint64, error) {
	data, err := readAttribute(path, name)
	if err != nil {
		return 0, err
	}

	value, err := strconv.ParseUint(strings.TrimSpace(string(data)), base, bits)
	if err != nil {
		return 0, parseError(path, name)
	}
	return value, nil
}

func parseError(path, name string) error {
	return &os.PathError{Op: "parse", Path: filepath.Join(path, name), Err: ErrParse}
}
