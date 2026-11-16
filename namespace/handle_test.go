//go:build linux

package namespace_test

import (
	"errors"
	"io"
	"os"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/namespace"
	"github.com/stretchr/testify/require"
)

var _ io.Closer = (*namespace.Handle)(nil)

func TestCurrentNamespace(t *testing.T) {
	for _, scenario := range []struct {
		name string
		kind namespace.Type
	}{
		{"mnt", namespace.Mount},
		{"cgroup", namespace.Cgroup},
		{"ipc", namespace.IPC},
		{"net", namespace.Network},
		{"pid", namespace.PID},
		{"time", namespace.Time},
		{"user", namespace.User},
		{"uts", namespace.UTS},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			handle, err := namespace.Current(scenario.kind)
			require.NoError(t, err)
			test.Cleanup(t, handle)
			fromPath := test.OpenNamespace(t, "/proc/thread-self/ns/"+scenario.name)

			require.Equal(t, scenario.kind, test.Value(t, handle.Type))
			equal, err := handle.Equal(fromPath)
			require.NoError(t, err)
			require.True(t, equal, "Current must select the calling thread's namespace")
		})
	}
}

func TestNamespaceIdentity(t *testing.T) {
	first := test.Namespace(t, namespace.Network)
	second := test.Namespace(t, namespace.Network)
	user := test.Namespace(t, namespace.User)

	equal, err := first.Equal(first) //nolint:gocritic // Self-comparison must also pin the same owner safely.
	require.NoError(t, err)
	require.True(t, equal, "a handle equals itself")

	equal, err = first.Equal(second)
	require.NoError(t, err)
	require.True(t, equal, "independent opens identify the same namespace")

	equal, err = first.Equal(user)
	require.NoError(t, err)
	require.False(t, equal, "different namespace types have different identities")

	require.NoError(t, second.Close())
	require.Equal(t, namespace.Network, test.Value(t, first.Type), "each handle owns its reference")
}

func TestUserNamespaceOwnerUID(t *testing.T) {
	user := test.Namespace(t, namespace.User)
	file := test.NamespaceFile(t, "/proc/thread-self/ns/user")

	require.Equal(t, test.NamespaceOwnerUID(t, file), test.Value(t, user.OwnerUID))
}

func TestNetworkNamespaceRelationships(t *testing.T) {
	network := test.Namespace(t, namespace.Network)

	owner, err := network.UserNamespace()
	if errors.Is(err, test.ErrOperationNotPermitted) {
		require.Nil(t, owner, "an inaccessible ancestor must not return a handle")
	} else {
		require.NoError(t, err)
		test.Cleanup(t, owner)
		require.Equal(t, namespace.User, test.Value(t, owner.Type))
	}

	parent, err := network.Parent()
	test.NoResource(t, parent, err, test.ErrInvalidArgument)

	uid, err := network.OwnerUID()
	require.ErrorIs(t, err, test.ErrInvalidArgument)
	require.Zero(t, uid, "only user namespaces have an owner UID")
}

func TestInvalidNamespaceTypes(t *testing.T) {
	for name, kind := range map[string]namespace.Type{
		"zero":           0,
		"unknown bit":    1,
		"high bit":       1 << 31,
		"multiple types": namespace.Network | namespace.UTS,
	} {
		t.Run(name, func(t *testing.T) {
			handle, err := namespace.Current(kind)
			test.NoResource(t, handle, err, os.ErrInvalid)
		})
	}
}

func TestInvalidUnshareFlags(t *testing.T) {
	for name, flags := range map[string]namespace.Type{
		"zero":              0,
		"unknown bit":       1,
		"high bit":          1 << 31,
		"known and unknown": namespace.Network | 1,
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, namespace.Unshare(flags), os.ErrInvalid)
		})
	}
}

func TestOpenInvalidNamespacePath(t *testing.T) {
	for name, path := range map[string]string{
		"empty":     "",
		"missing":   t.TempDir() + "/absent",
		"device":    "/dev/null",
		"directory": "/proc/self",
	} {
		t.Run(name, func(t *testing.T) {
			before := test.DescriptorCount(t)

			handle, err := namespace.Open(path)
			require.Error(t, err)
			require.Nil(t, handle)
			require.Equal(t, before, test.DescriptorCount(t), "failed Open must close its file")
		})
	}
}

func TestAdoptOwnsFile(t *testing.T) {
	file := test.NamespaceFile(t, "/proc/thread-self/ns/net")
	fd := test.FileDescriptor(t, file)
	flags := test.FileFlags(t, file)

	handle, err := namespace.Adopt(file)
	require.NoError(t, err)
	test.Cleanup(t, handle)

	require.Equal(t, flags, test.DescriptorFlags(t, fd), "adoption preserves status flags")
	test.CloseOnExec(t, fd)
	require.NoError(t, handle.Close())
	require.ErrorIs(t, test.DescriptorError(fd), test.ErrBadDescriptor)
}

func TestAdoptClosesInvalidFile(t *testing.T) {
	for name, file := range test.InvalidNamespaceFiles(t) {
		t.Run(name, func(t *testing.T) {
			fd := test.FileDescriptor(t, file)

			handle, err := namespace.Adopt(file)
			test.NoResource(t, handle, err, os.ErrInvalid)
			require.ErrorIs(t, test.DescriptorError(fd), test.ErrBadDescriptor)
		})
	}
}

func TestAdoptNilOrClosedFile(t *testing.T) {
	handle, err := namespace.Adopt(nil)
	test.NoResource(t, handle, err, os.ErrInvalid)

	file := test.NamespaceFile(t, "/proc/thread-self/ns/net")
	require.NoError(t, file.Close())

	handle, err = namespace.Adopt(file)
	test.NoResource(t, handle, err, os.ErrClosed)
}

func TestHandleLifetimeErrors(t *testing.T) {
	for _, scenario := range []struct {
		err    error
		name   string
		closed bool
	}{
		{err: os.ErrInvalid, name: "zero"},
		{err: os.ErrClosed, name: "closed", closed: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var handle *namespace.Handle
			if scenario.closed {
				handle = test.Namespace(t, namespace.Network)
				require.NoError(t, handle.Close())
			} else {
				handle = &namespace.Handle{}
			}

			kind, err := handle.Type()
			require.ErrorIs(t, err, scenario.err)
			require.Zero(t, kind)

			equal, err := handle.Equal(nil)
			require.ErrorIs(t, err, scenario.err)
			require.False(t, equal)

			parent, err := handle.Parent()
			test.NoResource(t, parent, err, scenario.err)
			owner, err := handle.UserNamespace()
			test.NoResource(t, owner, err, scenario.err)

			uid, err := handle.OwnerUID()
			require.ErrorIs(t, err, scenario.err)
			require.Zero(t, uid)

			require.ErrorIs(t, handle.Enter(), scenario.err)
			require.ErrorIs(t, handle.Close(), scenario.err)
		})
	}
}

func TestEqualNilOrClosedHandle(t *testing.T) {
	first := test.Namespace(t, namespace.Network)
	second := test.Namespace(t, namespace.Network)

	equal, err := first.Equal(nil)
	require.ErrorIs(t, err, os.ErrInvalid)
	require.False(t, equal)

	require.NoError(t, second.Close())

	equal, err = first.Equal(second)
	require.ErrorIs(t, err, os.ErrClosed)
	require.False(t, equal)
}

func TestConcurrentEqualAndClose(t *testing.T) {
	first := test.Namespace(t, namespace.Network)
	second := test.Namespace(t, namespace.Network)
	results := make(chan test.ReadResult[bool], 16)
	for range cap(results) {
		go func() {
			for range 20 {
				equal, err := first.Equal(second)
				results <- test.ReadResult[bool]{Value: equal, Err: err}
			}
		}()
	}

	require.NoError(t, first.Close())

	for range cap(results) * 20 {
		result := test.Receive(t, results)
		if errors.Is(result.Err, os.ErrClosed) {
			continue
		}
		require.NoError(t, result.Err)
		require.True(t, result.Value, "an open handle must retain its identity")
	}
	require.Equal(t, namespace.Network, test.Value(t, second.Type))
}

func TestNamespaceOutlivesProcess(t *testing.T) {
	child, input := test.NamespaceChild(t, 0)
	handle := test.OpenNamespace(t, test.NamespacePath(child.Process.Pid, "net"))
	current := test.Namespace(t, namespace.Network)

	require.NoError(t, input.Close())
	require.NoError(t, child.Wait())

	require.Equal(t, namespace.Network, test.Value(t, handle.Type))
	equal, err := current.Equal(handle)
	require.NoError(t, err)
	require.True(t, equal, "namespace identity survives the source process")
}

func TestOpenWithoutAvailableDescriptors(t *testing.T) {
	if os.Args[len(os.Args)-1] != "namespace-exhaustion" {
		ctx, command := test.Command(t, t.Name(), "--", "namespace-exhaustion")
		output, err := command.CombinedOutput()
		require.NoError(t, ctx.Err())
		require.NoError(t, err, "%s", output)
		return
	}
	release := test.ExhaustDescriptors(t)

	handle, err := namespace.Current(namespace.Network)
	test.NoResource(t, handle, err, test.ErrTooManyOpenFiles)

	release()
	handle, err = namespace.Current(namespace.Network)
	require.NoError(t, err, "acquisition works once a descriptor is available")
	require.NoError(t, handle.Close())
	test.DescriptorSlot(t)
}

func TestOpenRejectsFIFOWithoutBlocking(t *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-2] != "namespace-fifo" {
		ctx, command := test.Command(t, t.Name(), "--", "namespace-fifo", test.FIFO(t))
		output, err := command.CombinedOutput()
		require.NoError(t, ctx.Err(), "opening a FIFO must not wait for a writer")
		require.NoError(t, err, "%s", output)
		return
	}
	before := test.DescriptorCount(t)

	handle, err := namespace.Open(os.Args[len(os.Args)-1])
	test.NoResource(t, handle, err, os.ErrInvalid)
	require.Equal(t, before, test.DescriptorCount(t), "failed Open must close its FIFO")
}
