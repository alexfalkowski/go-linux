//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexfalkowski/go-linux/examples/watch/internal/command"
	"github.com/alexfalkowski/go-linux/examples/watch/internal/watcher"
)

func main() {
	if err := run(); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := configure()
	if err != nil {
		return err
	}
	logger := log.New(os.Stderr, "", 0)
	child := command.New(command.Options{Directory: cfg.directory, Arguments: cfg.arguments, Grace: cfg.grace}, logger)
	application, err := watcher.New(watcher.Options{Directory: cfg.directory, Debounce: cfg.debounce, Initial: cfg.initial}, child, logger)
	if err != nil {
		return err
	}
	defer application.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return application.Run(ctx)
}
