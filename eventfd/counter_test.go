//go:build linux

package eventfd_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/eventfd"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

var _ io.ReadWriteCloser = (*eventfd.Counter)(nil)

func TestReadsReturnTheAccumulatedCountAndDrainIt(t *testing.T) {
	counter, fd := test.CounterDescriptor(t, math.MaxUint32, eventfd.Default)
	require.NoError(t, counter.Add(1<<40))
	require.Equal(t, uint64(math.MaxUint32)+1<<40, test.Value(t, counter.ReadValue))
	test.NoPendingRecord(t, fd)
}

func TestSemaphoreReadsConsumeOneUnitAtATime(t *testing.T) {
	counter, fd := test.CounterDescriptor(t, 2, eventfd.Semaphore)
	require.NoError(t, counter.Add(2))
	for range 4 {
		require.Equal(t, uint64(1), test.Value(t, counter.ReadValue))
	}
	test.NoPendingRecord(t, fd)
}

func TestByteWritesAddOneNativeEndianValue(t *testing.T) {
	counter := test.Counter(t, 0, eventfd.Default)
	const value = uint64(0x0102030405060708)
	var record [8]byte
	binary.NativeEndian.PutUint64(record[:], value)

	n, err := counter.Write(record[:])
	require.NoError(t, err)
	require.Equal(t, len(record), n)
	require.Equal(t, value, test.Value(t, counter.ReadValue))
}

func TestByteReadsReturnOneRecordAndPreserveTheBufferTail(t *testing.T) {
	const value = uint64(0x0102030405060708)
	counter := test.Counter(t, 0, eventfd.Default)
	require.NoError(t, counter.Add(value))
	buffer := bytes.Repeat([]byte{0xff}, 16)
	expected := bytes.Clone(buffer)
	binary.NativeEndian.PutUint64(expected, value)

	n, err := counter.Read(buffer)
	require.NoError(t, err)
	require.Equal(t, 8, n)
	require.Equal(t, expected, buffer, "one native-endian record followed by the untouched tail")
}

func TestShortReadsFailWithoutConsumingTheCount(t *testing.T) {
	for size := range 8 {
		t.Run(fmt.Sprintf("%d byte buffer", size), func(t *testing.T) {
			counter := test.Counter(t, 5, eventfd.Default)
			n, err := counter.Read(make([]byte, size))
			require.Zero(t, n)
			require.ErrorIs(t, err, unix.EINVAL)

			require.Equal(t, uint64(5), test.Value(t, counter.ReadValue), "invalid reads must not consume the counter")
		})
	}
}

func TestWrongSizedWritesFailWithoutChangingTheCount(t *testing.T) {
	for _, size := range []int{0, 1, 7, 9, 16} {
		t.Run(fmt.Sprintf("%d byte buffer", size), func(t *testing.T) {
			counter := test.Counter(t, 5, eventfd.Default)
			n, err := counter.Write(make([]byte, size))
			require.Zero(t, n)
			require.ErrorIs(t, err, unix.EINVAL)

			var pathErr *os.PathError
			require.ErrorAs(t, err, &pathErr)
			require.Equal(t, "write", pathErr.Op)

			require.Equal(t, uint64(5), test.Value(t, counter.ReadValue), "invalid writes must not change the counter")
		})
	}
}

func TestMaximumUint64AdditionsFailWithoutChangingTheCount(t *testing.T) {
	counter := test.Counter(t, 5, eventfd.Default)
	require.ErrorIs(t, counter.Add(math.MaxUint64), unix.EINVAL)

	var record [8]byte
	binary.NativeEndian.PutUint64(record[:], math.MaxUint64)
	n, err := counter.Write(record[:])
	require.Zero(t, n)
	require.ErrorIs(t, err, unix.EINVAL)

	var errno syscall.Errno
	require.ErrorAs(t, err, &errno)
	require.Equal(t, unix.EINVAL, errno)

	require.Equal(t, uint64(5), test.Value(t, counter.ReadValue), "forbidden values must not change the counter")
}

func TestCreatedCountersAreNonblockingAndClosedOnExec(t *testing.T) {
	_, fd := test.CounterDescriptor(t, 0, eventfd.Default)
	test.CloseOnExec(t, fd)
	require.NotZero(t, test.Fcntl(t, fd, unix.F_GETFL)&unix.O_NONBLOCK)
	test.NoPendingRecord(t, fd)
}

func TestZeroAdditionsPreserveEmptyAndMaximumCounters(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		value uint64
	}{
		{name: "empty counter"},
		{name: "maximum counter", value: math.MaxUint64 - 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			counter, fd := test.CounterDescriptor(t, 0, eventfd.Default)

			require.NoError(t, counter.Add(scenario.value))
			require.NoError(t, counter.Add(0))

			if scenario.value == 0 {
				test.NoPendingRecord(t, fd)
				return
			}

			require.Equal(t, scenario.value, test.Value(t, counter.ReadValue))
		})
	}
}

func TestClosingCountersReleasesTheirDescriptors(t *testing.T) {
	counter := test.Counter(t, 0, eventfd.Default)
	require.NoError(t, counter.Close())
	before := test.DescriptorCount(t)

	for range 32 {
		counter, err := eventfd.New(0, eventfd.Default)
		require.NoError(t, err)
		require.NoError(t, counter.Close())
	}

	after := test.DescriptorCount(t)
	require.LessOrEqual(t, after, before, "Close must release owned descriptors")
}

func TestReadsWaitUntilTheCounterIsIncremented(t *testing.T) {
	counter := test.Counter(t, 0, eventfd.Default)
	done := test.StartRead(t, counter.ReadValue)
	require.NoError(t, counter.Add(7))
	require.Equal(t, test.ReadResult[uint64]{Value: 7}, test.Receive(t, done))
}

func TestFullCounterWritesWaitUntilTheCountIsDrained(t *testing.T) {
	counter := test.Counter(t, 0, eventfd.Default)
	require.NoError(t, counter.Add(math.MaxUint64-2))
	done := test.Start(t, func() error {
		return counter.Add(2)
	})

	require.Equal(t, uint64(math.MaxUint64-2), test.Value(t, counter.ReadValue), "waiting additions do not partially commit")
	require.NoError(t, test.Receive(t, done))
	require.Equal(t, uint64(2), test.Value(t, counter.ReadValue), "the whole addition commits after draining")
}

func TestClearingAReadDeadlineRestoresReadsWithoutLosingTheCount(t *testing.T) {
	counter := test.Counter(t, 0, eventfd.Default)
	require.NoError(t, counter.SetReadDeadline(time.Now().Add(30*time.Millisecond)))
	value, err := counter.ReadValue()
	require.Zero(t, value)
	require.ErrorIs(t, err, os.ErrDeadlineExceeded)
	require.True(t, os.IsTimeout(err), "deadline failures must expose timeout semantics")

	require.NoError(t, counter.Add(9), "a read deadline must not expire writes")
	_, err = counter.ReadValue()
	require.ErrorIs(t, err, os.ErrDeadlineExceeded, "expired read deadlines persist")
	require.NoError(t, counter.SetReadDeadline(time.Time{}))
	require.Equal(t, uint64(9), test.Value(t, counter.ReadValue), "clearing the deadline must restore reads without data loss")
}

func TestClearingAWriteDeadlineRestoresWritesWithoutChangingTheCount(t *testing.T) {
	counter := test.Counter(t, 0, eventfd.Default)
	require.NoError(t, counter.Add(math.MaxUint64-1))
	require.NoError(t, counter.SetWriteDeadline(time.Now().Add(30*time.Millisecond)))
	require.ErrorIs(t, counter.Add(1), os.ErrDeadlineExceeded)

	require.Equal(t, uint64(math.MaxUint64-1), test.Value(t, counter.ReadValue), "a timed-out addition must not change the count")
	require.ErrorIs(t, counter.Add(1), os.ErrDeadlineExceeded, "expired write deadlines persist")
	require.NoError(t, counter.SetWriteDeadline(time.Time{}))
	require.NoError(t, counter.Add(1))
	require.Equal(t, uint64(1), test.Value(t, counter.ReadValue))
}

func TestExpiredDeadlinesInterruptPendingIOAndCanBeCleared(t *testing.T) {
	for _, operation := range []string{"read", "write"} {
		t.Run(operation, func(t *testing.T) {
			counter := test.Counter(t, 0, eventfd.Default)
			done := test.Start(t, waitingOperation(t, counter, operation))

			require.NoError(t, counter.SetDeadline(time.Now().Add(-time.Second)))
			require.ErrorIs(t, test.Receive(t, done), os.ErrDeadlineExceeded)
			require.NoError(t, counter.SetDeadline(time.Time{}))

			if operation == "write" {
				_, err := counter.ReadValue()
				require.NoError(t, err)
			}

			require.NoError(t, counter.Add(1))
			require.Equal(t, uint64(1), test.Value(t, counter.ReadValue), "clearing both deadlines must restore I/O")
		})
	}
}

func TestClosingACounterInterruptsPendingIO(t *testing.T) {
	for _, operation := range []string{"read", "write"} {
		t.Run(operation, func(t *testing.T) {
			counter := test.Counter(t, 0, eventfd.Default)
			done := test.Start(t, waitingOperation(t, counter, operation))

			require.NoError(t, counter.Close())
			require.ErrorIs(t, test.Receive(t, done), os.ErrClosed)
			require.ErrorIs(t, counter.Close(), os.ErrClosed)
		})
	}
}

func TestZeroCountersRejectOperations(t *testing.T) {
	var counter eventfd.Counter
	checkUnavailableCounter(t, &counter, os.ErrInvalid)
}

func TestClosedCountersRejectOperations(t *testing.T) {
	counter := test.Counter(t, 0, eventfd.Default)
	require.NoError(t, counter.Close())
	checkUnavailableCounter(t, counter, os.ErrClosed)

	_, err := counter.Read(nil)
	require.ErrorIs(t, err, unix.EINVAL, "invalid sizes take precedence after close")
	_, err = counter.Write(nil)
	require.ErrorIs(t, err, unix.EINVAL)
}

func TestUnknownCounterModesFailWithoutRetainingDescriptors(t *testing.T) {
	// Initialize the runtime poller before comparing descriptor counts.
	counter := test.Counter(t, 0, eventfd.Default)
	require.NoError(t, counter.Close())
	before := test.DescriptorCount(t)

	for _, scenario := range []struct {
		name string
		mode eventfd.Mode
	}{
		{name: "first unsupported mode", mode: 2},
		{name: "mixed mode bits", mode: 3},
		{name: "all mode bits", mode: 255},
	} {
		for range 16 {
			counter, err := eventfd.New(0, scenario.mode)
			require.Nil(t, counter, scenario.name)
			require.ErrorIs(t, err, os.ErrInvalid, scenario.name)
		}
	}

	after := test.DescriptorCount(t)
	require.LessOrEqual(t, after, before, "rejected modes must not retain descriptors")
}

func TestConcurrentAdditionsAreNotLost(t *testing.T) {
	counter := test.Counter(t, 0, eventfd.Default)
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

	require.Equal(t, uint64(256), test.Value(t, counter.ReadValue), "concurrent additions must not be lost")
}

func waitingOperation(t *testing.T, counter *eventfd.Counter, operation string) func() error {
	t.Helper()

	if operation == "write" {
		require.NoError(t, counter.Add(math.MaxUint64-1))
		return func() error {
			return counter.Add(1)
		}
	}
	return func() error {
		_, err := counter.ReadValue()
		return err
	}
}

func checkUnavailableCounter(t *testing.T, counter *eventfd.Counter, expected error) {
	t.Helper()

	for _, scenario := range []struct {
		name      string
		operation func([]byte) (int, error)
	}{
		{name: "byte read", operation: counter.Read},
		{name: "byte write", operation: counter.Write},
	} {
		n, err := scenario.operation(make([]byte, 8))
		require.Zero(t, n, scenario.name)
		require.ErrorIs(t, err, expected, scenario.name)
	}

	value, err := counter.ReadValue()
	require.Zero(t, value, "typed read")
	require.ErrorIs(t, err, expected, "typed read")

	for _, scenario := range []struct {
		name      string
		operation func() error
	}{
		{name: "addition", operation: func() error {
			return counter.Add(1)
		}},

		{name: "shared deadline", operation: func() error {
			return counter.SetDeadline(time.Time{})
		}},

		{name: "read deadline", operation: func() error {
			return counter.SetReadDeadline(time.Time{})
		}},

		{name: "write deadline", operation: func() error {
			return counter.SetWriteDeadline(time.Time{})
		}},

		{name: "close", operation: counter.Close},
	} {
		require.ErrorIs(t, scenario.operation(), expected, scenario.name)
	}
}
