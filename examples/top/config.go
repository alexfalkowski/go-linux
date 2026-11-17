//go:build linux

package main

import (
	"errors"
	"flag"
	"time"
)

type config struct {
	root     string
	order    string
	interval time.Duration
	limit    int
	count    int
	once     bool
}

func configure() (config, error) {
	var cfg config
	flag.StringVar(&cfg.root, "proc", "/proc", "trusted procfs root")
	flag.DurationVar(&cfg.interval, "interval", time.Second, "sampling interval")
	flag.IntVar(&cfg.limit, "limit", 15, "maximum displayed processes")
	flag.StringVar(&cfg.order, "sort", "cpu", "sort by cpu, memory, or pid")
	flag.IntVar(&cfg.count, "count", 0, "number of frames; zero runs until interrupted")
	flag.BoolVar(&cfg.once, "once", false, "print one frame after sampling")
	flag.Parse()
	validOrder := cfg.order == "cpu" || cfg.order == "memory" || cfg.order == "pid"
	validCounts := cfg.limit > 0 && cfg.count >= 0
	if flag.NArg() != 0 || cfg.interval <= 0 || !validCounts || !validOrder {
		return cfg, errors.New("use positive interval/limit, nonnegative count, and -sort cpu|memory|pid; see -help")
	}
	if cfg.once {
		cfg.count = 1
	}
	return cfg, nil
}
