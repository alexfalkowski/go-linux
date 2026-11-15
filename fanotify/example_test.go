//go:build linux

package fanotify_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/fanotify"
	"github.com/alexfalkowski/go-linux/internal/test"
)

func ExampleGroup() { //nolint:testableexamples // Executed by TestExamples in an isolated privileged subprocess.
	group, err := fanotify.New(fanotify.Notification)
	if err != nil {
		panic(err)
	}
	defer group.Close()

	if err := group.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		panic(err)
	}

	directory, err := os.MkdirTemp("", "fanotify-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(directory)

	if err := group.AddMark(directory, fanotify.CloseWrite|fanotify.EventOnChild, fanotify.OnlyDir); err != nil {
		panic(err)
	}

	if err := os.WriteFile(filepath.Join(directory, "message"), []byte("hello"), 0o600); err != nil {
		panic(err)
	}

	for event, err := range group.Events() {
		if err != nil {
			panic(err)
		}

		if _, err := event.Stat(); err != nil {
			panic(err)
		}
		// The event's file closes here, even when the loop breaks.
		break
	}
}

func ExampleEvent_Respond() { //nolint:testableexamples // Executed by TestExamples in an isolated privileged subprocess.
	group, err := fanotify.New(fanotify.Content)
	if err != nil {
		panic(err)
	}
	defer group.Close()

	if err := group.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		panic(err)
	}

	file, err := os.CreateTemp("", "fanotify-permission-")
	if err != nil {
		panic(err)
	}
	defer os.Remove(file.Name())
	defer file.Close()

	if err := group.AddMark(file.Name(), fanotify.OpenPermission, 0); err != nil {
		panic(err)
	}

	opened := make(chan error, 1)
	go func() {
		file, err := os.Open(file.Name())
		if err == nil {
			err = file.Close()
		}

		opened <- err
	}()

	for event, err := range group.Events() {
		if err != nil {
			panic(err)
		}

		// Inspect content with event.ReadAt if needed, then decide explicitly.
		if err := event.Respond(fanotify.Allow); err != nil {
			panic(err)
		}
		break
	}

	if err := <-opened; err != nil {
		panic(err)
	}
}

func TestExamples(t *testing.T) {
	if !test.FanotifyNamespace(t) {
		return
	}

	ExampleGroup()
	ExampleEvent_Respond()
}
