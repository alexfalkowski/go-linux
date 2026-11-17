//go:build linux

package cgroup

import (
	"os"
	"slices"
	"strconv"
	"strings"
)

// Controller is a Linux controller name. Inspection preserves unknown names;
// mutation currently accepts only CPU, Memory, and PIDs.
type Controller string

// CPU selects the v2 CPU controller (cpu), including fair-class bandwidth.
const CPU Controller = "cpu"

// Memory selects the v2 memory controller (memory), with byte-valued limits.
const Memory Controller = "memory"

// PIDs selects the v2 task-count controller (pids); it counts threads, not just
// the process leaders returned by Processes.
const PIDs Controller = "pids"

// Controllers returns names from cgroup.controllers, including unknown names.
// Availability for children depends on the parent's enabled controllers and
// kernel configuration. It does not establish delegated write permission.
func (g *Group) Controllers() ([]Controller, error) {
	return g.controllers("cgroup.controllers")
}

// SubtreeControl reads cgroup.subtree_control: controllers enabled for immediate
// children. This differs from the limits applying to this group itself.
func (g *Group) SubtreeControl() ([]Controller, error) {
	return g.controllers("cgroup.subtree_control")
}

// SetControllers enables and disables controllers for immediate children in one
// cgroup.subtree_control write. At least one change is required; unsupported or
// duplicate names (including overlap) match os.ErrInvalid. Unmentioned names
// remain unchanged. Linux applies the whole command or rejects it, including
// unavailable controllers, missing delegation, populated internal domains, or
// controllers still required by descendants. No ancestor is modified.
func (g *Group) SetControllers(enable, disable []Controller) error {
	var commands []string
	seen := make(map[Controller]bool)
	for index, controllers := range [][]Controller{enable, disable} {
		for _, controller := range controllers {
			supported := slices.Contains([]Controller{CPU, Memory, PIDs}, controller)
			duplicate := seen[controller]
			if !supported || duplicate {
				return &os.PathError{Op: "write", Path: "cgroup.subtree_control", Err: os.ErrInvalid}
			}
			seen[controller] = true

			var prefix string
			if index == 0 {
				prefix = "+"
			} else {
				prefix = "-"
			}
			commands = append(commands, prefix+string(controller))
		}
	}
	if len(commands) == 0 {
		return &os.PathError{Op: "write", Path: "cgroup.subtree_control", Err: os.ErrInvalid}
	}
	return g.write("cgroup.subtree_control", []byte(strings.Join(commands, " ")))
}

// CPUWeight reads cpu.weight, the relative fair-class scheduling weight.
func (g *Group) CPUWeight() (uint64, error) {
	return g.number("cpu.weight")
}

// SetCPUWeight writes cpu.weight. weight must be in [1, 10000]; 100 is Linux's
// default. Invalid weights match os.ErrInvalid before opening any control file.
func (g *Group) SetCPUWeight(weight uint64) error {
	if weight < 1 || weight > 10000 {
		return &os.PathError{Op: "write", Path: "cpu.weight", Err: os.ErrInvalid}
	}
	return g.write("cpu.weight", []byte(strconv.FormatUint(weight, 10)))
}

// CPUMax reads the complete fair-class cpu.max bandwidth record.
func (g *Group) CPUMax() (CPUBandwidth, error) {
	data, err := g.read("cpu.max")
	if err != nil {
		return CPUBandwidth{}, err
	}

	var value CPUBandwidth
	err = value.UnmarshalText(data)
	return value, err
}

// SetCPUMax writes both cpu.max fields in one command. Invalid durations match
// os.ErrInvalid. Only fair-class scheduling is controlled; this does not promise
// a process-wide CPU reservation or override ancestor limits.
func (g *Group) SetCPUMax(value CPUBandwidth) error {
	data, err := value.MarshalText()
	if err != nil {
		return &os.PathError{Op: "write", Path: "cpu.max", Err: err}
	}
	return g.write("cpu.max", data)
}

// CPUStat returns cpu.stat counters, retaining unknown keys and Linux units
// (notably usage_usec, user_usec, and system_usec are microseconds).
func (g *Group) CPUStat() (map[string]uint64, error) {
	return g.counters("cpu.stat")
}

// MemoryHigh reads memory.high, a byte-valued reclaim/throttling boundary or Max.
func (g *Group) MemoryHigh() (Limit, error) {
	return g.limit("memory.high")
}

// SetMemoryHigh writes memory.high. Crossing it throttles/reclaims rather than
// invoking the OOM killer directly. O_NONBLOCK bypasses synchronous reclaim in
// the writer; subsequent charges trigger reclaim/throttling. Usage can remain
// above the requested limit indefinitely when the workload makes no new charges.
func (g *Group) SetMemoryHigh(value Limit) error {
	return g.setLimit("memory.high", value)
}

// MemoryMax reads memory.max, the hard byte-valued boundary or Max.
func (g *Group) MemoryMax() (Limit, error) {
	return g.limit("memory.max")
}

// SetMemoryMax writes memory.max. Exceeding it can cause cgroup OOM kills.
// O_NONBLOCK bypasses synchronous reclaim/OOM in the writer; enforcement occurs
// during subsequent charges. The kernel may round byte limits to page size.
func (g *Group) SetMemoryMax(value Limit) error {
	return g.setLimit("memory.max", value)
}

// MemoryCurrent reads memory.current in bytes, including descendants.
func (g *Group) MemoryCurrent() (uint64, error) {
	return g.number("memory.current")
}

// MemoryEvents reads memory.events, retaining all unsigned counters. Values are
// hierarchical unless the mount's memory_localevents option changes semantics.
// A snapshot does not establish that a particular process caused an event.
func (g *Group) MemoryEvents() (map[string]uint64, error) {
	return g.counters("memory.events")
}

// PIDsMax reads pids.max, a task-count boundary or Max.
func (g *Group) PIDsMax() (Limit, error) {
	return g.limit("pids.max")
}

// SetPIDsMax writes pids.max. Zero prevents new tasks; migration is still allowed
// even above the limit. New fork/clone attempts exceeding a limit fail EAGAIN.
func (g *Group) SetPIDsMax(value Limit) error {
	return g.setLimit("pids.max", value)
}

// PIDsCurrent reads pids.current, counting tasks including descendants.
func (g *Group) PIDsCurrent() (uint64, error) {
	return g.number("pids.current")
}

// PIDsEvents reads pids.events, preserving unknown counters. The max counter
// reports limit hits; pids_localevents changes its hierarchical semantics.
func (g *Group) PIDsEvents() (map[string]uint64, error) {
	return g.counters("pids.events")
}

func (g *Group) controllers(name string) ([]Controller, error) {
	data, err := g.read(name)
	if err != nil {
		return nil, err
	}

	var controllers []Controller
	for field := range strings.FieldsSeq(string(data)) {
		controllers = append(controllers, Controller(field))
	}
	return controllers, nil
}

func (g *Group) number(name string) (uint64, error) {
	data, err := g.read(name)
	if err != nil {
		return 0, err
	}
	return decimal(strings.TrimSpace(string(data)))
}

func (g *Group) limit(name string) (Limit, error) {
	data, err := g.read(name)
	if err != nil {
		return 0, err
	}

	var value Limit
	err = value.UnmarshalText(data)
	return value, err
}

func (g *Group) setLimit(name string, value Limit) error {
	data, err := value.MarshalText()
	if err != nil {
		return &os.PathError{Op: "write", Path: name, Err: err}
	}
	return g.write(name, data)
}

func (g *Group) counters(name string) (map[string]uint64, error) {
	data, err := g.read(name)
	if err != nil {
		return nil, err
	}
	return ParseCounters(data)
}
