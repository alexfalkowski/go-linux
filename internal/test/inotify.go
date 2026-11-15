//go:build linux

package test

import (
	"encoding/binary"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/inotify"
	"github.com/alexfalkowski/go-linux/internal/testenv"
	"github.com/stretchr/testify/require"
)

// InotifyQueueLimit reads the host's existing per-instance queue limit without
// changing policy. Excessively large limits fail required coverage and otherwise
// skip the bounded overflow fixture.
func InotifyQueueLimit(t *testing.T) int {
	t.Helper()
	value, err := os.ReadFile("/proc/sys/fs/inotify/max_queued_events")
	require.NoError(t, err)
	limit, err := strconv.Atoi(strings.TrimSpace(string(value)))
	require.NoError(t, err)
	require.GreaterOrEqual(t, limit, 0, "inotify queue limit must be nonnegative")
	if limit > 65536 {
		options, err := testenv.FromEnvironment()
		require.NoError(t, err)
		require.False(t, options.Required, "overflow fixture requires max_queued_events <= 65536; observed %d", limit)
		t.Skipf("overflow fixture bounds queue generation at 65536; host max_queued_events=%d", limit)
	}
	return limit
}

// Watcher creates an empty watcher with cleanup and bounded reads.
func Watcher(t *testing.T) *inotify.Watcher {
	t.Helper()
	watcher, err := inotify.New()
	require.NoError(t, err)
	Cleanup(t, watcher)
	require.NoError(t, watcher.SetReadDeadline(time.Now().Add(5*time.Second)))
	return watcher
}

// InotifyRecord encodes a fixture using Linux's fixed-width native-endian
// inotify_event layout, independently of the production decoder. Names are
// padded to sixteen bytes as in Linux 7.0's copy_event_to_user.
func InotifyRecord(event inotify.Event) []byte {
	length := 0
	if event.Name != "" {
		length = (len(event.Name) + 16) &^ 15
	}
	record := make([]byte, 16+length)
	binary.NativeEndian.PutUint32(record[:4], uint32(event.Watch)) //nolint:gosec // Preserve the signed wd bit pattern, including -1.
	binary.NativeEndian.PutUint32(record[4:8], uint32(event.Mask))
	binary.NativeEndian.PutUint32(record[8:12], event.Cookie)
	binary.NativeEndian.PutUint32(record[12:16], uint32(length))
	copy(record[16:], event.Name)
	return record
}
