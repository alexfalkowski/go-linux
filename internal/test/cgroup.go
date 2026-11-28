//go:build linux

package test

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alexfalkowski/go-linux/cgroup"
	"github.com/alexfalkowski/go-linux/internal/testenv"
	"github.com/alexfalkowski/go-linux/mmap"
	"github.com/stretchr/testify/require"
)

// CurrentCgroup opens the current process's v2 group without changing it.
// The conventional cgroup mount must expose the process's membership path.
func CurrentCgroup(t *testing.T) *cgroup.Group {
	t.Helper()
	data, err := os.ReadFile("/proc/self/cgroup")
	require.NoError(t, err)
	for line := range strings.SplitSeq(string(data), "\n") {
		if path, found := strings.CutPrefix(line, "0::"); found {
			group, err := cgroup.Open(filepath.Join("/sys/fs/cgroup", path))
			require.NoError(t, err, "current cgroup must be visible in /sys/fs/cgroup")
			Cleanup(t, group)
			return group
		}
	}
	t.Skip("read-only cgroup tests require a visible v2 membership and /sys/fs/cgroup mount")
	return nil
}

// CgroupWithEvents selects a test-owned group in the cgroup profile, or the
// current group otherwise. Only unselected event coverage may skip when the
// current group is the real hierarchy root, which has no cgroup.events file.
func CgroupWithEvents(t *testing.T) *cgroup.Group {
	t.Helper()
	options, err := testenv.FromEnvironment()
	require.NoError(t, err)
	if options.Profile == "cgroup" {
		return DelegatedCgroup(t)
	}

	group := CurrentCgroup(t)
	_, err = group.Events()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skip("current cgroup has no cgroup.events file; event coverage requires a non-root group")
		}
		require.NoError(t, err)
	}
	return group
}

// DelegatedCgroup creates one test-owned child of the explicitly selected
// delegation. It never changes the supplied parent's controllers or membership.
// Missing selected prerequisites skip only in optional mode; facility failures
// after selecting a valid directory always fail.
func DelegatedCgroup(t *testing.T) *cgroup.Group {
	t.Helper()
	options, err := testenv.FromEnvironment()
	require.NoError(t, err)
	if options.Profile != "cgroup" {
		t.Skip("live cgroup mutations require profile=cgroup delegation=/absolute/delegated/path")
	}
	if options.Delegation == "" {
		require.False(t, options.Required(), "profile=cgroup requires delegation=/absolute/delegated/path")
		t.Skip("profile=cgroup has no explicitly delegated subtree")
	}
	parent, err := cgroup.Open(options.Delegation)
	require.NoError(t, err)
	Cleanup(t, parent)
	controllers, err := parent.SubtreeControl()
	require.NoError(t, err)
	for _, controller := range []cgroup.Controller{cgroup.CPU, cgroup.Memory, cgroup.PIDs} {
		require.Contains(t, controllers, controller, "delegator must enable the selected controller for children")
	}
	name := "go-linux-" + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	group, err := parent.New(name)
	require.NoError(t, err, "create test-owned child inside explicit delegation")
	t.Cleanup(func() {
		require.NoError(t, parent.Remove(name), "test-owned subtree must be empty after cleanup")
	})
	Cleanup(t, group)
	return group
}

// CgroupChild creates a child and registers handle/directory cleanup in order.
func CgroupChild(t *testing.T, parent *cgroup.Group, name string) *cgroup.Group {
	t.Helper()
	child, err := parent.New(name)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := parent.Remove(name); !errors.Is(err, os.ErrNotExist) {
			require.NoError(t, err)
		}
	})
	Cleanup(t, child)
	return child
}

// CgroupWithController creates an empty leaf with one explicitly enabled
// controller. Its test-owned parent and both handles are cleaned up in order.
func CgroupWithController(t *testing.T, controller cgroup.Controller) *cgroup.Group {
	t.Helper()
	parent := DelegatedCgroup(t)
	require.NoError(t, parent.SetControllers([]cgroup.Controller{controller}, nil))
	return CgroupChild(t, parent, "leaf")
}

// CgroupWatcher creates a watcher with cleanup and bounded notification waits.
func CgroupWatcher(t *testing.T, group *cgroup.Group) *cgroup.EventWatcher {
	t.Helper()
	watcher, err := group.WatchEvents()
	require.NoError(t, err)
	Cleanup(t, watcher)
	require.NoError(t, watcher.SetReadDeadline(time.Now().Add(5*time.Second)))
	return watcher
}

// StartCgroupWorkload waits for a bounded command's readiness byte before
// returning its input. The caller migrates the process, then writes a newline
// to release its workload. Closing input releases any final memory hold.
func StartCgroupWorkload(t *testing.T, command *exec.Cmd) io.WriteCloser {
	t.Helper()
	input, err := command.StdinPipe()
	require.NoError(t, err)
	Cleanup(t, input)
	output := CaptureCommand(t, command)
	var ready [1]byte
	_, err = io.ReadFull(output, ready[:])
	require.NoError(t, err, "workload must announce readiness before migration")
	require.Equal(t, byte(1), ready[0], "workload must use the readiness protocol")
	return input
}

// CgroupWorkloadChild serves bounded CPU and memory workers in the current test
// binary. Initialization precedes readiness so new memory charges follow the
// parent's explicit migration. Memory pressure is capped at a 64-MiB mapping.
func CgroupWorkloadChild(t *testing.T) {
	t.Helper()
	mode := os.Args[len(os.Args)-1]
	if mode != "cgroup-cpu" && mode != "cgroup-memory" {
		return
	}
	time.AfterFunc(15*time.Second, func() {
		os.Exit(125)
	})

	var region *mmap.Region
	if mode == "cgroup-memory" {
		region = Anonymous(t, 64<<20, mmap.Options{Protection: mmap.Read | mmap.Write})
	}

	_, err := os.Stdout.Write([]byte{1})
	require.NoError(t, err)
	var start [1]byte
	_, err = io.ReadFull(os.Stdin, start[:])
	require.NoError(t, err)

	if mode == "cgroup-cpu" {
		until := time.Now().Add(2 * time.Second)
		for time.Now().Before(until) {
		}
		return
	}

	data := region.Bytes()
	for offset := 0; offset < len(data); offset += os.Getpagesize() {
		data[offset] = 1
	}
	_, err = io.Copy(io.Discard, os.Stdin)
	require.NoError(t, err)
}
