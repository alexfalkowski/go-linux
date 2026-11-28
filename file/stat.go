//go:build linux

package file

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// StatMask identifies requested or returned statx fields. Returned masks can
// include unrequested or unknown bits; test the returned mask before use.
type StatMask uint32

// MaskType identifies the file type portion of Metadata.Mode (STATX_TYPE).
const MaskType StatMask = unix.STATX_TYPE

// MaskMode identifies the permissions and special mode bits (STATX_MODE).
const MaskMode StatMask = unix.STATX_MODE

// MaskLinks identifies the hard-link count (STATX_NLINK).
const MaskLinks StatMask = unix.STATX_NLINK

// MaskUID identifies the owner user ID (STATX_UID).
const MaskUID StatMask = unix.STATX_UID

// MaskGID identifies the owner group ID (STATX_GID).
const MaskGID StatMask = unix.STATX_GID

// MaskAccessTime identifies the last-access timestamp (STATX_ATIME).
const MaskAccessTime StatMask = unix.STATX_ATIME

// MaskModifyTime identifies the last-data-modification timestamp (STATX_MTIME).
const MaskModifyTime StatMask = unix.STATX_MTIME

// MaskChangeTime identifies the last-status-change timestamp (STATX_CTIME).
const MaskChangeTime StatMask = unix.STATX_CTIME

// MaskInode identifies the filesystem-local inode number (STATX_INO).
const MaskInode StatMask = unix.STATX_INO

// MaskSize identifies the byte size (STATX_SIZE).
const MaskSize StatMask = unix.STATX_SIZE

// MaskBlocks identifies the allocated 512-byte block count (STATX_BLOCKS).
const MaskBlocks StatMask = unix.STATX_BLOCKS

// MaskBasic requests all basic stat fields (STATX_BASIC_STATS).
const MaskBasic StatMask = unix.STATX_BASIC_STATS

// MaskBirthTime requests the creation timestamp (STATX_BTIME). Filesystem
// support is not universal; absence is indicated by the returned mask.
const MaskBirthTime StatMask = unix.STATX_BTIME

// MaskMountID requests the mount ID used by procfs mountinfo (STATX_MNT_ID).
// This ID can be reused after unmount.
const MaskMountID StatMask = unix.STATX_MNT_ID

// MaskUniqueMountID requests the nonreused, system-lifetime mount ID
// (STATX_MNT_ID_UNIQUE). If both ID masks are requested, Linux prefers this
// kind; the returned mask identifies which kind Metadata.MountID contains.
const MaskUniqueMountID StatMask = unix.STATX_MNT_ID_UNIQUE

// MaskDirectIOAlignment identifies direct-I/O memory and offset alignment
// (STATX_DIOALIGN).
const MaskDirectIOAlignment StatMask = unix.STATX_DIOALIGN

// MaskDirectIOReadAlignment identifies separate direct-I/O read offset
// alignment (STATX_DIO_READ_ALIGN). Request MaskDirectIOAlignment as well
// for the memory requirement and the general offset alignment.
const MaskDirectIOReadAlignment StatMask = unix.STATX_DIO_READ_ALIGN

// MaskSubvolumeID identifies the filesystem-local subvolume ID (STATX_SUBVOL).
const MaskSubvolumeID StatMask = unix.STATX_SUBVOL

// MaskAtomicWrite identifies the atomic-write unit and segment limits
// (STATX_WRITE_ATOMIC).
const MaskAtomicWrite StatMask = unix.STATX_WRITE_ATOMIC

// Attributes is the statx file-attribute bitmask. Unknown output bits are
// preserved. An unset bit means false only when set in AttributesMask.
type Attributes uint64

// Compressed indicates compressed file storage (STATX_ATTR_COMPRESSED).
const Compressed Attributes = unix.STATX_ATTR_COMPRESSED

// Immutable indicates that the file is immutable (STATX_ATTR_IMMUTABLE).
const Immutable Attributes = unix.STATX_ATTR_IMMUTABLE

// AppendOnly indicates append-only writes (STATX_ATTR_APPEND).
const AppendOnly Attributes = unix.STATX_ATTR_APPEND

// NoDump indicates exclusion from dump-style backups (STATX_ATTR_NODUMP).
const NoDump Attributes = unix.STATX_ATTR_NODUMP

// Encrypted indicates filesystem encryption (STATX_ATTR_ENCRYPTED).
const Encrypted Attributes = unix.STATX_ATTR_ENCRYPTED

// Automount indicates an automount trigger (STATX_ATTR_AUTOMOUNT).
const Automount Attributes = unix.STATX_ATTR_AUTOMOUNT

// MountRoot indicates the root of a mount (STATX_ATTR_MOUNT_ROOT).
const MountRoot Attributes = unix.STATX_ATTR_MOUNT_ROOT

// Verity indicates filesystem verity protection (STATX_ATTR_VERITY).
const Verity Attributes = unix.STATX_ATTR_VERITY

// DAX indicates direct-access file semantics (STATX_ATTR_DAX).
const DAX Attributes = unix.STATX_ATTR_DAX

// AtomicWrite indicates support for direct I/O with torn-write protection
// (STATX_ATTR_WRITE_ATOMIC). AttributesMask indicates whether the attribute
// was reported, and MaskAtomicWrite identifies whether the limits are valid.
const AtomicWrite Attributes = unix.STATX_ATTR_WRITE_ATOMIC

// SyncMode selects statx metadata synchronization, not a bitmask.
type SyncMode uint32

// SyncDefault uses the filesystem's normal metadata synchronization policy.
const SyncDefault SyncMode = unix.AT_STATX_SYNC_AS_STAT

// SyncForce requests synchronization with the backing server (AT_STATX_FORCE_SYNC).
const SyncForce SyncMode = unix.AT_STATX_FORCE_SYNC

// SyncDont permits cached metadata without server synchronization (AT_STATX_DONT_SYNC).
const SyncDont SyncMode = unix.AT_STATX_DONT_SYNC

// StatOptions controls a statx query. Zero requests no particular fields, but
// Linux may return available fields anyway. Use MaskBasic for ordinary metadata.
type StatOptions struct {
	// Mask requests the supported fields of interest; it is not an exact filter.
	Mask StatMask

	// Sync selects metadata freshness policy; zero is SyncDefault.
	Sync SyncMode

	// NoFollow queries a final symlink itself. Intermediate symlinks are followed.
	// An empty name selects the handle itself regardless of this option.
	NoFollow bool

	// NoAutomount prevents triggering an automount at the final component.
	// This does not restrict intermediate mount points or confine the lookup.
	NoAutomount bool
}

// Metadata is a statx observation, not an atomic filesystem snapshot. Fields
// can reflect different instants under concurrent modification. Mask and
// AttributesMask determine validity; zero alone does not establish absence.
// Unsupported basic fields may contain Linux compatibility values.
type Metadata struct {
	// AccessTime is the last access time in UTC, valid with MaskAccessTime.
	// An unavailable timestamp is the zero time, not the Unix epoch.
	AccessTime time.Time

	// BirthTime is creation time in UTC, valid with MaskBirthTime.
	BirthTime time.Time

	// ChangeTime is last inode-status change in UTC, valid with MaskChangeTime.
	ChangeTime time.Time

	// ModifyTime is last data modification in UTC, valid with MaskModifyTime.
	ModifyTime time.Time

	// Attributes contains Linux attribute bits, interpreted through AttributesMask.
	Attributes Attributes

	// AttributesMask identifies attributes supported for this inode.
	AttributesMask Attributes

	// Inode is the filesystem-local inode number, valid with MaskInode.
	Inode uint64

	// Size is the byte size, valid with MaskSize; unsigned Linux range is retained.
	Size uint64

	// Blocks counts allocated 512-byte units, valid with MaskBlocks.
	Blocks uint64

	// MountID is the mount identity selected by MaskMountID or MaskUniqueMountID.
	MountID uint64

	// Mask reports available fields, including extra or unknown Linux bits.
	Mask StatMask

	// BlockSize is the preferred I/O block size in bytes; it is always returned.
	BlockSize uint32

	// Links is the hard-link count, valid with MaskLinks.
	Links uint32

	// UID is the owner user ID in the calling user namespace, valid with MaskUID.
	UID uint32

	// GID is the owner group ID in the calling user namespace, valid with MaskGID.
	GID uint32

	// Mode uses Go FileMode bits; MaskType and MaskMode validate its two portions.
	Mode os.FileMode

	// DeviceMajor identifies the containing filesystem's device; always returned.
	DeviceMajor uint32

	// DeviceMinor complements DeviceMajor; always returned.
	DeviceMinor uint32

	// RdevMajor is the represented device's major number for a device inode.
	RdevMajor uint32

	// RdevMinor is the represented device's minor number for a device inode.
	RdevMinor uint32

	// SubvolumeID is the filesystem-local subvolume ID, valid with MaskSubvolumeID.
	SubvolumeID uint64

	// DirectIOMemoryAlignment is the buffer-address alignment in bytes for
	// direct I/O, valid with MaskDirectIOAlignment. Zero means direct I/O is
	// unsupported for this file.
	DirectIOMemoryAlignment uint32

	// DirectIOOffsetAlignment is the file-offset and segment-length alignment
	// in bytes for direct I/O, valid with MaskDirectIOAlignment. Zero means
	// direct I/O is unsupported for this file.
	DirectIOOffsetAlignment uint32

	// DirectIOReadOffsetAlignment is the file-offset and segment-length
	// alignment in bytes for direct reads, valid with MaskDirectIOReadAlignment.
	// A reported zero leaves DirectIOOffsetAlignment applicable to reads.
	DirectIOReadOffsetAlignment uint32

	// AtomicWriteUnitMin is the minimum protected direct-write size in bytes,
	// valid with MaskAtomicWrite. Zero means atomic writes are unsupported.
	AtomicWriteUnitMin uint32

	// AtomicWriteUnitMax is the maximum protected direct-write size in bytes,
	// valid with MaskAtomicWrite. Zero means atomic writes are unsupported.
	AtomicWriteUnitMax uint32

	// AtomicWriteUnitMaxOptimized is the optimized maximum protected
	// direct-write size in bytes, valid with MaskAtomicWrite. When atomic
	// writes are supported and this is zero, AtomicWriteUnitMax applies.
	AtomicWriteUnitMaxOptimized uint32

	// AtomicWriteSegmentsMax is the maximum number of I/O vectors for one
	// protected direct write, valid with MaskAtomicWrite.
	AtomicWriteSegmentsMax uint32
}

// Stat queries an open file itself using [statx(2)], including an unlinked file
// or a Path|NoFollow symlink handle. It wraps StatAt with an empty name and
// default synchronization. The file is borrowed and its offset is unchanged.
//
// [statx(2)]: https://man7.org/linux/man-pages/man2/statx.2.html
func Stat(file *os.File, mask StatMask) (Metadata, error) {
	return StatAt(file, "", StatOptions{Mask: mask})
}

// StatAt queries name relative to dir through [statx(2)]. An empty name selects
// dir itself (AT_EMPTY_PATH), which may be any open file. A nonempty absolute
// name ignores the directory anchor; a live, nonnil dir is still required.
// Ordinary resolution permits .. and intermediate symlinks and mount crossings.
// Use Open with resolution restrictions followed by Stat for confined lookup.
//
// Unknown request masks and invalid Sync values match os.ErrInvalid before
// borrowing dir. Unsupported requested fields are absent from the returned
// mask, not an error. Kernel errors retain errno in an os.PathError; all errors
// return zero Metadata. This call never retries or falls back to stat/fstat.
//
// [statx(2)]: https://man7.org/linux/man-pages/man2/statx.2.html
func StatAt(dir *os.File, name string, options StatOptions) (Metadata, error) {
	flags, err := statFlags(options)
	if err != nil {
		return Metadata{}, &os.PathError{Op: "statx", Path: name, Err: err}
	}
	if name == "" {
		flags |= unix.AT_EMPTY_PATH
	}

	var stat unix.Statx_t
	err = control(dir, func(fd int) error {
		return unix.Statx(fd, name, flags, int(options.Mask), &stat)
	})
	if err != nil {
		return Metadata{}, &os.PathError{Op: "statx", Path: name, Err: err}
	}

	return metadata(stat), nil
}

func statFlags(options StatOptions) (int, error) {
	const masks = MaskBasic | MaskBirthTime | MaskMountID | MaskUniqueMountID |
		MaskDirectIOAlignment | MaskDirectIOReadAlignment | MaskSubvolumeID | MaskAtomicWrite
	if options.Mask & ^masks != 0 {
		return 0, os.ErrInvalid
	}
	switch options.Sync {
	case SyncDefault, SyncForce, SyncDont:
	default:
		return 0, os.ErrInvalid
	}
	flags := int(options.Sync)
	if options.NoFollow {
		flags |= unix.AT_SYMLINK_NOFOLLOW
	}
	if options.NoAutomount {
		flags |= unix.AT_NO_AUTOMOUNT
	}
	return flags, nil
}

func metadata(stat unix.Statx_t) Metadata {
	return Metadata{
		AccessTime: statTime(stat.Atime, stat.Mask&unix.STATX_ATIME != 0),
		BirthTime:  statTime(stat.Btime, stat.Mask&unix.STATX_BTIME != 0),
		ChangeTime: statTime(stat.Ctime, stat.Mask&unix.STATX_CTIME != 0),
		ModifyTime: statTime(stat.Mtime, stat.Mask&unix.STATX_MTIME != 0),
		Attributes: Attributes(stat.Attributes), AttributesMask: Attributes(stat.Attributes_mask),
		Inode: stat.Ino, Size: stat.Size, Blocks: stat.Blocks, MountID: stat.Mnt_id,
		Mask: StatMask(stat.Mask), BlockSize: stat.Blksize, Links: stat.Nlink,
		UID: stat.Uid, GID: stat.Gid, Mode: fileMode(stat.Mode),
		DeviceMajor: stat.Dev_major, DeviceMinor: stat.Dev_minor,
		RdevMajor: stat.Rdev_major, RdevMinor: stat.Rdev_minor,
		SubvolumeID:                 stat.Subvol,
		DirectIOMemoryAlignment:     stat.Dio_mem_align,
		DirectIOOffsetAlignment:     stat.Dio_offset_align,
		DirectIOReadOffsetAlignment: stat.Dio_read_offset_align,
		AtomicWriteUnitMin:          stat.Atomic_write_unit_min,
		AtomicWriteUnitMax:          stat.Atomic_write_unit_max,
		AtomicWriteUnitMaxOptimized: stat.Atomic_write_unit_max_opt,
		AtomicWriteSegmentsMax:      stat.Atomic_write_segments_max,
	}
}

func statTime(stamp unix.StatxTimestamp, valid bool) time.Time {
	if !valid {
		return time.Time{}
	}
	return time.Unix(stamp.Sec, int64(stamp.Nsec)).UTC()
}

func fileMode(raw uint16) os.FileMode {
	mode := os.FileMode(raw & 0o777)
	switch raw & unix.S_IFMT {
	case unix.S_IFDIR:
		mode |= os.ModeDir
	case unix.S_IFLNK:
		mode |= os.ModeSymlink
	case unix.S_IFIFO:
		mode |= os.ModeNamedPipe
	case unix.S_IFSOCK:
		mode |= os.ModeSocket
	case unix.S_IFBLK:
		mode |= os.ModeDevice
	case unix.S_IFCHR:
		mode |= os.ModeDevice | os.ModeCharDevice
	}
	if raw&unix.S_ISUID != 0 {
		mode |= os.ModeSetuid
	}
	if raw&unix.S_ISGID != 0 {
		mode |= os.ModeSetgid
	}
	if raw&unix.S_ISVTX != 0 {
		mode |= os.ModeSticky
	}
	return mode
}
