//go:build linux

package test

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// InvalidProcessFiles supplies ordinary files and unsupported pidfd variants
// for ownership-transfer rejection tests. Each row acquires its own resources.
func InvalidProcessFiles() []ProcessFileCase {
	return []ProcessFileCase{
		{Name: "ordinary file", Open: func(t *testing.T) *os.File {
			t.Helper()

			return TempFile(t, 0)
		}},
		{Name: "proc directory", Open: func(t *testing.T) *os.File {
			t.Helper()

			return OpenFile(t, "/proc/self")
		}},
		{Name: "event counter", Open: func(t *testing.T) *os.File {
			t.Helper()

			fd, err := unix.Eventfd(0, unix.EFD_CLOEXEC)
			require.NoError(t, err)
			return OwnDescriptor(t, fd, "eventfd")
		}},
		{Name: "thread handle", Open: func(t *testing.T) *os.File {
			t.Helper()

			return Pidfd(t, os.Getpid(), unix.PIDFD_THREAD)
		}},
		{Name: "path handle", Open: func(t *testing.T) *os.File {
			t.Helper()

			original := Pidfd(t, os.Getpid(), 0)
			var fd int
			var err error
			FileControl(t, original, func(raw uintptr) {
				fd, err = unix.Open(fmt.Sprintf("/proc/self/fd/%d", raw), unix.O_PATH|unix.O_CLOEXEC, 0)
			})
			require.NoError(t, err)
			return OwnDescriptor(t, fd, "path-pidfd")
		}},
		{Name: "inheritable handle", Open: func(t *testing.T) *os.File {
			t.Helper()

			file := Pidfd(t, os.Getpid(), 0)
			var err error
			FileControl(t, file, func(fd uintptr) {
				_, err = unix.FcntlInt(fd, unix.F_SETFD, 0)
			})
			require.NoError(t, err)
			return file
		}},
	}
}

// ProcessFileCase names a raw input fixture, leaving adoption assertions at
// the public API boundary in the owning package's tests.
type ProcessFileCase struct {
	// Open acquires a fresh input with cleanup registered.
	Open func(*testing.T) *os.File
	// Name describes why adoption should reject the file.
	Name string
}

// CoreDump observes a terminated child's CLD_DUMPED status using WNOWAIT.
// The independent kernel observation leaves reaping to the test's wait owner.
func CoreDump(t *testing.T, pid int) bool {
	t.Helper()

	var status unix.Siginfo
	for {
		err := unix.Waitid(unix.P_PID, pid, &status, unix.WEXITED|unix.WNOWAIT|unix.WNOHANG, nil)
		if err == unix.EINTR {
			continue
		}
		require.NoError(t, err)

		break
	}

	require.Contains(t, []int32{2, 3}, status.Code, "kernel must report CLD_KILLED or CLD_DUMPED")

	t.Logf("kernel core-dump status: %t (si_code=%d)", status.Code == 3, status.Code)
	return status.Code == 3
}
