//go:build linux

package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/alexfalkowski/go-linux/examples/run/internal/example"
)

// New acquires an owned temporary directory for generated build artifacts.
func New() (*Workspace, error) {
	directory, err := os.MkdirTemp("", "go-linux-example-*")
	if err != nil {
		return nil, err
	}
	return &Workspace{directory: directory}, nil
}

// Workspace owns every generated executable and the optional demo rootfs.
// User-supplied root filesystems are never owned by this workspace.
type Workspace struct {
	directory string
}

// Close removes the generated files after the example and its children exit.
func (workspace *Workspace) Close() error {
	return os.RemoveAll(workspace.directory)
}

// Command builds the selected application and configures its literal argv.
func (workspace *Workspace) Command(ctx context.Context, selected example.Selection) (*exec.Cmd, error) {
	binary := filepath.Join(workspace.directory, selected.Name)
	if err := workspace.build(ctx, "./examples/"+selected.Name, binary, false); err != nil {
		return nil, err
	}
	arguments := selected.Arguments
	if selected.Name == "container" {
		root, err := workspace.prepareRoot(ctx)
		if err != nil {
			return nil, err
		}
		defaults := []string{"-rootfs", root}
		if delegation := os.Getenv("GO_LINUX_EXAMPLE_CGROUP"); delegation != "" {
			defaults = append(defaults, "-cgroup", delegation)
		}
		arguments = append(defaults, arguments...)
	}
	//nolint:gosec // The allowlisted binary receives literal operator argv and a local delegation path.
	command := exec.CommandContext(ctx, binary, arguments...)
	command.Cancel = nil // The pidfd owner allows the application's cleanup first.
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	return command, nil
}

func (workspace *Workspace) prepareRoot(ctx context.Context) (string, error) {
	root := filepath.Join(workspace.directory, "rootfs")
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		return "", err
	}
	if err := os.Mkdir(filepath.Join(root, "tmp"), 0o755); err != nil {
		return "", err
	}
	if err := workspace.build(ctx, "./examples/container/guest", filepath.Join(root, "bin", "hello"), true); err != nil {
		return "", err
	}
	return root, nil
}

func (workspace *Workspace) build(ctx context.Context, source, destination string, static bool) error {
	command := exec.CommandContext(ctx, "go", "build", "-mod=vendor", "-o", destination, source)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if static {
		command.Env = append(os.Environ(), "CGO_ENABLED=0")
	}
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("build %s: %w", source, err)
	}
	return nil
}
