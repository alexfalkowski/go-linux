//go:build linux

package fanotify //nolint:testpackage // P11 requires malformed live-descriptor ABI tests without a public raw-descriptor decoder.

import (
	"encoding/binary"
	"io"
	"os"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// These ABI tests implement P11's malformed-record acceptance requirement.
// A public decoder would incorrectly permit arbitrary bytes to transfer or
// close process descriptors. Ordinary scenarios live in package fanotify_test.
func TestDecoderPreservesUnknownRecordsAndMetadataExtensions(t *testing.T) {
	reader, _ := test.Pipe(t)
	group := &Group{file: reader}
	file := test.TempFile(t, 4)
	test.WriteAt(t, file, 0, "data")
	fd := test.DuplicateDescriptor(t, file)
	record := test.FanotifyRecord(int32(fd), 123, uint64(Open)|1<<63, []byte{255, 0, 7, 0, 1, 2, 3, 254, 0, 4, 0}) //nolint:gosec // A live descriptor fits the Linux signed field.
	// The metadata extension is opaque; info begins at metadata_len.
	record = append(record[:24:24], append([]byte{8, 9}, record[24:]...)...)
	binary.NativeEndian.PutUint16(record[6:8], 26)
	binary.NativeEndian.PutUint32(record[:4], uint32(len(record))) //nolint:gosec // The fixture contains a single bounded record.
	before := test.DescriptorCount(t)
	events, err := group.decode(record)
	require.NoError(t, err)
	require.Len(t, events, 1)
	event := events[0]
	test.Cleanup(t, event)
	clear(record)
	require.Equal(t, Open|1<<63, event.Mask)
	require.Equal(t, 123, event.PID)
	require.Equal(t, []Info{{Type: 255, Data: []byte{1, 2, 3}}, {Type: 254}}, event.Info)
	require.Equal(t, []byte("data"), test.ReadAt(t, event, 0, 4))
	require.NoError(t, event.Close())
	require.Equal(t, before-1, test.DescriptorCount(t), "the event must consume exactly its transferred file")
}

func TestDecoderRejectsMalformedBatchesAndClosesEveryIdentifiableFile(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func([]byte) []byte
		err    error
	}{
		{name: "short record", err: os.ErrInvalid, change: func(p []byte) []byte {
			return p[:23]
		}},
		{name: "short length", err: os.ErrInvalid, change: func(p []byte) []byte {
			binary.NativeEndian.PutUint32(p[:4], 1)
			return p
		}},
		{name: "long length", err: os.ErrInvalid, change: func(p []byte) []byte {
			binary.NativeEndian.PutUint32(p[:4], 1000)
			return p
		}},
		{name: "short metadata", err: os.ErrInvalid, change: func(p []byte) []byte {
			binary.NativeEndian.PutUint16(p[6:8], 23)
			return p
		}},
		{name: "long metadata", err: os.ErrInvalid, change: func(p []byte) []byte {
			binary.NativeEndian.PutUint16(p[6:8], 99)
			return p
		}},
		{name: "wrong version", err: unix.EPROTO, change: func(p []byte) []byte {
			p[4]++
			return p
		}},
		{name: "short info", err: os.ErrInvalid, change: func(p []byte) []byte {
			p = append(p[:24], 1, 0, 3)
			binary.NativeEndian.PutUint32(p[:4], 27)
			return p
		}},
		{name: "zero info length", err: os.ErrInvalid, change: func(p []byte) []byte {
			p[26], p[27] = 0, 0
			return p
		}},
		{name: "oversized info", err: os.ErrInvalid, change: func(p []byte) []byte {
			p[26], p[27] = 255, 255
			return p
		}},
		{name: "invalid descriptor", err: os.ErrInvalid, change: func(p []byte) []byte {
			binary.NativeEndian.PutUint32(p[16:20], ^uint32(1))
			return p
		}},
		{name: "missing file", err: os.ErrInvalid, change: func(p []byte) []byte {
			binary.NativeEndian.PutUint64(p[8:16], uint64(Open))
			return p
		}},
		{name: "permission without file", err: os.ErrInvalid, change: func(p []byte) []byte {
			binary.NativeEndian.PutUint64(p[8:16], uint64(QueueOverflow|OpenPermission))
			return p
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			reader, _ := test.Pipe(t)
			group := &Group{file: reader}
			fd := test.DuplicateDescriptor(t, test.TempFile(t, 0))
			// The malformed record itself has no resource. A valid prefix owns
			// a real descriptor, proving that errors cannot leak partial results.
			prefix := test.FanotifyRecord(int32(fd), 1, uint64(Open), nil) //nolint:gosec // Linux descriptors fit int32.
			bad := scenario.change(test.FanotifyRecord(-1, 0, uint64(QueueOverflow), []byte{99, 0, 4, 0}))
			events, err := group.decode(append(prefix, bad...))
			require.Nil(t, events)
			require.ErrorIs(t, err, scenario.err)
			_, err = unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
			require.ErrorIs(t, err, unix.EBADF)
			_, err = reader.Read(make([]byte, 1))
			require.ErrorIs(t, err, os.ErrClosed)
		})
	}
}

func TestDecoderContinuesCleanupAfterMalformedInformation(t *testing.T) {
	reader, _ := test.Pipe(t)
	group := &Group{file: reader}
	file := test.TempFile(t, 0)
	first, second := test.DuplicateDescriptor(t, file), test.DuplicateDescriptor(t, file)
	p := test.FanotifyRecord(int32(first), 1, uint64(Open), []byte{99, 0, 0, 0}) //nolint:gosec // Linux descriptors fit int32.
	p = append(p, test.FanotifyRecord(int32(second), 1, uint64(Open), nil)...)   //nolint:gosec // Linux descriptors fit int32.
	events, err := group.decode(p)
	require.Nil(t, events)
	require.ErrorIs(t, err, os.ErrInvalid)
	for _, fd := range []int{first, second} {
		_, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		require.ErrorIs(t, err, unix.EBADF, "descriptor %d must close even after an earlier malformed extension", fd)
	}
}

func TestDecoderOverflowOwnsNoFile(t *testing.T) {
	reader, _ := test.Pipe(t)
	group := &Group{file: reader}
	events, err := group.decode(test.FanotifyRecord(-1, 0, uint64(QueueOverflow)|1<<63, nil))
	require.NoError(t, err)
	require.Len(t, events, 1)
	event := events[0]
	require.Equal(t, QueueOverflow|1<<63, event.Mask)
	_, err = event.Stat()
	require.ErrorIs(t, err, unix.ENODATA)
	_, err = event.ReadAt(make([]byte, 1), 0)
	require.ErrorIs(t, err, unix.ENODATA)
	require.NoError(t, event.Close())
	events, err = group.decode(nil)
	require.NoError(t, err)
	require.Empty(t, events)
}

func TestPermissionBatchCleanupEncodesDenyForEveryUnreadEvent(t *testing.T) {
	reader, writer := test.Pipe(t)
	group := &Group{file: writer}
	file := test.TempFile(t, 0)
	descriptors := make([]int, 0, 3)
	records := make([]byte, 0, 72)
	for range 3 {
		fd := test.DuplicateDescriptor(t, file)
		descriptors = append(descriptors, fd)
		records = append(records, test.FanotifyRecord(int32(fd), 1, uint64(OpenPermission), nil)...) //nolint:gosec // Linux descriptors fit int32.
	}
	events, err := group.decode(records)
	require.NoError(t, err)
	// Exported observations must not control the actual permission lifecycle.
	events[0].Mask = 0
	require.False(t, lendEvents(events, func(event *Event, err error) bool {
		require.NoError(t, err)
		require.Same(t, events[0], event)
		return false
	}), "breaking must stop iteration while releasing the whole batch")
	responses := make([]byte, 24)
	_, err = io.ReadFull(reader, responses)
	require.NoError(t, err)
	for index, fd := range descriptors {
		response := responses[index*8 : (index+1)*8]
		require.EqualValues(t, fd, binary.NativeEndian.Uint32(response[:4]), "response must retain its owned event identity")
		require.Equal(t, uint32(Deny), binary.NativeEndian.Uint32(response[4:]), "unanswered events must receive Deny")
		_, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		require.ErrorIs(t, err, unix.EBADF)
	}
}

func TestResponseFailureRetainsOwnershipUntilCloseReleasesGroupAndFile(t *testing.T) {
	reader, _ := test.Pipe(t)
	group := &Group{file: reader}
	fd := test.DuplicateDescriptor(t, test.TempFile(t, 0))
	events, err := group.decode(test.FanotifyRecord(int32(fd), 1, uint64(OpenPermission), nil)) //nolint:gosec // Linux descriptors fit int32.
	require.NoError(t, err)
	require.Len(t, events, 1)
	event := events[0]
	test.Cleanup(t, event)
	require.ErrorIs(t, event.Respond(Allow), unix.EBADF, "a read-only pipe rejects the encoded response")
	_, err = event.Stat()
	require.NoError(t, err, "failed Respond must leave the event caller-owned")
	require.ErrorIs(t, event.Close(), unix.EBADF)
	_, err = reader.Read(make([]byte, 1))
	require.ErrorIs(t, err, os.ErrClosed, "failed cleanup response must close the group")
	_, err = unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
	require.ErrorIs(t, err, unix.EBADF, "response errors must not leak the event file")
	require.ErrorIs(t, event.Close(), os.ErrClosed)
}
