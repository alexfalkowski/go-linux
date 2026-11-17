//go:build linux

package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/alexfalkowski/go-linux/capability"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread() // Inspection only.
	sets, err := capability.Current()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "hello from pid=%d uid=%d; effective capabilities=%#x\n", os.Getpid(), os.Getuid(), sets.Effective)
	file, err := os.CreateTemp("/tmp", "hello-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err := file.WriteString("scratch space belongs to this run\n"); err != nil {
		return err
	}
	data, err := os.ReadFile(file.Name())
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "%s: %s", file.Name(), data)
	return nil
}
