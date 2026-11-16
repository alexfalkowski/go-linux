//go:build linux

package xattr_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alexfalkowski/go-linux/file"
	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/xattr"
	"github.com/stretchr/testify/require"
)

func TestAttributesRoundTripAsWholeBinaryValues(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		value []byte
	}{
		{name: "nil"},
		{name: "empty", value: []byte{}},
		{name: "binary", value: []byte{0, 255, 1, 0}},
		{name: "variable size", value: bytes.Repeat([]byte{42}, 1024)},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			opened := test.TempFile(t, 0)
			require.NoError(t, xattr.Set(opened, "user.roundtrip", scenario.value, xattr.Upsert))

			value, err := xattr.Get(opened, "user.roundtrip")
			require.NoError(t, err)
			require.NotNil(t, value)
			require.Equal(t, append([]byte{}, scenario.value...), value)

			names, err := xattr.List(opened)
			require.NoError(t, err)
			require.Contains(t, names, "user.roundtrip")

			require.NoError(t, xattr.Remove(opened, "user.roundtrip"))

			value, err = xattr.Get(opened, "user.roundtrip")
			require.ErrorIs(t, err, test.ErrNoData)
			require.Nil(t, value)

			names, err = xattr.List(opened)
			require.NoError(t, err)
			require.NotNil(t, names)
			require.NotContains(t, names, "user.roundtrip")

			_, err = opened.Stat()
			require.NoError(t, err)
		})
	}
}

func TestSetModesPreserveAtomicExistenceConditions(t *testing.T) {
	opened := test.TempFile(t, 0)
	require.ErrorIs(t, xattr.Set(opened, "user.mode", []byte("missing"), xattr.Replace), test.ErrNoData)
	require.NoError(t, xattr.Set(opened, "user.mode", []byte("created"), xattr.Create))
	require.ErrorIs(t, xattr.Set(opened, "user.mode", []byte("duplicate"), xattr.Create), test.ErrExists)

	value, err := xattr.Get(opened, "user.mode")
	require.NoError(t, err)
	require.Equal(t, "created", string(value))

	require.NoError(t, xattr.Set(opened, "user.mode", []byte("replaced"), xattr.Replace))

	value, err = xattr.Get(opened, "user.mode")
	require.NoError(t, err)
	require.Equal(t, "replaced", string(value))

	require.NoError(t, xattr.Set(opened, "user.mode", nil, xattr.Upsert))

	value, err = xattr.Get(opened, "user.mode")
	require.NoError(t, err)
	require.Equal(t, []byte{}, value)

	require.NoError(t, xattr.Remove(opened, "user.mode"))
	require.ErrorIs(t, xattr.Remove(opened, "user.mode"), test.ErrNoData)
}

func TestPathOnlyFilesRejectAttributeOperations(t *testing.T) {
	opened := test.TempFile(t, 0)
	dir := test.OpenFile(t, filepath.Dir(opened.Name()))
	pathOnly, err := file.Open(dir, filepath.Base(opened.Name()), file.OpenOptions{Flags: file.Path})
	require.NoError(t, err)
	test.Cleanup(t, pathOnly)

	_, err = xattr.Get(pathOnly, "user.path")
	require.ErrorIs(t, err, test.ErrBadDescriptor)
	require.ErrorIs(t, xattr.Set(pathOnly, "user.path", nil, xattr.Upsert), test.ErrBadDescriptor)
	require.ErrorIs(t, xattr.Remove(pathOnly, "user.path"), test.ErrBadDescriptor)
	_, err = xattr.List(pathOnly)
	require.ErrorIs(t, err, test.ErrBadDescriptor)
}

func TestRelativeAndAbsoluteAttributePaths(t *testing.T) {
	opened := test.TempFile(t, 0)
	dir := test.OpenFile(t, filepath.Dir(opened.Name()))
	name := filepath.Base(opened.Name())

	require.NoError(t, xattr.SetAt(dir, name, "user.path", []byte("relative"), xattr.Create, xattr.PathOptions{}))

	value, err := xattr.Get(opened, "user.path")
	require.NoError(t, err)
	require.Equal(t, "relative", string(value))

	require.NoError(t, xattr.Set(opened, "user.path", []byte("handle"), xattr.Replace))

	value, err = xattr.GetAt(dir, opened.Name(), "user.path", xattr.PathOptions{})
	require.NoError(t, err)
	require.Equal(t, "handle", string(value))

	names, err := xattr.ListAt(dir, name, xattr.PathOptions{})
	require.NoError(t, err)
	require.Contains(t, names, "user.path")
	require.NoError(t, xattr.RemoveAt(dir, name, "user.path", xattr.PathOptions{}))
}

func TestEmptyAttributePathSelectsDirectory(t *testing.T) {
	dir := test.OpenFile(t, t.TempDir())
	require.NoError(t, xattr.Set(dir, "user.directory", []byte("dir"), xattr.Create))

	value, err := xattr.GetAt(dir, "", "user.directory", xattr.PathOptions{NoFollow: true})
	require.NoError(t, err)
	require.Equal(t, "dir", string(value))
}

func TestAttributeHandlesSurviveRenameAndUnlink(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "original")
	require.NoError(t, os.Mkdir(root, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "data"), nil, 0o600))
	dir := test.OpenFile(t, root)
	opened := test.OpenFile(t, filepath.Join(root, "data"))
	moved := filepath.Join(parent, "moved")

	require.NoError(t, os.Rename(root, moved))
	require.NoError(t, xattr.SetAt(dir, "data", "user.unlinked", []byte("still open"), xattr.Upsert, xattr.PathOptions{}))
	require.NoError(t, os.Remove(filepath.Join(moved, "data")))

	value, err := xattr.Get(opened, "user.unlinked")
	require.NoError(t, err)
	require.Equal(t, "still open", string(value))

	require.NoError(t, xattr.Remove(opened, "user.unlinked"))

	names, err := xattr.List(opened)
	require.NoError(t, err)
	require.NotContains(t, names, "user.unlinked")
}

func TestSymlinkOperationsSelectTheLinkOrItsTarget(t *testing.T) {
	root := t.TempDir()
	dir := test.OpenFile(t, root)
	require.NoError(t, os.WriteFile(filepath.Join(root, "target"), nil, 0o600))

	require.NoError(t, os.Symlink("target", filepath.Join(root, "link")))
	require.NoError(t, os.Symlink("missing", filepath.Join(root, "dangling")))

	require.NoError(t, xattr.SetAt(dir, "link", "user.link", []byte("target"), xattr.Create, xattr.PathOptions{}))

	value, err := xattr.GetAt(dir, "target", "user.link", xattr.PathOptions{})
	require.NoError(t, err)
	require.Equal(t, "target", string(value))

	noFollow := xattr.PathOptions{NoFollow: true}
	for _, path := range []string{"link", "dangling"} {
		t.Run(path, func(t *testing.T) {
			value, err := xattr.GetAt(dir, path, "user.link", noFollow)
			require.ErrorIs(t, err, test.ErrNoData, "user attributes are unavailable on symlinks")
			require.Nil(t, value)

			require.ErrorIs(t, xattr.SetAt(dir, path, "user.link", nil, xattr.Upsert, noFollow), test.ErrOperationNotPermitted)

			require.ErrorIs(t, xattr.RemoveAt(dir, path, "user.link", noFollow), test.ErrOperationNotPermitted)

			names, err := xattr.ListAt(dir, path, noFollow)
			require.NoError(t, err)
			require.NotContains(t, names, "user.link")
		})
	}

	link, err := file.Open(dir, "link", file.OpenOptions{Flags: file.Path | file.NoFollow})
	require.NoError(t, err)
	test.Cleanup(t, link)

	value, err = xattr.Get(link, "user.link")
	require.ErrorIs(t, err, test.ErrBadDescriptor)
	require.Nil(t, value)

	require.NoError(t, xattr.RemoveAt(dir, "link", "user.link", xattr.PathOptions{}))

	value, err = xattr.GetAt(dir, "target", "user.link", xattr.PathOptions{})
	require.ErrorIs(t, err, test.ErrNoData)
	require.Nil(t, value)
}

func TestErrorsPreserveInputsAndReturnNoPartialResults(t *testing.T) {
	dir := test.OpenFile(t, t.TempDir())
	for _, scenario := range []struct {
		want error
		name string
		path string
		attr string
	}{
		{name: "missing path", path: "missing", attr: "user.a", want: test.ErrNotExist},
		{name: "NUL path", path: "bad\x00path", attr: "user.a", want: test.ErrInvalidArgument},
		{name: "NUL name", path: ".", attr: "user.bad\x00name", want: test.ErrInvalidArgument},
		{name: "empty name", path: ".", want: test.ErrRange},
		{name: "long name", path: ".", attr: strings.Repeat("x", 256), want: test.ErrRange},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			value, err := xattr.GetAt(dir, scenario.path, scenario.attr, xattr.PathOptions{})
			require.ErrorIs(t, err, scenario.want)
			require.Nil(t, value)

			var pathErr *os.PathError
			require.ErrorAs(t, err, &pathErr)
			require.Equal(t, scenario.path, pathErr.Path)

			require.ErrorIs(t, xattr.SetAt(dir, scenario.path, scenario.attr, nil, xattr.Upsert, xattr.PathOptions{}), scenario.want)

			require.ErrorIs(t, xattr.RemoveAt(dir, scenario.path, scenario.attr, xattr.PathOptions{}), scenario.want)
		})
	}

	for _, path := range []string{"missing", "bad\x00path"} {
		names, err := xattr.ListAt(dir, path, xattr.PathOptions{})
		require.Error(t, err)
		require.Nil(t, names)
	}

	for _, mode := range []xattr.SetMode{xattr.Create | xattr.Replace, 1 << 31} {
		require.ErrorIs(t, xattr.Set(nil, "user.mode", nil, mode), os.ErrInvalid)
	}

	require.ErrorIs(t, xattr.Set(dir, "user.large", make([]byte, 65537), xattr.Upsert), test.ErrArgumentListTooLong)

	_, err := dir.Stat()
	require.NoError(t, err, "errors leave borrowed handles open")
}

func TestUnavailableHandlesRejectEveryOperation(t *testing.T) {
	closed := test.TempFile(t, 0)
	require.NoError(t, closed.Close())

	for _, scenario := range []struct {
		want error
		file *os.File
		name string
	}{
		{name: "nil", want: os.ErrInvalid},
		{name: "closed", file: closed, want: os.ErrClosed},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			value, err := xattr.Get(scenario.file, "user.a")
			require.ErrorIs(t, err, scenario.want)
			require.Nil(t, value)

			names, err := xattr.List(scenario.file)
			require.ErrorIs(t, err, scenario.want)
			require.Nil(t, names)

			require.ErrorIs(t, xattr.Set(scenario.file, "user.a", nil, xattr.Upsert), scenario.want)

			require.ErrorIs(t, xattr.Remove(scenario.file, "user.a"), scenario.want)
		})
	}
}

func TestConcurrentSizeChangesReturnWholeValues(t *testing.T) {
	opened := test.TempFile(t, 0)
	small, large := []byte{}, bytes.Repeat([]byte{255}, 1024)
	require.NoError(t, xattr.Set(opened, "user.race", small, xattr.Upsert))

	start := make(chan struct{})
	results := make(chan error, 1)
	go func() {
		<-start
		for range 300 {
			for _, value := range [][]byte{small, large} {
				if err := xattr.Set(opened, "user.race", value, xattr.Replace); err != nil {
					results <- err
					return
				}
			}
		}

		results <- nil
	}()

	close(start)
	var successes int
	for range 300 {
		value, err := xattr.Get(opened, "user.race")
		if errors.Is(err, test.ErrRange) {
			require.Nil(t, value)
			continue
		}

		require.NoError(t, err)

		successes++
		require.True(t, bytes.Equal(value, small) || bytes.Equal(value, large), "read must return one complete version")
	}

	require.NoError(t, test.Receive(t, results))
	require.Positive(t, successes, "concurrent reads must exercise successful whole-value observations")
}

func TestConcurrentCloseKeepsBorrowedIdentity(t *testing.T) {
	for range 20 {
		opened := test.TempFile(t, 0)
		require.NoError(t, xattr.Set(opened, "user.close", []byte("value"), xattr.Create))

		start := make(chan struct{})
		results := make(chan error, 1)
		go func() {
			<-start
			_, err := xattr.Get(opened, "user.close")
			results <- err
		}()

		close(start)
		require.NoError(t, opened.Close())

		if err := test.Receive(t, results); !errors.Is(err, os.ErrClosed) {
			require.NoError(t, err)
		}
	}
}

func TestListsRemainCompleteDuringConcurrentAttributeChanges(t *testing.T) {
	opened := test.TempFile(t, 0)
	require.NoError(t, xattr.Set(opened, "user.stable", nil, xattr.Create))

	before, err := xattr.List(opened)
	require.NoError(t, err)

	start := make(chan struct{})
	results := make(chan error, 1)
	go func() {
		<-start
		for range 300 {
			if err := xattr.Set(opened, "user.changing", nil, xattr.Create); err != nil {
				results <- err
				return
			}

			if err := xattr.Remove(opened, "user.changing"); err != nil {
				results <- err
				return
			}
		}

		results <- nil
	}()

	close(start)
	var successes int
	for range 300 {
		names, err := xattr.List(opened)
		if errors.Is(err, test.ErrRange) {
			require.Nil(t, names)
			continue
		}

		require.NoError(t, err)

		successes++
		withoutChanging := slices.DeleteFunc(slices.Clone(names), func(name string) bool {
			return name == "user.changing"
		})
		require.ElementsMatch(t, before, withoutChanging, "list must retain every stable name without truncated records")
	}

	require.NoError(t, test.Receive(t, results))
	require.Positive(t, successes, "concurrent listings must exercise successful results")
}

func TestUnsupportedNamespacesAndNonDirectoryAnchorsStayObservable(t *testing.T) {
	opened := test.TempFile(t, 0)
	require.ErrorIs(t, xattr.Set(opened, "unsupported.attribute", nil, xattr.Upsert), test.ErrNotSupported)

	value, err := xattr.Get(opened, "unsupported.attribute")
	require.ErrorIs(t, err, test.ErrNotSupported)
	require.Nil(t, value)

	require.ErrorIs(t, xattr.Remove(opened, "unsupported.attribute"), test.ErrNotSupported)

	_, err = xattr.GetAt(opened, "child", "user.a", xattr.PathOptions{})
	require.ErrorIs(t, err, test.ErrNotDirectory)

	names, err := xattr.ListAt(opened, "child", xattr.PathOptions{})
	require.ErrorIs(t, err, test.ErrNotDirectory)
	require.Nil(t, names)

	require.ErrorIs(t, xattr.SetAt(opened, "child", "user.a", nil, xattr.Upsert, xattr.PathOptions{}), test.ErrNotDirectory)

	require.ErrorIs(t, xattr.RemoveAt(opened, "child", "user.a", xattr.PathOptions{}), test.ErrNotDirectory)
}

func TestPathOnlyDirectoryAnchorsNonemptyLookups(t *testing.T) {
	root := t.TempDir()
	dir := test.OpenFile(t, root)
	require.NoError(t, os.WriteFile(filepath.Join(root, "data"), nil, 0o600))

	anchor, err := file.Open(dir, ".", file.OpenOptions{Flags: file.Path | file.Directory})
	require.NoError(t, err)
	test.Cleanup(t, anchor)
	require.NoError(t, xattr.SetAt(anchor, "data", "user.anchor", []byte("anchored"), xattr.Create, xattr.PathOptions{}))

	value, err := xattr.GetAt(anchor, "data", "user.anchor", xattr.PathOptions{})
	require.NoError(t, err)
	require.Equal(t, "anchored", string(value))

	names, err := xattr.ListAt(anchor, "data", xattr.PathOptions{})
	require.NoError(t, err)
	require.Contains(t, names, "user.anchor")

	require.NoError(t, xattr.RemoveAt(anchor, "data", "user.anchor", xattr.PathOptions{}))
}

func TestAttributeCallsPreserveBorrowedFileState(t *testing.T) {
	// Focused lifetime/ABI observations supplement ordinary caller scenarios.
	opened := test.TempFile(t, 16)
	_, err := opened.Seek(7, io.SeekStart)
	require.NoError(t, err)

	flags := test.FileFlags(t, opened)
	descriptors := test.Descriptors(t)
	for range 10 {
		require.NoError(t, xattr.Set(opened, "user.borrowed", []byte("value"), xattr.Create))

		_, err := xattr.Get(opened, "user.borrowed")
		require.NoError(t, err)

		_, err = xattr.List(opened)
		require.NoError(t, err)

		require.ErrorIs(t, xattr.Set(opened, "user.borrowed", nil, xattr.Create), test.ErrExists)

		require.NoError(t, xattr.Remove(opened, "user.borrowed"))
	}

	offset, err := opened.Seek(0, io.SeekCurrent)
	require.NoError(t, err)
	require.Equal(t, int64(7), offset, "attribute operations must preserve ordinary I/O position")
	require.Equal(t, flags, test.FileFlags(t, opened), "attribute operations must preserve shared file status flags")
	require.Equal(t, descriptors, test.Descriptors(t), "success and failure must retain no descriptors")
}
