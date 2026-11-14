//go:build linux

package test

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// DescriptorCount counts open descriptors using procfs, including its temporary
// directory descriptor. Compare counts at equivalent points in the lifecycle.
func DescriptorCount(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	require.NoError(t, err)
	return len(entries)
}

// OwnDescriptor wraps an acquired descriptor and closes it during test cleanup.
// The caller transfers ownership even when a later assertion fails.
func OwnDescriptor(t *testing.T, fd int, name string) *os.File {
	t.Helper()
	file := os.NewFile(uintptr(fd), name)
	Cleanup(t, file)
	return file
}

// Pidfd acquires a raw process file for ownership/adoption scenarios.
func Pidfd(t *testing.T, id, flags int) *os.File {
	t.Helper()
	fd, err := unix.PidfdOpen(id, flags)
	require.NoError(t, err)
	return OwnDescriptor(t, fd, "fixture-pidfd")
}

// FileFlags observes a standard-library file's status flags before adoption.
func FileFlags(t *testing.T, file *os.File) int {
	t.Helper()

	var flags int
	var err error
	FileControl(t, file, func(fd uintptr) {
		flags, err = unix.FcntlInt(fd, unix.F_GETFL, 0)
	})
	require.NoError(t, err)
	return flags
}

// DescriptorSlot duplicates stdout then closes the duplicate, proving that a
// descriptor slot is available. Use during isolated exhaustion scenarios.
func DescriptorSlot(t *testing.T) int {
	t.Helper()

	fd, err := unix.Dup(1)
	require.NoError(t, err, "acquisition failure must release its descriptor slot")
	require.NoError(t, unix.Close(fd))
	return fd
}

// Descriptors snapshots open descriptor numbers and their procfs targets.
// It initializes Go polling before observation so first-use poller descriptors
// cannot be mistaken for the resource acquired between snapshots. Use only
// while the test controls descriptor acquisition and closure.
func Descriptors(t *testing.T) map[int]string {
	t.Helper()

	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	require.NoError(t, reader.SetReadDeadline(time.Time{}))
	require.NoError(t, reader.Close())
	require.NoError(t, writer.Close())

	directory, err := os.Open("/proc/self/fd")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, directory.Close())
	}()

	entries, err := directory.ReadDir(-1)
	require.NoError(t, err)
	result := make(map[int]string, len(entries))
	for _, entry := range entries {
		fd, err := strconv.Atoi(entry.Name())
		require.NoError(t, err)
		if uintptr(fd) == directory.Fd() {
			continue
		}
		target, err := os.Readlink("/proc/self/fd/" + entry.Name())
		require.NoError(t, err)
		result[fd] = target
	}
	return result
}

// NewDescriptor finds the single descriptor acquired since before. The caller
// must keep its resource open and avoid concurrent Close during ABI inspection;
// this observation neither duplicates nor assumes ownership of the descriptor.
func NewDescriptor(t *testing.T, before map[int]string) int {
	t.Helper()
	return newDescriptor(t, before, "")
}

// NewPidfdDescriptor selects the newly acquired pidfd, excluding its private
// wake eventfd. Lifetime and observation requirements follow NewDescriptor.
func NewPidfdDescriptor(t *testing.T, before map[int]string) int {
	t.Helper()
	return newDescriptor(t, before, "anon_inode:[pidfd]")
}

// NewEventfdDescriptor selects a newly acquired eventfd, such as a process
// owner's wake file. Lifetime requirements follow NewDescriptor.
func NewEventfdDescriptor(t *testing.T, before map[int]string) int {
	t.Helper()
	return newDescriptor(t, before, "anon_inode:[eventfd]")
}

// FileDescriptor observes a file's number before an ownership transfer. Keep
// the receiving owner open while inspecting that number after the transfer.
func FileDescriptor(t *testing.T, file *os.File) int {
	t.Helper()
	var result int
	FileControl(t, file, func(fd uintptr) {
		result = int(fd)
	})
	return result
}

// FileControl pins a standard-library file during a focused kernel observation.
func FileControl(t *testing.T, file *os.File, operation func(uintptr)) {
	t.Helper()
	conn, err := file.SyscallConn()
	require.NoError(t, err)

	require.NoError(t, conn.Control(operation))
}

// Fcntl reads flags from a live descriptor observed by an ABI fixture. The
// fixture retains its owner; this helper does not synchronize concurrent Close.
func Fcntl(t *testing.T, fd, command int) int {
	t.Helper()
	flags, err := unix.FcntlInt(uintptr(fd), command, 0)
	require.NoError(t, err)
	return flags
}

// CloseOnExec requires the observed descriptor to have FD_CLOEXEC set.
func CloseOnExec(t *testing.T, fd int) {
	t.Helper()
	require.NotZero(t, Fcntl(t, fd, unix.F_GETFD)&unix.FD_CLOEXEC, "descriptor must not leak across exec")
}

// FDInfo reads procfs information for a fixture's still-open resource.
func FDInfo(t *testing.T, fd int) string {
	t.Helper()
	data, err := os.ReadFile(fmt.Sprintf("/proc/self/fdinfo/%d", fd))
	require.NoError(t, err)
	return string(data)
}

// NoPendingRecord requires a live nonblocking eventfd/timerfd descriptor to
// return EAGAIN. Keep its owner open without concurrent Close during the check.
func NoPendingRecord(t *testing.T, fd int) {
	t.Helper()
	var record [8]byte
	_, err := unix.Read(fd, record[:])
	require.ErrorIs(t, err, unix.EAGAIN, "descriptor must have no pending record")
}

// Readable waits for POLLIN without consuming the fixture's record. Keep its
// owner open without concurrent Close until the observation has finished.
func Readable(t *testing.T, fd int) {
	t.Helper()
	require.GreaterOrEqual(t, fd, 0, "descriptor must be live")
	require.LessOrEqual(t, fd, math.MaxInt32, "descriptor must fit Linux pollfd")

	require.Eventually(t, func() bool {
		ready, err := unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, 0) //nolint:gosec // Range checked above.
		require.NoError(t, err)
		return ready == 1
	}, 5*time.Second, time.Millisecond, "descriptor must become readable")
}

// ExhaustDescriptors fills a lowered soft descriptor limit until EMFILE.
// Call only inside a disposable subprocess. Cleanup closes fillers and restores
// the original limit. The result frees one filler and returns its descriptor
// number, allowing tests to reserve an acquisition slot without leaking it.
func ExhaustDescriptors(t *testing.T) func() int {
	t.Helper()

	entries, err := os.ReadDir("/proc/self/fd")
	require.NoError(t, err)

	maximum := 0
	for _, entry := range entries {
		fd, err := strconv.Atoi(entry.Name())
		require.NoError(t, err)

		maximum = max(maximum, fd)
	}

	var original unix.Rlimit
	require.NoError(t, unix.Getrlimit(unix.RLIMIT_NOFILE, &original))

	limit := uint64(maximum + 32)
	require.NoError(t, unix.Setrlimit(unix.RLIMIT_NOFILE, &unix.Rlimit{Cur: min(limit, original.Cur), Max: original.Max}))

	var fillers []int
	t.Cleanup(func() {
		for _, fd := range fillers {
			require.NoError(t, unix.Close(fd))
		}

		require.NoError(t, unix.Setrlimit(unix.RLIMIT_NOFILE, &original))
	})

	for {
		fd, err := unix.Dup(1)
		if err != nil {
			require.ErrorIs(t, err, unix.EMFILE)

			break
		}

		fillers = append(fillers, fd)
	}
	return func() int {
		require.NotEmpty(t, fillers, "no filler slot remains to release")

		last := len(fillers) - 1
		fd := fillers[last]
		fillers = fillers[:last]

		require.NoError(t, unix.Close(fd))
		return fd
	}
}

func newDescriptor(t *testing.T, before map[int]string, target string) int {
	t.Helper()
	created := make(map[int]string)
	for fd, name := range Descriptors(t) {
		if _, existed := before[fd]; !existed && (target == "" || name == target) {
			created[fd] = name
		}
	}
	require.Len(t, created, 1, "expected one newly acquired descriptor matching %q: %v", target, created)
	for fd := range created {
		return fd
	}
	return 0
}
