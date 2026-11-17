//go:build linux

package landlock_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/landlock"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	test.LockThread()
	os.Exit(m.Run())
}

func TestABIMatchesKernelDiscovery(t *testing.T) {
	version := test.LandlockAvailable(t, 8)
	require.Equal(t, test.LandlockABI(t), version)
	t.Logf("Landlock ABI %d", version)
}

func TestNewRejectsInvalidPolicies(t *testing.T) {
	for name, config := range map[string]landlock.Config{
		"empty":                   {},
		"unknown filesystem":      {HandledFS: 1 << 63},
		"unknown network":         {HandledNet: 1 << 63},
		"unknown scope":           {Scoped: 1 << 63},
		"mixed known and unknown": {HandledFS: landlock.ReadFile | 1<<63},
	} {
		t.Run(name, func(t *testing.T) {
			ruleset, err := landlock.New(config)

			require.ErrorIs(t, err, os.ErrInvalid)
			require.Nil(t, ruleset)
		})
	}
}

func TestNewAcceptsIndependentPolicyClasses(t *testing.T) {
	for name, config := range map[string]landlock.Config{
		"filesystem": {HandledFS: landlock.ReadFile},
		"network":    {HandledNet: landlock.ConnectTCP},
		"scope":      {Scoped: landlock.Signal},
		"combined":   {HandledFS: landlock.Refer | landlock.Truncate | landlock.IOCTLDev, HandledNet: landlock.BindTCP, Scoped: landlock.AbstractUnixSocket},
	} {
		t.Run(name, func(t *testing.T) {
			test.LandlockRuleset(t, config)
		})
	}
}

func TestNewPreservesOptionalRights(t *testing.T) {
	version := test.LandlockAvailable(t, 8)
	for _, scenario := range []struct {
		name    string
		minimum int
		config  landlock.Config
	}{
		{"pathname Unix", 9, landlock.Config{HandledFS: landlock.ResolveUnix}},
		{"UDP", 10, landlock.Config{HandledNet: landlock.BindUDP | landlock.ConnectSendUDP}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ruleset, err := landlock.New(scenario.config)
			if version < scenario.minimum {
				require.ErrorIs(t, err, test.ErrNotSupported)
				require.Nil(t, ruleset, "unsupported restrictions cannot become an empty policy")
				return
			}
			require.NoError(t, err)
			require.NoError(t, ruleset.Close())
		})
	}
}

func TestRulesetOwnsOneCloseOnExecDescriptor(t *testing.T) {
	test.LandlockAvailable(t, 8)
	before := test.Descriptors(t)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadFile})
	fd := test.NewDescriptor(t, before)
	test.CloseOnExec(t, fd)

	require.NoError(t, ruleset.Close())

	require.ErrorIs(t, test.DescriptorError(fd), test.ErrBadDescriptor)
	require.ErrorIs(t, ruleset.Close(), os.ErrClosed)
}

func TestClosedRulesetRejectsValidOperations(t *testing.T) {
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadFile, HandledNet: landlock.BindTCP})
	file := test.TempFile(t, 0)
	require.NoError(t, ruleset.Close())

	require.ErrorIs(t, ruleset.AddPath(file.Name(), landlock.ReadFile), os.ErrClosed)
	require.ErrorIs(t, ruleset.AddFile(file, landlock.ReadFile), os.ErrClosed)
	require.ErrorIs(t, ruleset.AddPort(0, landlock.BindTCP), os.ErrClosed)
	require.ErrorIs(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread}), os.ErrClosed)
}

func TestZeroRulesetOwnsNothing(t *testing.T) {
	var ruleset landlock.Ruleset
	require.ErrorIs(t, ruleset.Close(), os.ErrInvalid)
	require.ErrorIs(t, ruleset.AddPath("/", landlock.ReadFile), os.ErrInvalid)
	require.ErrorIs(t, ruleset.AddPort(0, landlock.BindTCP), os.ErrInvalid)
	require.ErrorIs(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread}), os.ErrInvalid)

	var absent *landlock.Ruleset
	require.ErrorIs(t, absent.Close(), os.ErrInvalid)
	require.ErrorIs(t, absent.AddFile(nil, landlock.ReadFile), os.ErrInvalid)
	require.ErrorIs(t, absent.Restrict(landlock.Options{Synchronization: landlock.CallingThread}), os.ErrInvalid)
}

func TestAddRejectsRightsOutsideHandledPolicy(t *testing.T) {
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadFile, HandledNet: landlock.BindTCP})
	file := test.TempFile(t, 0)
	for _, access := range []landlock.FilesystemAccess{0, landlock.WriteFile, 1 << 63, landlock.ReadFile | landlock.WriteFile} {
		require.ErrorIs(t, ruleset.AddPath(file.Name(), access), os.ErrInvalid)
		require.ErrorIs(t, ruleset.AddFile(file, access), os.ErrInvalid)
	}
	for _, access := range []landlock.NetworkAccess{0, landlock.ConnectTCP, 1 << 63, landlock.BindTCP | landlock.ConnectTCP} {
		require.ErrorIs(t, ruleset.AddPort(0, access), os.ErrInvalid)
	}
	require.ErrorIs(t, ruleset.AddFile(nil, landlock.ReadFile), os.ErrInvalid)

	_, err := file.Stat()
	require.NoError(t, err, "rejected borrowing leaves the file open")
}

func TestAddFilePreservesBorrowedLifetimeOnFailure(t *testing.T) {
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadDir})
	file := test.TempFile(t, 0)

	require.ErrorIs(t, ruleset.AddFile(file, landlock.ReadDir), test.ErrInvalidArgument)

	_, err := file.Stat()
	require.NoError(t, err)
	require.NoError(t, file.Close())
	require.ErrorIs(t, ruleset.AddFile(file, landlock.ReadDir), os.ErrClosed)
}

func TestAddPathReleasesTemporaryDescriptorAfterKernelRejection(t *testing.T) {
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadDir})
	file := test.TempFile(t, 0)
	before := test.Descriptors(t)

	require.ErrorIs(t, ruleset.AddPath(file.Name(), landlock.ReadDir), test.ErrInvalidArgument)

	require.Equal(t, before, test.Descriptors(t))
	missing := filepath.Join(t.TempDir(), "missing")
	require.ErrorIs(t, ruleset.AddPath(missing, landlock.ReadDir), os.ErrNotExist)
}

func TestConcurrentAddAndClosePreserveLifetime(t *testing.T) {
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledNet: landlock.BindTCP})
	start := make(chan struct{})
	results := make(chan error, 16)
	var workers sync.WaitGroup
	for range cap(results) {
		workers.Go(func() {
			<-start
			results <- ruleset.AddPort(0, landlock.BindTCP)
		})
	}
	close(start)
	require.NoError(t, ruleset.Close())
	workers.Wait()
	close(results)

	for err := range results {
		if err != nil {
			require.ErrorIs(t, err, os.ErrClosed)
		}
	}
	require.ErrorIs(t, ruleset.AddPort(0, landlock.BindTCP), os.ErrClosed)
}

func TestCreationUnderDescriptorExhaustionReturnsNoOwner(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.ExhaustDescriptors(t)

	ruleset, err := landlock.New(landlock.Config{HandledFS: landlock.ReadFile})

	require.ErrorIs(t, err, test.ErrTooManyOpenFiles)
	require.Nil(t, ruleset)
}

func TestAddPathUsesPathHandleForFIFO(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	path := test.FIFO(t)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadFile})

	require.NoError(t, ruleset.AddPath(path, landlock.ReadFile), "rule creation must not block waiting for a FIFO writer")
}
