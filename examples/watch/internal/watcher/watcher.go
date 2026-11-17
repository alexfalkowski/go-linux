//go:build linux

package watcher

import (
	"context"
	"errors"
	"log"
	"os"
	"time"

	"github.com/alexfalkowski/go-linux/examples/watch/internal/command"
	"github.com/alexfalkowski/go-linux/inotify"
)

// Options selects the directory and command scheduling policy.
type Options struct {
	// Directory is watched nonrecursively for changes to immediate children.
	Directory string
	// Debounce is the positive quiet period before replacement.
	Debounce time.Duration
	// Initial requests a command run after installing the watch.
	Initial bool
}

// New installs a watch, cleaning up the source if acquisition fails.
func New(cfg Options, child *command.Runner, logger *log.Logger) (*Watcher, error) {
	source, err := inotify.New()
	if err != nil {
		return nil, err
	}
	mask := inotify.CloseWrite | inotify.Create | inotify.Delete | inotify.MoveEvents | inotify.DeleteSelf | inotify.MoveSelf | inotify.OnlyDir
	if _, err := source.AddWatch(cfg.Directory, mask); err != nil {
		return nil, errors.Join(err, source.Close())
	}
	return &Watcher{
		source:   source,
		logger:   logger,
		options:  cfg,
		command:  child,
		events:   make(chan inotify.Event, 64),
		failures: make(chan error, 1),
		readDone: make(chan struct{}),
	}, nil
}

// Watcher owns notifications and schedules a separately owned command runner.
type Watcher struct {
	source   *inotify.Watcher
	logger   *log.Logger
	events   chan inotify.Event
	failures chan error
	readDone chan struct{}
	command  *command.Runner
	options  Options
}

// Close interrupts pending notification reads and releases the watch.
func (watcher *Watcher) Close() error {
	return watcher.source.Close()
}

// Run consumes one notification session and joins its reader and child on return.
func (watcher *Watcher) Run(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	interrupt := context.AfterFunc(ctx, func() {
		_ = watcher.Close()
	})
	defer interrupt()
	defer func() {
		cancel()
		_ = watcher.Close()
		<-watcher.readDone
	}()
	go watcher.readEvents(ctx)

	defer watcher.command.Stop()
	watcher.logger.Printf("watching %q", watcher.options.Directory)
	if watcher.options.Initial {
		if err := watcher.command.Start(ctx); err != nil {
			return err
		}
	}
	return watcher.watchChanges(ctx)
}

func (watcher *Watcher) readEvents(ctx context.Context) {
	defer close(watcher.readDone)
	for event, err := range watcher.source.Events() {
		if err != nil {
			watcher.failures <- err
			return
		}
		select {
		case watcher.events <- event:
		case <-ctx.Done():
			return
		}
	}
}

func (watcher *Watcher) watchChanges(ctx context.Context) error {
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-watcher.failures:
			return errors.Join(ctx.Err(), err)
		case err := <-watcher.command.Done():
			watcher.command.Complete(err)
		case event := <-watcher.events:
			if err := watcher.reconcileEvent(event); err != nil {
				return err
			}
			timer.Reset(watcher.options.Debounce)
		case <-timer.C:
			if err := watcher.command.Start(ctx); err != nil {
				return err
			}
		}
	}
}

func (watcher *Watcher) reconcileEvent(event inotify.Event) error {
	lostWatch := event.Mask&(inotify.Ignored|inotify.DeleteSelf|inotify.MoveSelf|inotify.Unmount) != 0
	if lostWatch {
		return errors.New("watched directory moved, disappeared, or was unmounted; restart with its current path")
	}
	if event.Mask&inotify.QueueOverflow != 0 {
		entries, err := os.ReadDir(watcher.options.Directory)
		if err != nil {
			return err
		}
		watcher.logger.Printf("notification overflow: rescanned %d entries; scheduling a full command run", len(entries))
	}
	return nil
}
