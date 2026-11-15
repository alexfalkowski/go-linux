//go:build linux

package inotify_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/alexfalkowski/go-linux/inotify"
)

func ExampleWatcher() {
	directory, err := os.MkdirTemp("", "inotify-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(directory)
	watcher, err := inotify.New()
	if err != nil {
		panic(err)
	}
	defer watcher.Close()
	if err := watcher.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		panic(err)
	}
	id, err := watcher.AddWatch(directory, inotify.Create|inotify.OnlyDir)
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "message"), []byte("hello"), 0o600); err != nil {
		panic(err)
	}
	for event, err := range watcher.Events() {
		if err != nil {
			panic(err)
		}
		fmt.Println(event.Watch == id, event.Mask == inotify.Create, event.Name)
		break
	}
	// Output: true true message
}

func ExampleWatcher_Events() {
	directory, err := os.MkdirTemp("", "inotify-events-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(directory)
	watcher, err := inotify.New()
	if err != nil {
		panic(err)
	}
	defer watcher.Close()
	if err := watcher.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		panic(err)
	}
	if _, err := watcher.AddWatch(directory, inotify.Create|inotify.OnlyDir); err != nil {
		panic(err)
	}
	for _, name := range []string{"first", "second"} {
		if err := os.WriteFile(filepath.Join(directory, name), nil, 0o600); err != nil {
			panic(err)
		}
	}
	// Reuse this sequence to retain the unread portion of a batch after break.
	events := watcher.Events()
	for range 2 {
		for event, err := range events {
			if err != nil {
				panic(err)
			}
			fmt.Println(event.Name)
			break
		}
	}
	// Output:
	// first
	// second
}

func ExampleWatcher_Events_channel() {
	directory, err := os.MkdirTemp("", "inotify-channel-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(directory)
	watcher, err := inotify.New()
	if err != nil {
		panic(err)
	}
	defer watcher.Close()
	if _, err := watcher.AddWatch(directory, inotify.Create); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "message"), nil, 0o600); err != nil {
		panic(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// This application owns the whole watch session: cancellation closes it.
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		watcher.Close()
		close(closed)
	})
	defer func() {
		if !stop() {
			<-closed
		}
	}()
	events := make(chan inotify.Event)
	done := make(chan error, 1)
	go func() {
		defer close(events)
		done <- forwardEvents(ctx, watcher, events)
	}()
	select {
	case event := <-events:
		fmt.Println(event.Name)
	case <-ctx.Done():
		panic(ctx.Err())
	}
	cancel()
	if err := <-done; !errors.Is(err, os.ErrClosed) && !errors.Is(err, context.Canceled) {
		panic(err)
	}
	// Output: message
}

func ExampleWatcher_RemoveWatch() {
	directory, err := os.MkdirTemp("", "inotify-removal-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(directory)
	watcher, err := inotify.New()
	if err != nil {
		panic(err)
	}
	defer watcher.Close()
	if err := watcher.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		panic(err)
	}
	id, err := watcher.AddWatch(directory, inotify.AllEvents)
	if err != nil {
		panic(err)
	}
	if err := watcher.RemoveWatch(id); err != nil {
		panic(err)
	}
	events, err := watcher.ReadEvents(make([]byte, 4096))
	if err != nil {
		panic(err)
	}
	for _, event := range events {
		fmt.Println(event.Watch == id, event.Mask == inotify.Ignored)
	}
	// Output: true true
}

// forwardEvents is an application-owned channel adapter. Cancelable sends keep
// a stopped consumer from stranding its producer; session cancellation must also
// close the watcher to interrupt a read, as the example arranges above.
func forwardEvents(ctx context.Context, watcher *inotify.Watcher, events chan<- inotify.Event) error {
	for event, err := range watcher.Events() {
		if err != nil {
			return err
		}
		select {
		case events <- event:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
