//go:build linux

package namespace_test

import (
	"io"
	"os"
	"runtime"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/namespace"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	// Go parks its startup thread rather than destroying it. Reserve that
	// thread so the teardown tests observe ordinary worker threads.
	runtime.LockOSThread()
	os.Exit(m.Run())
}

func TestUnshareChangesCallingThread(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	for name, kind := range map[string]namespace.Type{
		"mnt":    namespace.Mount,
		"cgroup": namespace.Cgroup,
		"ipc":    namespace.IPC,
		"net":    namespace.Network,
		"uts":    namespace.UTS,
	} {
		t.Run(name, func(t *testing.T) {
			original := test.Namespace(t, kind)

			thread := test.LockedThread(t, func() {
				require.NoError(t, namespace.Unshare(kind))
				child, _ := test.NamespaceChild(t, 0)
				inherited := test.OpenNamespace(t, test.NamespacePath(child.Process.Pid, name))

				current := test.Namespace(t, kind)
				equal, err := original.Equal(current)
				require.NoError(t, err)
				require.False(t, equal, "Unshare creates a distinct namespace")

				equal, err = current.Equal(inherited)
				require.NoError(t, err)
				require.True(t, equal, "child processes inherit the calling thread's namespace")
			})
			test.WaitForThreadExit(t, thread)

			test.LockedThread(t, func() {
				current := test.Namespace(t, kind)
				equal, err := original.Equal(current)
				require.NoError(t, err)
				require.True(t, equal, "unrelated goroutines keep their original namespace")
			})
		})
	}
}

func TestUnshareChangesFutureChildren(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	for name, kind := range map[string]namespace.Type{"pid": namespace.PID, "time": namespace.Time} {
		t.Run(name, func(t *testing.T) {
			original := test.Namespace(t, kind)

			thread := test.LockedThread(t, func() {
				require.NoError(t, namespace.Unshare(kind))
				child, _ := test.NamespaceChild(t, 0)
				inherited := test.OpenNamespace(t, test.NamespacePath(child.Process.Pid, name))
				future := test.OpenNamespace(t, "/proc/thread-self/ns/"+name+"_for_children")

				current := test.Namespace(t, kind)
				equal, err := original.Equal(current)
				require.NoError(t, err)
				require.True(t, equal, "PID/time creation leaves active membership unchanged")

				equal, err = future.Equal(inherited)
				require.NoError(t, err)
				require.True(t, equal, "the child enters the new namespace")

				equal, err = original.Equal(future)
				require.NoError(t, err)
				require.False(t, equal, "future children receive a distinct namespace")
			})
			test.WaitForThreadExit(t, thread)

			test.LockedThread(t, func() {
				future := test.OpenNamespace(t, "/proc/thread-self/ns/"+name+"_for_children")
				equal, err := original.Equal(future)
				require.NoError(t, err)
				require.True(t, equal, "unrelated threads keep their child namespace")
			})
		})
	}
}

func TestFirstChildAfterPIDUnshare(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}

	// No process lookup/start has initialized Go's lazy pidfd support in this
	// fresh subprocess. Unshare must leave PID 1 for the intended first child.
	thread := test.LockedThread(t, func() {
		before := test.Descriptors(t)

		require.NoError(t, namespace.Unshare(namespace.PID))
		require.Equal(t, before, test.Descriptors(t), "process preparation must release its temporary reference")

		ctx, child := test.Command(t, "TestNamespaceWaitingChild", "--", "namespace-init")
		output, err := child.CombinedOutput()
		require.NoError(t, ctx.Err(), "the first child must exit within its time bound")
		require.NoError(t, err, "the intended first child must run as PID 1: %s", output)
	})
	test.WaitForThreadExit(t, thread)
}

func TestEnterChangesCallingThread(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	for name, kind := range map[string]namespace.Type{
		"cgroup": namespace.Cgroup,
		"ipc":    namespace.IPC,
		"net":    namespace.Network,
		"uts":    namespace.UTS,
	} {
		t.Run(name, func(t *testing.T) {
			original := test.Namespace(t, kind)
			child, _ := test.NamespaceChild(t, kind)
			target := test.OpenNamespace(t, test.NamespacePath(child.Process.Pid, name))

			test.LockedThread(t, func() {
				require.NoError(t, target.Enter())

				current := test.Namespace(t, kind)
				equal, err := current.Equal(target)
				require.NoError(t, err)
				require.True(t, equal, "Enter selects the target namespace")
			})

			current := test.Namespace(t, kind)
			equal, err := original.Equal(current)
			require.NoError(t, err)
			require.True(t, equal, "entry leaves unrelated threads unchanged")
		})
	}
}

func TestEnterMountRequiresPrivateFilesystemState(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	original := test.Namespace(t, namespace.Mount)
	child, _ := test.NamespaceChild(t, namespace.Mount)
	target := test.OpenNamespace(t, test.NamespacePath(child.Process.Pid, "mnt"))

	test.LockedThread(t, func() {
		require.ErrorIs(t, target.Enter(), test.ErrInvalidArgument)
		require.NoError(t, namespace.Unshare(namespace.Mount))
		require.NoError(t, target.Enter())

		current := test.Namespace(t, namespace.Mount)
		equal, err := target.Equal(current)
		require.NoError(t, err)
		require.True(t, equal, "entry succeeds after filesystem state is unshared")
	})

	current := test.Namespace(t, namespace.Mount)
	equal, err := original.Equal(current)
	require.NoError(t, err)
	require.True(t, equal, "unrelated threads keep their mount namespace")
}

func TestEnterPIDChangesFutureChildren(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	original := test.Namespace(t, namespace.PID)
	child, _ := test.NamespaceChild(t, namespace.PID)
	target := test.OpenNamespace(t, test.NamespacePath(child.Process.Pid, "pid"))

	test.LockedThread(t, func() {
		require.NoError(t, target.Enter())

		future := test.OpenNamespace(t, "/proc/thread-self/ns/pid_for_children")
		equal, err := target.Equal(future)
		require.NoError(t, err)
		require.True(t, equal, "entry changes the namespace for future children")

		current := test.Namespace(t, namespace.PID)
		equal, err = original.Equal(current)
		require.NoError(t, err)
		require.True(t, equal, "the calling thread stays in its active PID namespace")
	})
}

func TestEnterTimeRequiresSingleThread(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	original := test.Namespace(t, namespace.Time)
	child, _ := test.NamespaceChild(t, namespace.Time)
	target := test.OpenNamespace(t, test.NamespacePath(child.Process.Pid, "time"))

	test.LockedThread(t, func() {
		require.ErrorIs(t, target.Enter(), test.ErrTooManyUsers)

		current := test.Namespace(t, namespace.Time)
		equal, err := original.Equal(current)
		require.NoError(t, err)
		require.True(t, equal, "refused time entry leaves membership unchanged")
	})
}

func TestNamespacePanicCleanup(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	original := test.Namespace(t, namespace.Network)
	uts := test.Namespace(t, namespace.UTS)
	before := test.Descriptors(t)

	thread := test.LockedThread(t, func() {
		require.PanicsWithValue(t, "after namespace change", func() {
			require.NoError(t, namespace.Unshare(namespace.Network|namespace.UTS))

			network, err := namespace.Current(namespace.Network)
			require.NoError(t, err)
			defer network.Close()
			hostname, err := namespace.Current(namespace.UTS)
			require.NoError(t, err)
			defer hostname.Close()

			equal, err := original.Equal(network)
			require.NoError(t, err)
			require.False(t, equal, "network membership must change before the panic")
			equal, err = uts.Equal(hostname)
			require.NoError(t, err)
			require.False(t, equal, "combined flags must also change UTS membership")
			panic("after namespace change")
		})
	})

	test.WaitForThreadExit(t, thread)
	require.Equal(t, before, test.Descriptors(t), "panic runs the deferred handle cleanup")

	current := test.Namespace(t, namespace.Network)
	equal, err := original.Equal(current)
	require.NoError(t, err)
	require.True(t, equal, "panic leaves unrelated threads unchanged")
}

func TestUserNamespaceHierarchy(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	user := test.Namespace(t, namespace.User)
	process, _ := test.NamespaceChild(t, namespace.User)
	child := test.OpenNamespace(t, test.NamespacePath(process.Process.Pid, "user"))

	parent := test.Value(t, child.Parent)
	test.Cleanup(t, parent)
	owner := test.Value(t, child.UserNamespace)
	test.Cleanup(t, owner)
	for _, handle := range []*namespace.Handle{parent, owner} {
		equal, err := user.Equal(handle)
		require.NoError(t, err)
		require.True(t, equal, "a user namespace's parent and owner identify its creator's namespace")
	}
	require.Equal(t, uint32(os.Getuid()), test.Value(t, child.OwnerUID)) //nolint:gosec // Linux UIDs fit uint32.
}

func TestUserNamespaceTransitionsRequireSingleThread(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	original := test.Namespace(t, namespace.User)
	child, _ := test.NamespaceChild(t, namespace.User)
	target := test.OpenNamespace(t, test.NamespacePath(child.Process.Pid, "user"))

	test.LockedThread(t, func() {
		require.ErrorIs(t, target.Enter(), test.ErrInvalidArgument)
		require.ErrorIs(t, namespace.Unshare(namespace.User), test.ErrInvalidArgument)

		current := test.Namespace(t, namespace.User)
		equal, err := original.Equal(current)
		require.NoError(t, err)
		require.True(t, equal, "refused user transitions leave membership unchanged")
	})
}

func TestPIDNamespaceHierarchySurvivesChildExit(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	original := test.Namespace(t, namespace.PID)
	user := test.Namespace(t, namespace.User)
	child, input := test.NamespaceChild(t, namespace.PID)
	target := test.OpenNamespace(t, test.NamespacePath(child.Process.Pid, "pid"))

	parent := test.Value(t, target.Parent)
	test.Cleanup(t, parent)
	equal, err := original.Equal(parent)
	require.NoError(t, err)
	require.True(t, equal, "PID hierarchy follows child creation")

	owner := test.Value(t, target.UserNamespace)
	test.Cleanup(t, owner)
	equal, err = user.Equal(owner)
	require.NoError(t, err)
	require.True(t, equal, "the PID namespace belongs to its creator's user namespace")

	require.NoError(t, input.Close())
	require.NoError(t, child.Wait())
	require.Equal(t, namespace.PID, test.Value(t, target.Type), "the handle pins identity after init exits")
}

func TestEnterRequiresTargetPrivilege(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	// This network namespace belongs to an ancestor user namespace, where
	// the fixture's private user namespace grants no CAP_SYS_ADMIN.
	original := test.Namespace(t, namespace.Network)

	test.LockedThread(t, func() {
		require.ErrorIs(t, original.Enter(), test.ErrOperationNotPermitted)

		current := test.Namespace(t, namespace.Network)
		equal, err := original.Equal(current)
		require.NoError(t, err)
		require.True(t, equal, "refused entry preserves membership and handle ownership")
	})

	owner, err := original.UserNamespace()
	test.NoResource(t, owner, err, test.ErrOperationNotPermitted)
}

func TestRelatedNamespacesWithoutAvailableDescriptors(t *testing.T) {
	if !test.NamespaceProcess(t) {
		return
	}
	child, _ := test.NamespaceChild(t, namespace.PID)
	target := test.OpenNamespace(t, test.NamespacePath(child.Process.Pid, "pid"))
	release := test.ExhaustDescriptors(t)

	parent, err := target.Parent()
	test.NoResource(t, parent, err, test.ErrTooManyOpenFiles)
	owner, err := target.UserNamespace()
	test.NoResource(t, owner, err, test.ErrTooManyOpenFiles)

	release()
	parent, err = target.Parent()
	require.NoError(t, err, "the original handle survives acquisition failure")
	require.NoError(t, parent.Close())
	test.DescriptorSlot(t)
}

func TestNamespaceWaitingChild(t *testing.T) {
	switch os.Args[len(os.Args)-1] {
	case "namespace-init":
		require.Equal(t, 1, os.Getpid(), "the intended first child must become the namespace init")
	case "namespace-wait":
	default:
		return
	}
	var data [1]byte
	_, err := io.ReadFull(os.Stdin, data[:])
	require.ErrorIs(t, err, io.EOF)
}
