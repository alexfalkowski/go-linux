//go:build linux

package procfs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/internal/testenv"
	"github.com/alexfalkowski/go-linux/procfs"
	"github.com/stretchr/testify/require"
)

func TestProcessViewsObserveDisappearanceAndPIDReplacement(t *testing.T) {
	root := test.ProcRoot(t, map[string]string{"42/stat": test.ProcStat("original")})
	process := test.ProcProcess(t, root)

	require.NoError(t, os.RemoveAll(filepath.Join(root, "42")))
	stat, err := process.Stat()
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Zero(t, stat)

	require.NoError(t, os.Mkdir(filepath.Join(root, "42"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "42/stat"), []byte(test.ProcStat("replacement")), 0o600))
	stat, err = process.Stat()
	require.NoError(t, err)

	require.Equal(t, "replacement", stat.Name, "the view must not imply pidfd-like identity")
}

func TestConcurrentSystemReadsOwnTheirResults(t *testing.T) {
	root := test.ProcRoot(t, map[string]string{"stat": test.ProcSystemStat})
	fs, err := procfs.Open(root)
	require.NoError(t, err)
	type result struct {
		err  error
		user float64
	}
	const workers = 16
	results := make(chan result, workers)
	start := make(chan struct{})

	for range workers {
		go func() {
			<-start
			stat, err := fs.Stat()
			if err != nil {
				results <- result{err: err}
				return
			}
			user := stat.CPUs[0].User
			stat.CPUs[0] = procfs.CPUTime{}
			results <- result{user: user}
		}()
	}
	close(start)

	for range workers {
		value := test.Receive(t, results)
		require.NoError(t, value.err)
		require.InDelta(t, 0.11, value.user, 1e-12, "concurrent callers must receive independent counters")
	}
}

func TestSuccessfulAndFailedReadsReleaseTemporaryDescriptors(t *testing.T) {
	root := test.ProcRoot(t, map[string]string{
		"42/stat": test.ProcStat("example"), "42/maps": "bad\n", "42/mountinfo": "bad\n", "meminfo": "",
	})
	before := test.DescriptorCount(t)
	fs, err := procfs.Open(root)
	require.NoError(t, err)
	process, err := fs.Self()
	require.NoError(t, err)

	for range 8 {
		_, err := process.Stat()
		require.NoError(t, err)

		_, err = process.Maps()
		require.ErrorIs(t, err, procfs.ErrParse)

		_, err = process.Mounts()
		require.ErrorIs(t, err, procfs.ErrParse)

		_, err = fs.Memory()
		require.ErrorIs(t, err, procfs.ErrParse)
	}

	require.Equal(t, before, test.DescriptorCount(t), "views and completed reads retain no descriptors")
}

func TestPermissionFailuresRetainNativeErrno(t *testing.T) {
	if os.Geteuid() == 0 {
		options, err := testenv.FromEnvironment()
		require.NoError(t, err)

		const reason = "permission denial requires an ordinary user without DAC override"
		require.False(t, options.Required(), reason)
		t.Skip(reason)
	}
	root := test.ProcRoot(t, map[string]string{"42/stat": test.ProcStat("example")})
	process := test.ProcProcess(t, root)
	require.NoError(t, os.Chmod(filepath.Join(root, "42/stat"), 0))

	stat, err := process.Stat()
	require.ErrorIs(t, err, test.ErrPermissionDenied)
	require.Zero(t, stat)
	require.Equal(t, test.ErrPermissionDenied, test.ErrorNumber(t, err))
}

func TestRemovingTheRootIsVisibleToEnumeration(t *testing.T) {
	root := test.ProcRoot(t, nil)
	fs, err := procfs.Open(root)
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(root))

	pids, err := fs.Processes()
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Nil(t, pids)

	process, err := fs.Self()
	test.NoResource(t, process, err, os.ErrNotExist)
}
