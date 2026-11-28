//go:build linux

package test

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// Signal names the native signal representation for invalid-input tables.
type Signal = syscall.Signal

// AbortSignal selects the child's abort/core-status fixture.
const AbortSignal = syscall.SIGABRT

// KillSignal selects the child's forced-termination fixture.
const KillSignal = syscall.SIGKILL

// ExitSignal observes a completed process's native terminating signal, or -1
// when it exited normally. Callers retain assertions about expected termination.
func ExitSignal(t *testing.T, state *os.ProcessState) syscall.Signal {
	t.Helper()
	require.NotNil(t, state)
	status, ok := state.Sys().(syscall.WaitStatus)
	require.True(t, ok)
	return status.Signal()
}

// SignalZero independently observes a standard process's existence/permissions.
func SignalZero(process *os.Process) error {
	return process.Signal(syscall.Signal(0))
}

// StartWithPidfd starts a bounded command and owns its independently acquired
// startup handle. The caller can transfer that file to pidfd.Adopt.
func StartWithPidfd(t *testing.T, command *exec.Cmd) (*os.File, io.WriteCloser) {
	t.Helper()
	fd := -1
	command.SysProcAttr = &syscall.SysProcAttr{PidFD: &fd}
	input := StartWithInput(t, command)
	require.NotEqual(t, -1, fd, "startup must return an independent pidfd")
	return OwnDescriptor(t, fd, "startup-pidfd"), input
}

// ProcessUID independently observes a process owner for permission prerequisites.
func ProcessUID(t *testing.T, id int) uint32 {
	t.Helper()
	var stat unix.Stat_t
	require.NoError(t, unix.Stat(filepath.Join("/proc", strconv.Itoa(id)), &stat))
	return stat.Uid
}

// ExecutableCopy copies the running test binary to a private executable inode
// so exec-open fixtures mark only their own program, never a host executable.
func ExecutableCopy(t *testing.T) string {
	t.Helper()
	path, err := os.Executable()
	require.NoError(t, err)
	source := OpenFile(t, path)
	destination := filepath.Join(t.TempDir(), "program")
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	require.NoError(t, err)
	Cleanup(t, file)
	_, err = io.Copy(file, source)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	return destination
}

// StartWithInput starts a bounded caller-supplied command and returns its input.
// Cleanup kills, waits, and releases even when the scenario chooses another
// reaper. The caller supplies the command's context and wait ownership.
func StartWithInput(t *testing.T, command *exec.Cmd) io.WriteCloser {
	t.Helper()

	input, err := command.StdinPipe()
	require.NoError(t, err)
	require.NoError(t, command.Start())
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
		_ = command.Process.Release()
	})
	return input
}

// CaptureCommand starts a command with bounded, combined stdout/stderr capture.
// The caller may consume a handshake before reading the remaining output.
func CaptureCommand(t *testing.T, command *exec.Cmd) *os.File {
	t.Helper()

	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	Cleanup(t, reader)
	Cleanup(t, writer)
	command.Stdout, command.Stderr = writer, writer

	require.NoError(t, command.Start())
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})

	require.NoError(t, writer.Close())
	require.NoError(t, reader.SetReadDeadline(time.Now().Add(5*time.Second)))
	return reader
}

// ChildMarker reads one byte from a child handshake. Use a bounded reader.
func ChildMarker(t *testing.T, reader io.Reader) byte {
	t.Helper()
	var marker [1]byte
	_, err := io.ReadFull(reader, marker[:])
	require.NoError(t, err, "read child handshake")
	return marker[0]
}

// Child starts the current binary's TestProcessChild fixture. It announces
// readiness, then exits with the byte written to the returned input pipe.
// Cleanup always kills, reaps, and releases the standard-library process owner.
func Child(t *testing.T) (*os.Process, *os.File) {
	t.Helper()

	executable, err := os.Executable()
	require.NoError(t, err)

	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		reader.Close()
		writer.Close()
	})

	ready, output, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		ready.Close()
		output.Close()
	})

	child, err := os.StartProcess(executable, []string{executable, "-test.run=^TestProcessChild$", "--", "pidfd-child"}, &os.ProcAttr{
		Files: []*os.File{reader, output, os.Stderr},
	})
	require.NoError(t, err)

	bound := time.AfterFunc(10*time.Second, func() {
		_ = child.Kill()
	})

	t.Cleanup(func() {
		bound.Stop()

		_ = child.Kill()
		_, _ = child.Wait()

		// An external reaper can leave Wait unable to release Go's handle.
		_ = child.Release()
	})

	require.NoError(t, reader.Close())
	require.NoError(t, output.Close())
	require.NoError(t, ready.SetReadDeadline(time.Now().Add(5*time.Second)))

	var message [1]byte
	_, err = io.ReadFull(ready, message[:])
	require.NoError(t, err, "child must start and announce readiness")
	return child, writer
}

// ProcessChild serves Child's byte-controlled subprocess protocol. Outside a
// fixture invocation it returns immediately; fixture children exit directly.
func ProcessChild(t *testing.T) {
	t.Helper()

	if os.Args[len(os.Args)-1] != "pidfd-child" {
		return
	}

	time.AfterFunc(10*time.Second, func() {
		os.Exit(125)
	})

	_, err := os.Stdout.Write([]byte{1})
	require.NoError(t, err)

	var code [1]byte
	_, err = io.ReadFull(os.Stdin, code[:])
	require.NoError(t, err)

	os.Exit(int(code[0]))
}
