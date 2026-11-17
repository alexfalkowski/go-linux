//go:build linux

package runner

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/alexfalkowski/go-linux/cgroup"
	"github.com/alexfalkowski/go-linux/examples/container/internal/config"
)

func newDelegation(cfg config.Config) (*delegation, error) {
	if cfg.Delegation == "" {
		return nil, nil
	}
	parent, err := cgroup.Open(cfg.Delegation)
	if err != nil {
		return nil, err
	}
	name := "example-" + strconv.Itoa(os.Getpid())
	group, err := parent.New(name)
	if err != nil {
		return nil, errors.Join(err, parent.Close())
	}
	delegation := &delegation{parent: parent, group: group, name: name}
	if err := group.SetMemoryMax(cgroup.Limit(cfg.Memory)); err != nil {
		return nil, errors.Join(err, delegation.Close())
	}
	if err := group.SetPIDsMax(cgroup.Limit(cfg.Tasks)); err != nil {
		return nil, errors.Join(err, delegation.Close())
	}
	fmt.Fprintf(os.Stderr, "configured cgroup memory.max=%d pids.max=%d\n", cfg.Memory, cfg.Tasks)
	return delegation, nil
}

// delegation owns one workload group and the parent handle needed to remove it.
type delegation struct {
	parent *cgroup.Group
	group  *cgroup.Group
	name   string
}

// Close releases the workload handle, removes its empty group, and closes the
// parent reference. The runner must finish reaping its child before calling it.
func (delegation *delegation) Close() error {
	return errors.Join(delegation.group.Close(), delegation.parent.Remove(delegation.name), delegation.parent.Close())
}
