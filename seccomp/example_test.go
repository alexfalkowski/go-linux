//go:build linux

package seccomp_test

import (
	"fmt"

	"github.com/alexfalkowski/go-linux/seccomp"
)

func ExampleNew() {
	architecture, err := seccomp.NativeArchitecture()
	if err != nil {
		fmt.Println(err)
		return
	}
	// Construction is safe in an ordinary caller. Loading is irreversible and
	// belongs on a locked goroutine in a disposable child during development.
	filter, err := seccomp.New(architecture, []seccomp.Instruction{
		{Code: seccomp.Return, K: uint32(seccomp.Allow)},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(filter != nil)
	// Output: true
}

func ExampleActionAvailable() {
	available, err := seccomp.ActionAvailable(seccomp.KillProcess)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(available)
	// Output: true
}
