//go:build linux

package memfd_test

import (
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/memfd"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

var (
	_ io.ReadWriteSeeker = (*memfd.File)(nil)
	_ io.ReaderAt        = (*memfd.File)(nil)
	_ io.WriterAt        = (*memfd.File)(nil)
	_ io.Closer          = (*memfd.File)(nil)
)

func TestStreamIO(t *testing.T) {
	file := newFile(t, memfd.Options{})

	_, err := file.WriteString("hello")
	require.NoError(t, err)

	_, err = file.Seek(0, io.SeekStart)
	require.NoError(t, err)

	contents, err := io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, "hello", string(contents))
}

func TestRandomAccessAndSizing(t *testing.T) {
	file := newFile(t, memfd.Options{})

	_, err := file.WriteAt([]byte("Hello"), 0)
	require.NoError(t, err)
	require.NoError(t, file.Truncate(8))

	contents := make([]byte, 8)
	n, err := file.ReadAt(contents, 0)
	require.NoError(t, err)
	require.Equal(t, len(contents), n, "read the complete grown file")
	require.Equal(t, []byte{'H', 'e', 'l', 'l', 'o', 0, 0, 0}, contents, "growth must zero-fill")

	require.NoError(t, file.Truncate(2))
	stat, err := file.Stat()
	require.NoError(t, err)
	require.Equal(t, int64(2), stat.Size(), "shrinking must change the file size")
}

func TestInvalidFileOperations(t *testing.T) {
	file := newFile(t, memfd.Options{})

	require.ErrorIs(t, file.Truncate(-1), unix.EINVAL)
	require.ErrorIs(t, file.SetDeadline(time.Time{}), os.ErrNoDeadline)
}

func TestCreationDefaults(t *testing.T) {
	file := newFile(t, memfd.Options{})

	stat, err := file.Stat()
	require.NoError(t, err)
	require.Zero(t, stat.Size(), "new files must be empty")
	require.True(t, stat.Mode().IsRegular(), "memfd must support ordinary regular-file operations")
	require.Zero(t, stat.Mode().Perm()&0o111, "default execute bits must be clear")
	checkSeals(t, file, memfd.SealExec)

	require.ErrorIs(t, file.Chmod(0o700), unix.EPERM, "default execute bits must be locked")

	require.NoError(t, file.AddSeals(memfd.SealExec))
	_, err = file.WriteString("still writable")
	require.NoError(t, err, "SealExec on a non-executable file must preserve writes")
}

func TestCloseOnExec(t *testing.T) {
	file := newFile(t, memfd.Options{})

	conn, err := file.SyscallConn()
	require.NoError(t, err)

	var flags int
	var callErr error
	err = conn.Control(func(fd uintptr) {
		flags, callErr = unix.FcntlInt(fd, unix.F_GETFD, 0)
	})
	require.NoError(t, err)
	require.NoError(t, callErr)
	require.NotZero(t, flags&unix.FD_CLOEXEC, "files must not leak across exec")
}

func TestNames(t *testing.T) {
	for _, test := range []struct{ name, label string }{
		{name: "empty"},
		{name: "slash", label: "debug/label"},
		{name: "maximum bytes", label: strings.Repeat("a", 249)},
		{name: "multibyte maximum", label: strings.Repeat("é", 124) + "a"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file, err := memfd.New(test.label, memfd.Options{})
			require.NoError(t, err)
			require.NoError(t, file.Close())

			require.Equal(t, test.label, file.Name())
		})
	}
}

func TestConstructorErrors(t *testing.T) {
	for _, name := range []string{"invalid\x00label", strings.Repeat("a", 250), strings.Repeat("é", 125)} {
		file, err := memfd.New(name, memfd.Options{})
		require.Nil(t, file, "failed construction must not return an owner")
		require.ErrorIs(t, err, unix.EINVAL)

		var pathErr *os.PathError
		require.ErrorAs(t, err, &pathErr)
		require.Equal(t, "memfd_create", pathErr.Op)
		require.Equal(t, name, pathErr.Path)
	}
}

func TestConstructorFailuresDoNotLeak(t *testing.T) {
	before, err := os.ReadDir("/proc/self/fd")
	require.NoError(t, err)

	for range 32 {
		file, err := memfd.New(strings.Repeat("a", 250), memfd.Options{})
		require.Nil(t, file)
		require.ErrorIs(t, err, unix.EINVAL)
	}

	after, err := os.ReadDir("/proc/self/fd")
	require.NoError(t, err)
	require.LessOrEqual(t, len(after), len(before), "constructor failures must not leak descriptors")
}

func TestSizeSeals(t *testing.T) {
	for _, test := range []struct {
		name            string
		seal            memfd.Seals
		denied, allowed int64
	}{
		{name: "shrink", seal: memfd.SealShrink, denied: 4, allowed: 16},
		{name: "grow", seal: memfd.SealGrow, denied: 16, allowed: 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := newFile(t, memfd.Options{})
			require.NoError(t, file.Truncate(8))

			require.NoError(t, file.AddSeals(test.seal))
			checkSeals(t, file, memfd.SealExec|test.seal)
			require.ErrorIs(t, file.Truncate(test.denied), unix.EPERM, "sealed resize must fail")
			require.NoError(t, file.Truncate(test.allowed), "opposite resize must remain allowed")

			_, err := file.WriteAt([]byte("a"), 0)
			require.NoError(t, err, "size seals must permit in-place writes")

			if test.seal == memfd.SealGrow {
				_, err := file.WriteAt([]byte("a"), test.allowed)
				require.ErrorIs(t, err, unix.EPERM, "grow seal must reject extending writes")
			}
		})
	}
}

func TestSealWrite(t *testing.T) {
	file := newFile(t, memfd.Options{})
	require.NoError(t, file.Truncate(int64(os.Getpagesize())))
	mapping := mapWritable(t, file, nil)

	require.ErrorIs(t, file.AddSeals(memfd.SealWrite|memfd.SealGrow), unix.EBUSY)
	checkSeals(t, file, memfd.SealExec)

	require.NoError(t, unix.Munmap(mapping))
	require.NoError(t, file.AddSeals(memfd.SealWrite))
	checkSeals(t, file, memfd.SealExec|memfd.SealWrite)

	_, err := file.WriteAt([]byte("a"), 0)
	require.ErrorIs(t, err, unix.EPERM)
	mapWritable(t, file, unix.EPERM)

	require.NoError(t, file.Truncate(1), "write seal alone must permit shrinking")
	require.NoError(t, file.Truncate(2), "write seal alone must permit growing")
}

func TestSealFutureWrite(t *testing.T) {
	file := newFile(t, memfd.Options{})
	require.NoError(t, file.Truncate(int64(os.Getpagesize())))
	mapping := mapWritable(t, file, nil)

	require.NoError(t, file.AddSeals(memfd.SealFutureWrite))
	checkSeals(t, file, memfd.SealExec|memfd.SealFutureWrite)

	mapping[0] = 'a'
	contents := make([]byte, 1)
	_, err := file.ReadAt(contents, 0)
	require.NoError(t, err)
	require.Equal(t, byte('a'), contents[0], "existing shared mapping must remain writable")

	_, err = file.WriteAt([]byte("b"), 0)
	require.ErrorIs(t, err, unix.EPERM, "descriptor writes must be denied")
	mapWritable(t, file, unix.EPERM)
	require.ErrorIs(t, file.AddSeals(memfd.SealWrite), unix.EBUSY)

	require.NoError(t, unix.Munmap(mapping))
	require.NoError(t, file.AddSeals(memfd.SealWrite), "unmapping must allow SealWrite")
}

func TestInvalidSealMasks(t *testing.T) {
	file := newFile(t, memfd.Options{})

	for _, invalid := range []memfd.Seals{1 << 6, 1 << 31, memfd.SealGrow | 1<<31} {
		require.ErrorIs(t, file.AddSeals(invalid), os.ErrInvalid, "unknown mask %#x", invalid)
		checkSeals(t, file, memfd.SealExec)
	}
}

func TestSealSeal(t *testing.T) {
	file := newFile(t, memfd.Options{})
	require.NoError(t, file.AddSeals(0))
	require.NoError(t, file.AddSeals(memfd.SealGrow))
	require.NoError(t, file.AddSeals(memfd.SealGrow), "reapplying a seal before locking must succeed")

	require.NoError(t, file.AddSeals(memfd.SealSeal))
	for _, mask := range []memfd.Seals{0, memfd.SealGrow, memfd.SealShrink} {
		err := file.AddSeals(mask)
		require.ErrorIs(t, err, unix.EPERM, "locked seals must reject mask %#x", mask)
		var pathErr *os.PathError
		require.ErrorAs(t, err, &pathErr)
		require.Equal(t, "F_ADD_SEALS", pathErr.Op)
		require.Equal(t, file.Name(), pathErr.Path)
	}

	checkSeals(t, file, memfd.SealExec|memfd.SealGrow|memfd.SealSeal)
}

func TestExecutableOptInAndSealExec(t *testing.T) {
	file := newFile(t, memfd.Options{Executable: true})
	checkSeals(t, file, 0)
	stat, err := file.Stat()
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o111), stat.Mode().Perm()&0o111)

	_, err = file.WriteString("data")
	require.NoError(t, err)
	require.NoError(t, file.Chmod(0o700))

	require.NoError(t, file.AddSeals(memfd.SealExec))
	checkSeals(t, file, memfd.SealExec|memfd.SealWrite|memfd.SealFutureWrite|memfd.SealShrink|memfd.SealGrow)

	require.ErrorIs(t, file.Chmod(0o600), unix.EPERM, "execute bits must stay locked")
	_, err = file.WriteAt([]byte("x"), 0)
	require.ErrorIs(t, err, unix.EPERM, "execute seal must imply write seal on executable files")
	require.ErrorIs(t, file.Truncate(0), unix.EPERM, "execute seal must imply shrink seal")
	require.ErrorIs(t, file.Truncate(8), unix.EPERM, "execute seal must imply grow seal")
}

func TestZeroAndClosedFile(t *testing.T) {
	var zero memfd.File
	_, err := zero.Seals()
	require.ErrorIs(t, err, os.ErrInvalid)
	require.ErrorIs(t, zero.AddSeals(memfd.SealGrow), os.ErrInvalid)
	require.ErrorIs(t, zero.AddSeals(1<<31), os.ErrInvalid)
	require.ErrorIs(t, zero.Close(), os.ErrInvalid)
	_, err = zero.Read(make([]byte, 1))
	require.ErrorIs(t, err, os.ErrInvalid)

	file := newFile(t, memfd.Options{})
	borrowed := file.File
	require.NoError(t, borrowed.Close())

	require.ErrorIs(t, file.Close(), os.ErrClosed)
	require.ErrorIs(t, file.AddSeals(memfd.SealGrow), os.ErrClosed)
	require.ErrorIs(t, file.AddSeals(1<<31), os.ErrInvalid, "mask validation takes precedence over closed state")
	require.ErrorIs(t, file.Truncate(1), os.ErrClosed)

	_, err = file.Seals()
	require.ErrorIs(t, err, os.ErrClosed)
	_, err = file.Read(make([]byte, 1))
	require.ErrorIs(t, err, os.ErrClosed)
}

func TestConcurrentSealingAndClose(t *testing.T) {
	file := newFile(t, memfd.Options{})
	failures := make(chan error, 8)
	var workers sync.WaitGroup

	for range 8 {
		workers.Go(func() {
			for range 64 {
				if err := file.AddSeals(memfd.SealGrow); err != nil && !errors.Is(err, os.ErrClosed) {
					failures <- err
					return
				}
				if _, err := file.Seals(); err != nil && !errors.Is(err, os.ErrClosed) {
					failures <- err
					return
				}
			}
		})
	}

	require.NoError(t, file.Close())
	workers.Wait()
	close(failures)

	for err := range failures {
		require.NoError(t, err)
	}
}

func newFile(t *testing.T, options memfd.Options) *memfd.File {
	t.Helper()

	file, err := memfd.New(t.Name(), options)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := file.Close(); !errors.Is(err, os.ErrClosed) {
			require.NoError(t, err)
		}
	})

	return file
}

func checkSeals(t *testing.T, file *memfd.File, want memfd.Seals) {
	t.Helper()

	seals, err := file.Seals()
	require.NoError(t, err)
	require.Equal(t, want, seals)
}

func mapWritable(t *testing.T, file *memfd.File, wantErr error) []byte {
	t.Helper()

	conn, err := file.SyscallConn()
	require.NoError(t, err)

	var mapping []byte
	var callErr error
	err = conn.Control(func(fd uintptr) {
		mapping, callErr = unix.Mmap(int(fd), 0, os.Getpagesize(), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	})
	require.NoError(t, err)

	if mapping != nil {
		t.Cleanup(func() {
			// Tests may unmap early to release the constraint before sealing.
			if err := unix.Munmap(mapping); !errors.Is(err, unix.EINVAL) {
				require.NoError(t, err)
			}
		})
	}

	require.ErrorIs(t, callErr, wantErr)
	return mapping
}
