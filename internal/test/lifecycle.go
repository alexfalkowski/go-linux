//go:build linux

package test

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Cleanup closes an owned resource after its test, allowing an earlier Close.
func Cleanup(t *testing.T, owner io.Closer) {
	t.Helper()

	t.Cleanup(func() {
		if err := owner.Close(); !errors.Is(err, os.ErrClosed) {
			require.NoError(t, err)
		}
	})
}

// Pending requires a result channel to remain blocked before its trigger.
func Pending[T any](t *testing.T, results <-chan T) {
	t.Helper()

	select {
	case result := <-results:
		require.FailNowf(t, "operation completed before its trigger", "result: %+v", result)
	case <-time.After(20 * time.Millisecond):
	}
}

// Receive waits up to five seconds for an asynchronous operation to finish.
func Receive[T any](t *testing.T, results <-chan T) T {
	t.Helper()

	select {
	case result := <-results:
		return result
	case <-time.After(5 * time.Second):
		require.FailNow(t, "operation did not finish within five seconds")

		var zero T
		return zero
	}
}

// Start runs an operation and requires it to block until its later trigger.
// The caller must trigger completion; the buffered channel cannot strand its sender.
func Start[T any](t *testing.T, operation func() T) <-chan T {
	t.Helper()

	results := make(chan T, 1)

	go func() {
		results <- operation()
	}()

	Pending(t, results)
	return results
}

// StartRead starts a typed read and requires it to wait for its later trigger.
func StartRead[T any](t *testing.T, operation func() (T, error)) <-chan ReadResult[T] {
	t.Helper()
	return Start(t, func() ReadResult[T] {
		value, err := operation()
		return ReadResult[T]{Value: value, Err: err}
	})
}

// ReadResult keeps a pending read's value and error together on one channel.
type ReadResult[T any] struct {
	Err   error
	Value T
}

// Command reexecutes one named test in the current test binary with fixed
// caller-supplied fixture arguments and a thirty-second bound. The caller runs
// and reaps the command, and may configure its streams and SysProcAttr first.
func Command(t *testing.T, name string, arguments ...string) (context.Context, *exec.Cmd) {
	t.Helper()

	executable, err := os.Executable()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	args := append([]string{"-test.run=^" + regexp.QuoteMeta(name) + "$"}, arguments...)
	return ctx, exec.CommandContext(ctx, executable, args...)
}

// TempFile creates a sized, zero-filled ordinary file and closes it at cleanup.
func TempFile(t *testing.T, size int64) *os.File {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), "fixture-")
	require.NoError(t, err)
	Cleanup(t, file)

	require.NoError(t, file.Truncate(size))
	return file
}

// Pipe creates a standard-library pipe with cleanup and bounded I/O.
func Pipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()

	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	Cleanup(t, reader)
	Cleanup(t, writer)
	require.NoError(t, reader.SetDeadline(time.Now().Add(5*time.Second)))
	require.NoError(t, writer.SetDeadline(time.Now().Add(5*time.Second)))
	return reader, writer
}

// OpenFile opens an ordinary file or directory read-only and registers cleanup.
func OpenFile(t *testing.T, path string) *os.File {
	t.Helper()
	file, err := os.Open(path)
	require.NoError(t, err)
	Cleanup(t, file)
	return file
}
