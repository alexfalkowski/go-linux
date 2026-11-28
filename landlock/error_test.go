//go:build linux

package landlock_test

import (
	"os"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/landlock"
	"github.com/alexfalkowski/go-linux/seccomp"
	"github.com/stretchr/testify/require"
)

func TestRestrictRejectsInvalidOptionsBeforePrivilegeChange(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledNet: landlock.BindTCP})
	blocker := test.SeccompErrnoFilter(t, test.PrctlSyscall, test.ErrPermissionDenied)
	require.NoError(t, blocker.Load(seccomp.Options{Synchronization: seccomp.CallingThread, Privileges: seccomp.SetNoNewPrivileges}))

	for _, options := range []landlock.Options{
		{},
		{Synchronization: 255, Privileges: landlock.SetNoNewPrivileges},
		{Synchronization: landlock.CallingThread, Log: 1 << 31, Privileges: landlock.SetNoNewPrivileges},
		{Synchronization: landlock.CallingThread, Privileges: 255},
	} {
		require.ErrorIs(t, ruleset.Restrict(options), os.ErrInvalid)
	}
	// The blocker has already set no-new-privileges; it also prevents any
	// attempted prctl in Restrict, so EINVAL proves validation ran first.
}

func TestABIDiscoveryPreservesPolicyDenial(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	blocker := test.SeccompErrnoFilter(t, test.LandlockCreateSyscall, test.ErrPermissionDenied)
	require.NoError(t, blocker.Load(seccomp.Options{Synchronization: seccomp.CallingThread, Privileges: seccomp.SetNoNewPrivileges}))

	version, err := landlock.ABI()
	require.ErrorIs(t, err, test.ErrPermissionDenied)
	require.Zero(t, version)
	ruleset, err := landlock.New(landlock.Config{HandledNet: landlock.ConnectTCP})
	require.ErrorIs(t, err, test.ErrPermissionDenied)
	require.Nil(t, ruleset)
}

func TestCreationFailureAfterDiscoveryReturnsNoOwner(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.RefuseLandlockCreation(t)
	before := test.Descriptors(t)

	version, err := landlock.ABI()
	require.NoError(t, err, "version query must remain allowed")
	require.GreaterOrEqual(t, version, 8)
	ruleset, err := landlock.New(landlock.Config{HandledFS: landlock.ReadFile})

	require.ErrorIs(t, err, test.ErrPermissionDenied)
	require.Nil(t, ruleset)
	require.Equal(t, before, test.Descriptors(t))
}

func TestDeniedAddPathClosesTemporaryFile(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadFile})
	file := test.TempFile(t, 0)
	blocker := test.SeccompErrnoFilter(t, test.LandlockAddSyscall, test.ErrPermissionDenied)
	require.NoError(t, blocker.Load(seccomp.Options{Synchronization: seccomp.CallingThread, Privileges: seccomp.SetNoNewPrivileges}))
	before := test.Descriptors(t)

	require.ErrorIs(t, ruleset.AddPath(file.Name(), landlock.ReadFile), test.ErrPermissionDenied)
	require.ErrorIs(t, ruleset.AddFile(file, landlock.ReadFile), test.ErrPermissionDenied)

	require.Equal(t, before, test.Descriptors(t))
	_, err := file.Stat()
	require.NoError(t, err, "borrowed input stays caller-owned after kernel rejection")
}

func TestDeniedPortRulePreservesErrno(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledNet: landlock.BindTCP})
	blocker := test.SeccompErrnoFilter(t, test.LandlockAddSyscall, test.ErrPermissionDenied)
	require.NoError(t, blocker.Load(seccomp.Options{Synchronization: seccomp.CallingThread, Privileges: seccomp.SetNoNewPrivileges}))

	require.ErrorIs(t, ruleset.AddPort(0, landlock.BindTCP), test.ErrPermissionDenied)
}

func TestRestrictPreservesKernelFailureWithoutFallback(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	_, denied := test.LandlockFiles(t)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadFile})
	blocker := test.SeccompErrnoFilter(t, test.LandlockRestrictSyscall, test.ErrPermissionDenied)
	require.NoError(t, blocker.Load(seccomp.Options{Synchronization: seccomp.CallingThread, Privileges: seccomp.SetNoNewPrivileges}))

	err := ruleset.Restrict(landlock.Options{Synchronization: landlock.AllThreads, Privileges: landlock.SetNoNewPrivileges})

	require.ErrorIs(t, err, test.ErrPermissionDenied)
	var operation *os.SyscallError
	require.ErrorAs(t, err, &operation)
	require.Equal(t, "landlock_restrict_self", operation.Syscall)
	require.True(t, test.NoNewPrivileges(t), "requested no-new-privileges persists on failure")
	data, err := os.ReadFile(denied)
	require.NoError(t, err, "failed enforcement leaves file access unchanged")
	require.Equal(t, "denied", string(data))
}

func TestNoNewPrivilegesFailurePreventsEnforcement(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	_, denied := test.LandlockFiles(t)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadFile})
	blocker := test.SeccompErrnoFilter(t, test.PrctlSyscall, test.ErrPermissionDenied)
	require.NoError(t, blocker.Load(seccomp.Options{Synchronization: seccomp.CallingThread, Privileges: seccomp.SetNoNewPrivileges}))

	err := ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread, Privileges: landlock.SetNoNewPrivileges})

	require.ErrorIs(t, err, test.ErrPermissionDenied)
	_, err = os.ReadFile(denied)
	require.NoError(t, err)
}

func TestExistingNoNewPrivilegesNeedsNoPrctl(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	_, denied := test.LandlockFiles(t)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadFile})
	blocker := test.SeccompErrnoFilter(t, test.PrctlSyscall, test.ErrPermissionDenied)
	require.NoError(t, blocker.Load(seccomp.Options{Synchronization: seccomp.CallingThread, Privileges: seccomp.SetNoNewPrivileges}))

	require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread}))

	_, err := os.ReadFile(denied)
	require.ErrorIs(t, err, test.ErrPermissionDenied)
}

func TestLayerLimitPreservesExistingPolicy(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	_, denied := test.LandlockFiles(t)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadFile})
	options := landlock.Options{Synchronization: landlock.CallingThread, Privileges: landlock.SetNoNewPrivileges}
	for range 16 {
		require.NoError(t, ruleset.Restrict(options))
	}

	require.ErrorIs(t, ruleset.Restrict(options), test.ErrArgumentListTooLong)

	_, err := os.ReadFile(denied)
	require.ErrorIs(t, err, test.ErrPermissionDenied)
}

func TestAuditOptionsRetainEnforcement(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	_, denied := test.LandlockFiles(t)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadFile})

	err := ruleset.Restrict(landlock.Options{
		Synchronization: landlock.CallingThread,
		Log:             landlock.LogSameExecOff | landlock.LogNewExecOn | landlock.LogSubdomainsOff,
		Privileges:      landlock.SetNoNewPrivileges,
	})

	require.NoError(t, err)
	_, err = os.ReadFile(denied)
	require.ErrorIs(t, err, test.ErrPermissionDenied)
}
