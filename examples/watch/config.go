//go:build linux

package main

import (
	"errors"
	"flag"
	"time"
)

type config struct {
	directory string
	arguments []string
	debounce  time.Duration
	grace     time.Duration
	initial   bool
}

func configure() (config, error) {
	var cfg config
	flag.StringVar(&cfg.directory, "dir", ".", "directory to watch (immediate children only)")
	flag.DurationVar(&cfg.debounce, "debounce", 150*time.Millisecond, "quiet period before running the command")
	flag.DurationVar(&cfg.grace, "grace", time.Second, "child TERM grace period")
	flag.BoolVar(&cfg.initial, "initial", true, "run once after installing the watch")
	flag.Parse()
	if cfg.debounce <= 0 || cfg.grace <= 0 {
		return cfg, errors.New("debounce and grace must be positive")
	}
	cfg.arguments = flag.Args()
	if len(cfg.arguments) == 0 {
		cfg.arguments = []string{"/bin/echo", "changed"}
	}
	return cfg, nil
}
