//go:build linux

package procfs

// ProcessStat contains selected /proc/PID/stat values. CPU and start counters
// retain Linux clock ticks; ResidentPages retains the kernel's page unit.
// Name may contain spaces, parentheses, and newlines. State retains unknown
// one-character Linux states. No wall-clock start time is inferred.
type ProcessStat struct {
	// Name is the kernel comm name, not an executable path or command line.
	Name string
	// State is the kernel's one-character task state.
	State string
	// ParentPID is zero when the parent is outside the visible PID namespace.
	ParentPID int
	// ProcessGroupID identifies the process group in this proc mount's namespace.
	ProcessGroupID int
	// SessionID identifies the session in this proc mount's namespace.
	SessionID int
	// Threads is the reported number of threads in the process.
	Threads int
	// UserTicks counts scheduled user CPU time in Linux USER_HZ ticks.
	UserTicks uint64
	// SystemTicks counts scheduled kernel CPU time in Linux USER_HZ ticks.
	SystemTicks uint64
	// StartTicks is the start time since boot in USER_HZ ticks, not Unix time.
	StartTicks uint64
	// VirtualMemory is the virtual address-space size in bytes.
	VirtualMemory uint64
	// ResidentPages is the approximate resident set in system pages, not bytes.
	ResidentPages int64
}

// Stat reads selected process identity and statistics from [proc_pid_stat(5)].
// It reads afresh on every call. On supported amd64/arm64 Linux, USER_HZ is 100;
// these counters are independent of the kernel's scheduler HZ configuration.
// Access checks may make some kernel-restricted fields appear as zero.
//
// [proc_pid_stat(5)]: https://man7.org/linux/man-pages/man5/proc_pid_stat.5.html
func (p *Process) Stat() (ProcessStat, error) {
	path, err := p.path("stat")
	if err != nil {
		return ProcessStat{}, err
	}

	native, err := read(path, p.process.Stat)
	if err != nil {
		return ProcessStat{}, err
	}

	validIdentity := native.PPID >= 0 && native.PGRP >= 0 && native.Session >= 0
	validCounts := native.NumThreads >= 0 && native.RSS >= 0
	if len(native.State) != 1 || !validIdentity || !validCounts {
		return ProcessStat{}, parseError(path)
	}
	return ProcessStat{
		Name: native.Comm, State: native.State, ParentPID: native.PPID,
		ProcessGroupID: native.PGRP, SessionID: native.Session, Threads: native.NumThreads,
		UserTicks: uint64(native.UTime), SystemTicks: uint64(native.STime), StartTicks: native.Starttime,
		VirtualMemory: uint64(native.VSize), ResidentPages: int64(native.RSS),
	}, nil
}
