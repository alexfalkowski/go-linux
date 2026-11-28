//go:build linux

package mount_test

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/alexfalkowski/go-linux/file"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

func TestFallocateUnsharesOnlyTheSelectedReflinkedRange(t *testing.T) {
	path := test.XFSFilesystem(t)
	if path == "" {
		return
	}

	for _, scenario := range []struct {
		name        string
		mode        file.AllocationMode
		extraBlocks int
	}{
		{name: "extend past EOF", mode: file.UnshareRange, extraBlocks: 1},
		{name: "preserve EOF", mode: file.UnshareRange | file.KeepSize},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			source, err := os.CreateTemp(path, "source-")
			require.NoError(t, err)
			test.Cleanup(t, source)
			clone, err := os.CreateTemp(path, "clone-")
			require.NoError(t, err)
			test.Cleanup(t, clone)
			block := test.FileAllocationBlockSize(t, source, false)
			selectedOffset := uint64(block) //nolint:gosec // statfs gives the positive block size of the fixture filesystem.
			originalEOF := 2 * selectedOffset
			original := append(bytes.Repeat([]byte("a"), block), bytes.Repeat([]byte("b"), block)...)
			_, err = source.Write(original)
			require.NoError(t, err)
			require.NoError(t, source.Sync())
			test.CloneFile(t, clone, source)

			neighbor := test.FileExtentAt(t, source, 0)
			selected := test.FileExtentAt(t, source, selectedOffset)
			require.True(t, neighbor.Shared)
			require.True(t, selected.Shared)
			require.Equal(t, neighbor, test.FileExtentAt(t, clone, 0))
			require.Equal(t, selected, test.FileExtentAt(t, clone, selectedOffset))
			require.False(t, test.FileBlockAllocatedAt(t, clone, originalEOF), "the range beyond the original EOF must start unmapped")
			_, err = clone.Seek(11, io.SeekStart)
			require.NoError(t, err)

			require.NoError(t, file.Fallocate(clone, scenario.mode, int64(block), int64(2*block)))

			require.True(t, test.FileBlockAllocatedAt(t, clone, originalEOF), "the block past the original EOF must be allocated")
			private := test.FileExtentAt(t, clone, selectedOffset)
			require.False(t, private.Shared, "the selected block must be private")
			require.NotEqual(t, selected.Physical, private.Physical)
			require.Equal(t, neighbor, test.FileExtentAt(t, clone, 0), "the preceding block must stay shared")
			require.Equal(t, neighbor, test.FileExtentAt(t, source, 0))
			offset, err := clone.Seek(0, io.SeekCurrent)
			require.NoError(t, err)
			require.EqualValues(t, 11, offset)

			growth := make([]byte, scenario.extraBlocks*block)
			info, err := clone.Stat()
			require.NoError(t, err)
			require.EqualValues(t, len(original)+len(growth), info.Size(), "logical EOF")
			contents, err := os.ReadFile(clone.Name())
			require.NoError(t, err)
			require.Equal(t, append(bytes.Clone(original), growth...), contents)
			contents, err = os.ReadFile(source.Name())
			require.NoError(t, err)
			require.Equal(t, original, contents)
		})
	}
}
