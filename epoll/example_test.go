//go:build linux

package epoll_test

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/alexfalkowski/go-linux/epoll"
)

func ExamplePoller() {
	reader, writer, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	defer reader.Close()
	defer writer.Close()

	poller, err := epoll.New()
	if err != nil {
		panic(err)
	}
	defer poller.Close()

	registration, err := poller.Add(reader, epoll.Readable|epoll.OneShot)
	if err != nil {
		panic(err)
	}
	defer registration.Close()

	if err := reader.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		panic(err)
	}
	if _, err := io.WriteString(writer, "hello"); err != nil {
		panic(err)
	}

	events := make([]epoll.Event, 4)
	n, err := poller.Wait(events, time.Second)
	if err != nil {
		panic(err)
	}
	fmt.Println("registered reader:", events[:n][0].Registration == registration)
	var message [5]byte
	if _, err := io.ReadFull(reader, message[:]); err != nil {
		panic(err)
	}
	fmt.Println(string(message[:]))
	if err := registration.Modify(epoll.Readable | epoll.OneShot); err != nil {
		panic(err)
	}
	// Output:
	// registered reader: true
	// hello
}
