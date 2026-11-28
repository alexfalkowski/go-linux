//go:build linux

package file_test

import (
	"io"
	"math"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/file"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

func TestRecordLockQueriesAnotherProcess(t *testing.T) {
	cases := []struct {
		name      string
		holder    file.RecordLockScope
		contender file.RecordLockScope
	}{
		{name: "POSIX owner", holder: file.POSIXLocks, contender: file.OFDLocks},
		{name: "OFD owner", holder: file.OFDLocks, contender: file.POSIXLocks},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := test.TempFile(t, 32)
			_, child := test.Command(t, "TestRecordLockChild", "--", "record-lock-child", "hold", strconv.Itoa(int(tc.holder)), "0", f.Name())
			child.Stdin, _ = test.Pipe(t)
			output := test.CaptureCommand(t, child)
			require.Equal(t, byte('L'), test.ChildMarker(t, output))

			write := file.RecordLock{Type: file.LockWrite, Range: file.LockRange{Length: 8}}
			read := file.RecordLock{Type: file.LockRead, Range: file.LockRange{Start: 4, Length: 4}}
			var pid int
			if tc.holder == file.POSIXLocks {
				pid = child.Process.Pid
			} else {
				pid = -1
			}
			conflict, found, err := file.QueryRecordLock(f, tc.contender, read)
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, file.RecordLockConflict{Lock: write, PID: pid}, conflict)
			require.ErrorIs(t, file.SetRecordLock(f, tc.contender, read), test.ErrTryAgain)
			_, err = f.ReadAt(make([]byte, 1), read.Range.Start)
			require.NoError(t, err, "advisory contention leaves the file usable")

			read.Range.Start = 8
			conflict, found, err = file.QueryRecordLock(f, tc.contender, read)
			require.NoError(t, err)
			require.False(t, found, "the adjacent range is available")
			require.Zero(t, conflict)
			require.NoError(t, file.SetRecordLock(f, tc.contender, read))
		})
	}
}

func TestRecordLockQueryLeavesRangeAvailable(t *testing.T) {
	f := test.TempFile(t, 8)
	other := test.OpenWritableFile(t, f.Name())
	lock := file.RecordLock{Type: file.LockWrite, Range: file.LockRange{Length: 8}}

	conflict, found, err := file.QueryRecordLock(f, file.OFDLocks, lock)
	require.NoError(t, err)
	require.False(t, found)
	require.Zero(t, conflict)

	require.NoError(t, file.SetRecordLock(other, file.OFDLocks, lock))
}

func TestRecordLockWaitsForRelease(t *testing.T) {
	for _, tc := range []struct {
		name  string
		scope file.RecordLockScope
	}{{name: "POSIX", scope: file.POSIXLocks}, {name: "OFD", scope: file.OFDLocks}} {
		t.Run(tc.name, func(t *testing.T) {
			f := test.TempFile(t, 8)
			lock := file.RecordLock{Type: file.LockWrite, Range: file.LockRange{Length: 8}}
			require.NoError(t, file.SetRecordLock(f, tc.scope, lock))
			_, child := test.Command(t, "TestRecordLockChild", "--", "record-lock-child", "wait", strconv.Itoa(int(tc.scope)), "0", f.Name())
			output := test.CaptureCommand(t, child)
			require.Equal(t, byte('W'), test.ChildMarker(t, output))

			require.NoError(t, output.SetReadDeadline(time.Now().Add(100*time.Millisecond)))
			var marker [1]byte
			_, err := io.ReadFull(output, marker[:])
			require.ErrorIs(t, err, os.ErrDeadlineExceeded, "the pending acquisition must wait")

			lock.Type = file.LockRelease
			require.NoError(t, file.SetRecordLock(f, tc.scope, lock))
			require.NoError(t, output.SetReadDeadline(time.Now().Add(5*time.Second)))
			require.Equal(t, byte('L'), test.ChildMarker(t, output))
			require.NoError(t, child.Wait())
		})
	}
}

func TestRecordLockPOSIXDetectsDeadlock(t *testing.T) {
	f := test.TempFile(t, 16)
	scope := strconv.Itoa(int(file.POSIXLocks))
	_, first := test.Command(t, "TestRecordLockChild", "--", "record-lock-child", "deadlock", scope, "0", f.Name())
	_, second := test.Command(t, "TestRecordLockChild", "--", "record-lock-child", "deadlock", scope, "8", f.Name())
	firstInput, firstStart := test.Pipe(t)
	secondInput, secondStart := test.Pipe(t)
	first.Stdin = firstInput
	second.Stdin = secondInput
	firstOutput := test.CaptureCommand(t, first)
	secondOutput := test.CaptureCommand(t, second)
	require.Equal(t, byte('L'), test.ChildMarker(t, firstOutput))
	require.Equal(t, byte('L'), test.ChildMarker(t, secondOutput))

	_, err := firstStart.WriteString("W")
	require.NoError(t, err)
	_, err = secondStart.WriteString("W")
	require.NoError(t, err)

	results := []byte{test.ChildMarker(t, firstOutput), test.ChildMarker(t, secondOutput)}
	require.ElementsMatch(t, []byte{'D', 'L'}, results, "one waiter detects deadlock and the other acquires")
	require.NoError(t, first.Wait())
	require.NoError(t, second.Wait())
}

func TestRecordLockUsesSignedRangesAndOrigins(t *testing.T) {
	cases := []struct {
		name     string
		request  file.LockRange
		absolute file.LockRange
	}{
		{name: "backward from current offset", request: file.LockRange{Start: -2, Length: -4, Whence: io.SeekCurrent}, absolute: file.LockRange{Start: 26, Length: 4}},
		{name: "forward from end", request: file.LockRange{Start: -4, Length: 3, Whence: io.SeekEnd}, absolute: file.LockRange{Start: 60, Length: 3}},
		{name: "zero length beyond EOF", request: file.LockRange{Start: 1 << 33}, absolute: file.LockRange{Start: 1 << 33}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := test.TempFile(t, 64)
			observer := test.OpenFile(t, f.Name())
			_, err := f.Seek(32, io.SeekStart)
			require.NoError(t, err)

			lock := file.RecordLock{Type: file.LockWrite, Range: tc.request}
			require.NoError(t, file.SetRecordLock(f, file.OFDLocks, lock))
			probe := file.RecordLock{Type: file.LockRead, Range: file.LockRange{Start: tc.absolute.Start + 1, Length: 1}}
			conflict, found, err := file.QueryRecordLock(observer, file.OFDLocks, probe)
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, tc.absolute, conflict.Lock.Range)

			position, err := f.Seek(0, io.SeekCurrent)
			require.NoError(t, err)
			require.EqualValues(t, 32, position, "resolving the range preserves the offset")
		})
	}
}

func TestRecordLockReleasesOnlyTheRequestedBytes(t *testing.T) {
	f := test.TempFile(t, 16)
	observer := test.OpenFile(t, f.Name())
	lock := file.RecordLock{Type: file.LockWrite, Range: file.LockRange{Length: 16}}
	require.NoError(t, file.SetRecordLock(f, file.OFDLocks, lock))

	release := file.RecordLock{Type: file.LockRelease, Range: file.LockRange{Start: 4, Length: 8}}
	require.NoError(t, file.SetRecordLock(f, file.OFDLocks, release))

	for _, tc := range []struct {
		name   string
		offset int64
		locked bool
	}{{name: "before", offset: 3, locked: true}, {name: "within", offset: 4}, {name: "after", offset: 12, locked: true}} {
		t.Run(tc.name, func(t *testing.T) {
			probe := file.RecordLock{Type: file.LockRead, Range: file.LockRange{Start: tc.offset, Length: 1}}
			_, found, err := file.QueryRecordLock(observer, file.OFDLocks, probe)
			require.NoError(t, err)
			require.Equal(t, tc.locked, found)
		})
	}
}

func TestRecordLockFailedConversionRetainsReadLock(t *testing.T) {
	f := test.TempFile(t, 16)
	other := test.OpenWritableFile(t, f.Name())
	read := file.RecordLock{Type: file.LockRead, Range: file.LockRange{Length: 8}}
	write := file.RecordLock{Type: file.LockWrite, Range: read.Range}
	require.NoError(t, file.SetRecordLock(f, file.OFDLocks, read))
	require.NoError(t, file.SetRecordLock(other, file.OFDLocks, read))

	require.ErrorIs(t, file.SetRecordLock(f, file.OFDLocks, write), test.ErrTryAgain)
	conflict, found, err := file.QueryRecordLock(other, file.OFDLocks, write)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, read, conflict.Lock)
}

func TestRecordLockClosingAnotherHandleReleasesPOSIXLocks(t *testing.T) {
	f := test.TempFile(t, 16)
	other := test.OpenFile(t, f.Name())
	lock := file.RecordLock{Type: file.LockWrite, Range: file.LockRange{Start: 8, Length: 8}}
	require.NoError(t, file.SetRecordLock(f, file.POSIXLocks, lock))

	_, found, err := file.QueryRecordLock(f, file.POSIXLocks, lock)
	require.NoError(t, err)
	require.False(t, found, "a POSIX query ignores this process's locks")
	_, found, err = file.QueryRecordLock(f, file.OFDLocks, lock)
	require.NoError(t, err)
	require.True(t, found, "an OFD query detects the process's lock")

	require.NoError(t, other.Close())

	_, found, err = file.QueryRecordLock(f, file.OFDLocks, lock)
	require.NoError(t, err)
	require.False(t, found, "closing an unrelated open releases the process's lock")
}

func TestRecordLockOFDRemainsUntilFinalClose(t *testing.T) {
	f := test.TempFile(t, 16)
	duplicate := test.OwnDescriptor(t, test.DuplicateDescriptor(t, f), f.Name())
	other := test.OpenFile(t, f.Name())
	lock := file.RecordLock{Type: file.LockWrite, Range: file.LockRange{Length: 8}}
	require.NoError(t, file.SetRecordLock(f, file.OFDLocks, lock))

	_, found, err := file.QueryRecordLock(duplicate, file.OFDLocks, lock)
	require.NoError(t, err)
	require.False(t, found, "duplicates share the lock owner")
	require.NoError(t, f.Close())

	_, found, err = file.QueryRecordLock(other, file.OFDLocks, lock)
	require.NoError(t, err)
	require.True(t, found, "an independent open still conflicts while the duplicate is open")
	require.NoError(t, duplicate.Close())

	_, found, err = file.QueryRecordLock(other, file.OFDLocks, lock)
	require.NoError(t, err)
	require.False(t, found, "the last reference releases the lock")
}

func TestRecordLockInvalidOptionsPrecedeFileAccess(t *testing.T) {
	f := test.TempFile(t, 0)
	require.NoError(t, f.Close())

	require.ErrorIs(t, file.SetRecordLock(f, 0, file.RecordLock{}), os.ErrInvalid)
	require.ErrorIs(t, file.SetRecordLock(f, file.POSIXLocks, file.RecordLock{Type: 100}), os.ErrInvalid)
	require.ErrorIs(t, file.SetRecordLock(f, file.OFDLocks, file.RecordLock{Range: file.LockRange{Whence: 1 << 16}}), os.ErrInvalid)
	_, _, err := file.QueryRecordLock(f, file.OFDLocks, file.RecordLock{Type: file.LockRelease})
	require.ErrorIs(t, err, os.ErrInvalid)
}

func TestRecordLockRejectsUnavailableFiles(t *testing.T) {
	f := test.TempFile(t, 0)
	require.NoError(t, f.Close())
	lock := file.RecordLock{Type: file.LockRead}

	require.ErrorIs(t, file.SetRecordLock(nil, file.POSIXLocks, lock), os.ErrInvalid)
	require.ErrorIs(t, file.SetRecordLock(new(os.File), file.OFDLocks, lock), os.ErrInvalid)
	require.ErrorIs(t, file.SetRecordLock(f, file.POSIXLocks, lock), os.ErrClosed)
}

func TestRecordLockPreservesKernelRangeErrors(t *testing.T) {
	f := test.TempFile(t, 8)

	beforeStart := file.RecordLock{Type: file.LockWrite, Range: file.LockRange{Start: -1}}
	require.ErrorIs(t, file.SetRecordLock(f, file.POSIXLocks, beforeStart), test.ErrInvalidArgument)
	overflow := file.RecordLock{Type: file.LockWrite, Range: file.LockRange{Start: math.MaxInt64, Length: 2}}
	require.ErrorIs(t, file.SetRecordLock(f, file.OFDLocks, overflow), test.ErrOverflow)
}

func TestRecordLockPreservesAccessError(t *testing.T) {
	f := test.TempFile(t, 8)
	reader := test.OpenFile(t, f.Name())

	err := file.SetRecordLock(reader, file.OFDLocks, file.RecordLock{Type: file.LockWrite})
	require.ErrorIs(t, err, test.ErrBadDescriptor)
	var pathError *os.PathError
	require.ErrorAs(t, err, &pathError)
	require.Equal(t, "fcntl", pathError.Op)
	require.Equal(t, reader.Name(), pathError.Path)

	require.NoError(t, file.SetRecordLock(reader, file.OFDLocks, file.RecordLock{Type: file.LockRead}))
}

func TestRecordLockChild(t *testing.T) {
	args := os.Args
	if len(args) < 5 || args[len(args)-5] != "record-lock-child" {
		return
	}
	scope, err := strconv.Atoi(args[len(args)-3])
	require.NoError(t, err)
	start, err := strconv.ParseInt(args[len(args)-2], 10, 64)
	require.NoError(t, err)
	f := test.OpenWritableFile(t, args[len(args)-1])
	lock := file.RecordLock{Type: file.LockWrite, Range: file.LockRange{Start: start, Length: 8}}

	switch args[len(args)-4] {
	case "hold":
		require.NoError(t, file.SetRecordLock(f, file.RecordLockScope(scope), lock))
		_, err := os.Stdout.WriteString("L")
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, os.Stdin)
		require.NoError(t, err)
	case "wait":
		_, err := os.Stdout.WriteString("W")
		require.NoError(t, err)
		require.NoError(t, file.WaitRecordLock(f, file.RecordLockScope(scope), lock))
		_, err = os.Stdout.WriteString("L")
		require.NoError(t, err)
	case "deadlock":
		require.NoError(t, file.SetRecordLock(f, file.POSIXLocks, lock))
		_, err := os.Stdout.WriteString("L")
		require.NoError(t, err)
		require.Equal(t, byte('W'), test.ChildMarker(t, os.Stdin))

		lock.Range.Start = 8 - start
		err = file.WaitRecordLock(f, file.POSIXLocks, lock)
		if err != nil {
			require.ErrorIs(t, err, test.ErrDeadlock)
			_, err := os.Stdout.WriteString("D")
			require.NoError(t, err)
			return
		}
		_, err = os.Stdout.WriteString("L")
		require.NoError(t, err)
	default:
		require.FailNow(t, "unknown record lock child operation")
	}
}
