//go:build linux

package test

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/namespace"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// Namespace opens the current thread's namespace and registers cleanup.
func Namespace(t *testing.T, kind namespace.Type) *namespace.Handle {
	t.Helper()
	handle, err := namespace.Current(kind)
	require.NoError(t, err)
	Cleanup(t, handle)
	return handle
}

// OpenNamespace opens a namespace path and registers cleanup.
func OpenNamespace(t *testing.T, path string) *namespace.Handle {
	t.Helper()
	handle, err := namespace.Open(path)
	require.NoError(t, err)
	Cleanup(t, handle)
	return handle
}

// FIFO creates a test-owned named pipe with no writer for nonblocking lookup
// scenarios. A blocking read-only open would wait indefinitely.
func FIFO(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fifo")
	require.NoError(t, unix.Mkfifo(path, 0o600))
	return path
}

// NamespaceFile opens a close-on-exec native file for adoption/ABI scenarios.
func NamespaceFile(t *testing.T, path string) *os.File {
	t.Helper()
	return namespaceFile(t, path, unix.O_RDONLY|unix.O_CLOEXEC)
}

// InvalidNamespaceFiles supplies independently owned inputs that Adopt must
// reject and consume. Each file has cleanup registered if the test exits early.
func InvalidNamespaceFiles(t *testing.T) map[string]*os.File {
	t.Helper()
	return map[string]*os.File{
		"ordinary file":          TempFile(t, 0),
		"path descriptor":        namespaceFile(t, "/proc/thread-self/ns/net", unix.O_PATH|unix.O_CLOEXEC),
		"inheritable descriptor": namespaceFile(t, "/proc/thread-self/ns/net", unix.O_RDONLY),
	}
}

// NamespaceOwnerUID independently observes NS_GET_OWNER_UID for a live fixture.
func NamespaceOwnerUID(t *testing.T, file *os.File) uint32 {
	t.Helper()
	var uid uint32
	var err error
	FileControl(t, file, func(fd uintptr) {
		uid, err = unix.IoctlGetUint32(int(fd), unix.NS_GET_OWNER_UID)
	})
	require.NoError(t, err)
	return uid
}

// LockedThread runs an operation on a dedicated locked goroutine and waits for
// its defers, including failure cleanup. It deliberately never unlocks; Go
// destroys the OS thread on goroutine exit. The result allows independent
// observations of thread teardown without exposing production implementation.
func LockedThread(t *testing.T, operation func()) int {
	t.Helper()
	started := make(chan int, 1)
	done := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer close(done)
		started <- unix.Gettid()
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
	}, 5*time.Second, time.Millisecond, "namespace worker must exit")
}

// NamespaceChild starts the namespace package's waiting-child fixture with
// cleanup and a time bound. Zero inherits the calling thread's namespaces;
// otherwise standard Go startup creates kind, including mappings for User.
// Close the returned input and wait on the command to observe a normal exit.
func NamespaceChild(t *testing.T, kind namespace.Type) (*exec.Cmd, io.WriteCloser) {
	t.Helper()
	_, command := Command(t, "TestNamespaceWaitingChild", "--", "namespace-wait")
	command.SysProcAttr = &syscall.SysProcAttr{Cloneflags: uintptr(kind)}
	if kind == namespace.User {
		command.SysProcAttr.UidMappings = []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getuid(), Size: 1}}
		command.SysProcAttr.GidMappings = []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getgid(), Size: 1}}
	}
	return command, StartWithInput(t, command)
}

// NamespacePath returns a child process's procfs namespace path for a known
// native namespace name, avoiding PID formatting in individual scenarios.
func NamespacePath(pid int, name string) string {
	return fmt.Sprintf("/proc/%d/ns/%s", pid, name)
}

func namespaceFile(t *testing.T, path string, flags int) *os.File {
	t.Helper()
	fd, err := unix.Open(path, flags, 0)
	require.NoError(t, err)
	return OwnDescriptor(t, fd, "namespace-input")
}
