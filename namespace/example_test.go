//go:build linux

package namespace_test

import (
	"fmt"

	"github.com/alexfalkowski/go-linux/namespace"
)

func ExampleCurrent() {
	handle, err := namespace.Current(namespace.Network)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer handle.Close()

	kind, err := handle.Type()
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(kind == namespace.Network)
	// Output: true
}

func ExampleHandle_Equal() {
	first, err := namespace.Current(namespace.User)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer first.Close()
	second, err := namespace.Open("/proc/thread-self/ns/user")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer second.Close()

	equal, err := first.Equal(second)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(equal)
	// Output: true
}
