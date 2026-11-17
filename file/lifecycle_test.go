//go:build linux

package file_test

import (
	"errors"
	"os"
	"testing"

	"github.com/alexfalkowski/go-linux/file"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

func TestUnavailableAnchorsRejectEveryOperation(t *testing.T) {
	closed := test.OpenFile(t, t.TempDir())
	require.NoError(t, closed.Close())

	live := test.OpenFile(t, t.TempDir())
	for _, scenario := range []struct {
		wantErr error
		dir     *os.File
		name    string
	}{
		{name: "nil", wantErr: os.ErrInvalid},
		{name: "closed", dir: closed, wantErr: os.ErrClosed},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			opened, err := file.Open(scenario.dir, ".", file.OpenOptions{})
			require.ErrorIs(t, err, scenario.wantErr)
			require.Nil(t, opened)

			metadata, err := file.Stat(scenario.dir, file.MaskBasic)
			require.ErrorIs(t, err, scenario.wantErr)
			require.Zero(t, metadata)

			require.ErrorIs(t, file.Rename(scenario.dir, "a", live, "b", file.Replace), scenario.wantErr)
			require.ErrorIs(t, file.Rename(live, "a", scenario.dir, "b", file.Replace), scenario.wantErr)
		})
	}
}

func TestRegularFilesCannotAnchorChildOperations(t *testing.T) {
	live := test.OpenFile(t, t.TempDir())
	regular := test.TempFile(t, 0)
	opened, err := file.Open(regular, "child", file.OpenOptions{})
	require.ErrorIs(t, err, test.ErrNotDirectory)
	require.Nil(t, opened)

	_, err = file.StatAt(regular, "child", file.StatOptions{})
	require.ErrorIs(t, err, test.ErrNotDirectory)

	require.ErrorIs(t, file.Rename(live, ".", regular, "child", file.Replace), test.ErrNotDirectory)
}

func TestOpenedDescriptorsAreCloseOnExec(t *testing.T) {
	dir := test.OpenFile(t, t.TempDir())
	before := test.Descriptors(t)
	opened, err := file.Open(dir, ".", file.OpenOptions{Flags: file.Path | file.Directory})
	require.NoError(t, err)
	test.Cleanup(t, opened)
	test.CloseOnExec(t, test.NewDescriptor(t, before))
	require.NoError(t, opened.Close())
}

func TestSuccessfulAndFailedOpensReleaseDescriptors(t *testing.T) {
	dir := test.OpenFile(t, t.TempDir())
	before := test.Descriptors(t)

	for range 25 {
		opened, err := file.Open(dir, ".", file.OpenOptions{})
		require.NoError(t, err)

		require.NoError(t, opened.Close())

		opened, err = file.Open(dir, "absent", file.OpenOptions{Resolve: file.Beneath})
		require.ErrorIs(t, err, test.ErrNotExist)
		require.Nil(t, opened)
	}

	require.Equal(t, before, test.Descriptors(t), "successful and failed opens must release every descriptor")
}

func TestConcurrentAnchorCloseCannotLeakOrRetargetOperations(t *testing.T) {
	before := test.Descriptors(t)
	root := t.TempDir()
	for range 30 {
		dir := test.OpenFile(t, root)
		start := make(chan struct{})
		results := make(chan error, 2)
		rename := make(chan error, 1)
		go func() {
			<-start
			opened, err := file.Open(dir, ".", file.OpenOptions{Flags: file.Path})
			if err != nil {
				results <- err
				return
			} else {
				results <- opened.Close()
			}
		}()

		go func() {
			<-start
			_, err := file.Stat(dir, file.MaskBasic)
			results <- err
		}()

		go func() {
			<-start
			rename <- file.Rename(dir, "absent", dir, "other", file.NoReplace)
		}()

		close(start)
		require.NoError(t, dir.Close())

		for range 2 {
			err := test.Receive(t, results)
			if !errors.Is(err, os.ErrClosed) {
				require.NoError(t, err)
			}
		}

		if err := test.Receive(t, rename); !errors.Is(err, os.ErrClosed) {
			require.ErrorIs(t, err, test.ErrNotExist)
		}
	}

	require.Equal(t, before, test.Descriptors(t))
}

func TestOpenExhaustionLeavesBorrowedAnchorUsable(t *testing.T) {
	if os.Args[len(os.Args)-1] != "file-limit" {
		ctx, command := test.Command(t, "TestOpenExhaustionLeavesBorrowedAnchorUsable", "-test.v", "--", "file-limit")
		output, err := command.CombinedOutput()
		require.NoError(t, ctx.Err())
		require.NoError(t, err, "descriptor-limit subprocess: %s", output)
		require.Contains(t, string(output), "verified open exhaustion")
		return
	}

	dir := test.OpenFile(t, t.TempDir())
	freeSlot := test.ExhaustDescriptors(t)
	opened, err := file.Open(dir, ".", file.OpenOptions{})
	require.ErrorIs(t, err, test.ErrTooManyOpenFiles)
	require.Nil(t, opened)

	_, err = file.Stat(dir, file.MaskBasic)
	require.NoError(t, err, "failed open must not consume its borrowed directory")

	freeSlot()
	opened, err = file.Open(dir, ".", file.OpenOptions{})
	require.NoError(t, err)

	require.NoError(t, opened.Close())

	t.Log("verified open exhaustion")
}
