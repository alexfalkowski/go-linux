//go:build linux

package container

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/alexfalkowski/go-linux/examples/container/internal/config"
	"github.com/alexfalkowski/go-linux/examples/container/internal/startup"
	"github.com/alexfalkowski/go-linux/examples/internal/process"
	"github.com/alexfalkowski/go-linux/namespace"
)

// NewInitializer retains validated helper settings without changing thread state.
func NewInitializer(cfg config.Config) *Initializer { return &Initializer{config: cfg} }

// Initializer configures the child's private filesystem and inherited policy.
// Namespace and credential changes stay on its dedicated locked OS thread.
type Initializer struct {
	config config.Config
}

// Run configures the locked helper thread and joins the payload before returning.
func (initializer *Initializer) Run(ctx context.Context) error {
	cfg := initializer.config
	// Never return a thread with changed namespaces or credentials to Go's pool.
	runtime.LockOSThread()
	if err := verifyPrivateMount(cfg.ParentMount); err != nil {
		return err
	}
	if cfg.Stage == "" || !filepath.IsAbs(cfg.Stage) {
		return errors.New("missing absolute private mount target")
	}
	if err := startup.Wait(os.Stdin); err != nil {
		return err
	}
	rootMount, err := prepareRoot(cfg.Root, cfg.Stage)
	if err != nil {
		return err
	}
	defer rootMount.Close()
	// Only the payload becomes PID 1; its exit destroys its PID namespace and
	// terminates its descendants. This helper stays outside that PID namespace.
	if err := namespace.Unshare(namespace.PID); err != nil {
		return err
	}
	policy := securityPolicy{root: cfg.Stage}
	if err := policy.apply(); err != nil {
		return err
	}
	//nolint:gosec // The operator chooses an absolute argv path inside the supplied rootfs.
	command := exec.CommandContext(ctx, cfg.Arguments[0], cfg.Arguments[1:]...)
	command.Cancel = nil // The pidfd owner performs bounded shutdown.
	command.Env = []string{"PATH=/bin:/usr/bin", "HOME=/tmp", "TMPDIR=/tmp"}
	command.Stdin = os.Stdin // The completed startup pipe supplies EOF without opening /dev/null.
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	command.Dir = "/"
	command.SysProcAttr = &syscall.SysProcAttr{Chroot: cfg.Stage, Pdeathsig: syscall.SIGKILL}
	child, err := process.Start(command)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "private user/mount/network/UTS/IPC/PID namespaces; read-only root; 16 MiB tmpfs")
	// Namespace destruction releases the mounts when this helper exits. Avoid
	// host cleanup paths and keep payload shutdown on the same locked thread.
	return child.Wait(ctx, time.Second)
}

func verifyPrivateMount(parent string) error {
	// Read the identity captured before clone: a mapped child cannot necessarily
	// inspect its parent's namespace links across the user-namespace boundary.
	current, err := os.Readlink("/proc/thread-self/ns/mnt")
	if err != nil {
		return err
	}
	if parent == "" || current == parent || os.Geteuid() != 0 {
		return errors.New("internal helper requires a private mapped user/mount namespace")
	}
	return nil
}
