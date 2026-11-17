//go:build linux

package procfs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/procfs"
	"github.com/stretchr/testify/require"
)

func TestProcessStatisticsPreserveNamesAndNativeUnits(t *testing.T) {
	for _, name := range []string{"simple", " leading (and) trailing ", "a) b( c\nnext"} {
		t.Run(name, func(t *testing.T) {
			root := test.ProcRoot(t, map[string]string{"42/stat": test.ProcStat(name)})
			process := test.ProcProcess(t, root)

			stat, err := process.Stat()
			require.NoError(t, err)

			require.Equal(t, procfs.ProcessStat{
				Name: name, State: "S", ParentPID: 7, ProcessGroupID: 8, SessionID: 9, Threads: 2,
				UserTicks: 123, SystemTicks: 45, StartTicks: 6789, VirtualMemory: 1048576, ResidentPages: 17,
			}, stat)
		})
	}
}

func TestProcessStatisticsReadChangingRecordsAndRetainUnknownStates(t *testing.T) {
	root := test.ProcRoot(t, map[string]string{"42/stat": test.ProcStat("before")})
	process := test.ProcProcess(t, root)
	before, err := process.Stat()
	require.NoError(t, err)

	updated := strings.Replace(test.ProcStat("after"), ") S ", ") Q ", 1)
	require.NoError(t, os.WriteFile(filepath.Join(root, "42/stat"), []byte(updated), 0o600))
	after, err := process.Stat()
	require.NoError(t, err)

	require.Equal(t, "before", before.Name)
	require.Equal(t, "after", after.Name)
	require.Equal(t, "Q", after.State)
}

func TestMalformedProcessStatisticsReturnErrorsWithoutPanics(t *testing.T) {
	for name, contents := range map[string]string{
		"empty": "", "missing name": "42 S 1 2", "reversed delimiters": "42 ) (", "truncated suffix": "42 (name)",
		"missing fields": "42 (name) S 1", "invalid number": strings.Replace(test.ProcStat("name"), " 123 45 ", " bad 45 ", 1),
		"invalid state": strings.Replace(test.ProcStat("name"), ") S ", ") invalid ", 1),
		"negative RSS":  strings.Replace(test.ProcStat("name"), "1048576 17 ", "1048576 -1 ", 1),
	} {
		t.Run(name, func(t *testing.T) {
			root := test.ProcRoot(t, map[string]string{"42/stat": contents})
			process := test.ProcProcess(t, root)

			stat, err := process.Stat()
			require.ErrorIs(t, err, procfs.ErrParse)
			require.Zero(t, stat)
			test.PathError(t, err, "parse", filepath.Join(root, "42/stat"))
		})
	}
}

func TestSystemStatisticsConvertCPUTicksToSeconds(t *testing.T) {
	root := test.ProcRoot(t, map[string]string{"stat": test.ProcSystemStat})
	fs, err := procfs.Open(root)
	require.NoError(t, err)

	stat, err := fs.Stat()
	require.NoError(t, err)

	require.Equal(t, procfs.SystemStat{
		CPUs: map[int]procfs.CPUTime{0: {
			User: 0.11, Nice: 0.22, System: 0.33, Idle: 0.44, IOWait: 0.55,
			IRQ: 0.66, SoftIRQ: 0.77, Steal: 0.88, Guest: 0.99, GuestNice: 1,
		}},
		Total: procfs.CPUTime{
			User: 1.1, Nice: 2.2, System: 3.3, Idle: 4.4, IOWait: 5.5,
			IRQ: 6.6, SoftIRQ: 7.7, Steal: 8.8, Guest: 9.9, GuestNice: 10,
		},
		BootTime: time.Unix(1700000000, 0).UTC(), ContextSwitches: 123, ProcessesCreated: 456, ProcessesRunning: 7, ProcessesBlocked: 8,
	}, stat)
}

func TestSystemStatisticsReturnIndependentCPUMaps(t *testing.T) {
	root := test.ProcRoot(t, map[string]string{"stat": test.ProcSystemStat})
	fs, err := procfs.Open(root)
	require.NoError(t, err)
	before, err := fs.Stat()
	require.NoError(t, err)
	before.CPUs[0] = procfs.CPUTime{}

	after, err := fs.Stat()
	require.NoError(t, err)

	require.InDelta(t, 0.11, after.CPUs[0].User, 1e-12)
}

func TestMalformedSystemStatisticsReturnNoPartialSnapshot(t *testing.T) {
	for name, contents := range map[string]string{
		"empty": "", "missing CPUs": "btime 1700000000\n", "invalid counter": test.ProcSystemStat + "ctxt bad\n",
		"negative CPU": "cpu0 -1 2 3 4\n", "not a number": "cpu0 NaN 2 3 4\n", "infinity": "cpu0 +Inf 2 3 4\n",
		"negative CPU ID": "cpu-1 1 2 3 4\n", "large CPU ID": "cpu2147483648 1 2 3 4\n",
		"boot overflow": test.ProcSystemStat + "btime 18446744073709551615\n",
	} {
		t.Run(name, func(t *testing.T) {
			root := test.ProcRoot(t, map[string]string{"stat": contents})
			fs, err := procfs.Open(root)
			require.NoError(t, err)

			stat, err := fs.Stat()
			require.ErrorIs(t, err, procfs.ErrParse)
			require.Zero(t, stat)
		})
	}
}

func TestMemoryStatisticsConvertKilobytesWithoutLosingZeroValues(t *testing.T) {
	contents := strings.Replace(test.ProcMemory, "SwapFree: 700 kB", "SwapFree: 0 kB", 1) + "FutureField: 9 kB\n"
	root := test.ProcRoot(t, map[string]string{"meminfo": contents})
	fs, err := procfs.Open(root)
	require.NoError(t, err)

	memory, err := fs.Memory()
	require.NoError(t, err)

	require.Equal(t, procfs.MemoryInfo{
		Total: 1000 * 1024, Free: 200 * 1024, Available: 600 * 1024, Buffers: 30 * 1024,
		Cached: 400 * 1024, SwapTotal: 800 * 1024, SwapFree: 0,
	}, memory)
}

func TestMalformedMemoryStatisticsReturnNoPartialSnapshot(t *testing.T) {
	for name, contents := range map[string]string{
		"empty": "", "short line": "MemTotal:\n", "nonnumeric": "MemTotal: bad kB\n", "unknown unit": "MemTotal: 1 MB\n",
		"missing field": strings.Replace(test.ProcMemory, "MemFree: 200 kB\n", "", 1),
		"overflow":      strings.Replace(test.ProcMemory, "1000", "18446744073709551615", 1),
		"missing unit":  strings.Replace(test.ProcMemory, "MemTotal: 1000 kB", "MemTotal: 1000", 1),
	} {
		t.Run(name, func(t *testing.T) {
			root := test.ProcRoot(t, map[string]string{"meminfo": contents})
			fs, err := procfs.Open(root)
			require.NoError(t, err)

			memory, err := fs.Memory()
			require.ErrorIs(t, err, procfs.ErrParse)
			require.Zero(t, memory)
		})
	}
}
