//go:build linux

package container

import (
	"errors"
	"path/filepath"

	"github.com/alexfalkowski/go-linux/mount"
)

func prepareRoot(root, stage string) (*mount.Handle, error) {
	handle, err := mount.Clone(nil, root, 0)
	if err != nil {
		return nil, err
	}
	if err := handle.SetAttributes(mount.AttributeChange{Set: mount.ReadOnly | mount.NoSUID | mount.NoDev}); err != nil {
		return nil, errors.Join(err, handle.Close())
	}
	if err := handle.Attach(nil, stage); err != nil {
		return nil, errors.Join(err, handle.Close())
	}
	if err := mountScratch(filepath.Join(stage, "tmp")); err != nil {
		return nil, errors.Join(err, handle.Close())
	}
	return handle, nil
}

func mountScratch(target string) error {
	config, err := mount.Open("tmpfs")
	if err != nil {
		return err
	}
	defer config.Close()
	if err := config.SetString("size", "16m"); err != nil {
		return err
	}
	if err := config.SetString("mode", "1777"); err != nil {
		return err
	}
	if err := config.Create(); err != nil {
		return err
	}
	handle, err := config.Mount(mount.NoSUID | mount.NoDev)
	if err != nil {
		return err
	}
	defer handle.Close()
	return handle.Attach(nil, target)
}
