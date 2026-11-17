//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexfalkowski/go-linux/examples/internal/process"
	"github.com/alexfalkowski/go-linux/examples/run/internal/example"
	"github.com/alexfalkowski/go-linux/examples/run/internal/workspace"
)

func main() {
	if err := run(); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	selected, err := example.Parse()
	if err != nil {
		return err
	}
	grace, err := selected.ShutdownGrace()
	if err != nil {
		return err
	}
	work, err := workspace.New()
	if err != nil {
		return err
	}
	defer work.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	command, err := work.Command(ctx, selected)
	if err != nil {
		return err
	}
	child, err := process.Start(command)
	if err != nil {
		return err
	}
	return child.Wait(ctx, grace)
}
