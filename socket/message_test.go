//go:build linux

package socket_test

import (
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/socket"
	"github.com/stretchr/testify/require"
)

func TestPassedFilesRemainUsableAfterSenderCloses(t *testing.T) {
	for _, mode := range []socket.Kind{socket.Datagram, socket.SeqPacket} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			a, b := test.MessagePair(t, mode)
			file := test.TempFile(t, 16)
			test.WriteAt(t, file, 0, "shared contents")

			n, err := a.Send([]byte("file"), nil, file)
			require.NoError(t, err)
			require.Equal(t, 4, n)

			require.NoError(t, file.Close())
			require.NoError(t, a.Close())

			message, err := b.Receive(make([]byte, 16), 1)
			require.NoError(t, err)
			require.Len(t, message.Files, 1)
			test.Cleanup(t, message.Files[0])
			require.Equal(t, "shared contents", string(test.ReadAt(t, message.Files[0], 0, 15)))

			require.NoError(t, b.Close())

			test.WriteAt(t, message.Files[0], 0, "alive")
		})
	}
}

func TestPayloadTruncationStillTransfersFileOwnership(t *testing.T) {
	a, b := test.MessagePair(t, socket.Datagram)
	file := test.TempFile(t, 0)
	_, err := a.Send([]byte("payload"), nil, file)
	require.NoError(t, err)

	message, err := b.Receive(make([]byte, 1), 1)
	require.NoError(t, err)
	require.NotZero(t, message.Flags&socket.Truncated)
	require.Len(t, message.Files, 1)
	test.Cleanup(t, message.Files[0])
	test.WriteAt(t, message.Files[0], 0, "still owned")
}

func TestAncillaryTruncationClosesAllReceivedRights(t *testing.T) {
	a, b := test.MessagePair(t, socket.Datagram)
	file := test.TempFile(t, 0)
	files := make([]*os.File, 32)
	for i := range files {
		files[i] = file
	}

	before := test.DescriptorCount(t)
	for range 16 {
		_, err := a.Send([]byte("rights"), nil, files...)
		require.NoError(t, err)

		message, err := b.Receive(make([]byte, 16), 0)
		require.ErrorIs(t, err, test.ErrMessageTooLarge)
		require.NotZero(t, message.Flags&socket.ControlTruncated)
		require.Nil(t, message.Files, "error must not transfer any cleanup responsibility")
	}

	require.Equal(t, before, test.DescriptorCount(t), "truncated rights must not leak")

	test.WriteAt(t, file, 0, "borrowed input stays open")
}

func TestFileLimitClosesRightsEvenWhenKernelControlBufferFits(t *testing.T) {
	a, b := test.MessagePair(t, socket.Datagram)
	file := test.TempFile(t, 0)
	before := test.DescriptorCount(t)
	_, err := a.Send([]byte("file"), nil, file)
	require.NoError(t, err)

	message, err := b.Receive(make([]byte, 8), 0)
	require.ErrorIs(t, err, test.ErrMessageTooLarge)
	require.Nil(t, message.Files)
	require.Equal(t, before, test.DescriptorCount(t))
}

func TestClosingReceiverReleasesQueuedFileReferences(t *testing.T) {
	a, b := test.MessagePair(t, socket.Datagram)
	reader, writer := test.Pipe(t)
	_, err := a.Send([]byte("writer"), nil, writer)
	require.NoError(t, err)

	require.NoError(t, writer.Close())

	pending := test.StartRead(t, func() (int, error) {
		return reader.Read(make([]byte, 1))
	})
	require.NoError(t, b.Close())
	require.ErrorIs(t, test.Receive(t, pending).Err, io.EOF, "discarding queued rights must release the last writer")
}

func TestMaximumRightsAndCredentialsFitOneMessage(t *testing.T) {
	a, b := test.MessagePair(t, socket.Datagram)
	require.NoError(t, b.SetPassCredentials(true))

	file := test.TempFile(t, 0)
	files := make([]*os.File, 253)
	for i := range files {
		files[i] = file
	}

	before := test.DescriptorCount(t)
	_, err := a.Send([]byte("maximum"), nil, files...)
	require.NoError(t, err)

	message, err := b.Receive(make([]byte, 16), 253)
	require.NoError(t, err)

	for _, received := range message.Files {
		test.Cleanup(t, received)
	}

	require.Len(t, message.Files, 253)
	require.NotNil(t, message.Credentials, "credential and rights buffers must fit together")
	require.Zero(t, message.Flags&socket.ControlTruncated)

	for _, received := range message.Files {
		require.NoError(t, received.Close())
	}

	require.Equal(t, before, test.DescriptorCount(t), "all received references and send duplicates must close")
}

func TestExpiredSendsReleaseBorrowedDuplicates(t *testing.T) {
	a, _ := test.MessagePair(t, socket.Datagram)
	file := test.TempFile(t, 0)
	before := test.DescriptorCount(t)
	require.NoError(t, a.SetWriteDeadline(time.Now().Add(-time.Second)))

	_, err := a.Send([]byte("deadline"), nil, file)
	require.ErrorIs(t, err, os.ErrDeadlineExceeded)
	require.Equal(t, before, test.DescriptorCount(t))

	test.WriteAt(t, file, 0, "retained")
}

func TestFailedSendsReleaseDuplicatesAndLeaveInputsOwned(t *testing.T) {
	a, _ := test.MessagePair(t, socket.Datagram)
	file := test.TempFile(t, 0)
	closed := test.TempFile(t, 0)
	require.NoError(t, closed.Close())

	before := test.DescriptorCount(t)
	for _, scenario := range []struct {
		name  string
		files []*os.File
		err   error
	}{
		{name: "nil after valid file", files: []*os.File{file, nil}, err: os.ErrInvalid},
		{name: "closed after valid file", files: []*os.File{file, closed}, err: os.ErrClosed},
		{name: "too many files", files: make([]*os.File, 254), err: os.ErrInvalid},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			_, err := a.Send([]byte("x"), nil, scenario.files...)
			require.ErrorIs(t, err, scenario.err)
			require.Equal(t, before, test.DescriptorCount(t))
		})
	}

	test.WriteAt(t, file, 0, "input retained")
}

func TestRejectedMessagePayloadsReleaseBorrowedDuplicates(t *testing.T) {
	a, _ := test.MessagePair(t, socket.Datagram)
	file := test.TempFile(t, 0)
	before := test.DescriptorCount(t)

	_, err := a.Send(nil, nil, file)
	require.ErrorIs(t, err, os.ErrInvalid)

	_, err = a.Send(make([]byte, 1<<20), nil, file)
	require.ErrorIs(t, err, test.ErrMessageTooLarge)
	require.Equal(t, before, test.DescriptorCount(t))

	test.WriteAt(t, file, 0, "input retained")
}

func TestUnixPeerCredentialsDescribeConnectedProcess(t *testing.T) {
	_, b := test.MessagePair(t, socket.SeqPacket)
	peer, err := b.PeerCredentials()
	require.NoError(t, err)
	require.Equal(t, socket.Credentials{PID: int32(os.Getpid()), UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}, peer) //nolint:gosec // Linux IDs fit these ABI fields.
}

func TestUnixMessageCredentialsCanBeEnabledAndDisabled(t *testing.T) {
	a, b := test.MessagePair(t, socket.SeqPacket)

	require.NoError(t, b.SetPassCredentials(true))

	_, err := a.Send([]byte("identified"), nil)
	require.NoError(t, err)

	message, err := b.Receive(make([]byte, 32), 0)
	require.NoError(t, err)
	require.Equal(t, &socket.Credentials{PID: int32(os.Getpid()), UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}, message.Credentials) //nolint:gosec // Linux ABI fields.

	require.NoError(t, b.SetPassCredentials(false))

	_, err = a.Send([]byte("anonymous"), nil)
	require.NoError(t, err)

	message, err = b.Receive(make([]byte, 32), 0)
	require.NoError(t, err)
	require.Nil(t, message.Credentials)
}

func TestReceivedRightsAreAtomicallyClosedOnExecAndShareOffsets(t *testing.T) {
	a, b := test.MessagePair(t, socket.Datagram)
	file := test.TempFile(t, 8)
	_, err := a.Send([]byte("x"), nil, file, file)
	require.NoError(t, err)

	message, err := b.Receive(make([]byte, 1), 2)
	require.NoError(t, err)
	require.Len(t, message.Files, 2)

	for _, received := range message.Files {
		test.Cleanup(t, received)
		test.CloseOnExec(t, test.FileDescriptor(t, received))
	}

	_, err = message.Files[0].Seek(3, 0)
	require.NoError(t, err)

	offset, err := message.Files[1].Seek(0, 1)
	require.NoError(t, err)
	require.Equal(t, int64(3), offset, "SCM_RIGHTS shares the open file description")
}
