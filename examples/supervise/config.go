//go:build linux

package main

import (
	"errors"
	"flag"
	"time"

	"github.com/alexfalkowski/go-linux/examples/supervise/internal/supervisor"
)

func configure() (supervisor.Options, error) {
	var cfg supervisor.Options
	flag.IntVar(&cfg.Restarts, "restarts", 3, "maximum restarts after a failed exit")
	flag.DurationVar(&cfg.Backoff, "backoff", time.Second, "initial restart delay, measured across suspend")
	flag.DurationVar(&cfg.MaxBackoff, "max-backoff", 10*time.Second, "maximum restart delay")
	flag.DurationVar(&cfg.Grace, "grace", 2*time.Second, "TERM grace period before KILL")
	flag.Parse()
	validBackoff := cfg.Backoff > 0 && cfg.MaxBackoff >= cfg.Backoff
	if cfg.Restarts < 0 || cfg.Grace <= 0 || !validBackoff {
		return cfg, errors.New("use nonnegative restarts, positive grace/backoff, and max-backoff >= backoff")
	}
	cfg.Arguments = flag.Args()
	if len(cfg.Arguments) == 0 {
		cfg.Arguments = []string{"/bin/sleep", "30"}
	}
	return cfg, nil
}
