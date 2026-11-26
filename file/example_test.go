//go:build linux

package file_test

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/alexfalkowski/go-linux/file"
)

func ExampleOpen() {
	path, err := os.MkdirTemp("", "file-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(path)

	dir, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer dir.Close()

	if err := os.WriteFile(filepath.Join(path, "config"), []byte("hello"), 0o600); err != nil {
		panic(err)
	}

	opened, err := file.Open(dir, "config", file.OpenOptions{
		Resolve: file.Beneath | file.NoSymlinks | file.NoXDev,
	})
	if err != nil {
		panic(err)
	}
	defer opened.Close()

	contents, err := io.ReadAll(opened)
	if err != nil {
		panic(err)
	}

	metadata, err := file.Stat(opened, file.MaskSize|file.MaskBirthTime)
	if err != nil {
		panic(err)
	}

	if metadata.Mask&file.MaskSize == 0 {
		panic("size unavailable")
	}

	fmt.Println(string(contents), metadata.Size)

	// Output: hello 5
}

func ExampleRename() {
	path, err := os.MkdirTemp("", "rename-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(path)

	dir, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer dir.Close()

	if err := os.WriteFile(filepath.Join(path, "ready"), []byte("new"), 0o600); err != nil {
		panic(err)
	}

	if err := os.WriteFile(filepath.Join(path, "active"), []byte("old"), 0o600); err != nil {
		panic(err)
	}

	if err := file.Rename(dir, "ready", dir, "active", file.Exchange); err != nil {
		panic(err)
	}

	active, err := os.ReadFile(filepath.Join(path, "active"))
	if err != nil {
		panic(err)
	}

	previous, err := os.ReadFile(filepath.Join(path, "ready"))
	if err != nil {
		panic(err)
	}

	fmt.Println(string(active), string(previous))

	// Output: new old
}

func ExampleFallocate() {
	path, err := os.MkdirTemp("", "allocation-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(path)

	segment, err := os.Create(filepath.Join(path, "segment"))
	if err != nil {
		panic(err)
	}
	defer segment.Close()

	// Reserve space for the next log segment before appending any records.
	if err := file.Fallocate(segment, file.Allocate|file.KeepSize, 0, 64*1024); err != nil {
		panic(err)
	}
	if _, err := segment.WriteString("record\n"); err != nil {
		panic(err)
	}

	info, err := segment.Stat()
	if err != nil {
		panic(err)
	}
	fmt.Println(info.Size())

	// Output: 7
}

func ExampleFallocate_unshareRange() {
	segment, err := os.CreateTemp("", "working-copy-*.db")
	if err != nil {
		panic(err)
	}
	defer os.Remove(segment.Name())
	defer segment.Close()
	if _, err := segment.WriteString("record\n"); err != nil {
		panic(err)
	}

	// For a reflinked working copy, make this segment private before writing.
	// This caller treats the reservation as optional on unsupported filesystems.
	const segmentSize = 64 * 1024
	if err := file.Fallocate(segment, file.UnshareRange|file.KeepSize, 0, segmentSize); err != nil && !errors.Is(err, errors.ErrUnsupported) {
		panic(err)
	}

	contents, err := os.ReadFile(segment.Name())
	if err != nil {
		panic(err)
	}
	fmt.Print(string(contents))

	// Output: record
}
