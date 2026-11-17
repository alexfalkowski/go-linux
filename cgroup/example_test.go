//go:build linux

package cgroup_test

import (
	"fmt"
	"log"
	"time"

	"github.com/alexfalkowski/go-linux/cgroup"
)

func ExampleCPUBandwidth() {
	bandwidth := cgroup.CPUBandwidth{Quota: 50 * time.Millisecond, Period: 100 * time.Millisecond}
	text, err := bandwidth.MarshalText()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(string(text))
	// Output: 50000 100000
}

func ExampleParseEvents() {
	event, err := cgroup.ParseEvents([]byte("populated 1\nfrozen 0\n"))
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(event.Populated, event.Frozen)
	// Output: true false
}

func ExampleOpen() {
	group, err := cgroup.Open("/sys/fs/cgroup")
	if err != nil {
		log.Fatal(err)
	}
	defer group.Close()

	_, err = group.Controllers()
	if err != nil {
		log.Print(err)
		return
	}

	fmt.Println("opened cgroup v2 hierarchy")
	// Output: opened cgroup v2 hierarchy
}
