//go:build linux

package process

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
	"time"

	"github.com/alexfalkowski/go-linux/pidfd"
)

// Start starts command and acquires an independent process handle. Failure kills
// and reaps any started child. The caller must call Wait exactly once on success.
func Start(command *exec.Cmd) (*Child, error) {
	if err := command.Start(); err != nil {
		return nil, err
	}
	handle, err := pidfd.OpenProcess(command.Process)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, err
	}
	return &Child{command: command, handle: handle}, nil
}

// Child owns a command and its independent pidfd until Wait returns.
type Child struct {
	command *exec.Cmd
	handle  *pidfd.Process
}

// Wait observes exit through pidfd and leaves reaping to os/exec. Cancellation
// sends TERM, then KILL after grace, and joins both observation and reaping.
// Only the direct child is signaled; the example's command owns its descendants.
func (child *Child) Wait(ctx context.Context, grace time.Duration) error {
	defer child.handle.Close()
	ready := make(chan error, 1)
	go func() {
		ready <- child.handle.WaitReady()
	}()

	var observationErr error
	select {
	case observationErr = <-ready:
	case <-ctx.Done():
		observationErr = child.stop(ready, grace)
	}
	if observationErr != nil {
		_ = child.command.Process.Kill()
	}
	waitErr := child.command.Wait()
	if ctx.Err() != nil {
		return errors.Join(ctx.Err(), observationErr)
	}
	return errors.Join(observationErr, waitErr)
}

func (child *Child) stop(ready <-chan error, grace time.Duration) error {
	if err := child.handle.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		_ = child.command.Process.Kill()
		return errors.Join(err, <-ready)
	}

	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case err := <-ready:
		return err
	case <-timer.C:
		if err := child.handle.Signal(syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			_ = child.command.Process.Kill()
			return errors.Join(err, <-ready)
		}
		return <-ready
	}
}
