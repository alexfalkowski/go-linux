//go:build linux

package mount_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alexfalkowski/go-linux/file"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

func TestStatReportsXFSDirectIOAlignment(t *testing.T) {
	path := test.XFSFilesystem(t)
	if path == "" {
		return
	}
	segment, err := os.CreateTemp(path, "segment-*")
	require.NoError(t, err)
	test.Cleanup(t, segment)
	source, err := os.CreateTemp(path, "source-*")
	require.NoError(t, err)
	test.Cleanup(t, source)
	test.WriteAt(t, source, 0, "record")
	test.CloneFile(t, segment, source)
	want := test.FileStatx(t, segment, test.ExtendedStatxMask)
	mask := file.MaskDirectIOAlignment | file.MaskDirectIOReadAlignment

	metadata, err := file.Stat(segment, mask)
	require.NoError(t, err)

	require.Equal(t, mask, metadata.Mask&mask)
	require.Positive(t, metadata.DirectIOMemoryAlignment, "buffer address alignment")
	require.Equal(t, want.Dio_mem_align, metadata.DirectIOMemoryAlignment)
	require.Equal(t, want.Dio_offset_align, metadata.DirectIOOffsetAlignment)
	require.Equal(t, want.Dio_read_offset_align, metadata.DirectIOReadOffsetAlignment)
	require.Positive(t, metadata.DirectIOReadOffsetAlignment, "read offset alignment")
	require.Less(t, metadata.DirectIOReadOffsetAlignment, metadata.DirectIOOffsetAlignment, "the shared extent requires a larger write alignment")
}

func TestStatReportsXFSAtomicWriteLimits(t *testing.T) {
	path := test.XFSFilesystem(t)
	if path == "" {
		return
	}
	segment, err := os.CreateTemp(path, "segment-*")
	require.NoError(t, err)
	test.Cleanup(t, segment)
	want := test.FileStatx(t, segment, test.ExtendedStatxMask)

	metadata, err := file.Stat(segment, file.MaskAtomicWrite)
	require.NoError(t, err)

	require.NotZero(t, metadata.Mask&file.MaskAtomicWrite, "limits are reported")
	require.NotZero(t, metadata.AttributesMask&file.AtomicWrite, "attribute support is reported")
	require.NotZero(t, metadata.Attributes&file.AtomicWrite, "reflink supports software atomic writes")
	require.Positive(t, metadata.AtomicWriteUnitMin, "minimum size")
	require.Less(t, metadata.AtomicWriteUnitMin, metadata.AtomicWriteUnitMax, "XFS supports multi-block atomic writes")
	require.Positive(t, metadata.AtomicWriteSegmentsMax, "vector limit")
	require.Equal(t, want.Atomic_write_unit_min, metadata.AtomicWriteUnitMin)
	require.Equal(t, want.Atomic_write_unit_max, metadata.AtomicWriteUnitMax)
	require.Equal(t, want.Atomic_write_unit_max_opt, metadata.AtomicWriteUnitMaxOptimized)
	require.Equal(t, want.Atomic_write_segments_max, metadata.AtomicWriteSegmentsMax)
}

func TestStatReportsContainingBtrfsSubvolume(t *testing.T) {
	path := test.BtrfsFilesystem(t)
	if path == "" {
		return
	}
	subvolume := filepath.Join(path, "backup")
	test.CreateBtrfsSubvolume(t, subvolume)
	name := "segment"
	require.NoError(t, os.WriteFile(filepath.Join(subvolume, name), []byte("record"), 0o600))
	top := test.OpenFile(t, path)
	dir := test.OpenFile(t, subvolume)
	topID := test.BtrfsSubvolumeID(t, path)
	subvolumeID := test.BtrfsSubvolumeID(t, subvolume)

	root, err := file.Stat(top, file.MaskSubvolumeID)
	require.NoError(t, err)
	require.NotZero(t, root.Mask&file.MaskSubvolumeID, "top-level subvolume ID is reported")
	require.Equal(t, topID, root.SubvolumeID)

	metadata, err := file.StatAt(dir, name, file.StatOptions{Mask: file.MaskSubvolumeID})
	require.NoError(t, err)
	require.NotZero(t, metadata.Mask&file.MaskSubvolumeID, "nested subvolume ID is reported")
	require.Equal(t, subvolumeID, metadata.SubvolumeID)
	require.NotEqual(t, root.SubvolumeID, metadata.SubvolumeID)
}
