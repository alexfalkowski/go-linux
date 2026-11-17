//go:build linux

package timerfd_test

import (
	"fmt"
	"time"

	"github.com/alexfalkowski/go-linux/epoll"
	"github.com/alexfalkowski/go-linux/timerfd"
)

func ExampleTimer() {
	timer, err := timerfd.New(timerfd.Monotonic)
	if err != nil {
		panic(err)
	}
	defer timer.Close()

	if err := timer.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		panic(err)
	}

	if _, err := timer.SetAfter(10*time.Millisecond, 0); err != nil {
		panic(err)
	}

	count, err := timer.ReadExpirations()
	if err != nil {
		panic(err)
	}

	fmt.Println("expirations:", count)

	// Output: expirations: 1
}

func ExampleTimer_SetAt() {
	timer, err := timerfd.New(timerfd.Boottime)
	if err != nil {
		panic(err)
	}
	defer timer.Close()

	deadline, err := timerfd.Now(timerfd.Boottime)
	if err != nil {
		panic(err)
	}

	deadline.Seconds++

	if _, err := timer.SetAt(deadline, 0, 0); err != nil {
		panic(err)
	}

	previous, err := timer.Disarm()
	if err != nil {
		panic(err)
	}

	fmt.Println("one-shot:", previous.Interval == 0)

	// Output: one-shot: true
}

func ExampleTimer_Register() {
	timer, err := timerfd.New(timerfd.Monotonic)
	if err != nil {
		panic(err)
	}
	defer timer.Close()

	poller, err := epoll.New()
	if err != nil {
		panic(err)
	}
	defer poller.Close()

	registration, err := timer.Register(poller, epoll.Readable)
	if err != nil {
		panic(err)
	}
	defer registration.Close()

	if err := timer.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		panic(err)
	}
	if _, err := timer.SetAfter(time.Millisecond, 0); err != nil {
		panic(err)
	}

	events := make([]epoll.Event, 1)
	n, err := poller.Wait(events, time.Second)
	if err != nil {
		panic(err)
	}

	fmt.Println("registered timer:", events[:n][0].Registration == registration)
	count, err := timer.ReadExpirations()
	if err != nil {
		panic(err)
	}
	fmt.Println("expirations:", count)

	// Output:
	// registered timer: true
	// expirations: 1
}
