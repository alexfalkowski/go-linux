//go:build linux

package seccomp_test

import (
	"os"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/seccomp"
	"github.com/stretchr/testify/require"
)

func TestFilterSurvivesOrdinaryExec(t *testing.T) {
	if !test.SeccompProcess(t) {
		return
	}
	test.LockThread()
	filter := test.SeccompSyscallFilter(t, test.ProcessGroupSyscall, uint32(seccomp.Errno)|uint32(test.ErrPermissionDenied))
	require.NoError(t, filter.Load(seccomp.Options{Synchronization: seccomp.CallingThread, NoNewPrivileges: true}))
	ctx, command := test.Command(t, "TestExecChild", "--", "seccomp-exec")

	output, err := command.CombinedOutput()

	require.NoError(t, ctx.Err())
	require.NoError(t, err, "exec must inherit policy: %s", output)
}

func TestExecChild(t *testing.T) {
	if os.Args[len(os.Args)-1] != "seccomp-exec" {
		return
	}
	test.LockThread()

	_, err := test.ProcessGroup(0)

	require.ErrorIs(t, err, test.ErrPermissionDenied)
	require.True(t, test.NoNewPrivileges(t))
}
