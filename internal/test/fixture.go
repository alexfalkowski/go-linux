//go:build linux

package test

import (
	"os"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/eventfd"
	"github.com/alexfalkowski/go-linux/memfd"
	"github.com/alexfalkowski/go-linux/mmap"
	"github.com/alexfalkowski/go-linux/mount"
	"github.com/alexfalkowski/go-linux/pidfd"
	"github.com/alexfalkowski/go-linux/timerfd"
	"github.com/stretchr/testify/require"
)

// Counter creates an owned counter with cleanup and a five-second I/O deadline.
func Counter(t *testing.T, initial uint32, mode eventfd.Mode) *eventfd.Counter {
	t.Helper()

	counter, err := eventfd.New(initial, mode)
	require.NoError(t, err)
	Cleanup(t, counter)

	require.NoError(t, counter.SetDeadline(time.Now().Add(5*time.Second)), "bound unexpected I/O waits")
	return counter
}

// Timer creates an owned timer with cleanup and a five-second read deadline.
func Timer(t *testing.T, clock timerfd.Clock) *timerfd.Timer {
	t.Helper()

	timer, err := timerfd.New(clock)
	require.NoError(t, err)
	Cleanup(t, timer)

	require.NoError(t, timer.SetReadDeadline(time.Now().Add(5*time.Second)), "bound unexpected I/O waits")
	return timer
}

// Memfd creates a memory-backed file labeled with the test name and registers cleanup.
func Memfd(t *testing.T, options memfd.Options) *memfd.File {
	t.Helper()

	file, err := memfd.New(t.Name(), options)
	require.NoError(t, err)
	Cleanup(t, file)
	return file
}

// Process opens a process handle for id and registers its cleanup.
func Process(t *testing.T, id int) *pidfd.Process {
	t.Helper()
	process, err := pidfd.Open(id)
	require.NoError(t, err)
	Cleanup(t, process)
	return process
}

// Anonymous creates an owned anonymous region and registers full-region cleanup.
func Anonymous(t *testing.T, length int, options mmap.Options) *mmap.Region {
	t.Helper()
	region, err := mmap.Anonymous(length, options)
	require.NoError(t, err)
	Cleanup(t, region)
	return region
}

// Mapping borrows file to create an owned region and registers full-region cleanup.
func Mapping(t *testing.T, file *os.File, offset int64, length int, options mmap.Options) *mmap.Region {
	t.Helper()
	region, err := mmap.Map(file, offset, length, options)
	require.NoError(t, err)
	Cleanup(t, region)
	return region
}

// MountConfig opens a tmpfs configuration context and registers its cleanup.
func MountConfig(t *testing.T) *mount.Config {
	t.Helper()
	config, err := mount.Open("tmpfs")
	require.NoError(t, err)
	Cleanup(t, config)
	return config
}

// Mount explicitly creates a detached tmpfs mount and registers both owners for cleanup.
func Mount(t *testing.T) *mount.Handle {
	t.Helper()

	config := MountConfig(t)

	require.NoError(t, config.Create())

	handle, err := config.Mount(0)
	require.NoError(t, err)
	Cleanup(t, handle)
	return handle
}

// CounterDescriptor creates a counter and independently observes its descriptor
// for focused ABI checks. Cleanup keeps the owner live through the scenario.
func CounterDescriptor(t *testing.T, initial uint32, mode eventfd.Mode) (*eventfd.Counter, int) {
	t.Helper()
	before := Descriptors(t)
	counter := Counter(t, initial, mode)
	return counter, NewDescriptor(t, before)
}

// TimerDescriptor creates a timer and independently observes its descriptor
// for focused ABI checks. Cleanup keeps the owner live through the scenario.
func TimerDescriptor(t *testing.T, clock timerfd.Clock) (*timerfd.Timer, int) {
	t.Helper()
	before := Descriptors(t)
	timer := Timer(t, clock)
	return timer, NewDescriptor(t, before)
}
