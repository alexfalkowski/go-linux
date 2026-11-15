//go:build linux

package fanotify //nolint:testpackage // P11 requires parser fuzzing; the live-descriptor decoder must remain private.

import (
	"encoding/binary"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

// FuzzDecode checks variable record framing without allowing fuzz bytes to own
// real process descriptors. Live descriptor cleanup is verified by ABI tests.
func FuzzDecode(f *testing.F) {
	f.Add(test.FanotifyRecord(-1, 0, uint64(QueueOverflow), nil))
	f.Add(test.FanotifyRecord(-1, 0, uint64(QueueOverflow), []byte{255, 0, 7, 0, 1, 2, 3}))
	f.Add([]byte{1, 2, 3})

	f.Fuzz(func(t *testing.T, p []byte) {
		p = append([]byte(nil), p...)
		for tail := p; len(tail) >= 24; {
			// Every header the decoder can reach has FAN_NOFD, never a live fd.
			binary.NativeEndian.PutUint32(tail[16:20], ^uint32(0))
			length := uint64(binary.NativeEndian.Uint32(tail[:4]))
			if length < 24 || length > uint64(len(tail)) {
				break
			}

			tail = tail[int(length):] //nolint:gosec // Bounded by len(tail) above.
		}

		events, err := new(Group).decode(p)
		if err != nil {
			require.Nil(t, events)
			return
		}

		for _, event := range events {
			require.NotZero(t, event.Mask&QueueOverflow)
			require.Zero(t, event.Mask&permissions)
			require.Nil(t, event.file)

			require.NoError(t, event.Close())
		}
	})
}
