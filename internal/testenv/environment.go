//go:build linux

package testenv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alexfalkowski/go-linux/cgroup"
)

// PrerequisitePolicy controls unavailable prerequisites for selected coverage.
type PrerequisitePolicy uint8

// SkipUnavailable permits a skip when a known prerequisite is unavailable.
const SkipUnavailable PrerequisitePolicy = 0

// FailUnavailable requires all selected prerequisites.
const FailUnavailable PrerequisitePolicy = 1

// Options selects verification coverage and optional runner identity checks.
type Options struct {
	// Profile selects rootless (the default), namespaces, or cgroup coverage.
	Profile string

	// Delegation is an explicitly supplied cgroup v2 subtree for live tests.
	// Empty means no delegation is selected; discovery never changes the subtree.
	Delegation string

	// Architecture optionally requires both Go and uname to match amd64 or arm64.
	Architecture string

	// Kernel selects supported (also the empty default), 7.0, or newer coverage.
	Kernel string

	// Prerequisites selects how missing prerequisites are handled. Zero permits skips.
	Prerequisites PrerequisitePolicy
}

// Required reports whether unavailable selected prerequisites must fail.
func (options Options) Required() bool {
	return options.Prerequisites == FailUnavailable
}

// Report records runner observations, including a namespace creation probe.
type Report struct {
	// Status contains CapEff, CapBnd, NoNewPrivs, and Seccomp from procfs.
	Status map[string]string `json:"status"`

	// Namespaces maps user, mnt, net, and pid to procfs identifiers or read errors.
	Namespaces map[string]string `json:"namespaces"`

	// Filesystems maps selected mount points to their filesystem type and options.
	Filesystems map[string]string `json:"filesystems"`

	// Controllers records cgroup v2 controller and subtree-control observations.
	Controllers map[string]string `json:"controllers"`

	// Go is the executing Go runtime version.
	Go string `json:"go"`

	// Kernel is the running kernel release read from procfs.
	Kernel string `json:"kernel"`

	// Machine is the architecture reported by uname -m.
	Machine string `json:"machine"`

	// Architecture is the executing binary's runtime.GOARCH.
	Architecture string `json:"architecture"`

	// Profile is the normalized coverage selection used for this check.
	Profile string `json:"profile"`

	// NamespaceProbe is not-run, available, unavailable, or error.
	NamespaceProbe string `json:"namespace_probe"`

	// NamespaceError retains a failed creation/mapping probe's error, if any.
	NamespaceError string `json:"namespace_error,omitempty"`

	// Skip explains an unavailable optional selected profile; otherwise it is empty.
	Skip string `json:"skip,omitempty"`

	// UID is the executing process's real user ID.
	UID int `json:"uid"`
}

// FromEnvironment reads only the documented GO_LINUX_TEST settings. It
// validates them without echoing unrecognized inherited values into diagnostics.
func FromEnvironment() (Options, error) {
	options := Options{
		Profile:      os.Getenv("GO_LINUX_TEST_PROFILE"),
		Architecture: os.Getenv("GO_LINUX_TEST_ARCH"),
		Kernel:       os.Getenv("GO_LINUX_TEST_KERNEL"),
		Delegation:   os.Getenv("GO_LINUX_TEST_CGROUP"),
	}
	if value := os.Getenv("GO_LINUX_TEST_REQUIRED"); value != "" {
		required, err := strconv.ParseBool(value)
		if err != nil {
			return Options{}, errors.New("GO_LINUX_TEST_REQUIRED must be a boolean")
		}
		if required {
			options.Prerequisites = FailUnavailable
		}
	}

	return normalize(options)
}

// Check observes the host and probes user/mount/network namespace creation in a
// bounded subprocess, or inspects an explicitly selected cgroup delegation. It
// never changes the calling process's namespaces, cgroup, or host policy. An
// optional known prerequisite denial is recorded in Skip; unexpected errors and
// runner identity mismatches always fail. Cgroup discovery establishes readable
// identity/controller files only; live tests must verify mutation permissions.
func Check(options Options) (Report, error) {
	options, err := normalize(options)
	if err != nil {
		return Report{}, err
	}
	report, err := inspect()
	if err != nil {
		return report, err
	}
	report.Profile = options.Profile
	if err := checkRunner(report, options); err != nil {
		return report, err
	}
	if options.Profile == "cgroup" {
		return checkDelegation(report, options)
	}

	err = probeNamespaces()
	if err != nil {
		report.NamespaceProbe = "error"
		report.NamespaceError = err.Error()
		// These errors describe the exact clone/mapping prerequisite attempted here,
		// not arbitrary failures from a facility operation under test.
		if !namespaceUnavailable(err) {
			return report, fmt.Errorf("namespace prerequisite probe: %w", err)
		}
		report.NamespaceProbe = "unavailable"
		if options.Profile == "rootless" {
			return report, nil
		}
		reason := fmt.Sprintf("profile=namespaces operation=clone(user,mount,network)+uid/gid-mapping: %v; "+
			"prerequisite=permitted user namespaces, mappings, and namespace quota", err)
		if options.Required() {
			return report, fmt.Errorf("required %s: %w", reason, err)
		}
		report.Skip = reason
	} else {
		report.NamespaceProbe = "available"
	}

	return report, nil
}

func normalize(options Options) (Options, error) {
	if options.Profile == "" {
		options.Profile = "rootless"
	}
	if options.Kernel == "" {
		options.Kernel = "supported"
	}
	switch options.Profile {
	case "rootless", "namespaces", "cgroup":
	default:
		return Options{}, errors.New("profile must be rootless, namespaces, or cgroup")
	}
	switch options.Architecture {
	case "", "amd64", "arm64":
	default:
		return Options{}, errors.New("architecture must be amd64 or arm64")
	}
	switch options.Kernel {
	case "supported", "7.0", "newer":
	default:
		return Options{}, errors.New("kernel must be supported, 7.0, or newer")
	}
	if options.Prerequisites > FailUnavailable {
		return Options{}, errors.New("invalid prerequisite policy")
	}

	return options, checkDelegationPath(options.Delegation)
}

func checkDelegationPath(path string) error {
	if path != "" && !filepath.IsAbs(path) {
		return errors.New("delegation must be an absolute cgroup v2 path")
	}
	return nil
}

func checkDelegation(report Report, options Options) (Report, error) {
	if options.Delegation == "" {
		const reason = "profile=cgroup requires delegation=/absolute/delegated/path"
		if options.Required() {
			return report, errors.New(reason)
		}
		report.Skip = reason
		return report, nil
	}
	group, err := cgroup.Open(options.Delegation)
	if err != nil {
		return report, fmt.Errorf("open explicit cgroup delegation: %w", err)
	}
	defer group.Close()
	for _, name := range []string{"cgroup.controllers", "cgroup.subtree_control"} {
		var controllers []cgroup.Controller
		if name == "cgroup.controllers" {
			controllers, err = group.Controllers()
		} else {
			controllers, err = group.SubtreeControl()
		}
		if err != nil {
			return report, err
		}
		report.Controllers["delegation/"+name] = fmt.Sprint(controllers)
	}
	return report, nil
}

func checkRunner(report Report, options Options) error {
	if err := checkKernel(report.Kernel, options.Kernel); err != nil {
		return err
	}
	if options.Architecture == "" {
		return nil
	}
	machines := map[string]string{"arm64": "aarch64", "amd64": "x86_64"}
	if report.Architecture != options.Architecture || report.Machine != machines[options.Architecture] {
		return errors.New("execution architecture does not match the requested runner")
	}

	return nil
}

func checkKernel(release, selection string) error {
	var major, minor uint
	if _, err := fmt.Sscanf(release, "%d.%d", &major, &minor); err != nil {
		return errors.New("cannot determine kernel major/minor")
	}
	var accepted bool
	switch selection {
	case "7.0":
		accepted = major == 7 && minor == 0
	case "newer":
		accepted = major > 7 || (major == 7 && minor > 0)
	default:
		accepted = major >= 7
	}
	if !accepted {
		return fmt.Errorf("kernel %s does not satisfy requested %s coverage (minimum Linux 7.0)", release, selection)
	}

	return nil
}

func inspect() (Report, error) {
	report := Report{
		Go: runtime.Version(), Architecture: runtime.GOARCH, UID: os.Getuid(),
		NamespaceProbe: "not-run",
		Status:         make(map[string]string), Namespaces: make(map[string]string),
		Filesystems: make(map[string]string), Controllers: make(map[string]string),
	}
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return report, err
	}
	report.Kernel = strings.TrimSpace(string(data))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err = exec.CommandContext(ctx, "uname", "-m").Output()
	if err != nil {
		return report, fmt.Errorf("uname: %w", err)
	}
	report.Machine = strings.TrimSpace(string(data))
	data, err = os.ReadFile("/proc/self/status")
	if err != nil {
		return report, err
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		key, value, _ := strings.Cut(line, ":")
		switch key {
		case "CapEff", "CapBnd", "NoNewPrivs", "Seccomp":
			report.Status[key] = strings.TrimSpace(value)
		}
	}
	for _, name := range []string{"user", "mnt", "net", "pid"} {
		target, err := os.Readlink("/proc/self/ns/" + name)
		if err != nil {
			target = err.Error()
		}
		report.Namespaces[name] = target
	}
	report.Filesystems, err = inspectMounts()
	if err != nil {
		return report, err
	}
	report.Controllers = inspectControllers()

	return report, nil
}

func inspectMounts() (map[string]string, error) {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}
	mounts := make(map[string]string)
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		switch fields[4] {
		case "/", "/proc", "/sys", "/sys/fs/cgroup":
			_, filesystem, _ := strings.Cut(line, " - ")
			kind, _, _ := strings.Cut(filesystem, " ")
			mounts[fields[4]] = kind + " " + fields[5]
		}
	}

	return mounts, nil
}

func inspectControllers() map[string]string {
	controllers := make(map[string]string)
	for _, name := range []string{"cgroup.controllers", "cgroup.subtree_control"} {
		data, err := os.ReadFile("/sys/fs/cgroup/" + name)
		if err != nil {
			controllers[name] = err.Error()
			continue
		}
		controllers[name] = strings.TrimSpace(string(data))
	}

	return controllers
}

func namespaceUnavailable(err error) bool {
	return errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) ||
		errors.Is(err, syscall.ENOSYS) || errors.Is(err, syscall.ENOSPC)
}

func probeNamespaces() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "true")
	command.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUSER | syscall.CLONE_NEWNS | syscall.CLONE_NEWNET,
		UidMappings: []syscall.SysProcIDMap{{
			ContainerID: 0, HostID: os.Getuid(), Size: 1,
		}},
		GidMappings: []syscall.SysProcIDMap{{
			ContainerID: 0, HostID: os.Getgid(), Size: 1,
		}},
	}

	return command.Run()
}
