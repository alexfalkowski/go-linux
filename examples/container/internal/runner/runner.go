//go:build linux

package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"

	"github.com/alexfalkowski/go-linux/examples/container/internal/config"
	"github.com/alexfalkowski/go-linux/examples/container/internal/startup"
	"github.com/alexfalkowski/go-linux/examples/internal/process"
)

// New retains validated settings; Run owns all resources it acquires.
func New(cfg config.Config) *Runner {
	return &Runner{config: cfg}
}

// Runner owns parent-side setup; its helper cannot start the payload until
// resource limits have been applied and the startup gate is released.
type Runner struct {
	config config.Config
}

// Run starts an isolated helper and reaps it before releasing setup resources.
func (runner *Runner) Run(ctx context.Context) (resultErr error) {
	// Pdeathsig follows the creating OS thread, which must outlive the helper.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	parentMount, err := os.Readlink("/proc/thread-self/ns/mnt")
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp("", "go-linux-container-*")
	if err != nil {
		return err
	}
	defer os.Remove(stage)

	gate, err := startup.New()
	if err != nil {
		return err
	}
	defer gate.Close()
	command, err := runner.helperCommand(ctx, stage, parentMount, gate.Reader())
	if err != nil {
		return err
	}
	group, err := newDelegation(runner.config)
	if err != nil {
		return err
	}
	if group != nil {
		defer func() {
			resultErr = errors.Join(resultErr, group.Close())
		}()
	}

	childContext, cancel := context.WithCancel(ctx)
	defer cancel()
	child, err := process.Start(command)
	if err != nil {
		return fmt.Errorf("create mapped user/network/mount namespaces: %w", err)
	}
	if err := releaseChild(gate, group, command.Process.Pid); err != nil {
		cancel()
		_ = child.Wait(childContext, time.Second)
		return err // Expected cleanup cancellation must not hide the setup failure.
	}
	return child.Wait(childContext, 3*time.Second)
}

func (runner *Runner) helperCommand(ctx context.Context, stage, parentMount string, input io.Reader) (*exec.Cmd, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	arguments := make([]string, 0, 8+len(runner.config.Arguments))
	arguments = append(arguments, "-init", "-rootfs", runner.config.Root, "-stage", stage, "-parent-mount", parentMount, "--")
	command := exec.CommandContext(ctx, executable, append(arguments, runner.config.Arguments...)...)
	command.Cancel = nil // The pidfd owner handles helper and payload shutdown.
	command.Stdin, command.Stdout, command.Stderr = input, os.Stdout, os.Stderr
	command.Env = []string{"PATH=/usr/bin:/bin"}
	// Go creates the user namespace and mappings before its child runtime starts.
	// Unshareflags also makes mount propagation recursively private in Go's child.
	command.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:   syscall.CLONE_NEWUSER | syscall.CLONE_NEWNET | syscall.CLONE_NEWUTS | syscall.CLONE_NEWIPC,
		Unshareflags: syscall.CLONE_NEWNS,
		UidMappings:  []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getuid(), Size: 1}},
		GidMappings:  []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getgid(), Size: 1}},
		Pdeathsig:    syscall.SIGKILL,
	}
	return command, nil
}

func releaseChild(gate *startup.Gate, group *delegation, pid int) error {
	if group != nil {
		if err := group.group.AddProcess(pid); err != nil {
			return err
		}
	}
	return gate.Release()
}
