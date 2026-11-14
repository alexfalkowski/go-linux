//go:build linux

package pidfd_test

import (
	"context"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/internal/testenv"
	"github.com/alexfalkowski/go-linux/pidfd"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

var (
	_ io.Closer    = (*pidfd.Process)(nil)
	_ syscall.Conn = (*pidfd.Process)(nil)
)

func TestObserveExitWithoutReaping(t *testing.T) {
	child, input := startChild(t)
	process := openProcess(t, child.Pid)
	ready := make(chan error, 1)
	go func() { ready <- process.WaitReady() }()
	assertPending(t, ready)
	_, err := input.Write([]byte{37})
	require.NoError(t, err)
	require.NoError(t, receive(t, ready))
	require.NoError(t, process.WaitReady(), "observation is repeatable")

	state, err := child.Wait()
	require.NoError(t, err, "WaitReady must leave status for os.Process.Wait")
	require.Equal(t, 37, state.ExitCode())
	require.NoError(t, process.WaitReady(), "a reaped target remains exit-ready")
	_, err = process.Wait()
	require.ErrorIs(t, err, unix.ECHILD)
	require.ErrorIs(t, process.Signal(0), unix.ESRCH)
}

func TestWaitNormalExit(t *testing.T) {
	for _, code := range []byte{0, 37, 255} {
		t.Run(strconv.Itoa(int(code)), func(t *testing.T) {
			child, input := startChild(t)
			process := openProcess(t, child.Pid)
			type result struct {
				err  error
				exit pidfd.Exit
			}
			waited := make(chan result, 1)
			go func() {
				exit, err := process.Wait()
				waited <- result{err: err, exit: exit}
			}()
			select {
			case got := <-waited:
				require.FailNowf(t, "Wait returned before child exit", "result: %+v", got)
			case <-time.After(20 * time.Millisecond):
			}
			_, err := input.Write([]byte{code})
			require.NoError(t, err)
			select {
			case got := <-waited:
				require.NoError(t, got.err)
				require.Equal(t, pidfd.Exit{Code: int(code)}, got.exit)
			case <-time.After(5 * time.Second):
				require.FailNow(t, "Wait did not observe child exit")
			}
			_, err = process.Wait()
			require.ErrorIs(t, err, unix.ECHILD, "status is consumed once")
			require.NoError(t, child.Release(), "release the standard-library owner after pidfd reaping")
		})
	}
}

func TestSignalAndWait(t *testing.T) {
	child, _ := startChild(t)
	process := openProcess(t, child.Pid)
	require.NoError(t, process.Signal(0), "signal zero must check a live same-user target")
	require.NoError(t, process.Signal(syscall.SIGKILL))
	exit, err := process.Wait()
	require.NoError(t, err)
	require.Equal(t, pidfd.Exit{Code: -1, Signal: syscall.SIGKILL}, exit)
	require.ErrorIs(t, process.Signal(0), unix.ESRCH)
	require.NoError(t, child.Release())
}

func TestCoreDumpStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	// Fixed shell builtins supply a native fatal-signal child without Go's
	// runtime signal handlers. Limit core files in this child only; piped
	// core handlers can ignore that limit, so observe the kernel's status.
	command := exec.CommandContext(ctx, "/bin/sh", "-c", "ulimit -c 0; read line; kill -ABRT $$")
	input, err := command.StdinPipe()
	require.NoError(t, err)
	require.NoError(t, command.Start())
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
		// pidfd.Wait may have reaped already, leaving Go's handle open.
		_ = command.Process.Release()
	})
	process := openProcess(t, command.Process.Pid)
	_, err = io.WriteString(input, "exit\n")
	require.NoError(t, err)
	require.NoError(t, process.WaitReady())
	// WNOWAIT observes the unreaped child's status independently of pidfd.Wait.
	var status unix.Siginfo
	for {
		err = unix.Waitid(unix.P_PID, command.Process.Pid, &status, unix.WEXITED|unix.WNOWAIT|unix.WNOHANG, nil)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	require.NoError(t, err)
	require.Contains(t, []int32{2, 3}, status.Code, "kernel must report CLD_KILLED or CLD_DUMPED")
	coreDump := status.Code == 3 // CLD_DUMPED
	t.Logf("kernel core-dump status: %t (si_code=%d)", coreDump, status.Code)

	// This test deliberately assigns reaping to pidfd; there are no managed
	// stdout/stderr pipes, and Cmd.Wait below is only fixture cleanup.
	exit, err := process.Wait()
	require.NoError(t, err)
	require.Equal(t, pidfd.Exit{Code: -1, Signal: syscall.SIGABRT, CoreDump: coreDump}, exit)
}

func TestConcurrentObserversAndWaiters(t *testing.T) {
	child, input := startChild(t)
	process := openProcess(t, child.Pid)
	observers := make(chan error, 4)
	waiters := make(chan error, 4)
	for range 4 {
		go func() { observers <- process.WaitReady() }()
		go func() {
			exit, err := process.Wait()
			if err == nil && exit != (pidfd.Exit{Code: 23}) {
				err = errors.New("incorrect child exit status")
			}
			waiters <- err
		}()
	}
	assertPending(t, observers)
	assertPending(t, waiters)
	_, err := input.Write([]byte{23})
	require.NoError(t, err)
	successes := 0
	for range 4 {
		require.NoError(t, receive(t, observers))
		if err := receive(t, waiters); err != nil {
			require.ErrorIs(t, err, unix.ECHILD)
		} else {
			successes++
		}
	}
	require.Equal(t, 1, successes, "exactly one waiter must consume the child status")
	require.NoError(t, child.Release())
}

func TestCloseInterruptsWaitsWithoutKilling(t *testing.T) {
	for _, flags := range []int{0, unix.PIDFD_NONBLOCK} {
		for _, reap := range []bool{false, true} {
			t.Run(strconv.Itoa(flags)+"/"+strconv.FormatBool(reap), func(t *testing.T) {
				child, _ := startChild(t)
				fd, err := unix.PidfdOpen(child.Pid, flags)
				require.NoError(t, err)
				process, err := pidfd.Adopt(os.NewFile(uintptr(fd), "cancel-pidfd"))
				require.NoError(t, err)
				cleanupProcess(t, process)
				results := make(chan error, 4)
				for range 4 {
					go func() {
						if reap {
							_, err := process.Wait()
							results <- err
						} else {
							results <- process.WaitReady()
						}
					}()
				}
				assertPending(t, results)
				require.NoError(t, process.Close())
				for range 4 {
					require.ErrorIs(t, receive(t, results), os.ErrClosed)
				}
				require.NoError(t, child.Signal(syscall.Signal(0)), "closing the handle must leave its live child alone")
			})
		}
	}
}

func TestNonChildWait(t *testing.T) {
	process := openProcess(t, os.Getpid())
	exit, err := process.Wait()
	require.ErrorIs(t, err, unix.ECHILD, "a live non-child must fail without waiting for its exit")
	require.Equal(t, pidfd.Exit{}, exit)
}

func TestNonChildObservation(t *testing.T) {
	if argument := os.Args[len(os.Args)-1]; strings.HasPrefix(argument, "pidfd-observe=") {
		id, err := strconv.Atoi(strings.TrimPrefix(argument, "pidfd-observe="))
		require.NoError(t, err)
		process := openProcess(t, id)
		_, err = process.Wait()
		require.ErrorIs(t, err, unix.ECHILD, "the sibling process is not this observer's child")
		_, err = os.Stdout.Write([]byte{1})
		require.NoError(t, err)
		require.NoError(t, process.WaitReady())
		_, err = os.Stdout.WriteString("observed non-child exit\n")
		require.NoError(t, err)
		return
	}
	child, input := startChild(t)
	executable, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	observer := exec.CommandContext(ctx, executable, "-test.run=^TestNonChildObservation$", "--", "pidfd-observe="+strconv.Itoa(child.Pid))
	ready, output, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { ready.Close(); output.Close() })
	observer.Stdout, observer.Stderr = output, output
	require.NoError(t, observer.Start())
	t.Cleanup(func() { _ = observer.Process.Kill(); _ = observer.Wait() })
	require.NoError(t, output.Close())
	require.NoError(t, ready.SetReadDeadline(time.Now().Add(5*time.Second)))
	var message [1]byte
	_, err = io.ReadFull(ready, message[:])
	require.NoError(t, err)
	require.Equal(t, byte(1), message[0], "observer must announce its acquired non-child handle")
	_, err = input.Write([]byte{0})
	require.NoError(t, err)
	remaining, err := io.ReadAll(ready)
	require.NoError(t, err, "observer must finish after the sibling exits")
	require.NoError(t, observer.Wait(), "observer subprocess: %s", remaining)
	require.Contains(t, string(remaining), "observed non-child exit")
	_, err = child.Wait()
	require.NoError(t, err, "non-child observation must leave reaping with the parent")
}

func TestExternalReaperRace(t *testing.T) {
	for range 8 {
		child, input := startChild(t)
		process := openProcess(t, child.Pid)
		waited := make(chan error, 1)
		external := make(chan error, 1)
		go func() { _, err := process.Wait(); waited <- err }()
		go func() { _, err := child.Wait(); external <- err }()
		_, err := input.Write([]byte{0})
		require.NoError(t, err)
		first, second := receive(t, waited), receive(t, external)
		if first == nil {
			require.ErrorIs(t, second, unix.ECHILD)
		} else {
			require.ErrorIs(t, first, unix.ECHILD)
			require.NoError(t, second)
		}
		require.NoError(t, process.WaitReady())
	}
}

func TestAdoptPreservesFlagsAndTransfersOwnership(t *testing.T) {
	for _, flags := range []int{0, unix.PIDFD_NONBLOCK} {
		t.Run(strconv.Itoa(flags), func(t *testing.T) {
			child, input := startChild(t)
			fd, err := unix.PidfdOpen(child.Pid, flags)
			require.NoError(t, err)
			file := os.NewFile(uintptr(fd), "adopted-pidfd")
			before, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
			require.NoError(t, err)
			process, err := pidfd.Adopt(file)
			require.NoError(t, err)
			cleanupProcess(t, process)
			after, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
			require.NoError(t, err)
			require.Equal(t, before, after, "adoption must not change the shared open-file description")
			require.NoError(t, process.Signal(0))
			_, err = input.Write([]byte{19})
			require.NoError(t, err)
			exit, err := process.Wait()
			require.NoError(t, err)
			require.Equal(t, pidfd.Exit{Code: 19}, exit)
			require.NoError(t, process.Close())
			_, err = file.Stat()
			require.ErrorIs(t, err, os.ErrClosed, "successful adoption must close the transferred file owner")
			require.NoError(t, child.Release())
		})
	}
}

func TestAdoptWithHandleDuplicate(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/cat")
	input, err := command.StdinPipe()
	require.NoError(t, err)
	require.NoError(t, command.Start())
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	var fd int
	var dupErr error
	err = command.Process.WithHandle(func(handle uintptr) {
		fd, dupErr = unix.FcntlInt(handle, unix.F_DUPFD_CLOEXEC, 0)
	})
	require.NoError(t, err)
	require.NoError(t, dupErr)
	file := os.NewFile(uintptr(fd), "duplicate-pidfd")
	process, err := pidfd.Adopt(file)
	require.NoError(t, err)
	cleanupProcess(t, process)
	managed := make(chan error, 1)
	go func() { managed <- command.Wait() }()
	assertPending(t, managed)
	require.NoError(t, input.Close())
	require.NoError(t, process.WaitReady())
	require.NoError(t, receive(t, managed), "adoption must preserve concurrent Cmd.Wait behavior")
	require.ErrorIs(t, process.Signal(0), unix.ESRCH)
}

func TestAdoptReapedHandle(t *testing.T) {
	child, input := startChild(t)
	fd, err := unix.PidfdOpen(child.Pid, 0)
	require.NoError(t, err)
	file := os.NewFile(uintptr(fd), "reaped-pidfd")
	t.Cleanup(func() { file.Close() })
	_, err = input.Write([]byte{0})
	require.NoError(t, err)
	_, err = child.Wait()
	require.NoError(t, err)
	process, err := pidfd.Adopt(file)
	require.NoError(t, err, "adoption must validate the handle independently of a live target")
	cleanupProcess(t, process)
	require.NoError(t, process.WaitReady())
	require.ErrorIs(t, process.Signal(0), unix.ESRCH)
	_, err = process.Wait()
	require.ErrorIs(t, err, unix.ECHILD)
}

func TestAdoptStartupDescriptor(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	fd := -1
	command := exec.CommandContext(ctx, "/bin/cat")
	command.SysProcAttr = &syscall.SysProcAttr{PidFD: &fd}
	input, err := command.StdinPipe()
	require.NoError(t, err)
	require.NoError(t, command.Start())
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	require.NotEqual(t, -1, fd, "startup must return an independent pidfd")
	file := os.NewFile(uintptr(fd), "startup-pidfd")
	process, err := pidfd.Adopt(file)
	require.NoError(t, err)
	cleanupProcess(t, process)
	require.NoError(t, process.Close())
	require.NoError(t, command.Process.Signal(syscall.Signal(0)), "Go must own a separate startup handle")
	require.NoError(t, input.Close())
	require.NoError(t, command.Wait())
}

func TestAdoptClosesRejectedFiles(t *testing.T) {
	for _, name := range []string{"regular", "proc", "eventfd", "thread", "path", "inheritable"} {
		t.Run(name, func(t *testing.T) {
			var file *os.File
			var err error
			switch name {
			case "regular":
				file, err = os.CreateTemp(t.TempDir(), "not-pidfd")
			case "proc":
				file, err = os.Open("/proc/self")
			default:
				var fd int
				if name == "eventfd" {
					fd, err = unix.Eventfd(0, unix.EFD_CLOEXEC)
				} else {
					flags := 0
					if name == "thread" {
						flags = unix.PIDFD_THREAD
					}
					fd, err = unix.PidfdOpen(os.Getpid(), flags)
				}
				require.NoError(t, err)
				if name == "inheritable" {
					_, err = unix.FcntlInt(uintptr(fd), unix.F_SETFD, 0)
				}
				if name == "path" {
					original := fd
					fd, err = unix.Open("/proc/self/fd/"+strconv.Itoa(original), unix.O_PATH|unix.O_CLOEXEC, 0)
					require.NoError(t, unix.Close(original))
				}
				file = os.NewFile(uintptr(fd), name)
			}
			require.NoError(t, err)
			process, err := pidfd.Adopt(file)
			require.Nil(t, process)
			require.ErrorIs(t, err, os.ErrInvalid)
			_, err = file.Stat()
			require.ErrorIs(t, err, os.ErrClosed, "failed adoption must close the consumed file")
		})
	}
}

func TestInvalidInputsAndErrors(t *testing.T) {
	for _, id := range []int{0, -1, math.MaxInt32 + 1, 1<<32 + os.Getpid()} {
		process, err := pidfd.Open(id)
		require.Nil(t, process)
		require.ErrorIs(t, err, os.ErrInvalid)
	}
	process, err := pidfd.Open(math.MaxInt32)
	require.Nil(t, process)
	require.ErrorIs(t, err, unix.ESRCH)
	var pathErr *os.PathError
	require.ErrorAs(t, err, &pathErr)
	require.Equal(t, "pidfd_open", pathErr.Op)

	process = openProcess(t, os.Getpid())
	for _, signal := range []syscall.Signal{-1, 65, 1 << 32} {
		err := process.Signal(signal)
		require.ErrorIs(t, err, unix.EINVAL)
		var errno syscall.Errno
		require.ErrorAs(t, err, &errno)
		require.Equal(t, unix.EINVAL, errno)
	}
	process, err = pidfd.Adopt(nil)
	require.Nil(t, process)
	require.ErrorIs(t, err, os.ErrInvalid)
	file, err := os.CreateTemp(t.TempDir(), "closed")
	require.NoError(t, err)
	require.NoError(t, file.Close())
	process, err = pidfd.Adopt(file)
	require.Nil(t, process)
	require.ErrorIs(t, err, os.ErrClosed)
}

func TestZeroAndClosedProcess(t *testing.T) {
	var zero pidfd.Process
	require.ErrorIs(t, zero.Close(), os.ErrInvalid)
	require.ErrorIs(t, zero.Signal(0), os.ErrInvalid)
	require.ErrorIs(t, zero.WaitReady(), os.ErrInvalid)
	_, err := zero.Wait()
	require.ErrorIs(t, err, os.ErrInvalid)
	_, err = zero.SyscallConn()
	require.ErrorIs(t, err, os.ErrInvalid)

	process := openProcess(t, os.Getpid())
	conn, err := process.SyscallConn()
	require.NoError(t, err)
	require.NoError(t, process.Close())
	require.ErrorIs(t, process.Close(), os.ErrClosed)
	require.ErrorIs(t, process.Signal(0), os.ErrClosed)
	require.ErrorIs(t, process.Signal(-1), os.ErrClosed, "closed state precedes kernel signal validation")
	require.ErrorIs(t, process.WaitReady(), os.ErrClosed)
	_, err = process.Wait()
	require.ErrorIs(t, err, os.ErrClosed)
	require.Error(t, conn.Control(func(uintptr) { require.Fail(t, "closed raw callback ran") }))
}

func TestConcurrentCloseAndSignals(t *testing.T) {
	process := openProcess(t, os.Getpid())
	var group sync.WaitGroup
	results := make(chan error, 16)
	for range 16 {
		group.Go(func() { results <- process.Signal(0) })
	}
	require.NoError(t, process.Close())
	group.Wait()
	for range 16 {
		if err := <-results; err != nil {
			require.ErrorIs(t, err, os.ErrClosed)
		}
	}
}

func TestDescriptorFlagsAndCleanup(t *testing.T) {
	before, err := os.ReadDir("/proc/self/fd")
	require.NoError(t, err)
	for range 30 {
		process, err := pidfd.Open(os.Getpid())
		require.NoError(t, err)
		conn, err := process.SyscallConn()
		require.NoError(t, err)
		var flags int
		var flagsErr error
		err = conn.Control(func(fd uintptr) { flags, flagsErr = unix.FcntlInt(fd, unix.F_GETFD, 0) })
		require.NoError(t, err)
		require.NoError(t, flagsErr)
		require.NotZero(t, flags&unix.FD_CLOEXEC)
		require.NoError(t, process.Close())
	}
	after, err := os.ReadDir("/proc/self/fd")
	require.NoError(t, err)
	require.Len(t, after, len(before), "Close must release both the pidfd and its private wake descriptor")
}

func TestSignalPermissionFailure(t *testing.T) {
	// Only signal zero is used against this different-UID target. Actual
	// delivery, success under cross-user capabilities, and PID 1 lifecycle
	// are outside the test. Missing prerequisites skip only when optional.
	var stat unix.Stat_t
	require.NoError(t, unix.Stat("/proc/1", &stat))
	if os.Getuid() == 0 || int64(stat.Uid) == int64(os.Getuid()) {
		options, err := testenv.FromEnvironment()
		require.NoError(t, err)
		const reason = "signal permission denial needs an ordinary-user runner with a different-UID PID 1"
		require.False(t, options.Required, reason)
		t.Skip(reason)
	}
	process := openProcess(t, 1)
	require.ErrorIs(t, process.Signal(0), unix.EPERM)
	_, err := process.Wait()
	require.ErrorIs(t, err, unix.ECHILD)
}

func TestConstructionFailureCleanup(t *testing.T) {
	if argument := os.Args[len(os.Args)-1]; strings.HasPrefix(argument, "pidfd-limit=") {
		checkConstructionFailure(t, strings.TrimPrefix(argument, "pidfd-limit="))
		return
	}
	executable, err := os.Executable()
	require.NoError(t, err)
	for _, scenario := range []string{"open", "adopt"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			//nolint:gosec // Re-exec only the current test binary with fixed test-owned arguments.
			command := exec.CommandContext(ctx, executable, "-test.run=^TestConstructionFailureCleanup$", "-test.v", "--", "pidfd-limit="+scenario)
			output, err := command.CombinedOutput()
			require.NoError(t, ctx.Err())
			require.NoError(t, err, "resource-limit subprocess: %s", output)
			require.Contains(t, string(output), "verified construction failure cleanup")
		})
	}
}

func TestProcessChild(t *testing.T) {
	if os.Args[len(os.Args)-1] != "pidfd-child" {
		return
	}
	time.AfterFunc(10*time.Second, func() { os.Exit(125) })
	_, err := os.Stdout.Write([]byte{1})
	require.NoError(t, err)
	var code [1]byte
	_, err = io.ReadFull(os.Stdin, code[:])
	require.NoError(t, err)
	os.Exit(int(code[0]))
}

func startChild(t *testing.T) (*os.Process, *os.File) {
	t.Helper()
	executable, err := os.Executable()
	require.NoError(t, err)
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { reader.Close(); writer.Close() })
	ready, output, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { ready.Close(); output.Close() })
	child, err := os.StartProcess(executable, []string{executable, "-test.run=^TestProcessChild$", "--", "pidfd-child"}, &os.ProcAttr{
		Files: []*os.File{reader, output, os.Stderr},
	})
	require.NoError(t, err)
	bound := time.AfterFunc(10*time.Second, func() { _ = child.Kill() })
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

func openProcess(t *testing.T, id int) *pidfd.Process {
	t.Helper()
	process, err := pidfd.Open(id)
	require.NoError(t, err)
	cleanupProcess(t, process)
	return process
}

func cleanupProcess(t *testing.T, process *pidfd.Process) {
	t.Helper()
	t.Cleanup(func() {
		if err := process.Close(); !errors.Is(err, os.ErrClosed) {
			require.NoError(t, err)
		}
	})
}

func assertPending(t *testing.T, results <-chan error) {
	t.Helper()
	select {
	case err := <-results:
		require.FailNowf(t, "wait returned before its trigger", "error: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
}

func receive(t *testing.T, results <-chan error) error {
	t.Helper()
	select {
	case err := <-results:
		return err
	case <-time.After(5 * time.Second):
		require.FailNow(t, "wait did not finish within its bound")
		return nil
	}
}

func checkConstructionFailure(t *testing.T, scenario string) {
	t.Helper()
	var source *os.File
	if scenario == "adopt" {
		fd, err := unix.PidfdOpen(os.Getpid(), 0)
		require.NoError(t, err)
		source = os.NewFile(uintptr(fd), "consumed-pidfd")
	}
	entries, err := os.ReadDir("/proc/self/fd")
	require.NoError(t, err)
	maximum := 0
	for _, entry := range entries {
		fd, err := strconv.Atoi(entry.Name())
		require.NoError(t, err)
		maximum = max(maximum, fd)
	}
	var limit unix.Rlimit
	require.NoError(t, unix.Getrlimit(unix.RLIMIT_NOFILE, &limit))
	limit.Cur = uint64(maximum + 16)
	require.NoError(t, unix.Setrlimit(unix.RLIMIT_NOFILE, &limit))
	var fillers []int
	defer func() {
		for _, fd := range fillers {
			unix.Close(fd)
		}
	}()
	for {
		fd, err := unix.Dup(1)
		if err != nil {
			require.ErrorIs(t, err, unix.EMFILE)
			break
		}
		fillers = append(fillers, fd)
	}
	if scenario == "open" {
		free := fillers[len(fillers)-1]
		require.NoError(t, unix.Close(free))
		fillers = fillers[:len(fillers)-1]
		process, err := pidfd.Open(os.Getpid())
		require.Nil(t, process)
		require.ErrorIs(t, err, unix.EMFILE, "pidfd succeeds but the private eventfd must fail")
		fd, err := unix.Dup(1)
		require.NoError(t, err, "failed Open must release its acquired pidfd")
		require.Equal(t, free, fd)
		require.NoError(t, unix.Close(fd))
	} else {
		process, err := pidfd.Adopt(source)
		require.Nil(t, process)
		require.ErrorIs(t, err, unix.EMFILE)
		_, err = source.Stat()
		require.ErrorIs(t, err, os.ErrClosed, "failed Adopt must close its consumed input")
		fd, err := unix.Dup(1)
		require.NoError(t, err, "failed Adopt must release the original pidfd's descriptor slot")
		require.NoError(t, unix.Close(fd))
	}
	t.Log("verified construction failure cleanup")
}
