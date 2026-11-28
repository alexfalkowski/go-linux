//go:build linux

package file_test

import (
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/file"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

func TestFlockAllowsSharedLocksAcrossProcesses(t *testing.T) {
	f := test.TempFile(t, 0)
	require.NoError(t, file.Flock(f, file.LockShared|file.LockNonblock))
	_, child := test.Command(t, "TestFlockChild", "--", "flock-child", "shared", f.Name())

	output, err := child.CombinedOutput()
	require.NoError(t, err, "%s", output)
}

func TestFlockReleasesExclusiveLockOnProcessExit(t *testing.T) {
	f := test.TempFile(t, 0)
	_, child := test.Command(t, "TestFlockChild", "--", "flock-child", "hold", f.Name())
	child.Stdin, _ = test.Pipe(t)
	output := test.CaptureCommand(t, child)
	var marker [1]byte
	_, err := io.ReadFull(output, marker[:])
	require.NoError(t, err, "child must announce lock acquisition")
	require.Equal(t, byte('L'), marker[0])

	require.ErrorIs(t, file.Flock(f, file.LockExclusive|file.LockNonblock), test.ErrWouldBlock)

	require.NoError(t, child.Process.Kill())
	var exitError *exec.ExitError
	require.ErrorAs(t, child.Wait(), &exitError)
	require.Equal(t, test.KillSignal, test.ExitSignal(t, exitError.ProcessState))

	require.NoError(t, file.Flock(f, file.LockExclusive|file.LockNonblock))
}

func TestFlockBlockingAcquisitionContinuesAfterUnlock(t *testing.T) {
	f := test.TempFile(t, 0)
	require.NoError(t, file.Flock(f, file.LockExclusive|file.LockNonblock))
	_, child := test.Command(t, "TestFlockChild", "--", "flock-child", "wait", f.Name())
	output := test.CaptureCommand(t, child)
	var marker [1]byte
	_, err := io.ReadFull(output, marker[:])
	require.NoError(t, err, "child must announce its lock attempt")
	require.Equal(t, byte('W'), marker[0])

	require.NoError(t, output.SetReadDeadline(time.Now().Add(100*time.Millisecond)))
	_, err = io.ReadFull(output, marker[:])
	require.ErrorIs(t, err, os.ErrDeadlineExceeded, "child must wait while the lock is held")

	require.NoError(t, file.Flock(f, file.Unlock))
	require.NoError(t, output.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, err = io.ReadFull(output, marker[:])
	require.NoError(t, err, "child must acquire the released lock")
	require.Equal(t, byte('L'), marker[0])
	require.NoError(t, child.Wait())
}

func TestFlockChild(t *testing.T) {
	args := os.Args
	if len(args) < 3 || args[len(args)-3] != "flock-child" {
		return
	}
	f := test.OpenFile(t, args[len(args)-1])

	switch args[len(args)-2] {
	case "shared":
		require.NoError(t, file.Flock(f, file.LockShared|file.LockNonblock))
	case "hold":
		require.NoError(t, file.Flock(f, file.LockExclusive|file.LockNonblock))
		_, err := os.Stdout.WriteString("L")
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, os.Stdin)
		require.NoError(t, err)
	case "wait":
		_, err := os.Stdout.WriteString("W")
		require.NoError(t, err)
		require.NoError(t, file.Flock(f, file.LockExclusive))
		_, err = os.Stdout.WriteString("L")
		require.NoError(t, err)
	default:
		require.FailNow(t, "unknown flock child operation")
	}
}

func TestFlockDuplicateRetainsLockUntilFinalClose(t *testing.T) {
	f := test.TempFile(t, 0)
	fd := test.DuplicateDescriptor(t, f)
	duplicate := test.OwnDescriptor(t, fd, f.Name())
	contender := test.OpenFile(t, f.Name())
	require.NoError(t, file.Flock(f, file.LockExclusive|file.LockNonblock))

	require.NoError(t, f.Close())
	require.ErrorIs(t, file.Flock(contender, file.LockExclusive|file.LockNonblock), test.ErrWouldBlock)

	require.NoError(t, duplicate.Close())
	require.NoError(t, file.Flock(contender, file.LockExclusive|file.LockNonblock))
}

func TestFlockUnlockThroughDuplicate(t *testing.T) {
	f := test.TempFile(t, 0)
	fd := test.DuplicateDescriptor(t, f)
	duplicate := test.OwnDescriptor(t, fd, f.Name())
	contender := test.OpenFile(t, f.Name())
	require.NoError(t, file.Flock(f, file.LockExclusive|file.LockNonblock))
	require.ErrorIs(t, file.Flock(contender, file.LockExclusive|file.LockNonblock), test.ErrWouldBlock)

	require.NoError(t, file.Flock(duplicate, file.Unlock|file.LockNonblock))

	require.NoError(t, file.Flock(contender, file.LockExclusive|file.LockNonblock))
}

func TestFlockFailedUpgradeReleasesSharedLock(t *testing.T) {
	f := test.TempFile(t, 0)
	contender := test.OpenFile(t, f.Name())
	require.NoError(t, file.Flock(f, file.LockShared|file.LockNonblock))
	require.NoError(t, file.Flock(contender, file.LockShared|file.LockNonblock))

	require.ErrorIs(t, file.Flock(f, file.LockExclusive|file.LockNonblock), test.ErrWouldBlock)

	require.NoError(t, file.Flock(contender, file.LockExclusive|file.LockNonblock))
}

func TestFlockRejectsInvalidModesBeforeBorrowing(t *testing.T) {
	f := test.TempFile(t, 0)
	require.NoError(t, f.Close())

	modes := []file.LockMode{0, file.LockNonblock, file.LockShared | file.LockExclusive, file.LockShared | file.Unlock, file.LockExclusive | 1<<30}
	for _, mode := range modes {
		require.ErrorIs(t, file.Flock(f, mode), os.ErrInvalid, "mode %#x", mode)
	}
}

func TestFlockRejectsUnavailableFiles(t *testing.T) {
	f := test.TempFile(t, 0)
	require.NoError(t, f.Close())

	require.ErrorIs(t, file.Flock(nil, file.LockShared), os.ErrInvalid)
	require.ErrorIs(t, file.Flock(new(os.File), file.LockShared), os.ErrInvalid)
	require.ErrorIs(t, file.Flock(f, file.LockShared), os.ErrClosed)
}

func TestFlockPreservesPathHandleError(t *testing.T) {
	dir := test.OpenFile(t, t.TempDir())
	f, err := file.Open(dir, ".", file.OpenOptions{Flags: file.Path | file.Directory})
	require.NoError(t, err)
	test.Cleanup(t, f)

	err = file.Flock(f, file.LockShared|file.LockNonblock)
	require.ErrorIs(t, err, test.ErrBadDescriptor)
	var pathError *os.PathError
	require.ErrorAs(t, err, &pathError)
	require.Equal(t, "flock", pathError.Op)
	require.Equal(t, f.Name(), pathError.Path)

	_, err = f.Stat()
	require.NoError(t, err)
}
