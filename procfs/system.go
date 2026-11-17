//go:build linux

package procfs

import (
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/prometheus/procfs"
)

// CPUTime contains cumulative CPU counters in seconds, as converted by the
// upstream reader from Linux USER_HZ ticks. Values are floating-point counters,
// not wall time or percentages. Guest is already included in User, and GuestNice
// in Nice. IOWait can decrease between reads; deltas need caller interpretation.
type CPUTime struct {
	// User includes time running guest virtual CPUs.
	User float64
	// Nice includes guest time for niced processes.
	Nice float64
	// System counts execution in kernel mode.
	System float64
	// Idle counts time in the idle task.
	Idle float64
	// IOWait is Linux's approximate I/O-wait accounting.
	IOWait float64
	// IRQ counts servicing hardware interrupts.
	IRQ float64
	// SoftIRQ counts servicing software interrupts.
	SoftIRQ float64
	// Steal counts involuntary wait while another virtual CPU runs.
	Steal float64
	// Guest is the guest component already included in User.
	Guest float64
	// GuestNice is the guest component already included in Nice.
	GuestNice float64
}

// SystemStat contains selected system-wide /proc/stat counters. The proc mount
// need not virtualize these values to the caller's cgroup or CPU affinity.
type SystemStat struct {
	// CPUs maps Linux CPU numbers to their counters; offline CPUs may be absent.
	CPUs map[int]CPUTime
	// BootTime is the reported Unix boot time in UTC, or zero if unavailable.
	BootTime time.Time
	// Total aggregates all CPUs and can grow faster than elapsed wall time.
	Total CPUTime
	// ContextSwitches counts context switches since boot.
	ContextSwitches uint64
	// ProcessesCreated counts forks, including thread creation, since boot.
	ProcessesCreated uint64
	// ProcessesRunning counts currently runnable tasks.
	ProcessesRunning uint64
	// ProcessesBlocked counts tasks blocked waiting for I/O.
	ProcessesBlocked uint64
}

// Stat reads basic CPU and process counters from [proc_stat(5)]. At least one
// per-CPU record is required; unreported individual counters remain zero.
// Each result owns its CPU map and may be changed independently of other reads.
//
// [proc_stat(5)]: https://man7.org/linux/man-pages/man5/proc_stat.5.html
func (fs *FS) Stat() (SystemStat, error) {
	if fs == nil || fs.root == "" {
		return SystemStat{}, os.ErrInvalid
	}

	path := filepath.Join(fs.root, "stat")
	native, err := read(path, fs.fs.Stat)
	if err != nil {
		return SystemStat{}, err
	}

	return systemStat(path, native)
}

// MemoryInfo contains basic RAM and swap quantities in bytes. These are system
// observations, not cgroup limits or the memory available to this process alone.
type MemoryInfo struct {
	// Total is usable physical RAM.
	Total uint64
	// Free is currently unused RAM.
	Free uint64
	// Available estimates RAM available to new workloads without swapping.
	Available uint64
	// Buffers is temporary raw block storage.
	Buffers uint64
	// Cached is the reported page cache excluding SwapCached.
	Cached uint64
	// SwapTotal is configured swap capacity; zero is a valid value.
	SwapTotal uint64
	// SwapFree is unused swap capacity; zero is a valid value.
	SwapFree uint64
}

// Memory reads [proc_meminfo(5)] and converts Linux kB (1024 bytes) to bytes.
// All seven exposed fields must be present and fit uint64 after conversion;
// absence never silently becomes zero. Additional upstream-supported numeric
// fields are ignored. Malformed input and conversion overflow match ErrParse.
//
// [proc_meminfo(5)]: https://man7.org/linux/man-pages/man5/proc_meminfo.5.html
func (fs *FS) Memory() (MemoryInfo, error) {
	if fs == nil || fs.root == "" {
		return MemoryInfo{}, os.ErrInvalid
	}

	path := filepath.Join(fs.root, "meminfo")
	native, err := read(path, fs.fs.Meminfo)
	if err != nil {
		return MemoryInfo{}, err
	}

	fields := []struct {
		kilobytes *uint64
		bytes     *uint64
	}{
		{kilobytes: native.MemTotal, bytes: native.MemTotalBytes},
		{kilobytes: native.MemFree, bytes: native.MemFreeBytes},
		{kilobytes: native.MemAvailable, bytes: native.MemAvailableBytes},
		{kilobytes: native.Buffers, bytes: native.BuffersBytes},
		{kilobytes: native.Cached, bytes: native.CachedBytes},
		{kilobytes: native.SwapTotal, bytes: native.SwapTotalBytes},
		{kilobytes: native.SwapFree, bytes: native.SwapFreeBytes},
	}
	for _, field := range fields {
		if field.kilobytes == nil || field.bytes == nil {
			return MemoryInfo{}, parseError(path)
		}

		validKilobytes := *field.kilobytes <= math.MaxUint64/1024
		if !validKilobytes || *field.bytes != *field.kilobytes*1024 {
			return MemoryInfo{}, parseError(path)
		}
	}
	return MemoryInfo{
		Total: *native.MemTotalBytes, Free: *native.MemFreeBytes, Available: *native.MemAvailableBytes,
		Buffers: *native.BuffersBytes, Cached: *native.CachedBytes, SwapTotal: *native.SwapTotalBytes, SwapFree: *native.SwapFreeBytes,
	}, nil
}

func cpuTime(native procfs.CPUStat) CPUTime {
	return CPUTime{
		User: native.User, Nice: native.Nice, System: native.System, Idle: native.Idle,
		IOWait: native.Iowait, IRQ: native.IRQ, SoftIRQ: native.SoftIRQ, Steal: native.Steal,
		Guest: native.Guest, GuestNice: native.GuestNice,
	}
}

func (cpu CPUTime) valid() bool {
	for _, value := range [...]float64{cpu.User, cpu.Nice, cpu.System, cpu.Idle, cpu.IOWait, cpu.IRQ, cpu.SoftIRQ, cpu.Steal, cpu.Guest, cpu.GuestNice} {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}

func systemStat(path string, native procfs.Stat) (SystemStat, error) {
	total := cpuTime(native.CPUTotal)
	validCPURecords := len(native.CPU) > 0 && total.valid()
	if !validCPURecords || native.BootTime > math.MaxInt64 {
		return SystemStat{}, parseError(path)
	}

	cpus := make(map[int]CPUTime, len(native.CPU))
	for id, counters := range native.CPU {
		cpu := cpuTime(counters)
		validID := id >= 0 && id <= math.MaxInt32
		if !validID || !cpu.valid() {
			return SystemStat{}, parseError(path)
		}

		cpus[int(id)] = cpu
	}

	var bootTime time.Time
	if native.BootTime != 0 {
		bootTime = time.Unix(int64(native.BootTime), 0).UTC()
	}
	return SystemStat{
		CPUs: cpus, BootTime: bootTime, Total: total, ContextSwitches: native.ContextSwitches,
		ProcessesCreated: native.ProcessCreated, ProcessesRunning: native.ProcessesRunning, ProcessesBlocked: native.ProcessesBlocked,
	}, nil
}
