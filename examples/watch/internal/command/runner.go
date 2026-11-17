//go:build linux

package command

import (
	"context"
	"errors"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/alexfalkowski/go-linux/examples/internal/process"
)

// Options describes one directly owned command.
type Options struct {
	// Directory is the command's working directory.
	Directory string
	// Arguments contains the executable and literal arguments.
	Arguments []string
	// Grace is the positive TERM grace period before KILL.
	Grace time.Duration
}

// New borrows logger and retains command settings for subsequent starts.
func New(options Options, logger *log.Logger) *Runner {
	return &Runner{arguments: options.Arguments, directory: options.Directory, grace: options.Grace, logger: logger}
}

// Runner owns the active child and joins its shutdown before replacement.
type Runner struct {
	cancel    context.CancelFunc
	done      chan error
	logger    *log.Logger
	directory string
	arguments []string
	grace     time.Duration
}

// Start joins the previous child before starting the next command.
func (runner *Runner) Start(ctx context.Context) error {
	runner.Stop()

	//nolint:gosec // The operator supplies argv explicitly; no shell is added.
	command := exec.CommandContext(ctx, runner.arguments[0], runner.arguments[1:]...)
	command.Cancel = nil // The pidfd owner performs graceful cancellation.
	command.Dir = runner.directory
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	child, err := process.Start(command)
	if err != nil {
		return err
	}

	childContext, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	runner.cancel, runner.done = cancel, done
	go func() {
		done <- child.Wait(childContext, runner.grace)
	}()
	return nil
}

// Stop cancels and reaps the active child; expected cancellation is not logged.
func (runner *Runner) Stop() {
	if runner.cancel != nil {
		runner.cancel()
		if err := <-runner.done; err != nil && !errors.Is(err, context.Canceled) {
			runner.logger.Printf("command failed during shutdown: %v", err)
		}
		runner.cancel, runner.done = nil, nil
	}
}

// Complete records a received child result and clears the active command.
func (runner *Runner) Complete(err error) {
	runner.cancel()
	runner.cancel, runner.done = nil, nil
	if err != nil {
		runner.logger.Printf("command failed: %v", err)
	}
}

// Done returns the current child's result channel, or nil while idle.
// Call Complete after receiving a result before selecting again.
func (runner *Runner) Done() <-chan error { return runner.done }
