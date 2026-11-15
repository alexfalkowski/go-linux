//go:build linux

package file_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alexfalkowski/go-linux/file"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestRenameMovesBetweenDirectoryAnchorsAndReplaces(t *testing.T) {
	oldPath, newPath := t.TempDir(), t.TempDir()
	oldDir, newDir := test.OpenFile(t, oldPath), test.OpenFile(t, newPath)
	require.NoError(t, os.WriteFile(filepath.Join(oldPath, "source"), []byte("new contents"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(newPath, "destination"), []byte("old contents"), 0o600))

	oldFile := test.OpenFile(t, filepath.Join(oldPath, "source"))

	require.NoError(t, file.Rename(oldDir, "source", newDir, "destination", file.Replace))

	_, err := os.Stat(filepath.Join(oldPath, "source"))
	require.ErrorIs(t, err, os.ErrNotExist)

	contents, err := os.ReadFile(filepath.Join(newPath, "destination"))
	require.NoError(t, err)
	require.Equal(t, "new contents", string(contents))
	require.Equal(t, "new contents", string(test.ReadAt(t, oldFile, 0, 12)), "open file identity must survive rename")

	_, err = oldDir.Stat()
	require.NoError(t, err)

	_, err = newDir.Stat()
	require.NoError(t, err)
}

func TestRenameNoReplaceLeavesExistingEntriesIntact(t *testing.T) {
	root := t.TempDir()
	dir := test.OpenFile(t, root)
	require.NoError(t, os.WriteFile(filepath.Join(root, "source"), []byte("source"), 0o600))

	require.NoError(t, os.Symlink("absent", filepath.Join(root, "destination")))

	err := file.Rename(dir, "source", dir, "destination", file.NoReplace)
	require.ErrorIs(t, err, unix.EEXIST, "a dangling destination symlink still exists")

	contents, err := os.ReadFile(filepath.Join(root, "source"))
	require.NoError(t, err)
	require.Equal(t, "source", string(contents))

	target, err := os.Readlink(filepath.Join(root, "destination"))
	require.NoError(t, err)
	require.Equal(t, "absent", target)

	require.NoError(t, file.Rename(dir, "source", dir, "new", file.NoReplace))

	_, err = os.Stat(filepath.Join(root, "source"))
	require.ErrorIs(t, err, unix.ENOENT)

	contents, err = os.ReadFile(filepath.Join(root, "new"))
	require.NoError(t, err)
	require.Equal(t, "source", string(contents))
}

func TestRenameExchangeSwapsDifferentTypesWithoutFollowingFinalLinks(t *testing.T) {
	root := t.TempDir()
	dir := test.OpenFile(t, root)
	require.NoError(t, os.Mkdir(filepath.Join(root, "directory"), 0o700))

	require.NoError(t, os.WriteFile(filepath.Join(root, "directory", "child"), []byte("nested"), 0o600))

	require.NoError(t, os.Symlink("absent", filepath.Join(root, "link")))

	require.NoError(t, file.Rename(dir, "directory", dir, "link", file.Exchange))

	target, err := os.Readlink(filepath.Join(root, "directory"))
	require.NoError(t, err)
	require.Equal(t, "absent", target)

	contents, err := os.ReadFile(filepath.Join(root, "link", "child"))
	require.NoError(t, err)
	require.Equal(t, "nested", string(contents))

	err = file.Rename(dir, "link", dir, "missing", file.Exchange)
	require.ErrorIs(t, err, unix.ENOENT)

	contents, err = os.ReadFile(filepath.Join(root, "link", "child"))
	require.NoError(t, err)
	require.Equal(t, "nested", string(contents), "failed exchange must preserve its existing entry")
}

func TestRenamePreservesBothNamesAndErrno(t *testing.T) {
	dir := test.OpenFile(t, t.TempDir())
	for _, scenario := range []struct {
		wantErr error
		name    string
		old     string
		mode    file.RenameMode
	}{
		{name: "missing source", old: "missing", wantErr: unix.ENOENT},
		{name: "empty source", wantErr: unix.ENOENT},
		{name: "embedded NUL", old: "bad\x00name", wantErr: unix.EINVAL},
		{name: "unknown mode", old: "source", mode: 1 << 31, wantErr: os.ErrInvalid},
		{name: "incompatible modes", old: "source", mode: file.NoReplace | file.Exchange, wantErr: os.ErrInvalid},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			err := file.Rename(dir, scenario.old, dir, "destination", scenario.mode)
			require.ErrorIs(t, err, scenario.wantErr)

			var linkErr *os.LinkError
			require.ErrorAs(t, err, &linkErr)
			require.Equal(t, scenario.old, linkErr.Old)
			require.Equal(t, "destination", linkErr.New)
			require.Equal(t, "renameat2", linkErr.Op)
		})
	}
}

func TestDirectoryAnchorsSurviveTheirOwnRename(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "original")
	require.NoError(t, os.Mkdir(original, 0o700))

	require.NoError(t, os.WriteFile(filepath.Join(original, "source"), []byte("original inode"), 0o600))

	dir := test.OpenFile(t, original)
	require.NoError(t, os.Rename(original, filepath.Join(root, "moved")))

	require.NoError(t, os.Mkdir(original, 0o700))

	require.NoError(t, file.Rename(dir, "source", dir, "destination", file.NoReplace))

	opened, err := file.Open(dir, "destination", file.OpenOptions{Resolve: file.Beneath})
	require.NoError(t, err)
	test.Cleanup(t, opened)
	require.Equal(t, "original inode", string(test.ReadAt(t, opened, 0, 14)))

	metadata, err := file.StatAt(dir, "destination", file.StatOptions{Mask: file.MaskSize})
	require.NoError(t, err)
	require.Equal(t, uint64(14), metadata.Size)

	entries, err := os.ReadDir(original)
	require.NoError(t, err)
	require.Empty(t, entries, "operations must use the handle, not its old pathname")
}
