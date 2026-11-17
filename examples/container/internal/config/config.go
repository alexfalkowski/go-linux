//go:build linux

package config

// Config carries validated settings shared by the parent and helper stages.
// Parsing flags belongs to the command entrypoint.
type Config struct {
	// Root is the absolute trusted filesystem root containing a real tmp directory.
	Root string
	// Delegation is an optional writable cgroup v2 subtree; empty disables limits.
	Delegation string
	// Stage is the internal helper's private mount target.
	Stage string
	// ParentMount is the mount identity captured before child namespace creation.
	ParentMount string
	// Arguments contains an absolute executable path inside Root and literal argv.
	Arguments []string
	// Memory is the positive memory.max value in bytes when Delegation is selected.
	Memory int64
	// Tasks is the positive pids.max value when Delegation is selected.
	Tasks int64
	// Initialize selects the internal helper stage after standard Go child startup.
	Initialize bool
}
