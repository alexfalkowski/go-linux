package testenv_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/testenv"
)

func TestConfiguredProfile(t *testing.T) {
	options, err := testenv.FromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	report, err := testenv.Check(options)
	encoded, encodeErr := json.Marshal(report)
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	t.Log(string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if report.Skip != "" {
		t.Skip(report.Skip)
	}
}

func TestRootlessDiscovery(t *testing.T) {
	report, err := testenv.Check(testenv.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Go != runtime.Version() || report.Architecture != runtime.GOARCH || report.UID != os.Getuid() {
		t.Fatal("discovery did not identify the executing Go process")
	}
	for _, observation := range []string{report.Kernel, report.Machine, report.Status["CapEff"], report.Status["CapBnd"]} {
		if observation == "" {
			t.Fatal("missing kernel, architecture, or capability observations")
		}
	}
	if report.Profile != "rootless" || report.Skip != "" {
		t.Fatal("rootless discovery must not skip because an optional namespace probe is unavailable")
	}
}

func TestDiscoveryLeavesNamespacesUnchanged(t *testing.T) {
	before, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil {
		t.Fatal(err)
	}
	report, err := testenv.Check(testenv.Options{})
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil {
		t.Fatal(err)
	}
	if before != after || report.Namespaces["mnt"] != before {
		t.Fatal("namespace discovery changed the caller's mount namespace")
	}
}

func TestNamespacePrerequisitePolicy(t *testing.T) {
	optional, err := testenv.Check(testenv.Options{Profile: "namespaces"})
	if err != nil {
		t.Fatal(err)
	}
	required, requiredErr := testenv.Check(testenv.Options{Profile: "namespaces", Required: true})
	if optional.NamespaceError == "" {
		if requiredErr != nil || required.Skip != "" || optional.Skip != "" {
			t.Fatal("a usable namespace prerequisite must run in both optional and required profiles")
		}
		return
	}
	if optional.Skip == "" || requiredErr == nil || required.Skip != "" {
		t.Fatal("an unavailable prerequisite must skip only when optional and fail when required")
	}
}

func TestRunnerMismatchFailsEvenWhenOptional(t *testing.T) {
	architecture := "arm64"
	if runtime.GOARCH == architecture {
		architecture = "amd64"
	}
	if _, err := testenv.Check(testenv.Options{Architecture: architecture}); err == nil {
		t.Fatal("a mismatched execution architecture must fail, not skip")
	}
}

func TestKernelSelection(t *testing.T) {
	report, err := testenv.Check(testenv.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, selection := range []string{"7.0", "newer"} {
		t.Run(selection, func(t *testing.T) {
			result, err := testenv.Check(testenv.Options{Kernel: selection})
			baseline := strings.HasPrefix(report.Kernel, "7.0.")
			matches := baseline == (selection == "7.0")
			if (err == nil) != matches || result.Skip != "" {
				t.Fatal("kernel selection must accept only the matching runtime cell and must never skip")
			}
		})
	}
}

func TestInvalidSettings(t *testing.T) {
	for _, name := range []string{"GO_LINUX_TEST_PROFILE", "GO_LINUX_TEST_REQUIRED", "GO_LINUX_TEST_ARCH", "GO_LINUX_TEST_KERNEL"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "invalid-setting")
			if _, err := testenv.FromEnvironment(); err == nil {
				t.Fatal("invalid verification settings must fail before any probe runs")
			}
		})
	}
}

func TestUnexpectedProbeFailureDoesNotSkip(t *testing.T) {
	uname, err := exec.LookPath("uname")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := os.Symlink(uname, filepath.Join(directory, "uname")); err != nil {
		t.Fatal(err)
	}
	// Discovery can run uname, but the namespace probe's executable is absent.
	t.Setenv("PATH", directory)
	report, err := testenv.Check(testenv.Options{Profile: "namespaces"})
	if !errors.Is(err, exec.ErrNotFound) || report.Skip != "" || report.NamespaceProbe != "error" {
		t.Fatal("a missing probe executable must fail, not become a prerequisite skip")
	}
}

func TestDefaultSettings(t *testing.T) {
	for _, name := range []string{"GO_LINUX_TEST_PROFILE", "GO_LINUX_TEST_REQUIRED", "GO_LINUX_TEST_ARCH", "GO_LINUX_TEST_KERNEL"} {
		t.Setenv(name, "")
	}
	options, err := testenv.FromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	want := testenv.Options{Profile: "rootless", Kernel: "supported"}
	if options != want {
		t.Fatalf("default settings: got %+v, want %+v", options, want)
	}
}
