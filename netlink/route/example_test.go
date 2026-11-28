//go:build linux

package route_test

import (
	"fmt"
	"time"

	"github.com/alexfalkowski/go-linux/netlink"
	"github.com/alexfalkowski/go-linux/netlink/route"
)

func ExampleConn_Links() {
	conn, err := route.Open(netlink.Options{Validation: netlink.StrictValidation})
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		panic(err)
	}
	links, err := conn.Links()
	if err != nil {
		panic(err)
	}
	for _, link := range links {
		if link.Flags&route.Loopback != 0 {
			fmt.Println(link.Name)
		}
	}

	// Output: lo
}

func ExampleConn_Routes() {
	conn, err := route.Open(netlink.Options{})
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		panic(err)
	}
	routes, err := conn.Routes(route.IPv4)
	if err != nil {
		panic(err)
	}
	for _, entry := range routes {
		if !entry.Destination.Addr().Is4() {
			panic("unexpected address family")
		}
	}
	fmt.Println("IPv4 route dump complete")

	// Output: IPv4 route dump complete
}
