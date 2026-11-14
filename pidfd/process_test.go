//go:build linux

package pidfd_test

import (
	"context"
	"errors"
	"fmt"
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

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/internal/testenv"
	"github.com/alexfalkowski/go-linux/pidfd"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

var _ io.Closer = (*pidfd.Process)(nil)

func TestExitObservationIsRepeatableAndLeavesReapingToTheCaller(t *testing.T) {
	child, input := test.Child(t)
	process := test.Process(t, child.Pid)
	ready := make(chan error, 1)

	go func() {
		ready <- process.WaitReady()
	}()

	test.Pending(t, ready)

	_, err := input.Write([]byte{37})
	require.NoError(t, err)
	require.NoError(t, test.Receive(t, ready))
	require.NoError(t, process.WaitReady(), "observation is repeatable")

	state, err := child.Wait()
	require.NoError(t, err, "WaitReady must leave status for os.Process.Wait")
	require.Equal(t, 37, state.ExitCode())
	require.NoError(t, process.WaitReady(), "a reaped target remains exit-ready")
	_, err = process.Wait()
	require.ErrorIs(t, err, unix.ECHILD)
	require.ErrorIs(t, process.Signal(0), unix.ESRCH)
}

func TestWaitingReturnsAndConsumesTheChildExitCode(t *testing.T) {
	for _, code := range []byte{0, 37, 255} {
		t.Run(fmt.Sprintf("exit code %d", code), func(t *testing.T) {
			child, input := test.Child(t)
			process := test.Process(t, child.Pid)
			type result struct {
				err  error
				exit pidfd.Exit
			}

			waited := make(chan result, 1)

			go func() {
				exit, err := process.Wait()
				waited <- result{err: err, exit: exit}
			}()

			test.Pending(t, waited)

			_, err := input.Write([]byte{code})
			require.NoError(t, err)

			got := test.Receive(t, waited)
			require.NoError(t, got.err)
			require.Equal(t, pidfd.Exit{Code: int(code)}, got.exit)

			_, err = process.Wait()
			require.ErrorIs(t, err, unix.ECHILD, "status is consumed once")
			require.NoError(t, child.Release(), "release the standard-library owner after pidfd reaping")
		})
	}
}

func TestSignalsTerminateTheChildAndPreserveItsExitStatus(t *testing.T) {
	child, _ := test.Child(t)
	process := test.Process(t, child.Pid)

	require.NoError(t, process.Signal(0), "signal zero must check a live same-user target")
	require.NoError(t, process.Signal(syscall.SIGKILL))

	exit, err := process.Wait()
	require.NoError(t, err)
	require.Equal(t, pidfd.Exit{Code: -1, Signal: syscall.SIGKILL}, exit)
	require.ErrorIs(t, process.Signal(0), unix.ESRCH)
	require.NoError(t, child.Release())
}

func TestWaitingPreservesTheKernelCoreDumpStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	// Fixed shell builtins supply a native fatal-signal child without Go's
	// runtime signal handlers. Limit core files in this child only; piped
	// core handlers can ignore that limit, so observe the kernel's status.
	command := exec.CommandContext(ctx, "/bin/sh", "-c", "ulimit -c 0; read line; kill -ABRT $$")
	input := test.StartWithInput(t, command)
	process := test.Process(t, command.Process.Pid)

	_, err := io.WriteString(input, "exit\n")
	require.NoError(t, err)
	require.NoError(t, process.WaitReady())

	coreDump := test.CoreDump(t, command.Process.Pid)

	// This test deliberately assigns reaping to pidfd; there are no managed
	// stdout/stderr pipes, and Cmd.Wait below is only fixture cleanup.
	exit, err := process.Wait()
	require.NoError(t, err)
	require.Equal(t, pidfd.Exit{Code: -1, Signal: syscall.SIGABRT, CoreDump: coreDump}, exit)
}

func TestConcurrentObserversShareReadinessAndOnlyOneWaiterReaps(t *testing.T) {
	child, input := test.Child(t)
	process := test.Process(t, child.Pid)
	observers := make(chan error, 4)
	waiters := make(chan error, 4)
	for range 4 {
		go func() {
			observers <- process.WaitReady()
		}()

		go func() {
			exit, err := process.Wait()
			if err == nil && exit != (pidfd.Exit{Code: 23}) {
				err = errors.New("incorrect child exit status")
			}

			waiters <- err
		}()
	}

	test.Pending(t, observers)
	test.Pending(t, waiters)

	_, err := input.Write([]byte{23})
	require.NoError(t, err)

	successes := 0
	for range 4 {
		require.NoError(t, test.Receive(t, observers))

		if err := test.Receive(t, waiters); err != nil {
			require.ErrorIs(t, err, unix.ECHILD)
		} else {
			successes++
		}
	}

	require.Equal(t, 1, successes, "exactly one waiter must consume the child status")
	require.NoError(t, child.Release())
}

func TestClosingAHandleInterruptsWaitersWithoutKillingTheChild(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		flags int
		reap  bool
	}{
		{name: "blocking handle waiting for readiness"},
		{name: "blocking handle waiting for child status", reap: true},
		{name: "nonblocking handle waiting for readiness", flags: unix.PIDFD_NONBLOCK},
		{name: "nonblocking handle waiting for child status", flags: unix.PIDFD_NONBLOCK, reap: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			child, _ := test.Child(t)
			process, err := pidfd.Adopt(test.Pidfd(t, child.Pid, scenario.flags))
			require.NoError(t, err)
			test.Cleanup(t, process)
			results := make(chan error, 4)
			for range 4 {
				go func() {
					if scenario.reap {
						_, err := process.Wait()
						results <- err
					} else {
						results <- process.WaitReady()
					}
				}()
			}

			test.Pending(t, results)
			require.NoError(t, process.Close())

			for range 4 {
				require.ErrorIs(t, test.Receive(t, results), os.ErrClosed)
			}

			require.NoError(t, child.Signal(syscall.Signal(0)), "closing the handle must leave its live child alone")
		})
	}
}

func TestWaitingForANonChildReturnsECHILDImmediately(t *testing.T) {
	process := test.Process(t, os.Getpid())
	exit, err := process.Wait()
	require.ErrorIs(t, err, unix.ECHILD, "a live non-child must fail without waiting for its exit")
	require.Equal(t, pidfd.Exit{}, exit)
}

func TestNonChildExitCanBeObservedWithoutReapingIt(t *testing.T) {
	if argument := os.Args[len(os.Args)-1]; strings.HasPrefix(argument, "pidfd-observe=") {
		id, err := strconv.Atoi(strings.TrimPrefix(argument, "pidfd-observe="))
		require.NoError(t, err)

		process := test.Process(t, id)

		_, err = process.Wait()
		require.ErrorIs(t, err, unix.ECHILD, "the sibling process is not this observer's child")

		_, err = os.Stdout.Write([]byte{1})
		require.NoError(t, err)
		require.NoError(t, process.WaitReady())

		_, err = os.Stdout.WriteString("observed non-child exit\n")
		require.NoError(t, err)
		return
	}

	child, input := test.Child(t)
	_, observer := test.Command(t, "TestNonChildExitCanBeObservedWithoutReapingIt", "--", "pidfd-observe="+strconv.Itoa(child.Pid))
	ready := test.CaptureCommand(t, observer)
	var message [1]byte
	_, err := io.ReadFull(ready, message[:])
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

func TestCompetingReapersConsumeChildStatusExactlyOnce(t *testing.T) {
	for range 8 {
		child, input := test.Child(t)
		process := test.Process(t, child.Pid)
		waited := make(chan error, 1)
		external := make(chan error, 1)

		go func() {
			_, err := process.Wait()
			waited <- err
		}()

		go func() {
			_, err := child.Wait()
			external <- err
		}()

		_, err := input.Write([]byte{0})
		require.NoError(t, err)

		first, second := test.Receive(t, waited), test.Receive(t, external)
		if first == nil {
			require.ErrorIs(t, second, unix.ECHILD)
		} else {
			require.ErrorIs(t, first, unix.ECHILD)
			require.NoError(t, second)
		}

		require.NoError(t, process.WaitReady())
	}
}

func TestAdoptionPreservesDescriptorFlagsAndTransfersOwnership(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		flags int
	}{
		{name: "blocking descriptor"},
		{name: "nonblocking descriptor", flags: unix.PIDFD_NONBLOCK},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			child, input := test.Child(t)
			file := test.Pidfd(t, child.Pid, scenario.flags)
			before := test.FileFlags(t, file)
			fd := test.FileDescriptor(t, file)
			process, err := pidfd.Adopt(file)
			require.NoError(t, err)
			test.Cleanup(t, process)
			after := test.Fcntl(t, fd, unix.F_GETFL)
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

func TestOpeningAProcessPreservesStandardLibraryWaiting(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/cat")
	input := test.StartWithInput(t, command)
	process, err := pidfd.OpenProcess(command.Process)
	require.NoError(t, err)
	test.Cleanup(t, process)
	managed := make(chan error, 1)

	go func() {
		managed <- command.Wait()
	}()

	test.Pending(t, managed)
	require.NoError(t, input.Close())
	require.NoError(t, process.WaitReady())
	require.NoError(t, test.Receive(t, managed), "borrowing must preserve concurrent Cmd.Wait behavior")
	require.ErrorIs(t, process.Signal(0), unix.ESRCH)
}

func TestOpeningAProcessCreatesIndependentOwnership(t *testing.T) {
	for _, scenario := range []struct {
		name          string
		releaseSource bool
	}{
		{name: "standard library owner", releaseSource: true},
		{name: "library owner"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			source, err := os.FindProcess(os.Getpid())
			require.NoError(t, err)
			t.Cleanup(func() {
				_ = source.Release()
			})

			before := test.Descriptors(t)
			process, err := pidfd.OpenProcess(source)
			require.NoError(t, err)
			test.Cleanup(t, process)
			test.CloseOnExec(t, test.NewPidfdDescriptor(t, before))

			if scenario.releaseSource {
				require.NoError(t, source.Release())
				require.NoError(t, process.Signal(0))
				return
			}

			require.NoError(t, process.Close())
			require.NoError(t, source.Signal(syscall.Signal(0)))
		})
	}
}

func TestOpeningAnUnavailableProcessReturnsNoHandle(t *testing.T) {
	released, err := os.FindProcess(os.Getpid())
	require.NoError(t, err)
	require.NoError(t, released.Release())

	child, input := test.Child(t)

	_, err = input.Write([]byte{0})
	require.NoError(t, err)

	_, err = child.Wait()
	require.NoError(t, err)

	for _, scenario := range []struct {
		name   string
		source *os.Process
		err    error
	}{
		{name: "nil process", err: os.ErrInvalid},
		{name: "process without a native handle", source: &os.Process{}, err: os.ErrNoHandle},
		{name: "released process", source: released},
		{name: "waited process", source: child, err: os.ErrProcessDone},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			process, err := pidfd.OpenProcess(scenario.source)

			// Go's released-process error is private; all other identities are public.
			require.Error(t, err)

			if scenario.err != nil {
				require.ErrorIs(t, err, scenario.err)
			}

			require.Nil(t, process)
		})
	}
}

func TestAdoptionAcceptsHandlesWhoseTargetWasAlreadyReaped(t *testing.T) {
	child, input := test.Child(t)
	file := test.Pidfd(t, child.Pid, 0)

	_, err := input.Write([]byte{0})
	require.NoError(t, err)

	_, err = child.Wait()
	require.NoError(t, err)

	process, err := pidfd.Adopt(file)
	require.NoError(t, err, "adoption must validate the handle independently of a live target")
	test.Cleanup(t, process)

	require.NoError(t, process.WaitReady())
	require.ErrorIs(t, process.Signal(0), unix.ESRCH)

	_, err = process.Wait()
	require.ErrorIs(t, err, unix.ECHILD)
}

func TestAdoptingAStartupDescriptorLeavesGoWithItsOwnHandle(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	fd := -1
	command := exec.CommandContext(ctx, "/bin/cat")
	command.SysProcAttr = &syscall.SysProcAttr{PidFD: &fd}
	input := test.StartWithInput(t, command)
	require.NotEqual(t, -1, fd, "startup must return an independent pidfd")

	file := os.NewFile(uintptr(fd), "startup-pidfd")
	process, err := pidfd.Adopt(file)
	require.NoError(t, err)
	test.Cleanup(t, process)

	require.NoError(t, process.Close())
	require.NoError(t, command.Process.Signal(syscall.Signal(0)), "Go must own a separate startup handle")
	require.NoError(t, input.Close())
	require.NoError(t, command.Wait())
}

func TestRejectedAdoptionConsumesEveryInputFile(t *testing.T) {
	for _, scenario := range test.InvalidProcessFiles() {
		t.Run(scenario.Name, func(t *testing.T) {
			file := scenario.Open(t)
			process, err := pidfd.Adopt(file)
			test.NoResource(t, process, err, os.ErrInvalid)

			_, err = file.Stat()
			require.ErrorIs(t, err, os.ErrClosed, "failed adoption must consume the file")
		})
	}
}

func TestInvalidProcessIDsReturnNoHandle(t *testing.T) {
	for _, scenario := range []struct {
		name string
		id   int
	}{
		{name: "zero process ID"},
		{name: "negative process ID", id: -1},
		{name: "ID exceeds signed 32 bits", id: math.MaxInt32 + 1},
		{name: "ID would truncate to a live process", id: 1<<32 + os.Getpid()},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			process, err := pidfd.Open(scenario.id)
			test.NoResource(t, process, err, os.ErrInvalid)
		})
	}
}

func TestMissingProcessesReturnESRCH(t *testing.T) {
	process, err := pidfd.Open(math.MaxInt32)
	test.NoResource(t, process, err, unix.ESRCH)
	var pathErr *os.PathError
	require.ErrorAs(t, err, &pathErr)
	require.Equal(t, "pidfd_open", pathErr.Op)
}

func TestInvalidSignalsPreserveEINVAL(t *testing.T) {
	process := test.Process(t, os.Getpid())
	for _, scenario := range []struct {
		name   string
		signal syscall.Signal
	}{
		{name: "negative signal", signal: -1},
		{name: "signal exceeds the kernel range", signal: 65},
		{name: "signal would truncate to zero", signal: 1 << 32},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			err := process.Signal(scenario.signal)
			require.ErrorIs(t, err, unix.EINVAL)

			var errno syscall.Errno
			require.ErrorAs(t, err, &errno)
			require.Equal(t, unix.EINVAL, errno)
		})
	}
}

func TestAdoptionRejectsNilAndClosedFiles(t *testing.T) {
	closed := test.TempFile(t, 0)

	require.NoError(t, closed.Close())

	for _, scenario := range []struct {
		name string
		file *os.File
		err  error
	}{
		{name: "nil file", err: os.ErrInvalid},
		{name: "closed file", file: closed, err: os.ErrClosed},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			process, err := pidfd.Adopt(scenario.file)
			test.NoResource(t, process, err, scenario.err)
		})
	}
}

func TestUnavailableProcessHandlesRejectOperations(t *testing.T) {
	closed := test.Process(t, os.Getpid())

	require.NoError(t, closed.Close())

	for _, scenario := range []struct {
		name    string
		process *pidfd.Process
		err     error
	}{
		{name: "zero handle", process: &pidfd.Process{}, err: os.ErrInvalid},
		{name: "closed handle", process: closed, err: os.ErrClosed},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			process := scenario.process
			for _, operation := range []struct {
				name string
				run  func() error
			}{
				{name: "close", run: process.Close},
				{name: "signal zero", run: func() error {
					return process.Signal(0)
				}},

				{name: "wait for readiness", run: process.WaitReady},
				{name: "wait for status", run: func() error {
					_, err := process.Wait()
					return err
				}},
			} {
				require.ErrorIs(t, operation.run(), scenario.err, operation.name)
			}
		})
	}

	t.Run("closed state precedes signal validation", func(t *testing.T) {
		require.ErrorIs(t, closed.Signal(-1), os.ErrClosed)
	})
}

func TestClosingAProcessHandleSafelyRacesWithSignals(t *testing.T) {
	process := test.Process(t, os.Getpid())
	var group sync.WaitGroup
	results := make(chan error, 16)
	for range 16 {
		group.Go(func() {
			results <- process.Signal(0)
		})
	}

	require.NoError(t, process.Close())

	group.Wait()

	for range 16 {
		if err := <-results; err != nil {
			require.ErrorIs(t, err, os.ErrClosed)
		}
	}
}

func TestCreatedProcessDescriptorsAreClosedOnExecAndWakeIsNonblocking(t *testing.T) {
	before := test.Descriptors(t)
	test.Process(t, os.Getpid())
	pid := test.NewPidfdDescriptor(t, before)
	wake := test.NewEventfdDescriptor(t, before)

	test.CloseOnExec(t, pid)
	test.CloseOnExec(t, wake)
	require.NotZero(t, test.Fcntl(t, wake, unix.F_GETFL)&unix.O_NONBLOCK, "close interruption must use a nonblocking wake descriptor")
}

func TestClosingProcessHandlesReleasesAllDescriptors(t *testing.T) {
	before := test.Descriptors(t)
	for range 30 {
		process, err := pidfd.Open(os.Getpid())
		require.NoError(t, err)
		require.NoError(t, process.Close())
	}

	require.Len(t, test.Descriptors(t), len(before), "Close must release the pidfd and private wake descriptor")
}

func TestSignalZeroRejectsAnUnpermittedDifferentUserProcess(t *testing.T) {
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

	process := test.Process(t, 1)

	require.ErrorIs(t, process.Signal(0), unix.EPERM)

	_, err := process.Wait()
	require.ErrorIs(t, err, unix.ECHILD)
}

func TestFailedProcessAcquisitionCleansUpEveryConsumedDescriptor(t *testing.T) {
	if argument := os.Args[len(os.Args)-1]; strings.HasPrefix(argument, "pidfd-limit=") {
		checkConstructionFailure(t, strings.TrimPrefix(argument, "pidfd-limit="))
		return
	}

	for _, scenario := range []string{"open", "adopt", "process duplicate", "process wake"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, command := test.Command(t, "TestFailedProcessAcquisitionCleansUpEveryConsumedDescriptor", "-test.v", "--", "pidfd-limit="+scenario)

			output, err := command.CombinedOutput()
			require.NoError(t, ctx.Err())
			require.NoError(t, err, "resource-limit subprocess: %s", output)
			require.Contains(t, string(output), "verified construction failure cleanup")
		})
	}
}

func TestProcessChild(t *testing.T) {
	test.ProcessChild(t)
}

func checkConstructionFailure(t *testing.T, scenario string) {
	t.Helper()

	var source *os.File
	var standard *os.Process
	if strings.HasPrefix(scenario, "process ") {
		var err error
		standard, err = os.FindProcess(os.Getpid())
		require.NoError(t, err)
		t.Cleanup(func() {
			_ = standard.Release()
		})
	}

	if scenario == "adopt" {
		source = test.Pidfd(t, os.Getpid(), 0)
	}

	freeSlot := test.ExhaustDescriptors(t)

	switch {
	case standard != nil:
		free := -1
		if scenario == "process wake" {
			free = freeSlot()
		}

		process, err := pidfd.OpenProcess(standard)
		test.NoResource(t, process, err, unix.EMFILE)
		require.NoError(t, standard.Signal(syscall.Signal(0)), "acquisition must leave the borrowed process usable")

		if free >= 0 {
			require.Equal(t, free, test.DescriptorSlot(t))
		}
	case scenario == "open":
		free := freeSlot()
		process, err := pidfd.Open(os.Getpid())
		require.Nil(t, process)
		require.ErrorIs(t, err, unix.EMFILE, "pidfd succeeds but the private eventfd must fail")
		require.Equal(t, free, test.DescriptorSlot(t))
	default:
		process, err := pidfd.Adopt(source)
		test.NoResource(t, process, err, unix.EMFILE)

		_, err = source.Stat()
		require.ErrorIs(t, err, os.ErrClosed, "failed Adopt must close its consumed input")

		test.DescriptorSlot(t)
	}

	t.Log("verified construction failure cleanup")
}
