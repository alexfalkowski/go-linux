//go:build linux

package test

import (
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

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
