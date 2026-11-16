//go:build linux

package netlink_test

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/alexfalkowski/go-linux/netlink"
	"github.com/alexfalkowski/go-linux/socket"
)

func ExampleConn_LookupFamily() {
	conn, err := netlink.Open(socket.Generic, netlink.Options{})
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		panic(err)
	}

	family, err := conn.LookupFamily("nlctrl")
	if err != nil {
		panic(err)
	}
	fmt.Println(family.Name, family.ID)

	// Output: nlctrl 16
}

func ExampleConn_Dump() {
	conn, err := netlink.Open(socket.Generic, netlink.Options{})
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		panic(err)
	}

	data, err := (netlink.GenericMessage{Command: netlink.GetFamilyCommand, Version: 2}).MarshalBinary()
	if err != nil {
		panic(err)
	}
	response, err := conn.Dump(netlink.Message{Header: netlink.Header{Type: netlink.ControllerID}, Data: data})
	if err != nil {
		panic(err) // response.Messages still holds any complete replies received.
	}
	fmt.Println(response.Complete, response.Done.Code)

	// Output: true errno 0
}

func ExampleUnmarshalAttributes() {
	data, err := netlink.MarshalAttributes([]netlink.Attribute{
		{Type: 1, Data: []byte("name\x00")},
		{Type: 2 | netlink.NetworkByteOrder, Data: binary.BigEndian.AppendUint32(nil, 42)},
	})
	if err != nil {
		panic(err)
	}

	attributes, err := netlink.UnmarshalAttributes(data)
	if err != nil {
		panic(err)
	}
	name, err := attributes[0].String()
	if err != nil {
		panic(err)
	}
	value, err := attributes[1].Uint32()
	if err != nil {
		panic(err)
	}
	fmt.Println(name, value)

	// Output: name 42
}
