//go:build linux

package file_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alexfalkowski/go-linux/file"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/stretchr/testify/require"
)

func TestLinkPublishesTheSameTemporaryInode(t *testing.T) {
	root := t.TempDir()
	dir := test.OpenFile(t, root)
	const permissions os.FileMode = 0o666
	temp, err := file.Open(dir, ".", file.OpenOptions{
		Flags:   file.TempFile | file.ReadWrite,
		Perm:    permissions,
		Resolve: file.Beneath | file.NoSymlinks,
	})
	require.NoError(t, err)
	test.Cleanup(t, temp)

	test.WriteAt(t, temp, 0, "manifest")
	unnamed, err := temp.Stat()
	require.NoError(t, err)
	require.True(t, unnamed.Mode().IsRegular())
	require.Equal(t, permissions&^test.FileCreationMask(t), unnamed.Mode().Perm())
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Empty(t, entries, "opening and writing must leave the directory empty")

	require.NoError(t, file.Link(temp, dir, "ready"))
	require.NoError(t, temp.Close())
	_, err = dir.Stat()
	require.NoError(t, err, "the destination anchor remains open")

	published, err := os.Stat(filepath.Join(root, "ready"))
	require.NoError(t, err)
	require.True(t, os.SameFile(unnamed, published))
	contents, err := os.ReadFile(filepath.Join(root, "ready"))
	require.NoError(t, err)
	require.Equal(t, "manifest", string(contents))
}

func TestLinkDoesNotReplaceAnExistingEntry(t *testing.T) {
	root := t.TempDir()
	dir := test.OpenFile(t, root)
	require.NoError(t, os.WriteFile(filepath.Join(root, "ready"), []byte("existing"), 0o600))
	temp, err := file.Open(dir, ".", file.OpenOptions{Flags: file.TempFile | file.WriteOnly, Perm: 0o600})
	require.NoError(t, err)
	test.Cleanup(t, temp)

	err = file.Link(temp, dir, "ready")
	require.ErrorIs(t, err, test.ErrExists)
	var linkErr *os.LinkError
	require.ErrorAs(t, err, &linkErr)
	require.Equal(t, ".", linkErr.Old)
	require.Equal(t, "ready", linkErr.New)

	contents, err := os.ReadFile(filepath.Join(root, "ready"))
	require.NoError(t, err)
	require.Equal(t, "existing", string(contents))
	metadata, err := file.Stat(temp, file.MaskLinks)
	require.NoError(t, err)
	require.NotZero(t, metadata.Mask&file.MaskLinks)
	require.Zero(t, metadata.Links, "the borrowed temporary inode is still unlinked")
}

func TestExclusiveTemporaryFileCannotBePublished(t *testing.T) {
	root := t.TempDir()
	dir := test.OpenFile(t, root)
	temp, err := file.Open(dir, ".", file.OpenOptions{Flags: file.TempFile | file.WriteOnly | file.Exclusive, Perm: 0o600})
	require.NoError(t, err)
	test.Cleanup(t, temp)
	test.WriteAt(t, temp, 0, "scratch data")

	err = file.Link(temp, dir, "ready")
	require.ErrorIs(t, err, test.ErrNotExist)
	require.NoError(t, temp.Close())

	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestLinkRejectsUnavailableHandles(t *testing.T) {
	dir := test.OpenFile(t, t.TempDir())
	closed := test.OpenFile(t, t.TempDir())
	require.NoError(t, closed.Close())

	for _, scenario := range []struct {
		name string
		file *os.File
		want error
	}{
		{name: "nil", want: os.ErrInvalid},
		{name: "zero", file: new(os.File), want: os.ErrInvalid},
		{name: "closed", file: closed, want: os.ErrClosed},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			require.ErrorIs(t, file.Link(scenario.file, dir, "ready"), scenario.want)
			require.ErrorIs(t, file.Link(dir, scenario.file, "ready"), scenario.want)
		})
	}
}
