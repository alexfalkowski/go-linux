//go:build linux

package sysfs_test

import (
	"fmt"
	"net"

	"github.com/alexfalkowski/go-linux/sysfs"
)

func ExampleOpen() {
	fs, err := sysfs.Open("/sys")
	if err != nil {
		panic(err)
	}

	names, err := fs.NetworkInterfaces()
	if err != nil {
		panic(err)
	}

	fmt.Println(len(names) > 0)

	// Output: true
}

func ExampleNetworkInterface_Info() {
	fs, err := sysfs.Open("/sys")
	if err != nil {
		panic(err)
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		panic(err)
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback == 0 {
			continue
		}

		device, err := fs.NetworkInterface(iface.Name)
		if err != nil {
			panic(err)
		}
		info, err := device.Info()
		if err != nil {
			panic(err)
		}

		fmt.Println(info.Index > 0)
		return
	}
	panic("network namespace has no loopback interface")

	// Output: true
}
