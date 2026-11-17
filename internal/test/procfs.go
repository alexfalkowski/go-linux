//go:build linux

package test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexfalkowski/go-linux/procfs"
	"github.com/stretchr/testify/require"
)

// ProcMemory supplies distinct synthetic kB values for every exposed field.
const ProcMemory = "MemTotal: 1000 kB\nMemFree: 200 kB\nMemAvailable: 600 kB\nBuffers: 30 kB\nCached: 400 kB\nSwapTotal: 800 kB\nSwapFree: 700 kB\n"

// ProcSystemStat supplies distinct synthetic CPU ticks and system counters.
const ProcSystemStat = "cpu 110 220 330 440 550 660 770 880 990 1000\ncpu0 11 22 33 44 55 66 77 88 99 100\n" +
	"btime 1700000000\nctxt 123\nprocesses 456\nprocs_running 7\nprocs_blocked 8\n"

// ProcDescriptorInfo supplies common fdinfo fields with a close-on-exec reader.
const ProcDescriptorInfo = "pos:\t17\nflags:\t02000000\nmnt_id:\t29\nino:\t31\n"

// ProcMountInfo supplies synthetic mountinfo with encoded path characters.
const ProcMountInfo = "29 17 8:3 /root\\040dir /mnt\\011point rw,nosuid,custom=yes shared:2 future:9 - ext4 /dev/source\\134040 rw,errors=continue\n"

// ProcRoot creates only caller-supplied synthetic files, plus PID 42 and a self
// symlink. It never captures process names, paths, arguments, or host statistics.
func ProcRoot(t *testing.T, entries map[string]string) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "42"), 0o700))
	require.NoError(t, os.Symlink("42", filepath.Join(root, "self")))
	for name, contents := range entries {
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	}
	return root
}

// ProcProcess opens the conventional synthetic PID 42 below root.
func ProcProcess(t *testing.T, root string) *procfs.Process {
	t.Helper()
	fs, err := procfs.Open(root)
	require.NoError(t, err)
	process, err := fs.Process(42)
	require.NoError(t, err)
	return process
}

// ProcDescriptor creates an fdinfo/3 fixture with an fd symlink, without opening
// or allocating descriptor 3 in any real process. The returned root is mutable.
func ProcDescriptor(t *testing.T, contents string) (string, *procfs.Process) {
	t.Helper()
	root := ProcRoot(t, map[string]string{"42/fdinfo/3": contents})
	require.NoError(t, os.Mkdir(filepath.Join(root, "42/fd"), 0o700))
	require.NoError(t, os.Symlink("/synthetic/file (deleted)", filepath.Join(root, "42/fd/3")))
	return root, ProcProcess(t, root)
}

// ProcStat supplies the complete native stat record for PID 42 with a chosen
// comm name. Values are synthetic, including every address and clock counter.
func ProcStat(name string) string {
	return fmt.Sprintf("42 (%s) S 7 8 9 0 -1 4194304 11 12 13 14 123 45 0 0 20 0 2 0 6789 1048576 17 "+
		"18446744073709551615 4096 8192 12288 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n", name)
}
