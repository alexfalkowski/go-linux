//go:build linux

package file_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexfalkowski/go-linux/file"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

func TestOpenReturnsAnIndependentStandardFile(t *testing.T) {
	root := t.TempDir()
	dir := test.OpenFile(t, root)
	opened, err := file.Open(dir, "data", file.OpenOptions{Flags: file.ReadWrite | file.Create | file.Exclusive, Perm: 0o600, Resolve: file.Beneath})
	require.NoError(t, err)
	test.Cleanup(t, opened)
	require.NoError(t, dir.Close())

	test.WriteAt(t, opened, 0, "hello")
	require.Equal(t, "hello", string(test.ReadAt(t, opened, 0, 5)))

	info, err := opened.Stat()
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	require.Equal(t, "data", opened.Name())

	require.NoError(t, opened.Close())

	_, err = opened.Read(make([]byte, 1))
	require.ErrorIs(t, err, os.ErrClosed)

	require.ErrorIs(t, opened.Close(), os.ErrClosed)
}

func TestOpenSelectsOneAccessMode(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		flags    file.OpenFlags
		readErr  error
		writeErr error
	}{
		{name: "read only", flags: file.ReadOnly, writeErr: test.ErrBadDescriptor},
		{name: "write only", flags: file.WriteOnly, readErr: test.ErrBadDescriptor},
		{name: "read write", flags: file.ReadWrite},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := t.TempDir()
			dir := test.OpenFile(t, root)
			require.NoError(t, os.WriteFile(filepath.Join(root, "data"), []byte("before"), 0o600))

			opened, err := file.Open(dir, "data", file.OpenOptions{Flags: scenario.flags})
			require.NoError(t, err)
			test.Cleanup(t, opened)

			value := make([]byte, 1)
			_, err = opened.ReadAt(value, 0)
			require.ErrorIs(t, err, scenario.readErr)

			if scenario.readErr == nil {
				require.Equal(t, []byte("b"), value)
			}

			_, err = opened.WriteAt([]byte("after!"), 0)
			require.ErrorIs(t, err, scenario.writeErr)

			contents, err := os.ReadFile(filepath.Join(root, "data"))
			require.NoError(t, err)

			if scenario.writeErr == nil {
				require.Equal(t, "after!", string(contents))
			} else {
				require.Equal(t, "before", string(contents), "read-only access must not modify the file")
			}
		})
	}
}

func TestOpenSelectsAppendOrTruncateWrites(t *testing.T) {
	root := t.TempDir()
	dir := test.OpenFile(t, root)
	require.NoError(t, os.WriteFile(filepath.Join(root, "data"), []byte("first"), 0o600))

	for _, scenario := range []struct {
		name  string
		want  string
		flags file.OpenFlags
	}{
		{name: "append", want: "firstnext", flags: file.WriteOnly | file.Append},
		{name: "truncate", want: "next", flags: file.WriteOnly | file.Truncate},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			opened, err := file.Open(dir, "data", file.OpenOptions{Flags: scenario.flags})
			require.NoError(t, err)
			test.Cleanup(t, opened)

			n, err := opened.WriteString("next")
			require.NoError(t, err)
			require.Equal(t, 4, n)

			contents, err := os.ReadFile(filepath.Join(root, "data"))
			require.NoError(t, err)
			require.Equal(t, scenario.want, string(contents))
		})
	}
}

func TestOpenSelectsWriteSynchronization(t *testing.T) {
	dir := test.OpenFile(t, t.TempDir())

	for _, scenario := range []struct {
		name  string
		flags file.OpenFlags
		want  int
	}{
		{name: "default"},
		{name: "data integrity", flags: file.DataSync, want: test.DataSyncFlag},
		{name: "file integrity", flags: file.Sync, want: test.SyncFlag},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			opened, err := file.Open(dir, scenario.name, file.OpenOptions{
				Flags: file.WriteOnly | file.Create | scenario.flags,
				Perm:  0o600,
			})
			require.NoError(t, err)
			test.Cleanup(t, opened)

			require.Equal(t, scenario.want, test.FileFlags(t, opened)&test.SyncFlag)
		})
	}
}

func TestOpenTemporaryFilePreservesKernelFlagErrors(t *testing.T) {
	dir := test.OpenFile(t, t.TempDir())

	for _, scenario := range []struct {
		name  string
		flags file.OpenFlags
	}{
		{name: "read only", flags: file.ReadOnly},
		{name: "named creation", flags: file.WriteOnly | file.Create},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			opened, err := file.Open(dir, ".", file.OpenOptions{Flags: file.TempFile | scenario.flags, Perm: 0o600})
			require.ErrorIs(t, err, test.ErrInvalidArgument)
			require.Nil(t, opened)
		})
	}
}

func TestExclusiveCreationPreservesExistingFileAndAnchor(t *testing.T) {
	dir := test.OpenFile(t, t.TempDir())
	created, err := file.Open(dir, "data", file.OpenOptions{Flags: file.Create | file.Exclusive, Perm: 0o600})
	require.NoError(t, err)
	test.Cleanup(t, created)

	opened, err := file.Open(dir, "data", file.OpenOptions{Flags: file.Create | file.Exclusive, Perm: 0o600})
	require.ErrorIs(t, err, test.ErrExists)
	require.Nil(t, opened)

	_, err = dir.Stat()
	require.NoError(t, err, "failed creation must leave the borrowed anchor open")
}

func TestCreateTranslatesGoSpecialPermissionBits(t *testing.T) {
	dir := test.OpenFile(t, t.TempDir())
	perm := os.FileMode(0o700) | os.ModeSetuid | os.ModeSetgid | os.ModeSticky
	opened, err := file.Open(dir, "special", file.OpenOptions{Flags: file.Create | file.Exclusive | file.ReadWrite, Perm: perm})
	require.NoError(t, err)
	test.Cleanup(t, opened)

	info, err := opened.Stat()
	require.NoError(t, err)
	require.Equal(t, perm, info.Mode(), "Go special bits must be translated to Linux mode bits at creation")
}

func TestOpenEnforcesResolutionAndPreservesPathSemantics(t *testing.T) {
	root := t.TempDir()
	anchor := filepath.Join(root, "anchor")
	require.NoError(t, os.Mkdir(anchor, 0o700))
	require.NoError(t, os.Mkdir(filepath.Join(anchor, "sub"), 0o700))

	require.NoError(t, os.WriteFile(filepath.Join(root, "outside"), []byte("outside"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(anchor, "data"), []byte("inside"), 0o600))

	require.NoError(t, os.Symlink("data", filepath.Join(anchor, "link")))
	require.NoError(t, os.Symlink("../outside", filepath.Join(anchor, "escape")))
	require.NoError(t, os.Symlink("/data", filepath.Join(anchor, "absolute")))
	require.NoError(t, os.Symlink("sub", filepath.Join(anchor, "linkdir")))

	dir := test.OpenFile(t, anchor)

	for _, scenario := range []struct {
		wantErr error
		name    string
		path    string
		want    string
		options file.OpenOptions
	}{
		{name: "ordinary escape", path: "../outside", want: "outside"},
		{name: "ordinary absolute", path: filepath.Join(anchor, "data"), want: "inside"},
		{name: "beneath regular", path: "data", want: "inside", options: file.OpenOptions{Resolve: file.Beneath | file.NoXDev}},
		{name: "beneath safe link", path: "link", want: "inside", options: file.OpenOptions{Resolve: file.Beneath}},
		{name: "beneath with no symlinks", path: "link", wantErr: test.ErrSymlinkLoop, options: file.OpenOptions{Resolve: file.Beneath | file.NoSymlinks}},
		{name: "no magic permits ordinary link", path: "link", want: "inside", options: file.OpenOptions{Resolve: file.NoMagicLinks}},
		{name: "beneath parent escape", path: "../outside", wantErr: test.ErrCrossDevice, options: file.OpenOptions{Resolve: file.Beneath}},
		{name: "beneath link escape", path: "escape", wantErr: test.ErrCrossDevice, options: file.OpenOptions{Resolve: file.Beneath}},
		{name: "beneath absolute", path: filepath.Join(anchor, "data"), wantErr: test.ErrCrossDevice, options: file.OpenOptions{Resolve: file.Beneath}},
		{name: "in root absolute", path: "/data", want: "inside", options: file.OpenOptions{Resolve: file.InRoot}},
		{name: "in root clamps parent", path: "../../data", want: "inside", options: file.OpenOptions{Resolve: file.InRoot}},
		{name: "in root absolute link", path: "absolute", want: "inside", options: file.OpenOptions{Resolve: file.InRoot}},
		{name: "in root with no symlinks", path: "absolute", wantErr: test.ErrSymlinkLoop, options: file.OpenOptions{Resolve: file.InRoot | file.NoSymlinks}},
		{name: "no symlinks final", path: "link", wantErr: test.ErrSymlinkLoop, options: file.OpenOptions{Resolve: file.NoSymlinks}},
		{name: "no symlinks intermediate", path: "linkdir/../data", wantErr: test.ErrSymlinkLoop, options: file.OpenOptions{Resolve: file.NoSymlinks}},
		{name: "nofollow final", path: "link", wantErr: test.ErrSymlinkLoop, options: file.OpenOptions{Flags: file.NoFollow}},
		{name: "nofollow intermediate", path: "linkdir/../data", want: "inside", options: file.OpenOptions{Flags: file.NoFollow}},
		{name: "empty name", wantErr: test.ErrNotExist},
		{name: "missing file", path: "missing", wantErr: test.ErrNotExist},
		{name: "trailing slash", path: "data/", wantErr: test.ErrNotDirectory},
		{name: "directory required", path: "data", wantErr: test.ErrNotDirectory, options: file.OpenOptions{Flags: file.Directory}},
		{name: "embedded NUL", path: "bad\x00name", wantErr: test.ErrInvalidArgument},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			opened, err := file.Open(dir, scenario.path, scenario.options)
			if scenario.wantErr != nil {
				require.ErrorIs(t, err, scenario.wantErr)
				require.Nil(t, opened)

				var pathErr *os.PathError
				require.ErrorAs(t, err, &pathErr)
				require.Equal(t, scenario.path, pathErr.Path)
				return
			}

			require.NoError(t, err)
			test.Cleanup(t, opened)
			require.Equal(t, scenario.want, string(test.ReadAt(t, opened, 0, len(scenario.want))))
		})
	}
}

func TestPathOnlyHandleInspectsSymlinkWithoutOpeningTarget(t *testing.T) {
	root := t.TempDir()
	dir := test.OpenFile(t, root)
	require.NoError(t, os.Symlink("missing", filepath.Join(root, "link")))

	link, err := file.Open(dir, "link", file.OpenOptions{Flags: file.Path | file.NoFollow, Resolve: file.NoSymlinks})
	require.NoError(t, err)
	test.Cleanup(t, link)

	metadata, err := file.Stat(link, file.MaskBasic)
	require.NoError(t, err)
	require.Equal(t, os.ModeSymlink, metadata.Mode.Type())

	_, err = link.Read(make([]byte, 1))
	require.ErrorIs(t, err, test.ErrBadDescriptor)
}

func TestPathOnlyDirectoryAnchorsCreation(t *testing.T) {
	dir := test.OpenFile(t, t.TempDir())

	anchor, err := file.Open(dir, ".", file.OpenOptions{Flags: file.Path | file.Directory})
	require.NoError(t, err)
	test.Cleanup(t, anchor)

	created, err := file.Open(anchor, "data", file.OpenOptions{Flags: file.Create | file.ReadWrite, Perm: 0o600, Resolve: file.Beneath})
	require.NoError(t, err)
	test.Cleanup(t, created)
	test.WriteAt(t, created, 0, "anchored")
}

func TestOpenRejectsMountCrossing(t *testing.T) {
	// Focused ABI scenarios use existing read-only mount points and procfs links.
	root := test.OpenFile(t, "/")
	proc := test.OpenFile(t, "/proc")
	rootStat, err := file.Stat(root, file.MaskMountID)
	require.NoError(t, err)

	procStat, err := file.Stat(proc, file.MaskMountID)
	require.NoError(t, err)
	require.NotEqual(t, rootStat.MountID, procStat.MountID, "fixture must cross a mount")

	opened, err := file.Open(root, "proc", file.OpenOptions{Flags: file.Path, Resolve: file.NoXDev})
	require.ErrorIs(t, err, test.ErrCrossDevice)
	require.Nil(t, opened)
}

func TestNoMagicLinksCanInspectButNotFollowProcLink(t *testing.T) {
	proc := test.OpenFile(t, "/proc")

	source := test.TempFile(t, 0)
	magicLink := fmt.Sprintf("self/fd/%d", test.FileDescriptor(t, source))
	opened, err := file.Open(proc, magicLink, file.OpenOptions{Flags: file.Path, Resolve: file.NoMagicLinks})
	require.ErrorIs(t, err, test.ErrSymlinkLoop)
	require.Nil(t, opened)

	link, err := file.Open(proc, magicLink, file.OpenOptions{Flags: file.Path | file.NoFollow, Resolve: file.NoMagicLinks})
	require.NoError(t, err)
	test.Cleanup(t, link)

	stat, err := file.Stat(link, file.MaskType)
	require.NoError(t, err)
	require.Equal(t, os.ModeSymlink, stat.Mode.Type())
}

func TestCachedLookupPreservesItsRequestedGuarantee(t *testing.T) {
	dir := test.OpenFile(t, t.TempDir())
	opened, err := file.Open(dir, ".", file.OpenOptions{Flags: file.Path | file.Directory, Resolve: file.Cached})
	if errors.Is(err, test.ErrTryAgain) {
		require.Nil(t, opened)
	} else {
		require.NoError(t, err)
		test.Cleanup(t, opened)
	}

	opened, err = file.Open(dir, "new", file.OpenOptions{Flags: file.Create | file.ReadWrite, Perm: 0o600, Resolve: file.Cached})
	require.ErrorIs(t, err, test.ErrTryAgain, "creation needs uncached work and must not be retried without Cached")
	require.Nil(t, opened)

	_, err = file.StatAt(dir, "new", file.StatOptions{Mask: file.MaskType})
	require.ErrorIs(t, err, test.ErrNotExist, "failed cached creation must not create an entry")
}

func TestOpenRejectsInvalidOptionsBeforeAcquiringAResource(t *testing.T) {
	dir := test.OpenFile(t, t.TempDir())
	closed := test.OpenFile(t, t.TempDir())
	require.NoError(t, closed.Close())

	for _, scenario := range []struct {
		name    string
		options file.OpenOptions
	}{
		{name: "unknown flags", options: file.OpenOptions{Flags: 1 << 63}},
		{name: "invalid access", options: file.OpenOptions{Flags: file.WriteOnly | file.ReadWrite}},
		{name: "unknown resolution", options: file.OpenOptions{Resolve: 1 << 63}},
		{name: "conflicting confinement", options: file.OpenOptions{Resolve: file.Beneath | file.InRoot}},
		{name: "mode without creation", options: file.OpenOptions{Perm: 0o600}},
		{name: "nonpermission mode", options: file.OpenOptions{Flags: file.Create, Perm: os.ModeDir}},
		{name: "path with write", options: file.OpenOptions{Flags: file.Path | file.WriteOnly}},
		{name: "path with read write", options: file.OpenOptions{Flags: file.ReadWrite | file.Path}},
		{name: "path with create", options: file.OpenOptions{Flags: file.Path | file.Create}},
		{name: "path with temporary file", options: file.OpenOptions{Flags: file.Path | file.TempFile}},
		{name: "directory with permissions", options: file.OpenOptions{Flags: file.Directory, Perm: 0o600}},
		{name: "incomplete temporary flag", options: file.OpenOptions{Flags: file.TempFile &^ file.Directory}},
		{name: "incomplete sync flag", options: file.OpenOptions{Flags: file.Sync &^ file.DataSync}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			for _, anchor := range []*os.File{dir, closed} {
				opened, err := file.Open(anchor, "data", scenario.options)
				require.ErrorIs(t, err, os.ErrInvalid, "option validation must precede borrowing even a closed anchor")
				require.Nil(t, opened)
			}
		})
	}

	_, err := dir.Stat()
	require.NoError(t, err)
}
