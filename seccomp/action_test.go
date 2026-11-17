//go:build linux

package seccomp_test

import (
	"os"
	"os/exec"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/seccomp"
	"github.com/stretchr/testify/require"
)

func TestPermissiveActionsExecuteSyscall(t *testing.T) {
	for name, action := range map[string]seccomp.Action{"allow": seccomp.Allow, "log": seccomp.Log} {
		t.Run(name, func(t *testing.T) {
			if !test.SeccompProcess(t) {
				return
			}
			test.LockThread()
			filter := test.SeccompSyscallFilter(t, test.ProcessGroupSyscall, uint32(action))

			err := filter.Load(seccomp.Options{
				Synchronization: seccomp.CallingThread,
				NoNewPrivileges: true,
				Log:             true,
			})
			require.NoError(t, err)

			group, err := test.ProcessGroup(0)
			require.NoError(t, err)
			require.Positive(t, group, "permissive action must execute getpgid")
		})
	}
}

func TestTraceWithoutTracerReturnsNotImplemented(t *testing.T) {
	if !test.SeccompProcess(t) {
		return
	}
	test.LockThread()
	filter := test.SeccompSyscallFilter(t, test.ProcessGroupSyscall, uint32(seccomp.Trace))

	err := filter.Load(seccomp.Options{
		Synchronization: seccomp.CallingThread,
		NoNewPrivileges: true,
		Log:             true,
	})
	require.NoError(t, err)

	_, err = test.ProcessGroup(0)
	require.ErrorIs(t, err, test.ErrNotImplemented, "TRACE without a tracer must skip the syscall")
}

func TestZeroErrnoReportsSuccessWithoutExecutingSyscall(t *testing.T) {
	if !test.SeccompProcess(t) {
		return
	}
	test.LockThread()
	filter := test.SeccompSyscallFilter(t, test.ProcessGroupSyscall, uint32(seccomp.Errno))

	err := filter.Load(seccomp.Options{
		Synchronization: seccomp.CallingThread,
		NoNewPrivileges: true,
		Log:             true,
	})
	require.NoError(t, err)

	group, err := test.ProcessGroup(0)
	require.NoError(t, err)
	require.Zero(t, group, "ERRNO with zero data must report success without executing getpgid")
}

func TestKillProcessTerminatesWithSIGSYS(t *testing.T) {
	const marker = "seccomp-kill-process"
	if os.Args[len(os.Args)-1] == marker {
		test.LockThread()
		filter := test.SeccompSyscallFilter(t, test.ProcessGroupSyscall, uint32(seccomp.KillProcess))
		require.NoError(t, filter.Load(seccomp.Options{Synchronization: seccomp.CallingThread, NoNewPrivileges: true}))
		_, err := os.Stdout.WriteString("loaded\n")
		require.NoError(t, err)

		_, _ = test.ProcessGroup(0)
		require.FailNow(t, "KILL_PROCESS must not return")
		return
	}
	ctx, command := test.Command(t, t.Name(), "--", marker)

	output, err := command.CombinedOutput()

	require.NoError(t, ctx.Err())
	require.Equal(t, "loaded\n", string(output))
	test.RequireSeccompTermination(t, err)
}

func TestTrapIsFatalToGoProcess(t *testing.T) {
	const marker = "seccomp-trap"
	if os.Args[len(os.Args)-1] == marker {
		test.LockThread()
		filter := test.SeccompSyscallFilter(t, test.ProcessGroupSyscall, uint32(seccomp.Trap)|123)
		require.NoError(t, filter.Load(seccomp.Options{Synchronization: seccomp.CallingThread, NoNewPrivileges: true}))
		_, err := os.Stdout.WriteString("loaded\n")
		require.NoError(t, err)

		_, _ = test.ProcessGroup(0)
		require.FailNow(t, "Go's synchronous SIGSYS handler must terminate the process")
		return
	}
	ctx, command := test.Command(t, t.Name(), "--", marker)

	output, err := command.CombinedOutput()

	require.NoError(t, ctx.Err())
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	require.Equal(t, 2, exit.ExitCode(), "Go's fatal signal path must exit")
	require.Contains(t, string(output), "loaded\nSIGSYS: bad system call")
}

func TestKillThreadLeavesOtherThreadsRunning(t *testing.T) {
	if !test.SeccompProcess(t) {
		return
	}
	test.LockThread()
	ready := make(chan test.ReadResult[int], 1)
	run := make(chan struct{})
	returned := make(chan struct{})
	go func() {
		id := test.LockThread()
		filter := test.SeccompSyscallFilter(t, test.ProcessGroupSyscall, uint32(seccomp.KillThread))
		if err := filter.Load(seccomp.Options{Synchronization: seccomp.CallingThread, NoNewPrivileges: true}); err != nil {
			ready <- test.ReadResult[int]{Err: err}
			return
		}
		ready <- test.ReadResult[int]{Value: id}
		test.Receive(t, run)
		_, _ = test.ProcessGroup(0)
		close(returned)
	}()
	worker := test.Receive(t, ready)
	require.NoError(t, worker.Err)

	close(run)

	test.WaitForThreadExit(t, worker.Value)
	select {
	case <-returned:
		require.FailNow(t, "KILL_THREAD must not return")
	default:
	}
	group, err := test.ProcessGroup(0)
	require.NoError(t, err, "another existing thread must remain runnable and unrestricted")
	require.Positive(t, group)
}
