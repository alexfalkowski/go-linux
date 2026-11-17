//go:build linux

package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"

	"github.com/alexfalkowski/go-linux/examples/container/internal/config"
)

func configure() (config.Config, error) {
	var cfg config.Config
	flag.StringVar(&cfg.Root, "rootfs", "", "trusted root filesystem with a real /tmp directory")
	flag.StringVar(&cfg.Delegation, "cgroup", "", "optional caller-owned cgroup v2 delegation with memory/pids enabled")
	flag.Int64Var(&cfg.Memory, "memory", 64<<20, "memory.max in bytes when -cgroup is supplied")
	flag.Int64Var(&cfg.Tasks, "pids", 32, "pids.max when -cgroup is supplied")
	flag.BoolVar(&cfg.Initialize, "init", false, "internal namespace helper; use the ordinary command")
	flag.StringVar(&cfg.Stage, "stage", "", "internal private mount target")
	flag.StringVar(&cfg.ParentMount, "parent-mount", "", "internal parent's mount namespace identity")
	flag.Parse()
	if cfg.Root == "" || cfg.Memory <= 0 || cfg.Tasks <= 0 {
		return cfg, errors.New("supply -rootfs and positive memory/pids limits; make example-container supplies a demo rootfs")
	}
	absolute, err := filepath.Abs(cfg.Root)
	if err != nil {
		return cfg, err
	}
	cfg.Root = absolute
	info, err := os.Lstat(filepath.Join(cfg.Root, "tmp"))
	if err != nil {
		return cfg, err
	}
	if !info.IsDir() {
		return cfg, errors.New("rootfs/tmp must be a real directory, not a symlink")
	}
	cfg.Arguments = flag.Args()
	if len(cfg.Arguments) == 0 {
		cfg.Arguments = []string{"/bin/hello"}
	}
	if !filepath.IsAbs(cfg.Arguments[0]) {
		return cfg, errors.New("use an absolute command path inside the rootfs")
	}
	return cfg, nil
}
