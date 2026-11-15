//go:build linux

package inotify_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"testing"

	"github.com/alexfalkowski/go-linux/inotify"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

func TestParsingPreservesWatchIDsCookiesNamesAndUnknownBits(t *testing.T) {
	expected := []inotify.Event{
		{Watch: 123, Mask: inotify.MovedFrom, Cookie: 0x12345678, Name: "old"},
		{Watch: math.MaxInt32, Mask: inotify.MovedTo | inotify.IsDir | 0x80000, Cookie: 0x12345678, Name: "new-\xff"},
		{Watch: -1, Mask: inotify.QueueOverflow},
		{Watch: 123, Mask: inotify.Ignored},
	}
	batch := make([]byte, 0, len(expected)*32)
	for _, event := range expected {
		batch = append(batch, test.InotifyRecord(event)...)
	}

	actual, err := inotify.ParseEvents(batch)
	require.NoError(t, err)

	clear(batch)
	require.Equal(t, expected, actual, "decoded names must survive buffer reuse")
}

func TestParsingEmptyBatchesReturnsNoEvents(t *testing.T) {
	for _, input := range [][]byte{nil, {}} {
		events, err := inotify.ParseEvents(input)
		require.NoError(t, err)
		require.Nil(t, events)
	}
}

func TestParsingRejectsMalformedBatchesWithoutReturningAPartialPrefix(t *testing.T) {
	valid := test.InotifyRecord(inotify.Event{Watch: 1, Mask: inotify.Create, Name: "child"})
	oversized := bytes.Clone(valid)
	binary.NativeEndian.PutUint32(oversized[12:16], math.MaxUint32)
	unterminated := bytes.Clone(valid)
	copy(unterminated[16:], bytes.Repeat([]byte{'x'}, len(unterminated)-16))
	badPadding := bytes.Clone(valid)
	badPadding[len(badPadding)-1] = 'x'

	for _, scenario := range []struct {
		name  string
		input []byte
	}{
		{name: "short header", input: valid[:15]},
		{name: "short name", input: valid[:len(valid)-1]},
		{name: "oversized name", input: oversized},
		{name: "missing terminator", input: unterminated},
		{name: "nonzero padding", input: badPadding},
		{name: "trailing fragment", input: append(bytes.Clone(valid), 1)},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			for _, prefix := range [][]byte{nil, valid} {
				input := append(bytes.Clone(prefix), scenario.input...)
				events, err := inotify.ParseEvents(input)
				require.ErrorIs(t, err, os.ErrInvalid)
				require.Nil(t, events, "a malformed suffix must not look like a complete batch")
			}
		})
	}
}
