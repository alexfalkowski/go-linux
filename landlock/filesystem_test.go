//go:build linux

package landlock_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alexfalkowski/go-linux/file"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/landlock"
	"github.com/stretchr/testify/require"
)

func TestFilesystemRulesAllowOnlySelectedHierarchy(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	allowed, denied := test.LandlockFiles(t)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadFile | landlock.WriteFile})
	require.NoError(t, ruleset.AddPath(filepath.Dir(allowed), landlock.ReadFile))

	require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread, NoNewPrivileges: true}))

	data, err := os.ReadFile(allowed)
	require.NoError(t, err)
	require.Equal(t, "allowed", string(data))
	_, err = os.ReadFile(denied)
	require.ErrorIs(t, err, test.ErrPermissionDenied)
	writer, err := os.OpenFile(allowed, os.O_WRONLY, 0)
	require.ErrorIs(t, err, test.ErrPermissionDenied, "read allowance must not grant write access")
	require.Nil(t, writer)
}

func TestAddFileRetainsObjectAfterBorrowerCloseAndRename(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	allowed, denied := test.LandlockFiles(t)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadFile})
	directory := test.OpenFile(t, filepath.Dir(allowed))
	parent, err := file.Open(directory, filepath.Base(allowed), file.OpenOptions{Flags: file.Path})
	require.NoError(t, err)
	test.Cleanup(t, parent)
	require.NoError(t, ruleset.AddFile(parent, landlock.ReadFile))
	require.NoError(t, parent.Close())
	renamed := allowed + "-renamed"
	require.NoError(t, os.Rename(allowed, renamed))
	require.NoError(t, os.WriteFile(allowed, []byte("replacement"), 0o600))

	require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread, NoNewPrivileges: true}))
	require.NoError(t, ruleset.Close())

	data, err := os.ReadFile(renamed)
	require.NoError(t, err)
	require.Equal(t, "allowed", string(data))
	_, err = os.ReadFile(allowed)
	require.ErrorIs(t, err, test.ErrPermissionDenied, "rule must not follow replacement at old path")
	_, err = os.ReadFile(denied)
	require.ErrorIs(t, err, test.ErrPermissionDenied, "Close must not undo policy")
}

func TestAlreadyOpenFileRemainsReadable(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	_, denied := test.LandlockFiles(t)
	opened := test.OpenFile(t, denied)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadFile})

	require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread, NoNewPrivileges: true}))

	buffer := make([]byte, 6)
	count, err := opened.Read(buffer)
	require.NoError(t, err)
	require.Equal(t, "denied", string(buffer[:count]))
	_, err = os.ReadFile(denied)
	require.ErrorIs(t, err, test.ErrPermissionDenied, "new open must check the policy")
}

func TestUnhandledMetadataRemainsAvailable(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	_, denied := test.LandlockFiles(t)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadFile})

	require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread, NoNewPrivileges: true}))

	info, err := os.Stat(denied)
	require.NoError(t, err)
	require.Equal(t, int64(6), info.Size())
}

func TestTruncateIsIndependentOfWritePermission(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	allowed, denied := test.LandlockFiles(t)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.WriteFile | landlock.Truncate})
	require.NoError(t, ruleset.AddPath(filepath.Dir(allowed), landlock.WriteFile|landlock.Truncate))
	require.NoError(t, ruleset.AddPath(denied, landlock.WriteFile))

	require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread, NoNewPrivileges: true}))

	require.NoError(t, os.Truncate(allowed, 1))
	require.ErrorIs(t, os.Truncate(denied, 1), test.ErrPermissionDenied)
	writer, err := os.OpenFile(denied, os.O_WRONLY, 0)
	require.NoError(t, err)
	require.NoError(t, writer.Close(), "writing without truncation stays permitted")
	data, err := os.ReadFile(denied)
	require.NoError(t, err)
	require.Equal(t, "denied", string(data), "denied truncation must preserve contents")
}

func TestCrossDirectoryRenameRequiresRefer(t *testing.T) {
	for _, allowRefer := range []bool{false, true} {
		var name string
		if allowRefer {
			name = "handled and allowed"
		} else {
			name = "unhandled is denied"
		}
		t.Run(name, func(t *testing.T) {
			test.LandlockAvailable(t, 8)
			if !test.LandlockProcess(t) {
				return
			}
			test.LockThread()
			allowed, denied := test.LandlockFiles(t)
			access := landlock.MakeReg | landlock.RemoveFile
			if allowRefer {
				access |= landlock.Refer
			}
			ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: access})
			require.NoError(t, ruleset.AddPath(filepath.Dir(allowed), access))
			require.NoError(t, ruleset.AddPath(filepath.Dir(denied), access))

			require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread, NoNewPrivileges: true}))

			err := os.Rename(allowed, denied)
			if allowRefer {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, test.ErrCrossDevice)
		})
	}
}

func TestLaterRulesDoNotRelaxAppliedDomain(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	test.LockThread()
	allowed, denied := test.LandlockFiles(t)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadFile})
	require.NoError(t, ruleset.AddPath(allowed, landlock.ReadFile))
	require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread, NoNewPrivileges: true}))

	require.NoError(t, ruleset.AddPath(denied, landlock.ReadFile))
	require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread}))

	_, err := os.ReadFile(denied)
	require.ErrorIs(t, err, test.ErrPermissionDenied, "neither later rules nor stacking can relax the first layer")
	data, err := os.ReadFile(allowed)
	require.NoError(t, err)
	require.Equal(t, "allowed", string(data))
}

func TestDirectoryReadRulesEnforceSelectedHierarchy(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	allowed, denied := test.LandlockFiles(t)
	ruleset := test.LandlockRuleset(t, landlock.Config{HandledFS: landlock.ReadDir})
	require.NoError(t, ruleset.AddPath(filepath.Dir(allowed), landlock.ReadDir))

	// Keep TempDir cleanup on the unrestricted test thread after the worker exits.
	test.LockedThread(t, func() {
		require.NoError(t, ruleset.Restrict(landlock.Options{Synchronization: landlock.CallingThread, NoNewPrivileges: true}))

		entries, err := os.ReadDir(filepath.Dir(allowed))
		require.NoError(t, err)
		require.Len(t, entries, 1)
		_, err = os.ReadDir(filepath.Dir(denied))
		require.ErrorIs(t, err, test.ErrPermissionDenied)
	})
}
