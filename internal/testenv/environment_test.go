//go:build linux

package testenv_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestSelectedProfileMeetsRunnerRequirements(t *testing.T) {
	options, err := testenv.FromEnvironment()
	require.NoError(t, err)

	report, err := testenv.Check(options)
	encoded, encodeErr := json.Marshal(report)
	require.NoError(t, encodeErr)

	t.Log(string(encoded))
	require.NoError(t, err)

	if report.Skip != "" {
		t.Skip(report.Skip)
	}
}

func TestDiscoveryIdentifiesTheRunningProcess(t *testing.T) {
	report, err := testenv.Check(testenv.Options{})
	require.NoError(t, err)

	for _, scenario := range []struct{ name, got, want string }{
		{name: "Go version", got: report.Go, want: runtime.Version()},
		{name: "architecture", got: report.Architecture, want: runtime.GOARCH},
		{name: "user ID", got: strconv.Itoa(report.UID), want: strconv.Itoa(os.Getuid())},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			require.Equal(t, scenario.want, scenario.got)
		})
	}
}

func TestDiscoveryReportsKernelAndCapabilityPrerequisites(t *testing.T) {
	report, err := testenv.Check(testenv.Options{})
	require.NoError(t, err)

	for _, scenario := range []struct{ name, value string }{
		{name: "kernel release", value: report.Kernel},
		{name: "machine architecture", value: report.Machine},
		{name: "effective capabilities", value: report.Status["CapEff"]},
		{name: "bounding capabilities", value: report.Status["CapBnd"]},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			require.NotEmpty(t, scenario.value)
		})
	}
}

func TestRootlessDiscoveryDoesNotRequireNamespaces(t *testing.T) {
	report, err := testenv.Check(testenv.Options{})
	require.NoError(t, err)
	require.Equal(t, "rootless", report.Profile)
	require.Empty(t, report.Skip, "an unavailable optional namespace probe must not skip ordinary-user coverage")
}

func TestDiscoveryLeavesTheCallingMountNamespaceUnchanged(t *testing.T) {
	before, err := os.Readlink("/proc/self/ns/mnt")
	require.NoError(t, err)

	report, err := testenv.Check(testenv.Options{})
	require.NoError(t, err)

	after, err := os.Readlink("/proc/self/ns/mnt")
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Equal(t, before, report.Namespaces["mnt"])
}

func TestUnavailableNamespacesSkipOnlyWhenOptional(t *testing.T) {
	optional, err := testenv.Check(testenv.Options{Profile: "namespaces"})
	require.NoError(t, err)

	required, err := testenv.Check(testenv.Options{Profile: "namespaces", Required: true})
	require.Empty(t, required.Skip, "required coverage must never report a skip")

	if optional.NamespaceError == "" {
		require.NoError(t, err, "available namespaces must run in both modes")
		require.Empty(t, optional.Skip)
		return
	}

	require.NotEmpty(t, optional.Skip, "optional coverage must explain the missing prerequisite")
	require.Error(t, err, "required coverage must fail the same missing prerequisite")
}

func TestRunnerArchitectureMismatchFailsEvenWhenOptional(t *testing.T) {
	architecture := "arm64"
	if runtime.GOARCH == architecture {
		architecture = "amd64"
	}
	_, err := testenv.Check(testenv.Options{Architecture: architecture})
	require.Error(t, err)
}

func TestKernelSelectionAcceptsOnlyTheMatchingRunner(t *testing.T) {
	report, err := testenv.Check(testenv.Options{})
	require.NoError(t, err)

	baseline := strings.HasPrefix(report.Kernel, "7.0.")
	for _, scenario := range []struct {
		name      string
		selection string
		accepted  bool
	}{
		{name: "Linux 7.0 baseline", selection: "7.0", accepted: baseline},
		{name: "newer Linux kernel", selection: "newer", accepted: !baseline},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			result, err := testenv.Check(testenv.Options{Kernel: scenario.selection})
			require.Equal(t, scenario.accepted, err == nil, "runner kernel: %s; result: %v", report.Kernel, err)
			require.Empty(t, result.Skip, "a kernel mismatch must fail rather than skip")
		})
	}
}

func TestInvalidVerificationSettingsFailBeforeProbing(t *testing.T) {
	for _, scenario := range []struct{ name, setting string }{
		{name: "profile", setting: "GO_LINUX_TEST_PROFILE"},
		{name: "required prerequisites", setting: "GO_LINUX_TEST_REQUIRED"},
		{name: "architecture", setting: "GO_LINUX_TEST_ARCH"},
		{name: "kernel", setting: "GO_LINUX_TEST_KERNEL"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Setenv(scenario.setting, "invalid-setting")
			_, err := testenv.FromEnvironment()
			require.Error(t, err)
		})
	}
}

func TestMissingProbeExecutableFailsInsteadOfSkipping(t *testing.T) {
	uname, err := exec.LookPath("uname")
	require.NoError(t, err)

	directory := t.TempDir()
	require.NoError(t, os.Symlink(uname, filepath.Join(directory, "uname")))

	// uname remains available; the namespace probe's true executable does not.
	t.Setenv("PATH", directory)
	report, err := testenv.Check(testenv.Options{Profile: "namespaces"})
	require.ErrorIs(t, err, exec.ErrNotFound)
	require.Empty(t, report.Skip)
	require.Equal(t, "error", report.NamespaceProbe)
}

func TestUnsetVerificationSettingsSelectOptionalRootlessCoverage(t *testing.T) {
	for _, name := range []string{"GO_LINUX_TEST_PROFILE", "GO_LINUX_TEST_REQUIRED", "GO_LINUX_TEST_ARCH", "GO_LINUX_TEST_KERNEL"} {
		t.Setenv(name, "")
	}
	options, err := testenv.FromEnvironment()
	require.NoError(t, err)
	require.Equal(t, testenv.Options{Profile: "rootless", Kernel: "supported"}, options)
}
