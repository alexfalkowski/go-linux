//go:build linux

package supervisor

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/alexfalkowski/go-linux/examples/internal/process"
	"github.com/alexfalkowski/go-linux/examples/supervise/internal/backoff"
)

// Options defines the command, restart budget, and shutdown policy.
type Options struct {
	// Arguments contains an executable followed by literal arguments.
	Arguments []string
	// Backoff is the positive initial restart delay.
	Backoff time.Duration
	// MaxBackoff caps exponential restart delays.
	MaxBackoff time.Duration
	// Grace bounds TERM handling before the direct child receives KILL.
	Grace time.Duration
	// Restarts counts retries after the initial attempt; zero disables retries.
	Restarts int
}

// New uses validated options and borrows logger for lifecycle diagnostics.
func New(options Options, logger *log.Logger) *Supervisor {
	return &Supervisor{options: options, logger: logger}
}

// Supervisor owns sequential child attempts and waits for each to be reaped.
type Supervisor struct {
	logger  *log.Logger
	options Options
}

// Run exits on success, cancellation, startup failure, or an exhausted budget.
func (supervisor *Supervisor) Run(ctx context.Context) error {
	delay := supervisor.options.Backoff
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		//nolint:gosec // The operator supplies argv explicitly; no shell is added.
		command := exec.CommandContext(ctx, supervisor.options.Arguments[0], supervisor.options.Arguments[1:]...)
		command.Cancel = nil // The pidfd owner performs graceful cancellation.
		command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
		child, err := process.Start(command)
		if err != nil {
			return err
		}
		supervisor.logger.Printf("started pid=%d attempt=%d", command.Process.Pid, attempt+1)
		if err := child.Wait(ctx, supervisor.options.Grace); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if attempt >= supervisor.options.Restarts {
				return fmt.Errorf("restart budget exhausted: %w", err)
			}
			supervisor.logger.Printf("child failed: %v; restarting in %s", err, delay)
		} else {
			return nil
		}
		if err := backoff.Wait(ctx, delay); err != nil {
			return err
		}
		if delay > supervisor.options.MaxBackoff/2 {
			delay = supervisor.options.MaxBackoff
		} else {
			delay *= 2
		}
	}
}
