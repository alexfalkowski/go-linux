//go:build linux

package capability

import "golang.org/x/sys/unix"

// Set is a capability bit mask, with the CAP_* number selecting its bit.
// Combine constants with | and remove them with &^. Zero is the empty set.
// Input accepts the capabilities defined by Linux 7.0; inspection preserves
// unknown output bits within the 64-bit capability ABI.
type Set uint64

// Chown selects CAP_CHOWN.
const Chown Set = 1 << unix.CAP_CHOWN

// DACOverride selects CAP_DAC_OVERRIDE.
const DACOverride Set = 1 << unix.CAP_DAC_OVERRIDE

// DACReadSearch selects CAP_DAC_READ_SEARCH.
const DACReadSearch Set = 1 << unix.CAP_DAC_READ_SEARCH

// Fowner selects CAP_FOWNER.
const Fowner Set = 1 << unix.CAP_FOWNER

// Fsetid selects CAP_FSETID.
const Fsetid Set = 1 << unix.CAP_FSETID

// Kill selects CAP_KILL.
const Kill Set = 1 << unix.CAP_KILL

// Setgid selects CAP_SETGID.
const Setgid Set = 1 << unix.CAP_SETGID

// Setuid selects CAP_SETUID.
const Setuid Set = 1 << unix.CAP_SETUID

// Setpcap selects CAP_SETPCAP.
const Setpcap Set = 1 << unix.CAP_SETPCAP

// LinuxImmutable selects CAP_LINUX_IMMUTABLE.
const LinuxImmutable Set = 1 << unix.CAP_LINUX_IMMUTABLE

// NetBindService selects CAP_NET_BIND_SERVICE.
const NetBindService Set = 1 << unix.CAP_NET_BIND_SERVICE

// NetBroadcast selects CAP_NET_BROADCAST.
const NetBroadcast Set = 1 << unix.CAP_NET_BROADCAST

// NetAdmin selects CAP_NET_ADMIN.
const NetAdmin Set = 1 << unix.CAP_NET_ADMIN

// NetRaw selects CAP_NET_RAW.
const NetRaw Set = 1 << unix.CAP_NET_RAW

// IPCLock selects CAP_IPC_LOCK.
const IPCLock Set = 1 << unix.CAP_IPC_LOCK

// IPCOwner selects CAP_IPC_OWNER.
const IPCOwner Set = 1 << unix.CAP_IPC_OWNER

// SysModule selects CAP_SYS_MODULE.
const SysModule Set = 1 << unix.CAP_SYS_MODULE

// SysRawIO selects CAP_SYS_RAWIO.
const SysRawIO Set = 1 << unix.CAP_SYS_RAWIO

// SysChroot selects CAP_SYS_CHROOT.
const SysChroot Set = 1 << unix.CAP_SYS_CHROOT

// SysPtrace selects CAP_SYS_PTRACE.
const SysPtrace Set = 1 << unix.CAP_SYS_PTRACE

// SysPacct selects CAP_SYS_PACCT.
const SysPacct Set = 1 << unix.CAP_SYS_PACCT

// SysAdmin selects CAP_SYS_ADMIN.
const SysAdmin Set = 1 << unix.CAP_SYS_ADMIN

// SysBoot selects CAP_SYS_BOOT.
const SysBoot Set = 1 << unix.CAP_SYS_BOOT

// SysNice selects CAP_SYS_NICE.
const SysNice Set = 1 << unix.CAP_SYS_NICE

// SysResource selects CAP_SYS_RESOURCE.
const SysResource Set = 1 << unix.CAP_SYS_RESOURCE

// SysTime selects CAP_SYS_TIME.
const SysTime Set = 1 << unix.CAP_SYS_TIME

// SysTTYConfig selects CAP_SYS_TTY_CONFIG.
const SysTTYConfig Set = 1 << unix.CAP_SYS_TTY_CONFIG

// Mknod selects CAP_MKNOD.
const Mknod Set = 1 << unix.CAP_MKNOD

// Lease selects CAP_LEASE.
const Lease Set = 1 << unix.CAP_LEASE

// AuditWrite selects CAP_AUDIT_WRITE.
const AuditWrite Set = 1 << unix.CAP_AUDIT_WRITE

// AuditControl selects CAP_AUDIT_CONTROL.
const AuditControl Set = 1 << unix.CAP_AUDIT_CONTROL

// Setfcap selects CAP_SETFCAP.
const Setfcap Set = 1 << unix.CAP_SETFCAP

// MACOverride selects CAP_MAC_OVERRIDE.
const MACOverride Set = 1 << unix.CAP_MAC_OVERRIDE

// MACAdmin selects CAP_MAC_ADMIN.
const MACAdmin Set = 1 << unix.CAP_MAC_ADMIN

// Syslog selects CAP_SYSLOG.
const Syslog Set = 1 << unix.CAP_SYSLOG

// WakeAlarm selects CAP_WAKE_ALARM.
const WakeAlarm Set = 1 << unix.CAP_WAKE_ALARM

// BlockSuspend selects CAP_BLOCK_SUSPEND.
const BlockSuspend Set = 1 << unix.CAP_BLOCK_SUSPEND

// AuditRead selects CAP_AUDIT_READ.
const AuditRead Set = 1 << unix.CAP_AUDIT_READ

// Perfmon selects CAP_PERFMON.
const Perfmon Set = 1 << unix.CAP_PERFMON

// BPF selects CAP_BPF.
const BPF Set = 1 << unix.CAP_BPF

// CheckpointRestore selects CAP_CHECKPOINT_RESTORE.
const CheckpointRestore Set = 1 << unix.CAP_CHECKPOINT_RESTORE

const supported = (CheckpointRestore << 1) - 1
