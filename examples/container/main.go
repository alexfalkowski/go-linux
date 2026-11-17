//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexfalkowski/go-linux/examples/container/internal/container"
	"github.com/alexfalkowski/go-linux/examples/container/internal/runner"
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cfg.Initialize {
		application := container.NewInitializer(cfg)
		return application.Run(ctx)
	}
	application := runner.New(cfg)
	return application.Run(ctx)
}
