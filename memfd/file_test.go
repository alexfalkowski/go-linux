//go:build linux

package memfd_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/memfd"
	"github.com/alexfalkowski/go-linux/mmap"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

var (
	_ io.ReadWriteSeeker = (*memfd.File)(nil)
	_ io.ReaderAt        = (*memfd.File)(nil)
	_ io.WriterAt        = (*memfd.File)(nil)
	_ io.Closer          = (*memfd.File)(nil)
	_ io.ReaderFrom      = (*memfd.File)(nil)
	_ io.WriterTo        = (*memfd.File)(nil)
)

func TestFilesSupportSequentialIO(t *testing.T) {
	file := test.Memfd(t, memfd.Options{})

	_, err := file.WriteString("hello")
	require.NoError(t, err)

	_, err = file.Seek(0, io.SeekStart)
	require.NoError(t, err)

	contents, err := io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, "hello", string(contents))
}

func TestCopyingPreservesContentsAndTheFileOffset(t *testing.T) {
	file := test.Memfd(t, memfd.Options{})

	_, err := file.WriteString("prefix:")
	require.NoError(t, err)

	n, err := file.ReadFrom(strings.NewReader("copied"))
	require.NoError(t, err)
	require.Equal(t, int64(6), n)

	_, err = file.Seek(0, io.SeekStart)
	require.NoError(t, err)

	var contents bytes.Buffer

	n, err = file.WriteTo(&contents)
	require.NoError(t, err)
	require.Equal(t, int64(contents.Len()), n)
	require.Equal(t, "prefix:copied", contents.String())
}

func TestTruncationChangesSizeAndZeroFillsGrowth(t *testing.T) {
	file := test.Memfd(t, memfd.Options{})

	test.WriteAt(t, file, 0, "Hello")
	require.NoError(t, file.Truncate(8))

	require.Equal(t, []byte{'H', 'e', 'l', 'l', 'o', 0, 0, 0}, test.ReadAt(t, file, 0, 8), "growth must zero-fill")

	require.NoError(t, file.Truncate(2))
	stat := test.Value(t, file.Stat)
	require.Equal(t, int64(2), stat.Size(), "shrinking must change the file size")
}

func TestFilesRejectNegativeSizesAndDeadlines(t *testing.T) {
	file := test.Memfd(t, memfd.Options{})

	require.ErrorIs(t, file.Truncate(-1), unix.EINVAL)
	require.ErrorIs(t, file.SetDeadline(time.Time{}), os.ErrNoDeadline)
}

func TestMappingsRetainContentsAfterTheFileCloses(t *testing.T) {
	file := test.Memfd(t, memfd.Options{})

	require.NoError(t, file.Truncate(int64(os.Getpagesize())))

	region, err := test.WritableMapping(t, file)
	require.NoError(t, err)

	copy(region.Bytes(), "shared")

	require.NoError(t, file.Close())
	require.Equal(t, "shared", string(region.Bytes()[:6]))
}

func TestRejectedMappingsLeaveTheFileUsable(t *testing.T) {
	file := test.Memfd(t, memfd.Options{})
	region, err := file.Map(1, os.Getpagesize(), mmap.Options{Protection: mmap.Read})
	test.NoResource(t, region, err, os.ErrInvalid)
	_, err = file.WriteString("still open")
	require.NoError(t, err)
}

func TestDefaultFilesLockExecuteBitsAndRemainWritable(t *testing.T) {
	file := test.Memfd(t, memfd.Options{})

	stat := test.Value(t, file.Stat)
	require.Zero(t, stat.Size(), "new files must be empty")
	require.True(t, stat.Mode().IsRegular(), "memfd must support ordinary regular-file operations")
	require.Zero(t, stat.Mode().Perm()&0o111, "default execute bits must be clear")
	require.Equal(t, memfd.SealExec, test.Value(t, file.Seals))

	require.ErrorIs(t, file.Chmod(0o700), unix.EPERM, "default execute bits must be locked")

	require.NoError(t, file.AddSeals(memfd.SealExec))
	_, err := file.WriteString("still writable")
	require.NoError(t, err, "SealExec on a non-executable file must preserve writes")
}

func TestCreatedFilesAreClosedOnExec(t *testing.T) {
	before := test.Descriptors(t)
	test.Memfd(t, memfd.Options{})
	test.CloseOnExec(t, test.NewDescriptor(t, before))
}

func TestFileNamesAcceptDebugLabelsUpTo249Bytes(t *testing.T) {
	for _, scenario := range []struct{ name, label string }{
		{name: "empty"},
		{name: "slash", label: "debug/label"},
		{name: "maximum bytes", label: strings.Repeat("a", 249)},
		{name: "multibyte maximum", label: strings.Repeat("é", 124) + "a"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			file, err := memfd.New(scenario.label, memfd.Options{})
			require.NoError(t, err)
			require.NoError(t, file.Close())

			require.Equal(t, scenario.label, file.Name())
		})
	}
}

func TestFileCreationRejectsInvalidNames(t *testing.T) {
	for _, scenario := range []struct{ name, label string }{
		{name: "embedded NUL", label: "invalid\x00label"},
		{name: "250 ASCII bytes", label: strings.Repeat("a", 250)},
		{name: "250 multibyte bytes", label: strings.Repeat("é", 125)},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			file, err := memfd.New(scenario.label, memfd.Options{})
			test.NoResource(t, file, err, unix.EINVAL)
			test.PathError(t, err, "memfd_create", scenario.label)
		})
	}
}

func TestRejectedFileNamesLeaveNoDescriptorBehind(t *testing.T) {
	before := test.DescriptorCount(t)

	for range 32 {
		file, err := memfd.New(strings.Repeat("a", 250), memfd.Options{})
		test.NoResource(t, file, err, unix.EINVAL)
	}

	after := test.DescriptorCount(t)
	require.LessOrEqual(t, after, before, "constructor failures must not leak descriptors")
}

func TestSizeSealsRejectOnlyTheRestrictedResize(t *testing.T) {
	for _, scenario := range []struct {
		name            string
		seal            memfd.Seals
		denied, allowed int64
	}{
		{name: "shrink", seal: memfd.SealShrink, denied: 4, allowed: 16},
		{name: "grow", seal: memfd.SealGrow, denied: 16, allowed: 4},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			file := test.Memfd(t, memfd.Options{})

			require.NoError(t, file.Truncate(8))

			require.NoError(t, file.AddSeals(scenario.seal))
			require.Equal(t, memfd.SealExec|scenario.seal, test.Value(t, file.Seals))
			require.ErrorIs(t, file.Truncate(scenario.denied), unix.EPERM, "sealed resize must fail")
			require.NoError(t, file.Truncate(scenario.allowed), "opposite resize must remain allowed")

			_, err := file.WriteAt([]byte("a"), 0)
			require.NoError(t, err, "size seals must permit in-place writes")

			if scenario.seal == memfd.SealGrow {
				_, err := file.WriteAt([]byte("a"), scenario.allowed)
				require.ErrorIs(t, err, unix.EPERM, "grow seal must reject extending writes")
			}
		})
	}
}

func TestWriteSealRequiresUnmappingAndRejectsNewWrites(t *testing.T) {
	file := test.Memfd(t, memfd.Options{})
	require.NoError(t, file.Truncate(int64(os.Getpagesize())))
	mapping, err := test.WritableMapping(t, file)
	require.NoError(t, err)

	require.ErrorIs(t, file.AddSeals(memfd.SealWrite|memfd.SealGrow), unix.EBUSY)
	require.Equal(t, memfd.SealExec, test.Value(t, file.Seals))

	require.NoError(t, mapping.Close())
	require.NoError(t, file.AddSeals(memfd.SealWrite))
	require.Equal(t, memfd.SealExec|memfd.SealWrite, test.Value(t, file.Seals))

	_, err = file.WriteAt([]byte("a"), 0)
	require.ErrorIs(t, err, unix.EPERM)
	_, err = test.WritableMapping(t, file)
	require.ErrorIs(t, err, unix.EPERM)

	require.NoError(t, file.Truncate(1), "write seal alone must permit shrinking")
	require.NoError(t, file.Truncate(2), "write seal alone must permit growing")
}

func TestFutureWriteSealPreservesExistingWritableMappings(t *testing.T) {
	file := test.Memfd(t, memfd.Options{})
	require.NoError(t, file.Truncate(int64(os.Getpagesize())))
	mapping, err := test.WritableMapping(t, file)
	require.NoError(t, err)

	require.NoError(t, file.AddSeals(memfd.SealFutureWrite))
	require.Equal(t, memfd.SealExec|memfd.SealFutureWrite, test.Value(t, file.Seals))

	mapping.Bytes()[0] = 'a'
	require.Equal(t, "a", string(test.ReadAt(t, file, 0, 1)), "existing shared mapping must remain writable")

	_, err = file.WriteAt([]byte("b"), 0)
	require.ErrorIs(t, err, unix.EPERM, "descriptor writes must be denied")
	_, err = test.WritableMapping(t, file)
	require.ErrorIs(t, err, unix.EPERM)
	require.ErrorIs(t, file.AddSeals(memfd.SealWrite), unix.EBUSY)

	require.NoError(t, mapping.Close())
	require.NoError(t, file.AddSeals(memfd.SealWrite), "unmapping must allow SealWrite")
}

func TestUnknownSealsLeaveExistingSealsUnchanged(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		seals memfd.Seals
	}{
		{name: "unknown low bit", seals: 1 << 6},
		{name: "unknown high bit", seals: 1 << 31},
		{name: "known and unknown bits", seals: memfd.SealGrow | 1<<31},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			file := test.Memfd(t, memfd.Options{})

			require.ErrorIs(t, file.AddSeals(scenario.seals), os.ErrInvalid)
			require.Equal(t, memfd.SealExec, test.Value(t, file.Seals))
		})
	}
}

func TestSealLockRejectsFurtherSealChanges(t *testing.T) {
	file := test.Memfd(t, memfd.Options{})
	require.NoError(t, file.AddSeals(0))
	require.NoError(t, file.AddSeals(memfd.SealGrow))
	require.NoError(t, file.AddSeals(memfd.SealGrow), "reapplying a seal before locking must succeed")

	require.NoError(t, file.AddSeals(memfd.SealSeal))
	for _, mask := range []memfd.Seals{0, memfd.SealGrow, memfd.SealShrink} {
		err := file.AddSeals(mask)
		require.ErrorIs(t, err, unix.EPERM, "locked seals must reject mask %#x", mask)
		test.PathError(t, err, "F_ADD_SEALS", file.Name())
	}

	require.Equal(t, memfd.SealExec|memfd.SealGrow|memfd.SealSeal, test.Value(t, file.Seals))
}

func TestSealingExecutableFilesAlsoLocksWritesAndSize(t *testing.T) {
	file := test.Memfd(t, memfd.Options{Executable: true})
	require.Equal(t, memfd.Seals(0), test.Value(t, file.Seals))
	stat := test.Value(t, file.Stat)
	require.Equal(t, os.FileMode(0o111), stat.Mode().Perm()&0o111)

	_, err := file.WriteString("data")
	require.NoError(t, err)
	require.NoError(t, file.Chmod(0o700))

	require.NoError(t, file.AddSeals(memfd.SealExec))
	require.Equal(t, memfd.SealExec|memfd.SealWrite|memfd.SealFutureWrite|memfd.SealShrink|memfd.SealGrow, test.Value(t, file.Seals))

	require.ErrorIs(t, file.Chmod(0o600), unix.EPERM, "execute bits must stay locked")
	_, err = file.WriteAt([]byte("x"), 0)
	require.ErrorIs(t, err, unix.EPERM, "execute seal must imply write seal on executable files")
	require.ErrorIs(t, file.Truncate(0), unix.EPERM, "execute seal must imply shrink seal")
	require.ErrorIs(t, file.Truncate(8), unix.EPERM, "execute seal must imply grow seal")
}

func TestUnavailableFilesRejectOperations(t *testing.T) {
	closed := test.Memfd(t, memfd.Options{})

	require.NoError(t, closed.Close())

	for _, scenario := range []struct {
		name string
		file *memfd.File
		err  error
	}{
		{name: "zero file", file: &memfd.File{}, err: os.ErrInvalid},
		{name: "closed file", file: closed, err: os.ErrClosed},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			file := scenario.file
			t.Run("read seals", func(t *testing.T) {
				_, err := file.Seals()
				require.ErrorIs(t, err, scenario.err)
			})

			t.Run("read contents", func(t *testing.T) {
				_, err := file.Read(make([]byte, 1))
				require.ErrorIs(t, err, scenario.err)
			})

			t.Run("map contents", func(t *testing.T) {
				region, err := file.Map(0, os.Getpagesize(), mmap.Options{Protection: mmap.Read})
				test.NoResource(t, region, err, scenario.err)
			})

			for _, operation := range []struct {
				name string
				run  func() error
				err  error
			}{
				{name: "close", run: file.Close, err: scenario.err},
				{name: "add seals", run: func() error {
					return file.AddSeals(memfd.SealGrow)
				}, err: scenario.err},

				{name: "invalid masks precede file state", run: func() error {
					return file.AddSeals(1 << 31)
				}, err: os.ErrInvalid},

				{name: "resize", run: func() error {
					return file.Truncate(1)
				}, err: scenario.err},
			} {
				require.ErrorIs(t, operation.run(), operation.err, operation.name)
			}
		})
	}
}

func TestClosingAFileSafelyRacesWithSealing(t *testing.T) {
	file := test.Memfd(t, memfd.Options{})
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
