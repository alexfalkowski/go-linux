//go:build linux

package inotify_test

import (
	"testing"

	"github.com/alexfalkowski/go-linux/inotify"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

// FuzzParseEvents exercises untrusted variable record lengths and name padding.
func FuzzParseEvents(f *testing.F) {
	f.Add([]byte{})
	f.Add(test.InotifyRecord(inotify.Event{Watch: 1, Mask: inotify.Create, Name: "child"}))
	f.Add(test.InotifyRecord(inotify.Event{Watch: -1, Mask: inotify.QueueOverflow}))
	f.Add([]byte{1, 2, 3})

	f.Fuzz(func(t *testing.T, input []byte) {
		events, err := inotify.ParseEvents(input)
		if err != nil {
			require.Nil(t, events, "malformed batches must not return partial results")
			return
		}

		require.LessOrEqual(t, len(events), len(input)/16, "each event requires a complete header")

		for _, event := range events {
			require.NotContains(t, event.Name, "\x00", "names must exclude ABI terminators and padding")
		}
	})
}
