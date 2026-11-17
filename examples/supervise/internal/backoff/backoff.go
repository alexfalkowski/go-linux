//go:build linux

package backoff

import (
	"context"
	"time"

	"github.com/alexfalkowski/go-linux/timerfd"
)

// Wait uses a boottime timer so suspend counts toward delay; cancellation closes it.
func Wait(ctx context.Context, delay time.Duration) error {
	timer, err := timerfd.New(timerfd.Boottime)
	if err != nil {
		return err
	}
	defer timer.Close()
	interrupt := context.AfterFunc(ctx, func() {
		_ = timer.Close()
	})
	defer interrupt()
	if _, err := timer.SetAfter(delay, 0); err != nil {
		return err
	}
	if _, err := timer.ReadExpirations(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	return ctx.Err()
}
