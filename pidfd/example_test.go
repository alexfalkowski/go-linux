//go:build linux

package pidfd_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/alexfalkowski/go-linux/pidfd"
	"golang.org/x/sys/unix"
)

func ExampleProcess() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/cat")
	input, err := command.StdinPipe()
	if err != nil {
		panic(err)
	}
	if err := command.Start(); err != nil {
		panic(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()

	process, err := pidfd.Open(command.Process.Pid)
	if err != nil {
		panic(err)
	}
	defer process.Close()
	if err := input.Close(); err != nil {
		panic(err)
	}
	if err := process.WaitReady(); err != nil {
		panic(err)
	}
	// Observation leaves child status and os/exec cleanup to its owner.
	if err := command.Wait(); err != nil {
		panic(err)
	}
	fmt.Println("child reaped:", command.ProcessState.Success())
	// Output: child reaped: true
}

func ExampleAdopt() {
	// WithHandle guarantees the borrowed pidfd's lifetime during its callback.
	// A close-on-exec duplicate gives this package independent ownership.
	self, err := os.FindProcess(os.Getpid())
	if err != nil {
		panic(err)
	}
	defer func() { _ = self.Release() }()
	var fd int
	var dupErr error
	err = self.WithHandle(func(handle uintptr) {
		fd, dupErr = unix.FcntlInt(handle, unix.F_DUPFD_CLOEXEC, 0)
	})
	if err != nil {
		panic(err)
	}
	if dupErr != nil {
		panic(dupErr)
	}
	process, err := pidfd.Adopt(os.NewFile(uintptr(fd), "self-pidfd"))
	if err != nil {
		panic(err)
	}
	defer process.Close()
	if err := process.Signal(0); err != nil {
		panic(err)
	}
	fmt.Println("process exists")
	// Output: process exists
}
