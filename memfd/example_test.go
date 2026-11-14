//go:build linux

package memfd_test

import (
	"fmt"
	"io"

	"github.com/alexfalkowski/go-linux/memfd"
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
