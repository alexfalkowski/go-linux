//go:build linux

package file_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/file"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

func TestStatReportsBasicMetadata(t *testing.T) {
	source := test.TempFile(t, 8193)
	test.WriteAt(t, source, 0, "contents")

	access := time.Unix(-1, 123456789).UTC()
	modified := time.Unix(1500000000, 987654321).UTC()
	require.NoError(t, os.Chtimes(source.Name(), access, modified))

	require.NoError(t, source.Chmod(0o640|os.ModeSetuid|os.ModeSetgid|os.ModeSticky))

	metadata, err := file.Stat(source, file.MaskBasic|file.MaskBirthTime|file.MaskMountID)
	require.NoError(t, err)
	require.Equal(t, file.MaskBasic, metadata.Mask&file.MaskBasic, "ordinary fixture must supply basic metadata")
	require.Equal(t, uint64(8193), metadata.Size)
	require.Equal(t, uint32(1), metadata.Links)
	require.NotZero(t, metadata.Inode)
	require.Positive(t, metadata.BlockSize)
	require.Positive(t, metadata.Blocks, "written data must occupy allocated blocks")

	require.Equal(t, access, metadata.AccessTime)
	require.Equal(t, modified, metadata.ModifyTime)
	require.False(t, metadata.ChangeTime.IsZero(), "status-change time must be present")

	info, err := source.Stat()
	require.NoError(t, err)
	require.Equal(t, info.Mode(), metadata.Mode, "Go mode translation must include special bits")
}

func TestStatLeavesBorrowedFileAndOffsetUnchanged(t *testing.T) {
	source := test.TempFile(t, 8)
	_, err := source.Seek(3, io.SeekStart)
	require.NoError(t, err)

	_, err = file.Stat(source, file.MaskBasic)
	require.NoError(t, err)

	position, err := source.Seek(0, io.SeekCurrent)
	require.NoError(t, err)
	require.Equal(t, int64(3), position)
}

func TestStatHonorsRequestedMetadataMasks(t *testing.T) {
	source := test.TempFile(t, 8193)

	for _, mask := range []file.StatMask{0, file.MaskSize, file.MaskBirthTime, file.MaskUniqueMountID, file.MaskMountID | file.MaskUniqueMountID} {
		observed, err := file.Stat(source, mask)
		require.NoError(t, err)

		if mask&file.MaskUniqueMountID != 0 {
			require.NotZero(t, observed.Mask&file.MaskUniqueMountID, "Linux 7.0 must supply unique mount IDs")
			require.Zero(t, observed.Mask&file.MaskMountID, "unique mount identity takes precedence")
			require.NotZero(t, observed.MountID)
		}

		if observed.Mask&file.MaskSize != 0 {
			require.Equal(t, uint64(8193), observed.Size)
		}

		if observed.Mask&file.MaskBirthTime != 0 {
			require.False(t, observed.BirthTime.IsZero(), "available birth time must be converted")
		} else {
			require.Zero(t, observed.BirthTime, "unavailable birth time must stay zero")
		}
	}
}

func TestStatAtDistinguishesLinksTargetsAndStableHandles(t *testing.T) {
	root := t.TempDir()
	dir := test.OpenFile(t, root)
	require.NoError(t, os.WriteFile(filepath.Join(root, "data"), []byte("contents"), 0o600))

	require.NoError(t, os.Symlink("data", filepath.Join(root, "link")))

	target, err := file.StatAt(dir, "link", file.StatOptions{Mask: file.MaskBasic})
	require.NoError(t, err)
	require.True(t, target.Mode.IsRegular())
	require.Equal(t, uint64(8), target.Size)

	link, err := file.StatAt(dir, "link", file.StatOptions{Mask: file.MaskBasic, NoFollow: true, NoAutomount: true})
	require.NoError(t, err)
	require.Equal(t, os.ModeSymlink, link.Mode.Type())
	require.Equal(t, uint64(4), link.Size)
	require.NotEqual(t, link.Inode, target.Inode)

	opened := test.OpenFile(t, filepath.Join(root, "data"))
	require.NoError(t, os.Remove(filepath.Join(root, "data")))

	_, err = file.StatAt(dir, "link", file.StatOptions{Mask: file.MaskBasic})
	require.ErrorIs(t, err, test.ErrNotExist)

	stillOpen, err := file.Stat(opened, file.MaskBasic)
	require.NoError(t, err)
	require.Equal(t, target.Inode, stillOpen.Inode)
	require.Zero(t, stillOpen.Links, "unlinked handle must still identify its original inode")

	_, err = opened.Stat()
	require.NoError(t, err, "Stat must leave the borrowed file open")
}

func TestStatAtSynchronizationOptionsAndAbsoluteNames(t *testing.T) {
	source := test.TempFile(t, 10)
	dir := test.OpenFile(t, t.TempDir())
	for _, mode := range []file.SyncMode{file.SyncDefault, file.SyncForce, file.SyncDont} {
		metadata, err := file.StatAt(dir, source.Name(), file.StatOptions{Mask: file.MaskSize, Sync: mode})
		require.NoError(t, err)
		require.Equal(t, uint64(10), metadata.Size)
	}

	metadata, err := file.StatAt(source, "", file.StatOptions{Mask: file.MaskSize, Sync: file.SyncForce})
	require.NoError(t, err)
	require.Equal(t, uint64(10), metadata.Size)
}

func TestStatAtReturnsZeroMetadataAndPreservesErrors(t *testing.T) {
	dir := test.OpenFile(t, t.TempDir())
	for _, scenario := range []struct {
		wantErr error
		name    string
		path    string
		options file.StatOptions
	}{
		{name: "missing", path: "missing", wantErr: test.ErrNotExist},
		{name: "embedded NUL", path: "bad\x00name", wantErr: test.ErrInvalidArgument},
		{name: "unknown mask", path: ".", wantErr: os.ErrInvalid, options: file.StatOptions{Mask: 1 << 31}},
		{name: "unknown sync", path: ".", wantErr: os.ErrInvalid, options: file.StatOptions{Sync: 1}},
		{name: "combined sync", path: ".", wantErr: os.ErrInvalid, options: file.StatOptions{Sync: file.SyncDont | file.SyncForce}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			metadata, err := file.StatAt(dir, scenario.path, scenario.options)
			require.ErrorIs(t, err, scenario.wantErr)
			require.Zero(t, metadata)

			var pathErr *os.PathError
			require.ErrorAs(t, err, &pathErr)
			require.Equal(t, scenario.path, pathErr.Path)
		})
	}
}

func TestStatTranslatesSpecialFileTypes(t *testing.T) {
	reader, _ := test.Pipe(t)
	socket, _ := test.SocketPair(t)
	for _, scenario := range []struct {
		name   string
		source *os.File
		want   os.FileMode
	}{
		{name: "directory", source: test.OpenFile(t, t.TempDir()), want: os.ModeDir},
		{name: "pipe", source: reader, want: os.ModeNamedPipe},
		{name: "socket", source: socket, want: os.ModeSocket},
		{name: "character device", source: test.OpenFile(t, "/dev/null"), want: os.ModeDevice | os.ModeCharDevice},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			metadata, err := file.Stat(scenario.source, file.MaskType)
			require.NoError(t, err)
			require.NotZero(t, metadata.Mask&file.MaskType)
			require.Equal(t, scenario.want, metadata.Mode.Type())
		})
	}
}

func TestStatTranslatesBlockDeviceMetadata(t *testing.T) {
	path := test.BlockDevice(t)
	dir := test.OpenFile(t, filepath.Dir(path))
	// O_PATH obtains an inode reference without opening the device for data I/O.
	device, err := file.Open(dir, filepath.Base(path), file.OpenOptions{Flags: file.Path | file.NoFollow})
	require.NoError(t, err)
	test.Cleanup(t, device)
	want := test.FileStatx(t, device, uint32(file.MaskType))

	metadata, err := file.Stat(device, file.MaskType)
	require.NoError(t, err)
	require.NotZero(t, metadata.Mask&file.MaskType)
	require.Equal(t, os.ModeDevice, metadata.Mode.Type())
	require.Equal(t, want.Rdev_major, metadata.RdevMajor)
	require.Equal(t, want.Rdev_minor, metadata.RdevMinor)
}

func TestStatReportsNoDumpChanges(t *testing.T) {
	source := test.TempFile(t, 0)
	test.SetNoDump(t, source, false)
	initial, err := file.Stat(source, file.MaskBasic)
	require.NoError(t, err)
	require.NotZero(t, initial.AttributesMask&file.NoDump, "the fixture filesystem must report NoDump support")
	require.Zero(t, initial.Attributes&file.NoDump)

	test.SetNoDump(t, source, true)
	set, err := file.Stat(source, file.MaskBasic)
	require.NoError(t, err)
	require.NotZero(t, set.AttributesMask&file.NoDump)
	require.NotZero(t, set.Attributes&file.NoDump)

	test.SetNoDump(t, source, false)
	cleared, err := file.Stat(source, file.MaskBasic)
	require.NoError(t, err)
	require.NotZero(t, cleared.AttributesMask&file.NoDump)
	require.Zero(t, cleared.Attributes&file.NoDump)
}

func TestStatMatchesIndependentKernelMetadata(t *testing.T) {
	// Focused ABI check: verify Linux identifiers and masks independently from
	// the package's conversion, without exposing raw access in its public API.
	source := test.TempFile(t, 4096)
	mask := file.MaskBasic | file.MaskBirthTime | file.MaskMountID
	want := test.FileStatx(t, source, uint32(mask))
	got, err := file.Stat(source, mask)
	require.NoError(t, err)

	require.Equal(t, file.StatMask(want.Mask), got.Mask)
	require.Equal(t, file.Attributes(want.Attributes), got.Attributes)
	require.Equal(t, file.Attributes(want.Attributes_mask), got.AttributesMask)

	require.Equal(t, want.Uid, got.UID)
	require.Equal(t, want.Gid, got.GID)

	require.Equal(t, want.Ino, got.Inode)
	require.Equal(t, want.Blksize, got.BlockSize)
	require.Equal(t, want.Blocks, got.Blocks)

	require.Equal(t, want.Mnt_id, got.MountID)
	require.Equal(t, want.Dev_major, got.DeviceMajor)
	require.Equal(t, want.Dev_minor, got.DeviceMinor)
	require.Equal(t, want.Rdev_major, got.RdevMajor)
	require.Equal(t, want.Rdev_minor, got.RdevMinor)

	require.Equal(t, time.Unix(want.Ctime.Sec, int64(want.Ctime.Nsec)).UTC(), got.ChangeTime)

	if file.StatMask(want.Mask)&file.MaskBirthTime != 0 {
		require.Equal(t, time.Unix(want.Btime.Sec, int64(want.Btime.Nsec)).UTC(), got.BirthTime)
	}
}

func TestStatMatchesIndependentExtendedMetadata(t *testing.T) {
	source := test.TempFile(t, 4096)
	want := test.FileStatx(t, source, test.ExtendedStatxMask)
	mask := file.MaskDirectIOAlignment | file.MaskDirectIOReadAlignment | file.MaskSubvolumeID | file.MaskAtomicWrite

	got, err := file.Stat(source, mask)
	require.NoError(t, err)

	require.Equal(t, file.StatMask(want.Mask), got.Mask, "returned field validity")
	require.Equal(t, file.Attributes(want.Attributes_mask), got.AttributesMask, "attribute support")
	require.Equal(t, file.Attributes(want.Attributes), got.Attributes, "attribute values")
	require.Equal(t, want.Subvol, got.SubvolumeID, "subvolume ID")
	require.Equal(t, want.Dio_mem_align, got.DirectIOMemoryAlignment, "buffer alignment")
	require.Equal(t, want.Dio_offset_align, got.DirectIOOffsetAlignment, "direct-I/O offset alignment")
	require.Equal(t, want.Dio_read_offset_align, got.DirectIOReadOffsetAlignment, "direct-read offset alignment")
	require.Equal(t, want.Atomic_write_unit_min, got.AtomicWriteUnitMin, "minimum atomic-write unit")
	require.Equal(t, want.Atomic_write_unit_max, got.AtomicWriteUnitMax, "maximum atomic-write unit")
	require.Equal(t, want.Atomic_write_unit_max_opt, got.AtomicWriteUnitMaxOptimized, "optimized maximum atomic-write unit")
	require.Equal(t, want.Atomic_write_segments_max, got.AtomicWriteSegmentsMax, "atomic-write vector limit")
}

func TestStatAtReportsUnavailableExtendedFields(t *testing.T) {
	proc := test.OpenFile(t, "/proc/self")
	mask := file.MaskDirectIOAlignment | file.MaskDirectIOReadAlignment | file.MaskSubvolumeID | file.MaskAtomicWrite

	metadata, err := file.StatAt(proc, "status", file.StatOptions{Mask: mask})
	require.NoError(t, err)

	require.Zero(t, metadata.Mask&mask, "procfs does not report any of the requested fields")
	require.Zero(t, metadata.AttributesMask&file.AtomicWrite, "procfs does not report atomic-write attribute support")
}

func TestStatReportsMountRootAndUnavailableBirthTime(t *testing.T) {
	proc := test.OpenFile(t, "/proc")
	metadata, err := file.Stat(proc, file.MaskBirthTime|file.MaskMountID)
	require.NoError(t, err)
	require.Zero(t, metadata.Mask&file.MaskBirthTime, "procfs supplies an unavailable-birth-time fixture")
	require.Zero(t, metadata.BirthTime)
	require.NotZero(t, metadata.AttributesMask&file.MountRoot)
	require.NotZero(t, metadata.Attributes&file.MountRoot)
}

func TestStatPreservesDeviceNumbers(t *testing.T) {
	device := test.OpenFile(t, "/dev/null")
	wantDevice := test.FileStatx(t, device, uint32(file.MaskType))
	gotDevice, err := file.Stat(device, file.MaskType)
	require.NoError(t, err)
	require.Equal(t, wantDevice.Rdev_major, gotDevice.RdevMajor)
	require.Equal(t, wantDevice.Rdev_minor, gotDevice.RdevMinor)
}
