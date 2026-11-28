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

func ExampleRegion_Sync() {
	file, err := os.CreateTemp("", "mmap-index-")
	if err != nil {
		panic(err)
	}
	defer os.Remove(file.Name())
	defer file.Close()

	if err := file.Truncate(8); err != nil {
		panic(err)
	}
	region, err := mmap.Map(file, 0, 8, mmap.Options{Mode: mmap.Shared, Protection: mmap.Read | mmap.Write})
	if err != nil {
		panic(err)
	}
	defer region.Close()

	copy(region.Bytes(), "index-v1")
	if err := region.Sync(mmap.SyncOptions{}); err != nil {
		panic(err)
	}

	header := make([]byte, 8)
	if _, err := file.ReadAt(header, 0); err != nil {
		panic(err)
	}
	fmt.Println(string(header))

	// Output: index-v1
}

func ExampleRegion_Residency() {
	pageSize := os.Getpagesize()
	region, err := mmap.Anonymous(pageSize+1, mmap.Options{Protection: mmap.Read | mmap.Write})
	if err != nil {
		panic(err)
	}
	defer region.Close()

	snapshot, err := region.Residency()
	if err != nil {
		panic(err)
	}
	fmt.Printf("pages: %d\n", len(snapshot.Resident))

	// Output: pages: 2
}

func ExampleRegion_Resize() {
	pageSize := os.Getpagesize()
	region, err := mmap.Anonymous(pageSize, mmap.Options{Protection: mmap.Read | mmap.Write})
	if err != nil {
		panic(err)
	}
	defer region.Close()

	copy(region.Bytes(), "index-v1")

	// Pause all readers and writers, then reacquire the view after Resize.
	if err := region.Resize(2 * pageSize); err != nil {
		panic(err)
	}

	index := region.Bytes()
	copy(index[pageSize:], "new entry")
	fmt.Println(string(index[:8]), string(index[pageSize:pageSize+9]))

	// Output: index-v1 new entry
}

func ExampleRegion_Advise() {
	region, err := mmap.Anonymous(os.Getpagesize(), mmap.Options{Protection: mmap.Read | mmap.Write})
	if err != nil {
		panic(err)
	}
	defer region.Close()

	if err := region.Advise(mmap.PopulateWrite); err != nil {
		panic(err)
	}
	copy(region.Bytes(), "ready")
	fmt.Println(string(region.Bytes()[:5]))

	// Output: ready
}
