//go:build linux

package landlock_test

import (
	"os"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/landlock"
	"github.com/stretchr/testify/require"
)

func TestCallingThreadLeavesExistingSiblingUnrestricted(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	_, denied := test.LandlockFiles(t)
	probe := test.LandlockReadThread(t, denied, nil)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadFile})

	require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread, Privileges: landlock.SetNoNewPrivileges}))

	_, err := os.ReadFile(denied)
	require.ErrorIs(t, err, test.ErrPermissionDenied)
	require.NoError(t, probe(), "existing sibling must keep its earlier policy")
}

func TestAllThreadsRestrictsExistingSibling(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	_, denied := test.LandlockFiles(t)
	probe := test.LandlockReadThread(t, denied, nil)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadFile})

	require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.AllThreads, Privileges: landlock.SetNoNewPrivileges}))

	_, err := os.ReadFile(denied)
	require.ErrorIs(t, err, test.ErrPermissionDenied)
	require.ErrorIs(t, probe(), test.ErrPermissionDenied, "existing sibling must receive the kernel TSYNC policy")
}

func TestAllThreadsReplacesDivergentSiblingDomain(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	allowed, _ := test.LandlockFiles(t)
	deny := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadFile})
	probe := test.LandlockReadThread(t, allowed, deny)
	require.ErrorIs(t, probe(), test.ErrPermissionDenied, "sibling must start in its own denying domain")
	allow := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadFile})
	require.NoError(t, allow.AddPath(allowed, landlock.ReadFile))

	require.NoError(t, allow.Restrict(landlock.Options{Synchronization: landlock.AllThreads, Privileges: landlock.SetNoNewPrivileges}))
	require.NoError(t, probe(), "TSYNC uses the caller's domain rather than stacking on the sibling's")
}

func TestPolicySurvivesOrdinaryExec(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	allowed, denied := test.LandlockFiles(t)
	// Write restrictions permit loading the Go executable and its runtime files.
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.WriteFile})
	require.NoError(t, ruleset.AddPath(allowed, landlock.WriteFile))
	require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread, Privileges: landlock.SetNoNewPrivileges}))
	require.NoError(t, ruleset.Close())
	arguments := []string{}
	if testing.CoverMode() != "" {
		arguments = append(arguments, "-test.gocoverdir="+test.LandlockCoverageDirectory())
	}
	arguments = append(arguments, "--", allowed, denied, "landlock-exec")
	ctx, command := test.Command(t, "TestExecChild", arguments...)

	output, err := command.CombinedOutput()

	require.NoError(t, ctx.Err())
	require.NoError(t, err, "exec child must retain policy: %s", output)
}

func TestExecChild(t *testing.T) {
	if os.Args[len(os.Args)-1] != "landlock-exec" {
		return
	}
	args := os.Args
	allowed, denied := args[len(args)-3], args[len(args)-2]
	writer, err := os.OpenFile(allowed, os.O_WRONLY, 0) //nolint:gosec // Paths come from this bounded fixture's parent.
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	writer, err = os.OpenFile(denied, os.O_WRONLY, 0) //nolint:gosec // Paths come from this bounded fixture's parent.
	require.ErrorIs(t, err, test.ErrPermissionDenied)
	require.Nil(t, writer)
	require.True(t, test.NoNewPrivileges(t))
}

func TestSignalScopeDeniesExistingProcessButAllowsOwnThreadGroup(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	child, input := test.Child(t)
	require.NoError(t, test.SignalZero(child))
	self, err := os.FindProcess(os.Getpid())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, self.Release())
	})
	ruleset := test.LandlockRuleset(t, landlock.Config{Scoped: landlock.Signal})

	require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread, Privileges: landlock.SetNoNewPrivileges}))

	require.ErrorIs(t, test.SignalZero(child), test.ErrOperationNotPermitted)
	require.NoError(t, test.SignalZero(self))
	_, err = input.Write([]byte{0})
	require.NoError(t, err)
}

func TestProcessChild(t *testing.T) {
	test.ProcessChild(t)
}
