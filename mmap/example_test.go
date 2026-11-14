//go:build linux

package mmap_test

import (
	"fmt"
	"os"

	"github.com/alexfalkowski/go-linux/mmap"
)

func ExampleAnonymous() {
	region, err := mmap.Anonymous(4096, mmap.Options{Protection: mmap.Read | mmap.Write})
	if err != nil {
		panic(err)
	}
	defer region.Close()

	copy(region.Bytes(), "hello")
	if err := region.Protect(mmap.Read); err != nil {
		panic(err)
	}
	fmt.Println(string(region.Bytes()[:5]))
	// Output: hello
}

func ExampleMap() {
	file, err := os.CreateTemp("", "mmap-example-")
	if err != nil {
		panic(err)
	}
	defer os.Remove(file.Name())
	defer file.Close()

	if _, err := file.WriteString("hello"); err != nil {
		panic(err)
	}
	region, err := mmap.Map(file, 0, 5, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read | mmap.Write})
	if err != nil {
		panic(err)
	}
	defer region.Close()

	copy(region.Bytes(), "world")
	contents := make([]byte, 5)
	if _, err := file.ReadAt(contents, 0); err != nil {
		panic(err)
	}
	// Stop using borrowed views before closing region. This example verifies
	// visibility through ordinary file I/O, not disk durability.
	fmt.Println(string(contents))
	// Output: world
}
