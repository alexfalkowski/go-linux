//go:build linux

package eventfd_test

import (
	"fmt"
	"time"

	"github.com/alexfalkowski/go-linux/eventfd"
	"golang.org/x/sys/unix"
)

func ExampleCounter() {
	counter, err := eventfd.New(0, eventfd.Default)
	if err != nil {
		panic(err)
	}
	defer counter.Close()

	if err := counter.SetDeadline(time.Now().Add(time.Second)); err != nil {
		panic(err)
	}
	if err := counter.Add(3); err != nil {
		panic(err)
	}
	if err := counter.Add(4); err != nil {
		panic(err)
	}
	completed, err := counter.ReadValue()
	if err != nil {
		panic(err)
	}
	fmt.Println("completed:", completed)
	// Output: completed: 7
}

func ExampleCounter_SyscallConn() {
	counter, err := eventfd.New(0, eventfd.Default)
	if err != nil {
		panic(err)
	}
	defer counter.Close()

	conn, err := counter.SyscallConn()
	if err != nil {
		panic(err)
	}

	var flags int
	var queryErr error
	err = conn.Control(func(fd uintptr) {
		flags, queryErr = unix.FcntlInt(fd, unix.F_GETFD, 0)
	})
	if err != nil {
		panic(err)
	}
	if queryErr != nil {
		panic(queryErr)
	}
	fmt.Println("close-on-exec:", flags&unix.FD_CLOEXEC != 0)
	// Output: close-on-exec: true
}
