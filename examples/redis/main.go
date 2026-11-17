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

	"github.com/alexfalkowski/go-linux/examples/redis/internal/client"
	"github.com/alexfalkowski/go-linux/examples/redis/internal/server"
	"github.com/alexfalkowski/go-linux/examples/redis/internal/store"
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
	if cfg.client {
		application := client.New(cfg.address, os.Stdout)
		return application.Run(ctx, cfg.arguments)
	}
	cache, err := store.Open(cfg.snapshot)
	if err != nil {
		return err
	}
	application, err := server.New(ctx, cfg.address, cache, log.New(os.Stderr, "", 0))
	if err != nil {
		return err
	}
	defer application.Close()
	return application.Serve(ctx)
}
