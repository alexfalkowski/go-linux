//go:build linux

package stats

import (
	"errors"
	"os"
	"syscall"
	"time"

	"github.com/alexfalkowski/go-linux/procfs"
)

// Open selects a trusted procfs root; the returned views retain no descriptors.
func Open(root string) (*Collector, error) {
	fs, err := procfs.Open(root)
	if err != nil {
		return nil, err
	}
	return &Collector{fs: fs}, nil
}

// Collector retains prior process identities and CPU counters between samples.
type Collector struct {
	fs       *procfs.FS
	previous map[int]procfs.ProcessStat
	sampled  time.Time
}

// Snapshot owns one frame's observations; its reads are not atomic.
type Snapshot struct {
	// Processes owns the process rows and may be reordered by the display.
	Processes []Process
	// Memory describes the selected proc mount's system memory view, in bytes.
	Memory procfs.MemoryInfo
	// Skipped counts processes that disappeared or denied access during sampling.
	Skipped int
}

// Process combines native process observations with a sampled CPU delta.
type Process struct {
	// Stat retains Linux units and the observed process identity/start ticks.
	Stat procfs.ProcessStat
	// PID identifies this observation in the selected proc mount.
	PID int
	// CPUPercent is percent of one logical CPU, and can exceed 100.
	CPUPercent float64
}

// Prime records a baseline before the first refresh interval.
func (sampler *Collector) Prime() error {
	_, _, err := sampler.processes()
	return err
}

// Read collects fresh process and system memory observations.
func (sampler *Collector) Read() (Snapshot, error) {
	rows, skipped, err := sampler.processes()
	if err != nil {
		return Snapshot{}, err
	}
	memory, err := sampler.fs.Memory()
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Processes: rows, Memory: memory, Skipped: skipped}, nil
}

func (sampler *Collector) processes() ([]Process, int, error) {
	processes, err := sampler.fs.Processes()
	if err != nil {
		return nil, 0, err
	}
	now := time.Now()
	elapsed := now.Sub(sampler.sampled).Seconds()
	current := make(map[int]procfs.ProcessStat, len(processes))
	rows := make([]Process, 0, len(processes))
	var skipped int
	for _, pid := range processes {
		stat, err := readProcess(sampler.fs, pid)
		if err != nil {
			disappeared := errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH)
			if disappeared || errors.Is(err, os.ErrPermission) {
				skipped++
				continue
			}
			return nil, skipped, err
		}
		previous, seen := sampler.previous[pid]
		var cpu float64
		if seen {
			cpu = cpuPercent(stat, previous, elapsed)
		}
		rows = append(rows, Process{PID: pid, Stat: stat, CPUPercent: cpu})
		current[pid] = stat
	}
	sampler.previous, sampler.sampled = current, now
	return rows, skipped, nil
}

func readProcess(fs *procfs.FS, pid int) (procfs.ProcessStat, error) {
	process, err := fs.Process(pid)
	if err != nil {
		return procfs.ProcessStat{}, err
	}
	return process.Stat()
}

func cpuPercent(current, previous procfs.ProcessStat, elapsed float64) float64 {
	sameProcess := previous.StartTicks == current.StartTicks
	countersIncreased := current.UserTicks >= previous.UserTicks && current.SystemTicks >= previous.SystemTicks
	if !sameProcess || !countersIncreased || elapsed <= 0 {
		return 0
	}
	ticks := float64(current.UserTicks-previous.UserTicks) + float64(current.SystemTicks-previous.SystemTicks)
	return ticks / elapsed // USER_HZ=100; convert seconds to percent.
}
