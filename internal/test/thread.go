//go:build linux

package test

import (
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// LockThread pins the calling goroutine to its OS thread and returns its Linux
// thread ID. It never unlocks or registers unlock cleanup: test and fixture
// goroutines keep the lock until they exit, including after failure or panic.
// This prevents a thread with changed credentials, namespaces, or syscall
// restrictions from returning to unrelated Go work. Read-only tests use the
// same lifetime rule.
//
// Go destroys a locked worker thread on goroutine exit, but may permanently park
// its startup thread. Reserve that thread in TestMain before observing worker
// destruction. LockThread does not start a new goroutine; use LockedThread when
// the scenario needs a separate worker. It does not replace subprocess isolation
// for irreversible mutations. Callers must not unlock the thread afterward.
func LockThread() int {
	runtime.LockOSThread()
	return unix.Gettid()
}

// LockedThread runs an operation on a dedicated locked goroutine and waits for
// its defers, including failure cleanup. It follows LockThread's lifetime rule.
// The returned Linux thread ID allows independent teardown observations without
// exposing production implementation.
func LockedThread(t *testing.T, operation func()) int {
	t.Helper()
	started := make(chan int, 1)
	done := make(chan struct{})
	go func() {
		id := LockThread()
		defer close(done)
		started <- id
		operation()
	}()
	id := Receive(t, started)
	Receive(t, done)
	return id
}

// WaitForThreadExit bounds observation of a test-owned worker's teardown.
func WaitForThreadExit(t *testing.T, id int) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, err := os.Stat("/proc/self/task/" + strconv.Itoa(id))
		return os.IsNotExist(err)
	}, 5*time.Second, time.Millisecond, "locked worker must exit")
}
