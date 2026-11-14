//go:build linux

package eventfd_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/eventfd"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

var (
	_ io.ReadWriteCloser = (*eventfd.Counter)(nil)
	_ syscall.Conn       = (*eventfd.Counter)(nil)
)

func TestAccumulationAndDrain(t *testing.T) {
	counter := newCounter(t, math.MaxUint32, eventfd.Default)
	require.NoError(t, counter.Add(1<<40))
	require.NoError(t, counter.Add(0), "zero additions are valid")

	value, err := counter.ReadValue()
	require.NoError(t, err)
	require.Equal(t, uint64(math.MaxUint32)+1<<40, value, "initial and added values must accumulate without truncation")

	checkEmptyCounter(t, counter)
	require.NoError(t, counter.Add(0), "zero additions must not make an empty counter readable")
	checkEmptyCounter(t, counter)
}

func TestSemaphore(t *testing.T) {
	counter := newCounter(t, 2, eventfd.Semaphore)
	require.NoError(t, counter.Add(2))

	for range 4 {
		value, err := counter.ReadValue()
		require.NoError(t, err)
		require.Equal(t, uint64(1), value, "semaphore reads must consume exactly one unit")
	}

	checkEmptyCounter(t, counter)
}

func TestByteRecords(t *testing.T) {
	counter := newCounter(t, 0, eventfd.Default)
	var record [8]byte
	const value = uint64(0x0102030405060708)
	binary.NativeEndian.PutUint64(record[:], value)

	n, err := counter.Write(record[:])
	require.NoError(t, err)
	require.Equal(t, 8, n, "writes must add a complete record")
	actual, err := counter.ReadValue()
	require.NoError(t, err)
	require.Equal(t, value, actual)

	require.NoError(t, counter.Add(value))
	buffer := bytes.Repeat([]byte{0xff}, 16)
	n, err = counter.Read(buffer)
	require.NoError(t, err)
	require.Equal(t, 8, n, "reads return one record even with a larger buffer")
	require.Equal(t, record[:], buffer[:8], "record encoding must use native byte order")
	require.Equal(t, bytes.Repeat([]byte{0xff}, 8), buffer[8:], "reads must preserve the unused buffer tail")
}

func TestInvalidReadBuffers(t *testing.T) {
	for size := range 8 {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			counter := newCounter(t, 5, eventfd.Default)
			n, err := counter.Read(make([]byte, size))
			require.Zero(t, n)
			require.ErrorIs(t, err, unix.EINVAL)

			value, err := counter.ReadValue()
			require.NoError(t, err)
			require.Equal(t, uint64(5), value, "invalid reads must not consume the counter")
		})
	}
}

func TestInvalidWriteBuffers(t *testing.T) {
	for _, size := range []int{0, 1, 7, 9, 16} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			counter := newCounter(t, 5, eventfd.Default)
			n, err := counter.Write(make([]byte, size))
			require.Zero(t, n)
			require.ErrorIs(t, err, unix.EINVAL)

			var pathErr *os.PathError
			require.ErrorAs(t, err, &pathErr)
			require.Equal(t, "write", pathErr.Op)

			value, err := counter.ReadValue()
			require.NoError(t, err)
			require.Equal(t, uint64(5), value, "invalid writes must not change the counter")
		})
	}
}

func TestInvalidValue(t *testing.T) {
	counter := newCounter(t, 5, eventfd.Default)
	require.ErrorIs(t, counter.Add(math.MaxUint64), unix.EINVAL)

	var record [8]byte
	binary.NativeEndian.PutUint64(record[:], math.MaxUint64)
	n, err := counter.Write(record[:])
	require.Zero(t, n)
	require.ErrorIs(t, err, unix.EINVAL)

	var errno syscall.Errno
	require.ErrorAs(t, err, &errno)
	require.Equal(t, unix.EINVAL, errno)

	value, err := counter.ReadValue()
	require.NoError(t, err)
	require.Equal(t, uint64(5), value, "forbidden values must not change the counter")
}

func TestCreationFlagsAndRawAccess(t *testing.T) {
	counter := newCounter(t, 0, eventfd.Default)
	var connection syscall.Conn = counter
	conn, err := connection.SyscallConn()
	require.NoError(t, err)

	var descriptorFlags, statusFlags int
	var flagsErr, statusErr, readErr error
	var record [8]byte
	err = conn.Control(func(fd uintptr) {
		descriptorFlags, flagsErr = unix.FcntlInt(fd, unix.F_GETFD, 0)
		statusFlags, statusErr = unix.FcntlInt(fd, unix.F_GETFL, 0)
		_, readErr = unix.Read(int(fd), record[:])
	})
	require.NoError(t, err)
	require.NoError(t, flagsErr)
	require.NoError(t, statusErr)
	require.NotZero(t, descriptorFlags&unix.FD_CLOEXEC, "counters must not leak across exec")
	require.NotZero(t, statusFlags&unix.O_NONBLOCK, "raw operations must remain nonblocking")
	require.ErrorIs(t, readErr, unix.EAGAIN, "a raw read of an empty counter must return EAGAIN")

	require.NoError(t, counter.Add(0), "zero must also be allowed on an empty counter")
	require.NoError(t, counter.Add(math.MaxUint64-1))
	require.NoError(t, counter.Add(0), "zero must be allowed at the maximum count")
	err = conn.Control(func(fd uintptr) {
		_, readErr = unix.Read(int(fd), record[:])
	})
	require.NoError(t, err)
	require.NoError(t, readErr)
	require.Equal(t, uint64(math.MaxUint64-1), binary.NativeEndian.Uint64(record[:]), "typed addition must reach the kernel unchanged")
}

func TestRawWriteWouldBlock(t *testing.T) {
	counter := newCounter(t, 0, eventfd.Default)
	require.NoError(t, counter.Add(math.MaxUint64-1))
	conn, err := counter.SyscallConn()
	require.NoError(t, err)

	var record [8]byte
	binary.NativeEndian.PutUint64(record[:], 1)
	var writeErr error
	err = conn.Control(func(fd uintptr) {
		_, writeErr = unix.Write(int(fd), record[:])
	})
	require.NoError(t, err)
	require.ErrorIs(t, writeErr, unix.EAGAIN)
	value, err := counter.ReadValue()
	require.NoError(t, err)
	require.Equal(t, uint64(math.MaxUint64-1), value, "raw overflow attempts must not change the count")
}

func TestCloseReleasesDescriptors(t *testing.T) {
	counter := newCounter(t, 0, eventfd.Default)
	require.NoError(t, counter.Close())
	before, err := os.ReadDir("/proc/self/fd")
	require.NoError(t, err)

	for range 32 {
		counter, err := eventfd.New(0, eventfd.Default)
		require.NoError(t, err)
		require.NoError(t, counter.Close())
	}

	after, err := os.ReadDir("/proc/self/fd")
	require.NoError(t, err)
	require.LessOrEqual(t, len(after), len(before), "Close must release owned descriptors")
}

func TestReadWaitsForAddition(t *testing.T) {
	counter := newCounter(t, 0, eventfd.Default)
	result := make(chan readResult, 1)
	go func() {
		value, err := counter.ReadValue()
		result <- readResult{value: value, err: err}
	}()
	select {
	case actual := <-result:
		t.Fatalf("empty read returned before addition: %+v", actual)
	case <-time.After(20 * time.Millisecond):
	}

	require.NoError(t, counter.Add(7))
	select {
	case actual := <-result:
		require.NoError(t, actual.err)
		require.Equal(t, uint64(7), actual.value)
	case <-time.After(5 * time.Second):
		t.Fatal("read did not wake after addition")
	}
}

func TestWriteWaitsForDrain(t *testing.T) {
	counter := newCounter(t, 0, eventfd.Default)
	require.NoError(t, counter.Add(math.MaxUint64-2))
	done := startWaiting(t, func() error { return counter.Add(2) })

	value, err := counter.ReadValue()
	require.NoError(t, err)
	require.Equal(t, uint64(math.MaxUint64-2), value, "waiting additions must not partially mutate the count")
	require.NoError(t, awaitResult(t, done))

	value, err = counter.ReadValue()
	require.NoError(t, err)
	require.Equal(t, uint64(2), value, "the entire waiting addition must commit after the drain")
}

func TestReadDeadlineAndReset(t *testing.T) {
	counter := newCounter(t, 0, eventfd.Default)
	require.NoError(t, counter.SetReadDeadline(time.Now().Add(30*time.Millisecond)))
	value, err := counter.ReadValue()
	require.Zero(t, value)
	require.ErrorIs(t, err, os.ErrDeadlineExceeded)
	require.True(t, os.IsTimeout(err), "deadline failures must expose timeout semantics")

	require.NoError(t, counter.Add(9), "a read deadline must not expire writes")
	_, err = counter.ReadValue()
	require.ErrorIs(t, err, os.ErrDeadlineExceeded, "expired read deadlines persist")
	require.NoError(t, counter.SetReadDeadline(time.Time{}))
	value, err = counter.ReadValue()
	require.NoError(t, err)
	require.Equal(t, uint64(9), value, "clearing the deadline must restore reads without data loss")
}

func TestWriteDeadlineAndReset(t *testing.T) {
	counter := newCounter(t, 0, eventfd.Default)
	require.NoError(t, counter.Add(math.MaxUint64-1))
	require.NoError(t, counter.SetWriteDeadline(time.Now().Add(30*time.Millisecond)))
	require.ErrorIs(t, counter.Add(1), os.ErrDeadlineExceeded)

	value, err := counter.ReadValue()
	require.NoError(t, err, "a write deadline must not expire reads")
	require.Equal(t, uint64(math.MaxUint64-1), value, "a timed-out addition must not change the count")
	require.ErrorIs(t, counter.Add(1), os.ErrDeadlineExceeded, "expired write deadlines persist")
	require.NoError(t, counter.SetWriteDeadline(time.Time{}))
	require.NoError(t, counter.Add(1))
	value, err = counter.ReadValue()
	require.NoError(t, err)
	require.Equal(t, uint64(1), value)
}

func TestDeadlineInterruptsPendingIO(t *testing.T) {
	for _, operation := range []string{"read", "write"} {
		t.Run(operation, func(t *testing.T) {
			counter := newCounter(t, 0, eventfd.Default)
			done := startWaiting(t, waitingOperation(t, counter, operation))

			require.NoError(t, counter.SetDeadline(time.Now().Add(-time.Second)))
			require.ErrorIs(t, awaitResult(t, done), os.ErrDeadlineExceeded)
			require.NoError(t, counter.SetDeadline(time.Time{}))
			if operation == "write" {
				_, err := counter.ReadValue()
				require.NoError(t, err)
			}
			require.NoError(t, counter.Add(1))
			value, err := counter.ReadValue()
			require.NoError(t, err)
			require.Equal(t, uint64(1), value, "clearing both deadlines must restore I/O")
		})
	}
}

func TestCloseInterruptsPendingIO(t *testing.T) {
	for _, operation := range []string{"read", "write"} {
		t.Run(operation, func(t *testing.T) {
			counter := newCounter(t, 0, eventfd.Default)
			done := startWaiting(t, waitingOperation(t, counter, operation))

			require.NoError(t, counter.Close())
			require.ErrorIs(t, awaitResult(t, done), os.ErrClosed)
			require.ErrorIs(t, counter.Close(), os.ErrClosed)
		})
	}
}

func TestZeroCounter(t *testing.T) {
	var counter eventfd.Counter
	checkUnavailableCounter(t, &counter, os.ErrInvalid)
	conn, err := counter.SyscallConn()
	require.Nil(t, conn)
	require.ErrorIs(t, err, os.ErrInvalid)
}

func TestClosedCounter(t *testing.T) {
	counter := newCounter(t, 0, eventfd.Default)
	conn, err := counter.SyscallConn()
	require.NoError(t, err)
	require.NoError(t, counter.Close())
	checkUnavailableCounter(t, counter, os.ErrClosed)

	called := false
	err = conn.Control(func(uintptr) { called = true })
	require.Error(t, err, "raw control must reject the closed descriptor")
	require.False(t, called, "callbacks must not receive a closed or reused descriptor")

	_, err = counter.Read(nil)
	require.ErrorIs(t, err, unix.EINVAL, "invalid sizes take precedence after close")
	_, err = counter.Write(nil)
	require.ErrorIs(t, err, unix.EINVAL)
}

func TestInvalidModesDoNotLeak(t *testing.T) {
	// Initialize the runtime poller before comparing descriptor counts.
	counter := newCounter(t, 0, eventfd.Default)
	require.NoError(t, counter.Close())
	before, err := os.ReadDir("/proc/self/fd")
	require.NoError(t, err)

	for _, mode := range []eventfd.Mode{2, 3, 255} {
		for range 16 {
			counter, err := eventfd.New(0, mode)
			require.Nil(t, counter)
			require.ErrorIs(t, err, os.ErrInvalid)
		}
	}

	after, err := os.ReadDir("/proc/self/fd")
	require.NoError(t, err)
	require.LessOrEqual(t, len(after), len(before), "rejected modes must not retain descriptors")
}

func TestConcurrentAdditions(t *testing.T) {
	counter := newCounter(t, 0, eventfd.Default)
	var workers sync.WaitGroup
	errors := make(chan error, 8)
	for range 8 {
		workers.Go(func() {
			for range 32 {
				if err := counter.Add(1); err != nil {
					errors <- err
					return
				}
			}
		})
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}

	value, err := counter.ReadValue()
	require.NoError(t, err)
	require.Equal(t, uint64(256), value, "concurrent additions must not be lost")
}

func newCounter(t *testing.T, initial uint32, mode eventfd.Mode) *eventfd.Counter {
	t.Helper()
	counter, err := eventfd.New(initial, mode)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := counter.Close(); !errors.Is(err, os.ErrClosed) {
			require.NoError(t, err)
		}
	})
	require.NoError(t, counter.SetDeadline(time.Now().Add(5*time.Second)), "bound unexpected I/O waits")
	return counter
}

func startWaiting(t *testing.T, operation func() error) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- operation() }()
	select {
	case err := <-done:
		t.Fatalf("operation returned before it could be unblocked: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	return done
}

func awaitResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("operation did not wake")
		return nil
	}
}

func waitingOperation(t *testing.T, counter *eventfd.Counter, operation string) func() error {
	t.Helper()
	if operation == "write" {
		require.NoError(t, counter.Add(math.MaxUint64-1))
		return func() error { return counter.Add(1) }
	}
	return func() error {
		_, err := counter.ReadValue()
		return err
	}
}

func checkUnavailableCounter(t *testing.T, counter *eventfd.Counter, expected error) {
	t.Helper()
	_, err := counter.Read(make([]byte, 8))
	require.ErrorIs(t, err, expected)
	_, err = counter.Write(make([]byte, 8))
	require.ErrorIs(t, err, expected)
	value, err := counter.ReadValue()
	require.Zero(t, value)
	require.ErrorIs(t, err, expected)
	require.ErrorIs(t, counter.Add(1), expected)
	require.ErrorIs(t, counter.SetDeadline(time.Time{}), expected)
	require.ErrorIs(t, counter.SetReadDeadline(time.Time{}), expected)
	require.ErrorIs(t, counter.SetWriteDeadline(time.Time{}), expected)
	require.ErrorIs(t, counter.Close(), expected)
}

func checkEmptyCounter(t *testing.T, counter *eventfd.Counter) {
	t.Helper()
	conn, err := counter.SyscallConn()
	require.NoError(t, err)
	var record [8]byte
	var readErr error
	err = conn.Control(func(fd uintptr) {
		_, readErr = unix.Read(int(fd), record[:])
	})
	require.NoError(t, err)
	require.ErrorIs(t, readErr, unix.EAGAIN, "the kernel count must be empty")
}

type readResult struct {
	err   error
	value uint64
}
