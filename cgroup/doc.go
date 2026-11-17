//go:build linux

// Package cgroup provides owned handles to Linux cgroup v2 directories.
//
// Open an explicitly selected hierarchy root or delegated subtree with Open.
// New creates one child, Open opens one child, and Remove removes an empty
// child. Close releases only a local handle; it never removes groups, moves
// processes, disables controllers, or restores limits. Handles are independent
// of the path used to acquire them. No descriptors or raw file operations are
// exposed.
//
// Controller enablement is explicit through SetControllers. The caller must
// arrange delegation, permissions, and any no-internal-process constraint;
// neither root privileges nor a systemd-created directory imply ownership.
// Operations preserve kernel errno, including unavailable-controller and
// delegation errors. Separate calls are not a transaction and have no rollback.
//
// The bounded controller surface covers cpu.weight, cpu.max, memory.high,
// memory.max, memory.current, memory.events, pids.max, pids.current, and
// pids.events. Membership uses whole processes through cgroup.procs; numeric
// PIDs can race with exit and reuse. Threaded subtree management, v1, mounting,
// permission delegation, systemd integration, and process startup are omitted.
// Use os/exec for startup and explicitly coordinate migration with the child.
//
// See the [cgroup v2 interface] and the package usage guide for prerequisites.
// The library targets Linux 7.0 or later; kernel configuration, enabled parent
// controllers, and delegated write permissions remain runtime requirements.
//
// [cgroup v2 interface]: https://docs.kernel.org/admin-guide/cgroup-v2.html
package cgroup
