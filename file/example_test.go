//go:build linux

package file_test

import (
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
