//go:build linux

package main

import (
	"errors"
	"flag"
)

type config struct {
	address   string
	snapshot  string
	arguments []string
	client    bool
}

func configure() (config, error) {
	var cfg config
	flag.StringVar(&cfg.address, "addr", "127.0.0.1:6380", "TCP listen address, or server address in client mode")
	flag.StringVar(&cfg.snapshot, "snapshot", "", "trusted snapshot path; SAVE is disabled when empty")
	flag.BoolVar(&cfg.client, "client", false, "send one command and print its response")
	flag.Parse()
	cfg.arguments = flag.Args()
	if !cfg.client && len(cfg.arguments) != 0 {
		return cfg, errors.New("server mode takes flags only; use -client -- COMMAND arguments")
	}
	return cfg, nil
}
