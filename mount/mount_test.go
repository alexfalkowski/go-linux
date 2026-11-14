//go:build linux

package mount_test

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/mount"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

var (
	_ io.Closer = (*mount.Config)(nil)
	_ io.Closer = (*mount.Handle)(nil)
)

func TestZeroMountOwnersRejectOperations(t *testing.T) {
	var config mount.Config
	var handle mount.Handle
	for _, operation := range []struct {
		name string
		run  func() error
	}{
		{name: "set configuration flag", run: func() error {
			return config.SetFlag("rw")
		}},

		{name: "set configuration string", run: func() error {
			return config.SetString("mode", "0700")
		}},

		{name: "create filesystem", run: config.Create},
		{name: "close configuration", run: config.Close},
		{name: "change mount attributes", run: func() error {
			return handle.SetAttributes(mount.AttributeChange{})
		}},

		{name: "attach mount", run: func() error {
			return handle.Attach(nil, "/unused")
		}},

		{name: "inspect root metadata", run: func() error {
			_, err := handle.Stat()
			return err
		}},

		{name: "inspect mount attributes", run: func() error {
			_, err := handle.Attributes()
			return err
		}},

		{name: "close mount", run: handle.Close},
	} {
		t.Run(operation.name, func(t *testing.T) {
			require.ErrorIs(t, operation.run(), os.ErrInvalid)
		})
	}

	got, err := config.Mount(0)
	test.NoResource(t, got, err, os.ErrInvalid)

	records, err := config.Diagnostics()
	require.ErrorIs(t, err, os.ErrInvalid)
	require.Nil(t, records)
}

func TestInvalidMountAcquisitionArgumentsRetainNoDescriptors(t *testing.T) {
	before := test.DescriptorCount(t)
	for range 10 {
		config, err := mount.Open("tmpfs\x00other")
		test.NoResource(t, config, err, unix.EINVAL)

		for _, flags := range []mount.CloneFlags{0, 1, 1 << 31} {
			handle, err := mount.Clone(nil, "", flags)
			test.NoResource(t, handle, err, os.ErrInvalid)
		}

		for _, flags := range []mount.CloneFlags{1, 1 << 31} {
			handle, err := mount.Clone(nil, "/", flags)
			require.Nil(t, handle)
			require.ErrorIs(t, err, os.ErrInvalid, "unknown clone bits must fail independently of empty-path validation")
		}
	}

	require.Equal(t, before, test.DescriptorCount(t), "rejected acquisitions must not retain descriptors")
}

func TestUnmountRejectsUnknownFlagsAndNULPaths(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		path  string
		flags mount.UnmountFlags
		err   error
	}{
		{name: "unsupported flag", path: "/unused", flags: 1, err: os.ErrInvalid},
		{name: "known and unknown flags", path: "/unused", flags: mount.Detach | 4, err: os.ErrInvalid},
		{name: "NUL in path", path: "invalid\x00mount", err: unix.EINVAL},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			err := mount.Unmount(scenario.path, scenario.flags)
			require.ErrorIs(t, err, scenario.err)

			var pathError *os.PathError
			require.ErrorAs(t, err, &pathError)
			require.Equal(t, scenario.path, pathError.Path)
		})
	}
}

func TestUnmountReleasesAttachmentsWithExplicitReferenceRules(t *testing.T) {
	if !test.MountNamespace(t) {
		return
	}

	for _, scenario := range []struct {
		name             string
		flags            mount.UnmountFlags
		retainReferences bool
	}{
		{name: "normal unmount requires unused mount"},
		{name: "detach retains open references", flags: mount.Detach, retainReferences: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			path := t.TempDir()
			handle := test.Mount(t)

			require.NoError(t, handle.Attach(nil, path))

			attached := true
			t.Cleanup(func() {
				if attached {
					require.NoError(t, mount.Unmount(path, mount.Detach))
				}
			})

			filename := filepath.Join(path, "contents")
			require.NoError(t, os.WriteFile(filename, []byte("retained"), 0o600))

			file := test.OpenFile(t, filename)
			if !scenario.retainReferences {
				require.ErrorIs(t, mount.Unmount(path, 0), unix.EBUSY)
				require.NoError(t, file.Close())
				require.NoError(t, handle.Close())
			}

			require.NoError(t, mount.Unmount(path, scenario.flags))
			attached = false

			_, err := os.Stat(filename)
			require.ErrorIs(t, err, os.ErrNotExist, "attachment must disappear")

			if scenario.retainReferences {
				contents, err := io.ReadAll(file)
				require.NoError(t, err)
				require.Equal(t, "retained", string(contents))

				_, err = handle.Stat()
				require.NoError(t, err, "detach must leave the handle open")
			}
		})
	}
}

func TestCloningAClosedDirectoryReturnsNoHandle(t *testing.T) {
	dir, err := os.Open(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, dir.Close())

	before := test.DescriptorCount(t)

	handle, err := mount.Clone(dir, ".", 0)
	test.NoResource(t, handle, err, os.ErrClosed)
	require.Equal(t, before, test.DescriptorCount(t), "closed borrowed inputs must not produce an owner")
}

func TestInvalidMountAttributesAndEmptyTargetsAreRejected(t *testing.T) {
	var config mount.Config
	var handle mount.Handle
	for _, scenario := range []struct {
		name       string
		attributes mount.Attributes
	}{
		{name: "unsupported atime bits", attributes: 0x10},
		{name: "unknown high bit", attributes: 1 << 31},
		{name: "all attribute bits", attributes: ^mount.Attributes(0)},
	} {
		attributes := scenario.attributes

		got, err := config.Mount(attributes)
		require.Nil(t, got, scenario.name)
		require.ErrorIs(t, err, os.ErrInvalid, scenario.name)
		require.ErrorIs(t, handle.SetAttributes(mount.AttributeChange{Set: attributes}), os.ErrInvalid, scenario.name)
		require.ErrorIs(t, handle.SetAttributes(mount.AttributeChange{Clear: attributes}), os.ErrInvalid, scenario.name)
	}

	for _, attribute := range []mount.Attributes{mount.ReadOnly, mount.NoSUID, mount.NoDev, mount.NoExec} {
		require.ErrorIs(t, handle.SetAttributes(mount.AttributeChange{Set: attribute, Clear: attribute}), os.ErrInvalid)
	}

	require.ErrorIs(t, handle.Attach(nil, ""), os.ErrInvalid)
}

func TestFailedMountAcquisitionPreservesErrnoAndRetainsNoDescriptor(t *testing.T) {
	before := test.DescriptorCount(t)
	_, expected := unix.Fsopen("go-linux-nonexistent-filesystem", unix.FSOPEN_CLOEXEC)
	require.Error(t, expected)

	config, err := mount.Open("go-linux-nonexistent-filesystem")
	test.NoResource(t, config, err, expected)

	path := filepath.Join(t.TempDir(), "missing")
	_, expected = unix.OpenTree(unix.AT_FDCWD, path, unix.OPEN_TREE_CLONE|unix.OPEN_TREE_CLOEXEC)
	require.Error(t, expected)

	handle, err := mount.Clone(nil, path, 0)
	test.NoResource(t, handle, err, expected)

	expected = unix.Unmount(path, 0)
	require.Error(t, expected)
	require.ErrorIs(t, mount.Unmount(path, 0), expected)
	require.Equal(t, before, test.DescriptorCount(t), "failed kernel acquisitions must not retain descriptors")
}

func TestDiagnosticRetrievalErrorsDoNotReplaceTheOperationError(t *testing.T) {
	err := &mount.Error{
		Err:             os.NewSyscallError("fsconfig", unix.EINVAL),
		DiagnosticError: unix.EMSGSIZE,
		Diagnostics:     []mount.Diagnostic{{Severity: 'e', Message: "invalid option"}},
	}

	require.ErrorIs(t, err, unix.EINVAL)
	require.NotErrorIs(t, err, unix.EMSGSIZE, "retrieval failure must not replace or masquerade as the primary errno")

	var syscallErr *os.SyscallError
	require.ErrorAs(t, err, &syscallErr)
	require.Contains(t, err.Error(), "invalid option")
	require.Contains(t, err.Error(), "diagnostics: "+unix.EMSGSIZE.Error())
}

func TestMountRequiresExplicitFilesystemCreation(t *testing.T) {
	if !test.MountNamespace(t) {
		return
	}
	config := test.MountConfig(t)
	handle, err := config.Mount(0)
	test.NoResource(t, handle, err, unix.EINVAL)
}

func TestFilesystemParametersAndMountRestrictionsReachTheKernel(t *testing.T) {
	if !test.MountNamespace(t) {
		return
	}

	before := test.Descriptors(t)
	config := test.MountConfig(t)
	test.CloseOnExec(t, test.NewDescriptor(t, before))
	require.NoError(t, config.SetFlag("rw"))
	require.NoError(t, config.SetString("size", "1m"))
	require.NoError(t, config.SetString("mode", "0700"))
	require.NoError(t, config.Create())

	const all = mount.ReadOnly | mount.NoSUID | mount.NoDev | mount.NoExec

	before = test.Descriptors(t)
	handle, err := config.Mount(all)
	require.NoError(t, err)
	test.Cleanup(t, handle)
	fd := test.NewDescriptor(t, before)
	test.CloseOnExec(t, fd)

	statfs := test.MountFilesystem(t, fd)
	require.Equal(t, int64(unix.TMPFS_MAGIC), statfs.Type)

	attributes := test.Value(t, handle.Attributes)
	require.Equal(t, all, attributes)

	stat := test.Value(t, handle.Stat)
	require.Equal(t, os.FileMode(0o700), stat.Mode().Perm())
}

func TestClosingConfigurationLeavesItsMountUsable(t *testing.T) {
	if !test.MountNamespace(t) {
		return
	}

	config := test.MountConfig(t)

	require.NoError(t, config.Create())

	handle, err := config.Mount(0)
	require.NoError(t, err)
	test.Cleanup(t, handle)

	require.NoError(t, config.Close())

	for _, scenario := range []struct {
		name string
		run  func() error
	}{
		{name: "flag parameters", run: func() error {
			return config.SetFlag("rw")
		}},

		{name: "string parameters", run: func() error {
			return config.SetString("mode", "0700")
		}},

		{name: "creation", run: config.Create},
		{name: "repeated close", run: config.Close},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			require.ErrorIs(t, scenario.run(), os.ErrClosed)
		})
	}

	got, err := config.Mount(0)
	test.NoResource(t, got, err, os.ErrClosed)

	_, err = config.Diagnostics()
	require.ErrorIs(t, err, os.ErrClosed)
	require.NoError(t, handle.SetAttributes(mount.AttributeChange{Set: mount.NoExec}))

	attributes := test.Value(t, handle.Attributes)
	require.NotZero(t, attributes&mount.NoExec)
}

func TestAttributeChangesSetAndClearOnlyTheRequestedRestrictions(t *testing.T) {
	if !test.MountNamespace(t) {
		return
	}

	handle := test.Mount(t)
	const all = mount.ReadOnly | mount.NoSUID | mount.NoDev | mount.NoExec
	for bits := range mount.Attributes(16) {
		t.Run(test.MountAttributesName(bits), func(t *testing.T) {
			require.NoError(t, handle.SetAttributes(mount.AttributeChange{Set: bits, Clear: all & ^bits}))

			attributes := test.Value(t, handle.Attributes)
			require.Equal(t, bits, attributes)
		})
	}

	require.NoError(t, handle.SetAttributes(mount.AttributeChange{}))
}

func TestConfigurationErrorsPreserveCompleteDiagnostics(t *testing.T) {
	if !test.MountNamespace(t) {
		return
	}

	config := test.MountConfig(t)

	require.Nil(t, test.Value(t, config.Diagnostics))

	key := strings.Repeat("x", 255)

	err := config.SetFlag(key)
	require.ErrorIs(t, err, unix.EINVAL)

	var failure *mount.Error
	require.ErrorAs(t, err, &failure)
	require.NoError(t, failure.DiagnosticError)
	require.NotEmpty(t, failure.Diagnostics)
	require.Equal(t, byte('e'), failure.Diagnostics[len(failure.Diagnostics)-1].Severity)
	require.Contains(t, failure.Error(), key)
	require.Nil(t, test.Value(t, config.Diagnostics), "failed operations already drain their records")
}

func TestRejectedParametersLeaveTheConfigurationContextUsable(t *testing.T) {
	if !test.MountNamespace(t) {
		return
	}

	for _, scenario := range []struct {
		name      string
		configure func(*mount.Config) error
		err       error
	}{
		{name: "invalid mode", configure: func(c *mount.Config) error {
			return c.SetString("mode", "invalid")
		}, err: unix.EINVAL},

		{name: "flag instead of string", configure: func(c *mount.Config) error {
			return c.SetFlag("size")
		}, err: unix.EINVAL},

		{name: "NUL in string", configure: func(c *mount.Config) error {
			return c.SetString("mode", "\x00")
		}, err: unix.EINVAL},

		{name: "256 byte key", configure: func(c *mount.Config) error {
			return c.SetFlag(strings.Repeat("x", 256))
		}, err: unix.EINVAL},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			config := test.MountConfig(t)
			require.ErrorIs(t, scenario.configure(config), scenario.err)
			require.NoError(t, config.SetString("source", ""))
			require.NoError(t, config.Create())
		})
	}
}

func TestCreatingAnAlreadyPreparedFilesystemReturnsEBUSY(t *testing.T) {
	if !test.MountNamespace(t) {
		return
	}

	config := test.MountConfig(t)

	require.NoError(t, config.Create())

	err := config.Create()
	require.ErrorIs(t, err, unix.EBUSY)

	var failure *mount.Error
	require.ErrorAs(t, err, &failure)
}

func TestMountRejectsInvalidAttributeChangesBeforeCallingTheKernel(t *testing.T) {
	if !test.MountNamespace(t) {
		return
	}

	config := test.MountConfig(t)

	handle := test.Mount(t)
	for _, scenario := range []struct {
		name       string
		attributes mount.Attributes
	}{
		{name: "unsupported atime bits", attributes: 0x10},
		{name: "unknown high bit", attributes: 1 << 31},
		{name: "all attribute bits", attributes: ^mount.Attributes(0)},
	} {
		attributes := scenario.attributes

		got, err := config.Mount(attributes)
		require.Nil(t, got, scenario.name)
		require.ErrorIs(t, err, os.ErrInvalid, "%s must fail before kernel phase validation", scenario.name)
		require.ErrorIs(t, handle.SetAttributes(mount.AttributeChange{Set: attributes}), os.ErrInvalid, scenario.name)
		require.ErrorIs(t, handle.SetAttributes(mount.AttributeChange{Clear: attributes}), os.ErrInvalid, scenario.name)
	}

	for _, attribute := range []mount.Attributes{mount.ReadOnly, mount.NoSUID, mount.NoDev, mount.NoExec} {
		require.ErrorIs(t, handle.SetAttributes(mount.AttributeChange{Set: attribute, Clear: attribute}), os.ErrInvalid)
	}

	require.ErrorIs(t, handle.Attach(nil, ""), os.ErrInvalid)
}

func TestDiagnosticsDrainsTheNewestEightKernelRecordsInOrder(t *testing.T) {
	if !test.MountNamespace(t) {
		return
	}
	before := test.Descriptors(t)
	config := test.MountConfig(t)
	fd := test.NewDescriptor(t, before)

	test.QueueMountDiagnostics(t, fd, 10)

	records, err := config.Diagnostics()
	require.NoError(t, err)
	require.Len(t, records, 8)

	for index, record := range records {
		require.Equal(t, byte('e'), record.Severity)
		require.Contains(t, record.Message, fmt.Sprintf("unknown%d", index+2))
		require.NotContains(t, record.Message, "\n")
	}

	records, err = config.Diagnostics()
	require.NoError(t, err)
	require.Nil(t, records)
}

func TestConcurrentConfigurationKeepsEachErrorWithItsDiagnostics(t *testing.T) {
	if !test.MountNamespace(t) {
		return
	}

	config := test.MountConfig(t)
	results := make(chan error, 16)
	for index := range 16 {
		go func() {
			key := fmt.Sprintf("unknown-%02d", index)

			err := config.SetFlag(key)
			var failure *mount.Error
			if !errors.As(err, &failure) || len(failure.Diagnostics) != 1 || !strings.Contains(failure.Diagnostics[0].Message, key) {
				results <- fmt.Errorf("diagnostics not attributed to %s: %w", key, err)
				return
			}

			results <- nil
		}()
	}

	for range 16 {
		require.NoError(t, <-results)
	}

	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			results <- config.SetFlag("rw")
		})
	}

	require.NoError(t, config.Close())

	workers.Wait()

	for range 8 {
		err := <-results
		require.True(t, err == nil || errors.Is(err, os.ErrClosed), "concurrent Close must not expose reused descriptors: %v", err)
	}
}

func TestAttachedMountSurvivesClosingItsHandleAndTargetDirectory(t *testing.T) {
	if !test.MountNamespace(t) {
		return
	}

	handle := test.Mount(t)

	attributes := test.Value(t, handle.Attributes)
	require.Zero(t, attributes, "zero mount attributes must add no restriction")

	path := t.TempDir()
	dir := test.OpenFile(t, path)

	require.ErrorIs(t, handle.Attach(dir, "missing"), unix.ENOENT)

	test.AttachMount(t, handle, dir, "", path)

	require.NoError(t, dir.Close(), "target is borrowed only for the operation")
	require.NoError(t, handle.Close())

	for _, operation := range []struct {
		name string
		run  func() error
	}{
		{name: "repeated close", run: handle.Close},
		{name: "change attributes", run: func() error {
			return handle.SetAttributes(mount.AttributeChange{})
		}},

		{name: "attach", run: func() error {
			return handle.Attach(nil, path)
		}},

		{name: "read metadata", run: func() error {
			_, err := handle.Stat()
			return err
		}},

		{name: "read attributes", run: func() error {
			_, err := handle.Attributes()
			return err
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			require.ErrorIs(t, operation.run(), os.ErrClosed)
		})
	}

	require.NoError(t, os.WriteFile(filepath.Join(path, "persists"), []byte("mounted"), 0o600), "Close must leave attachment intact")

	var stat unix.Statfs_t
	require.NoError(t, unix.Statfs(path, &stat))
	require.Equal(t, int64(unix.TMPFS_MAGIC), stat.Type)

	data, err := os.ReadFile(filepath.Join(path, "persists"))
	require.NoError(t, err)
	require.Equal(t, "mounted", string(data))
}

func TestClosingAHandleSafelyRacesWithAttributeChanges(t *testing.T) {
	if !test.MountNamespace(t) {
		return
	}

	handle := test.Mount(t)
	results := make(chan error, 8)
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			results <- handle.SetAttributes(mount.AttributeChange{})
		})
	}

	require.NoError(t, handle.Close())

	workers.Wait()

	for range 8 {
		err := <-results
		require.True(t, err == nil || errors.Is(err, os.ErrClosed), "concurrent Close must retain descriptor lifetime and normalize closing errors: %v", err)
	}
}

func TestCloningIncludesChildMountsOnlyWhenRequested(t *testing.T) {
	if !test.MountNamespace(t) {
		return
	}

	source := test.MountedTree(t)
	for _, scenario := range []struct {
		name    string
		content string
		flags   mount.CloneFlags
	}{
		{name: "single mount exposes the underlying directory", content: "lower"},
		{name: "recursive clone includes the child mount", content: "upper", flags: mount.Recursive},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			dir := test.OpenFile(t, source)

			before := test.Descriptors(t)
			handle, err := mount.Clone(dir, "", scenario.flags)
			require.NoError(t, err)
			test.Cleanup(t, handle)
			test.CloseOnExec(t, test.NewDescriptor(t, before))
			require.NoError(t, dir.Close(), "clone must outlive its borrowed directory")

			path := t.TempDir()
			target := test.OpenFile(t, filepath.Dir(path))
			test.AttachMount(t, handle, target, filepath.Base(path), path)
			data, err := os.ReadFile(filepath.Join(path, "nested", "value"))
			require.NoError(t, err)
			require.Equal(t, scenario.content, string(data))
		})
	}
}

func TestRecursiveAttributesAffectTheCloneAndItsChildMounts(t *testing.T) {
	if !test.MountNamespace(t) {
		return
	}

	source := test.MountedTree(t)
	for _, scenario := range []struct {
		name  string
		flags mount.CloneFlags
	}{
		{name: "single mount clone"},
		{name: "clone with child mounts", flags: mount.Recursive},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			handle, path := test.ClonedMount(t, source, scenario.flags)

			require.NoError(t, handle.SetAttributes(mount.AttributeChange{Set: mount.ReadOnly, Recursive: true}))

			for _, directory := range []string{path, filepath.Join(path, "nested")} {
				require.ErrorIs(t, os.WriteFile(filepath.Join(directory, "new"), nil, 0o600), unix.EROFS, directory)
			}

			require.NoError(t, os.WriteFile(filepath.Join(source, "original"), nil, 0o600), "source mount remains writable")
			require.NoError(t, handle.SetAttributes(mount.AttributeChange{Clear: mount.ReadOnly, Recursive: true}))
			require.NoError(t, os.WriteFile(filepath.Join(path, "nested", "new"), nil, 0o600))
		})
	}
}

func TestNonrecursiveAttributesLeaveChildMountsUnchanged(t *testing.T) {
	if !test.MountNamespace(t) {
		return
	}

	source := test.MountedTree(t)
	handle, path := test.ClonedMount(t, source, mount.Recursive)

	require.NoError(t, handle.SetAttributes(mount.AttributeChange{Set: mount.NoExec}))

	child, err := mount.Clone(nil, filepath.Join(path, "nested"), 0)
	require.NoError(t, err)
	test.Cleanup(t, child)

	childAttributes, err := child.Attributes()
	require.NoError(t, err)
	require.Zero(t, childAttributes&mount.NoExec, "child mount remains executable")

	attributes := test.Value(t, handle.Attributes)
	require.NotZero(t, attributes&mount.NoExec, "root mount must change")
}

func TestMountAcquisitionAndCloseLeaveNoDescriptorsBehind(t *testing.T) {
	if !test.MountNamespace(t) {
		return
	}

	path := t.TempDir()
	dir := test.OpenFile(t, path)
	before := test.DescriptorCount(t)
	for range 12 {
		config := test.MountConfig(t)

		require.NoError(t, config.Create())

		handle, err := config.Mount(0)
		require.NoError(t, err)
		require.NoError(t, handle.Close())
		require.NoError(t, config.Close())

		for _, source := range []struct {
			name string
			dir  *os.File
			path string
			err  error
		}{
			{name: "missing target", dir: dir, path: "missing", err: unix.ENOENT},
			{name: "absolute clone", path: path},
			{name: "relative clone", dir: dir, path: "."},
		} {
			handle, err := mount.Clone(source.dir, source.path, 0)
			if source.err != nil {
				test.NoResource(t, handle, err, source.err)
			} else {
				require.NoError(t, err, source.name)
				require.NoError(t, handle.Close(), source.name)
			}
		}

		_ = test.Value(t, dir.Stat)
		config, err = mount.Open("go-linux-nonexistent-filesystem")
		test.NoResource(t, config, err, unix.ENODEV)
	}

	require.Equal(t, before, test.DescriptorCount(t), "acquisition and Close must leave no descriptors")
}

func TestClosedBorrowedDirectoriesLeaveMountHandlesUsable(t *testing.T) {
	if !test.MountNamespace(t) {
		return
	}

	dir := test.OpenFile(t, t.TempDir())

	require.NoError(t, dir.Close())

	handle, err := mount.Clone(dir, ".", 0)
	test.NoResource(t, handle, err, os.ErrClosed)

	handle = test.Mount(t)

	require.ErrorIs(t, handle.Attach(dir, "."), os.ErrClosed)
	require.NoError(t, handle.SetAttributes(mount.AttributeChange{Set: mount.NoExec}))
}

func TestMountAcquisitionFailsWithoutAnOwnerWhenDescriptorsAreExhausted(t *testing.T) {
	if !test.MountNamespace(t) {
		return
	}

	config := test.MountConfig(t)

	require.NoError(t, config.Create())

	path := t.TempDir()
	before := test.DescriptorCount(t)
	t.Cleanup(func() {
		require.Equal(t, before, test.DescriptorCount(t), "failed acquisitions must not retain descriptors")
	})

	test.ExhaustDescriptors(t)
	got, err := mount.Open("tmpfs")
	test.NoResource(t, got, err, unix.EMFILE)

	handle, err := mount.Clone(nil, path, 0)
	test.NoResource(t, handle, err, unix.EMFILE)

	handle, err = config.Mount(0)
	test.NoResource(t, handle, err, unix.EMFILE)
}

func TestMountExamplesRunInsideAnIsolatedNamespace(t *testing.T) {
	if !test.MountNamespace(t) {
		return
	}

	t.Run("configure a detached filesystem", func(t *testing.T) {
		ExampleConfig()
	})

	t.Run("attach and explicitly unmount a filesystem", func(t *testing.T) {
		ExampleHandle_Attach()
	})
}
