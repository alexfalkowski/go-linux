//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexfalkowski/go-linux/examples/top/internal/display"
	"github.com/alexfalkowski/go-linux/examples/top/internal/monitor"
	"github.com/alexfalkowski/go-linux/examples/top/internal/stats"
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
	collector, err := stats.Open(cfg.root)
	if err != nil {
		return err
	}
	table := display.New(os.Stdout, display.Options{Sort: cfg.order, Limit: cfg.limit})
	application := monitor.New(collector, table, monitor.Options{Interval: cfg.interval, Count: cfg.count})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return application.Run(ctx)
}
