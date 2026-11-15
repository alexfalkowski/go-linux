//go:build linux && clockchange

package test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// ShiftRealtime changes the machine's wall clock and restores it, accounting
// for monotonic elapsed time, at cleanup. Run only on a disposable VM: neither
// a subprocess nor a time namespace isolates CLOCK_REALTIME from its host.
// The explicit acknowledgment is required even when the caller has CAP_SYS_TIME.
func ShiftRealtime(t *testing.T, offset time.Duration) {
	t.Helper()
	require.Equal(t, "true", os.Getenv("GO_LINUX_TEST_CLOCK_CHANGE"), "clock changes require clock_change=true on a disposable VM")

	var original unix.Timespec
	require.NoError(t, unix.ClockGettime(unix.CLOCK_REALTIME, &original))
	started := time.Now()
	t.Cleanup(func() {
		restored := unix.NsecToTimespec(original.Nano() + int64(time.Since(started)))
		require.NoError(t, unix.ClockSettime(unix.CLOCK_REALTIME, &restored), "restore the VM clock after the scenario")
	})

	shifted := unix.NsecToTimespec(original.Nano() + int64(offset))
	require.NoError(t, unix.ClockSettime(unix.CLOCK_REALTIME, &shifted), "clock changes require CAP_SYS_TIME in the initial user namespace")
}
