//go:build linux

package memfd_test

import (
	"fmt"
	"io"
	"os"

	"github.com/alexfalkowski/go-linux/memfd"
	"github.com/alexfalkowski/go-linux/mmap"
)

func ExampleFile_AddSeals() {
	file, err := memfd.New("message", memfd.Options{})
	if err != nil {
		panic(err)
	}
	defer file.Close()

	if _, err := file.WriteString("hello"); err != nil {
		panic(err)
	}

	// Freeze both contents and size before sharing the file with another owner.
	if err := file.AddSeals(memfd.SealWrite | memfd.SealGrow | memfd.SealShrink | memfd.SealSeal); err != nil {
		panic(err)
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		panic(err)
	}

	contents, err := io.ReadAll(file)
	if err != nil {
		panic(err)
	}

	fmt.Println(string(contents))

	// Output: hello
}

func ExampleFile_Map() {
	file, err := memfd.New("shared-page", memfd.Options{})
	if err != nil {
		panic(err)
	}

	defer file.Close()

	if err := file.Truncate(int64(os.Getpagesize())); err != nil {
		panic(err)
	}

	region, err := file.Map(0, os.Getpagesize(), mmap.Options{
		Mode: mmap.Shared, Protection: mmap.Read | mmap.Write,
	})
	if err != nil {
		panic(err)
	}

	defer region.Close()

	copy(region.Bytes(), "hello")
	contents := make([]byte, 5)
	if _, err := file.ReadAt(contents, 0); err != nil {
		panic(err)
	}

	fmt.Println(string(contents))

	// Output: hello
}
