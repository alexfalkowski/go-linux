//go:build linux

package monitor

import (
	"context"
	"time"

	"github.com/alexfalkowski/go-linux/examples/top/internal/display"
	"github.com/alexfalkowski/go-linux/examples/top/internal/stats"
)

// Options controls the refresh schedule.
type Options struct {
	// Interval is the positive duration between process samples.
	Interval time.Duration
	// Count limits frames; zero runs until the context is canceled.
	Count int
}

// New composes a collector and table without acquiring additional resources.
func New(collector *stats.Collector, table *display.Table, options Options) *Monitor {
	return &Monitor{collector: collector, table: table, interval: options.Interval, count: options.Count}
}

// Monitor schedules collection and rendering until completion or cancellation.
type Monitor struct {
	collector *stats.Collector
	table     *display.Table
	interval  time.Duration
	count     int
}

// Run primes CPU counters, then writes frames on the configured schedule.
func (monitor *Monitor) Run(ctx context.Context) error {
	if err := monitor.collector.Prime(); err != nil {
		return err
	}
	ticker := time.NewTicker(monitor.interval)
	defer ticker.Stop()

	for count := 0; monitor.count == 0 || count < monitor.count; count++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			frame, err := monitor.collector.Read()
			if err != nil {
				return err
			}
			if err := monitor.table.Write(frame); err != nil {
				return err
			}
		}
	}
	return nil
}
