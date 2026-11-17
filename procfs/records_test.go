//go:build linux

package procfs_test

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/procfs"
	"github.com/stretchr/testify/require"
)

func TestDescriptorInformationConvertsNumbersAndPreservesUnknownFlags(t *testing.T) {
	flags := procfs.CloseOnExec | procfs.ReadWrite | procfs.Append | procfs.OpenFlags(1<<55)
	contents := strings.Replace(test.ProcDescriptorInfo, "02000000", strconv.FormatUint(uint64(flags), 8), 1)
	_, process := test.ProcDescriptor(t, contents)

	info, err := process.Descriptor(3)
	require.NoError(t, err)

	require.Equal(t, procfs.DescriptorInfo{
		Number: 3, Position: 17, Flags: flags, MountID: 29, Inode: 31, Target: "/synthetic/file (deleted)",
	}, info)
}

func TestDescriptorListingUsesNumericOrder(t *testing.T) {
	root, process := test.ProcDescriptor(t, test.ProcDescriptorInfo)
	for _, name := range []string{"12", "2"} {
		require.NoError(t, os.Symlink("socket:[17]", filepath.Join(root, "42/fd", name)))
	}

	numbers, err := process.Descriptors()
	require.NoError(t, err)

	require.Equal(t, []int{2, 3, 12}, numbers)
}

func TestDescriptorListingRejectsInvalidNumbers(t *testing.T) {
	for _, name := range []string{"not-a-number", "-1", "2147483648"} {
		t.Run(name, func(t *testing.T) {
			root, process := test.ProcDescriptor(t, test.ProcDescriptorInfo)
			require.NoError(t, os.Symlink("/synthetic/target", filepath.Join(root, "42/fd", name)))

			numbers, err := process.Descriptors()
			require.ErrorIs(t, err, procfs.ErrParse)
			require.Nil(t, numbers)
		})
	}
}

func TestDescriptorRejectsInvalidNumbers(t *testing.T) {
	_, process := test.ProcDescriptor(t, test.ProcDescriptorInfo)
	for _, number := range []int{-1, math.MaxInt32 + 1} {
		t.Run(fmt.Sprintf("number %d", number), func(t *testing.T) {
			info, err := process.Descriptor(number)
			require.ErrorIs(t, err, os.ErrInvalid)
			require.Zero(t, info)
		})
	}
}

func TestMalformedDescriptorInformationReturnsNoPartialRecord(t *testing.T) {
	for name, contents := range map[string]string{
		"empty": "", "missing position": strings.Replace(test.ProcDescriptorInfo, "pos:\t17\n", "", 1),
		"missing flags":     strings.Replace(test.ProcDescriptorInfo, "flags:\t02000000\n", "", 1),
		"missing mount ID":  strings.Replace(test.ProcDescriptorInfo, "mnt_id:\t29\n", "", 1),
		"missing inode":     strings.Replace(test.ProcDescriptorInfo, "ino:\t31\n", "", 1),
		"invalid octal":     strings.Replace(test.ProcDescriptorInfo, "02000000", "089", 1),
		"position overflow": strings.Replace(test.ProcDescriptorInfo, "pos:\t17", "pos:\t18446744073709551615", 1),
		"inode overflow":    strings.Replace(test.ProcDescriptorInfo, "ino:\t31", "ino:\t18446744073709551616", 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, process := test.ProcDescriptor(t, contents)

			info, err := process.Descriptor(3)
			require.ErrorIs(t, err, procfs.ErrParse)
			require.Zero(t, info)
		})
	}
}

func TestDescriptorTargetFailureDiscardsNumericInformation(t *testing.T) {
	root, process := test.ProcDescriptor(t, test.ProcDescriptorInfo)
	require.NoError(t, os.Remove(filepath.Join(root, "42/fd/3")))

	info, err := process.Descriptor(3)
	require.ErrorIs(t, err, test.ErrNotExist)
	require.Zero(t, info)
	test.PathError(t, err, "readlink", filepath.Join(root, "42/fd/3"))
}

func TestMapsPreserveNamesAddressesAndPermissions(t *testing.T) {
	contents := "1000-2000 rw-p 00001000 08:0a 17   /synthetic/with  spaces\tand trailing \n" +
		"2000-3000 r-xs 0 00:00 0 [anon:label\\012line]\n" + "3000-4000 ---p 0 00:00 0\n" +
		"4000-5000 r--p 0 00:00 0 /synthetic/carriage\r\n"
	root := test.ProcRoot(t, map[string]string{"42/maps": contents})
	process := test.ProcProcess(t, root)

	mappings, err := process.Maps()
	require.NoError(t, err)

	require.Equal(t, []procfs.Mapping{
		{
			Start: 0x1000, End: 0x2000, Offset: 0x1000, Device: procfs.Device{Major: 8, Minor: 10}, Inode: 17,
			Permissions: procfs.MapRead | procfs.MapWrite | procfs.MapPrivate, Name: "/synthetic/with  spaces\tand trailing ",
		},
		{Start: 0x2000, End: 0x3000, Permissions: procfs.MapRead | procfs.MapExecute | procfs.MapShared, Name: `[anon:label\012line]`},
		{Start: 0x3000, End: 0x4000, Permissions: procfs.MapPrivate},
		{Start: 0x4000, End: 0x5000, Permissions: procfs.MapRead | procfs.MapPrivate, Name: "/synthetic/carriage\r"},
	}, mappings)
}

func TestMapsAcceptAnUnterminatedFinalRecord(t *testing.T) {
	root := test.ProcRoot(t, map[string]string{"42/maps": "1000-2000 r--p 0 00:00 0 /synthetic/final"})
	process := test.ProcProcess(t, root)

	mappings, err := process.Maps()
	require.NoError(t, err)

	require.Equal(t, []procfs.Mapping{{
		Start: 0x1000, End: 0x2000, Permissions: procfs.MapRead | procfs.MapPrivate, Name: "/synthetic/final",
	}}, mappings)
}

func TestEmptyMapsAreAValidObservation(t *testing.T) {
	root := test.ProcRoot(t, map[string]string{"42/maps": ""})
	process := test.ProcProcess(t, root)

	mappings, err := process.Maps()
	require.NoError(t, err)
	require.Empty(t, mappings)
}

func TestMapReadFailuresRetainFilesystemErrno(t *testing.T) {
	root := test.ProcRoot(t, nil)
	require.NoError(t, os.Mkdir(filepath.Join(root, "42/maps"), 0o700))
	process := test.ProcProcess(t, root)

	mappings, err := process.Maps()
	require.ErrorIs(t, err, test.ErrIsDirectory)
	require.Nil(t, mappings)
	test.PathError(t, err, "read", filepath.Join(root, "42/maps"))
}

func TestMalformedMapsDiscardEarlierRecords(t *testing.T) {
	valid := "1000-2000 r--p 0 00:00 0\n"
	for name, line := range map[string]string{
		"truncated": "1000-2000 rw-p", "address separator": "1000 rw-p 0 00:00 0",
		"reversed addresses": "2000-1000 rw-p 0 00:00 0", "invalid address": "no-2000 rw-p 0 00:00 0",
		"invalid end": "1000-no rw-p 0 00:00 0", "short permissions": "1000-2000 r-p 0 00:00 0",
		"invalid permissions": "1000-2000 w--p 0 00:00 0", "invalid sharing": "1000-2000 rw-q 0 00:00 0",
		"invalid offset": "1000-2000 rw-p nope 00:00 0", "invalid device": "1000-2000 rw-p 0 00 0",
		"device overflow": "1000-2000 rw-p 0 100000000:00 0", "invalid minor": "1000-2000 rw-p 0 00:no 0",
		"invalid inode": "1000-2000 rw-p 0 00:00 no", "overlong record": "1000-2000 rw-p 0 00:00 0 /" + strings.Repeat("x", 1024*1024),
	} {
		t.Run(name, func(t *testing.T) {
			root := test.ProcRoot(t, map[string]string{"42/maps": valid + line + "\n"})
			process := test.ProcProcess(t, root)

			mappings, err := process.Maps()
			require.ErrorIs(t, err, procfs.ErrParse)
			require.Nil(t, mappings)
		})
	}
}

func TestMountsDecodeEscapesOnceAndKeepOptions(t *testing.T) {
	root := test.ProcRoot(t, map[string]string{"42/mountinfo": test.ProcMountInfo})
	process := test.ProcProcess(t, root)

	mounts, err := process.Mounts()
	require.NoError(t, err)

	require.Equal(t, []procfs.MountInfo{{
		ID: 29, ParentID: 17, Device: procfs.Device{Major: 8, Minor: 3}, Root: "/root dir", MountPoint: "/mnt\tpoint",
		Filesystem: "ext4", Source: `/dev/source\040`,
		Options: map[string]string{"rw": "", "nosuid": "", "custom": "yes"}, SuperOptions: map[string]string{"rw": "", "errors": "continue"},
	}}, mounts)
}

func TestMountsReadBeyondOneMiB(t *testing.T) {
	count := (1024*1024)/len(test.ProcMountInfo) + 1
	root := test.ProcRoot(t, map[string]string{"42/mountinfo": strings.Repeat(test.ProcMountInfo, count)})
	process := test.ProcProcess(t, root)

	mounts, err := process.Mounts()
	require.NoError(t, err)
	require.Len(t, mounts, count)
}

func TestMountsReturnIndependentOptionMaps(t *testing.T) {
	root := test.ProcRoot(t, map[string]string{"42/mountinfo": test.ProcMountInfo})
	process := test.ProcProcess(t, root)
	before, err := process.Mounts()
	require.NoError(t, err)
	before[0].Options["rw"] = "changed"
	before[0].SuperOptions["errors"] = "changed"

	after, err := process.Mounts()
	require.NoError(t, err)

	require.Empty(t, after[0].Options["rw"])
	require.Equal(t, "continue", after[0].SuperOptions["errors"])
}

func TestMalformedMountsDiscardEarlierRecords(t *testing.T) {
	for name, line := range map[string]string{
		"truncated": "29 17 8:3", "missing separator": "29 17 8:3 / / rw ext4 source rw",
		"invalid ID": "bad 17 8:3 / / rw - ext4 source rw", "negative ID": "-1 17 8:3 / / rw - ext4 source rw",
		"invalid parent": "29 bad 8:3 / / rw - ext4 source rw", "negative parent": "29 -1 8:3 / / rw - ext4 source rw",
		"invalid device": "29 17 bad / / rw - ext4 source rw", "invalid minor": "29 17 8:no / / rw - ext4 source rw",
		"truncated suffix": "29 17 8:3 / / rw - ext4 source",
	} {
		t.Run(name, func(t *testing.T) {
			root := test.ProcRoot(t, map[string]string{"42/mountinfo": test.ProcMountInfo + line + "\n"})
			process := test.ProcProcess(t, root)

			mounts, err := process.Mounts()
			require.ErrorIs(t, err, procfs.ErrParse)
			require.Nil(t, mounts)
		})
	}
}
