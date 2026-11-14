//go:build linux

package mount_test

import (
	"os"
	"path/filepath"

	"github.com/alexfalkowski/go-linux/mount"
)

func ExampleConfig() { //nolint:testableexamples // Executed by the isolated namespace fixture; requires mount privileges.
	// Run in a caller-provided isolated mount namespace with CAP_SYS_ADMIN.
	// This example is executed by the namespace integration fixture.
	config, err := mount.Open("tmpfs")
	if err != nil {
		panic(err)
	}

	defer config.Close()

	if err := config.SetString("size", "1m"); err != nil {
		panic(err) // errors.As(err, &mountError) exposes kernel diagnostics.
	}

	if err := config.SetFlag("rw"); err != nil {
		panic(err)
	}

	if err := config.Create(); err != nil {
		panic(err)
	}

	handle, err := config.Mount(mount.NoSUID | mount.NoDev)
	if err != nil {
		panic(err)
	}

	defer handle.Close()

	if err := handle.SetAttributes(mount.AttributeChange{Set: mount.NoExec}); err != nil {
		panic(err)
	}

	// The mount remains detached; closing it releases this reference.
}

func ExampleHandle_Attach() { //nolint:testableexamples // Executed by the isolated namespace fixture; requires mount privileges.
	// The caller has already isolated its mount namespace and made propagation
	// private. This example runs only in the namespace integration fixture.
	path, err := os.MkdirTemp("", "go-linux-mount-example-")
	if err != nil {
		panic(err)
	}

	defer os.RemoveAll(path)
	config, err := mount.Open("tmpfs")
	if err != nil {
		panic(err)
	}

	defer config.Close()

	if err := config.Create(); err != nil {
		panic(err)
	}

	handle, err := config.Mount(mount.NoSUID | mount.NoDev | mount.NoExec)
	if err != nil {
		panic(err)
	}

	defer handle.Close()

	if err := handle.Attach(nil, path); err != nil {
		panic(err)
	}

	// Attachment persists after handle.Close. Explicit unmount belongs to the
	// caller; run it before removing the temporary directory.
	defer func() {
		if err := mount.Unmount(path, mount.Detach); err != nil {
			panic(err)
		}
	}()

	if err := os.WriteFile(filepath.Join(path, "hello"), []byte("hello\n"), 0o600); err != nil {
		panic(err)
	}
}
