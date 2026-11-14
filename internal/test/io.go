//go:build linux

package test

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

// ReadAt reads exactly size bytes for an ordinary file-content observation.
// Record-size and partial-I/O tests should inspect their own count and error.
func ReadAt(t *testing.T, reader io.ReaderAt, offset int64, size int) []byte {
	t.Helper()

	contents := make([]byte, size)

	n, err := reader.ReadAt(contents, offset)
	require.NoError(t, err)
	require.Equal(t, size, n)
	return contents
}

// WriteAt writes complete fixture contents without changing the file offset.
func WriteAt(t *testing.T, writer io.WriterAt, offset int64, contents string) {
	t.Helper()
	n, err := writer.WriteAt([]byte(contents), offset)
	require.NoError(t, err)
	require.Equal(t, len(contents), n)
}
