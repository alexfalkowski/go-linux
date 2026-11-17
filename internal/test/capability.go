//go:build linux

package test

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/alexfalkowski/go-linux/capability"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// CapabilityProcess runs a capability mutation scenario in a bounded subprocess
// with inherited credentials. It returns true only in the child. The caller
// locks its goroutine before mutations; no parent test thread changes state.
func CapabilityProcess(t *testing.T) bool {
	t.Helper()
	const marker = "capability-isolated"
	if os.Args[len(os.Args)-1] == marker {
		return true
	}

	ctx, command := Command(t, t.Name(), "-test.v", "--", marker)
	output, err := command.CombinedOutput()
	require.NoError(t, ctx.Err(), "capability scenario exceeded its time bound")
	require.NoError(t, err, "isolated capability scenario: %s", output)
	t.Log(string(output))
	return false
}

// CapabilityStatus independently reads all five capability masks from the
// calling thread's procfs status. The caller pins the goroutine for comparisons.
func CapabilityStatus(t *testing.T) map[string]capability.Set {
	t.Helper()
	data, err := os.ReadFile("/proc/thread-self/status")
	require.NoError(t, err)

	sets := make(map[string]capability.Set)
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !strings.HasPrefix(fields[0], "Cap") {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 16, 64)
		require.NoError(t, err)
		sets[strings.TrimSuffix(fields[0], ":")] = capability.Set(value)
	}
	for _, name := range []string{"CapPrm", "CapEff", "CapInh", "CapBnd", "CapAmb"} {
		require.Contains(t, sets, name)
	}
	return sets
}

// CapabilityExecRules disables UID-0 capability restoration for an isolated exec
// fixture, so exec tests observe ordinary file/ambient transformations. This
// changes only the locked child's securebits, never the main test runner.
func CapabilityExecRules(t *testing.T) {
	t.Helper()
	// SECBIT_NOROOT from Linux 7.0 include/uapi/linux/securebits.h.
	require.NoError(t, unix.Prctl(unix.PR_SET_SECUREBITS, 1, 0, 0, 0))
}

// BlockAmbientRaise sets SECBIT_NO_CAP_AMBIENT_RAISE on a locked isolated child.
func BlockAmbientRaise(t *testing.T) {
	t.Helper()
	// Linux 7.0 include/uapi/linux/securebits.h assigns this bit index 6.
	require.NoError(t, unix.Prctl(unix.PR_SET_SECUREBITS, 1<<6, 0, 0, 0))
}
