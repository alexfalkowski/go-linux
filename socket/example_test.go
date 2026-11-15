//go:build linux

package socket_test

import (
	"fmt"
	"os"
	"time"

	"github.com/alexfalkowski/go-linux/socket"
)

func ExamplePair() {
	sender, receiver, err := socket.Pair(socket.Datagram)
	if err != nil {
		panic(err)
	}
	defer sender.Close()
	defer receiver.Close()

	if err := receiver.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		panic(err)
	}

	if _, err := sender.Send([]byte("one record"), nil); err != nil {
		panic(err)
	}

	buffer := make([]byte, 64)
	message, err := receiver.Receive(buffer, 0)
	if err != nil {
		panic(err)
	}

	fmt.Println(string(buffer[:message.N]), message.Flags&socket.Truncated != 0)

	// Output: one record false
}

func ExampleConn_Send() {
	sender, receiver, err := socket.Pair(socket.SeqPacket)
	if err != nil {
		panic(err)
	}
	defer sender.Close()
	defer receiver.Close()

	if err := receiver.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		panic(err)
	}

	file, err := os.CreateTemp("", "socket-example-*")
	if err != nil {
		panic(err)
	}
	defer os.Remove(file.Name())
	defer file.Close()

	if _, err := file.WriteAt([]byte("shared file"), 0); err != nil {
		panic(err)
	}

	if _, err := sender.Send([]byte("file"), nil, file); err != nil {
		panic(err)
	}

	message, err := receiver.Receive(make([]byte, 32), 1)
	if err != nil {
		panic(err) // No received files need closing on error.
	}

	for _, received := range message.Files {
		defer received.Close()
	}

	buffer := make([]byte, 11)
	if _, err := message.Files[0].ReadAt(buffer, 0); err != nil {
		panic(err)
	}

	fmt.Println(string(buffer))

	// Output: shared file
}

func ExampleOpenNetlink() {
	conn, err := socket.OpenNetlink(socket.Route, socket.NetlinkAddress{})
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	if err := conn.SetExtendedAck(true); err != nil {
		panic(err)
	}

	address, err := conn.LocalAddress()
	if err != nil {
		panic(err)
	}

	fmt.Println(address.Network(), address.(socket.NetlinkAddress).PortID != 0)

	// Output: netlink true
}
