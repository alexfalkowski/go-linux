//go:build linux

package procfs_test

import (
	"fmt"
	"os"

	"github.com/alexfalkowski/go-linux/procfs"
)

func ExampleOpen() {
	fs, err := procfs.Open("/proc")
	if err != nil {
		panic(err)
	}

	process, err := fs.Self()
	if err != nil {
		panic(err)
	}

	fmt.Println(process.PID() == os.Getpid())

	// Output: true
}

func ExampleProcess_Stat() {
	fs, err := procfs.Open("/proc")
	if err != nil {
		panic(err)
	}
	process, err := fs.Self()
	if err != nil {
		panic(err)
	}

	stat, err := process.Stat()
	if err != nil {
		panic(err)
	}

	fmt.Println(stat.Threads > 0, stat.VirtualMemory > 0)

	// Output: true true
}

func ExampleFS_Memory() {
	fs, err := procfs.Open("/proc")
	if err != nil {
		panic(err)
	}

	memory, err := fs.Memory()
	if err != nil {
		panic(err)
	}

	fmt.Println(memory.Total > 0, memory.Free <= memory.Total)

	// Output: true true
}
