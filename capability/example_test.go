//go:build linux

package capability_test

import (
	"fmt"
	"runtime"

	"github.com/alexfalkowski/go-linux/capability"
)

func ExampleCurrent() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	sets, err := capability.Current()
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(sets.Effective&^sets.Permitted == 0)
	// Output: true
}

func ExampleSet() {
	services := capability.NetBindService | capability.NetRaw
	services &^= capability.NetRaw

	fmt.Println(services == capability.NetBindService)
	// Output: true
}
