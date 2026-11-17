//go:build linux

package eventfd_test

import (
	"fmt"
	"time"

	"github.com/alexfalkowski/go-linux/epoll"
	"github.com/alexfalkowski/go-linux/eventfd"
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

func ExampleCounter_ReadValue() {
	counter, err := eventfd.New(2, eventfd.Semaphore)
	if err != nil {
		panic(err)
	}

	defer counter.Close()

	if err := counter.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		panic(err)
	}

	for range 2 {
		value, err := counter.ReadValue()
		if err != nil {
			panic(err)
		}

		fmt.Println("permit:", value)
	}

	// Output:
	// permit: 1
	// permit: 1
}

func ExampleCounter_Register() {
	counter, err := eventfd.New(0, eventfd.Default)
	if err != nil {
		panic(err)
	}
	defer counter.Close()

	poller, err := epoll.New()
	if err != nil {
		panic(err)
	}
	defer poller.Close()

	registration, err := counter.Register(poller, epoll.Readable)
	if err != nil {
		panic(err)
	}
	defer registration.Close()

	if err := counter.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		panic(err)
	}
	if err := counter.Add(7); err != nil {
		panic(err)
	}

	events := make([]epoll.Event, 1)
	n, err := poller.Wait(events, time.Second)
	if err != nil {
		panic(err)
	}

	fmt.Println("registered counter:", events[:n][0].Registration == registration)
	value, err := counter.ReadValue()
	if err != nil {
		panic(err)
	}
	fmt.Println("completed:", value)

	// Output:
	// registered counter: true
	// completed: 7
}
