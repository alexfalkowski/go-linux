//go:build linux

package seccomp

import "golang.org/x/sys/unix"

// Operation identifies a supported classic-BPF instruction encoding.
type Operation uint16

// LoadWord loads a native-endian uint32 from seccomp_data at byte offset K.
const LoadWord Operation = unix.BPF_LD | unix.BPF_W | unix.BPF_ABS

// LoadConstant replaces the accumulator with K.
const LoadConstant Operation = unix.BPF_LD | unix.BPF_W | unix.BPF_IMM

// And replaces the accumulator with its bitwise intersection with K.
const And Operation = unix.BPF_ALU | unix.BPF_AND | unix.BPF_K

// Jump skips K instructions unconditionally, relative to the next instruction.
const Jump Operation = unix.BPF_JMP | unix.BPF_JA

// JumpEqual selects Jt when the accumulator equals K, otherwise Jf.
const JumpEqual Operation = unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K

// JumpGreater selects Jt when the unsigned accumulator exceeds K, otherwise Jf.
const JumpGreater Operation = unix.BPF_JMP | unix.BPF_JGT | unix.BPF_K

// JumpGreaterOrEqual selects Jt when the unsigned accumulator is at least K.
const JumpGreaterOrEqual Operation = unix.BPF_JMP | unix.BPF_JGE | unix.BPF_K

// JumpBitsSet selects Jt when the accumulator and K share any set bit.
const JumpBitsSet Operation = unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K

// Return ends evaluation with K, an Action optionally combined with 16-bit data.
const Return Operation = unix.BPF_RET | unix.BPF_K

// SyscallOffset is the byte offset of seccomp_data.nr, interpreted as uint32.
const SyscallOffset = 0

// ArchitectureOffset is the byte offset of seccomp_data.arch (an AUDIT_ARCH value).
const ArchitectureOffset = 4

// InstructionPointerOffset is the byte offset of the 64-bit instruction pointer.
const InstructionPointerOffset = 8

// ArgumentsOffset is the byte offset of six 64-bit syscall arguments. On the
// supported little-endian architectures, argument n's low/high words are at
// ArgumentsOffset+8*n and ArgumentsOffset+8*n+4, respectively.
const ArgumentsOffset = 16

// MaxInstructions bounds caller instructions, reserving room for the largest
// mandatory guard within Linux's 4096-instruction per-filter limit.
const MaxInstructions = 4089

// Instruction represents the supported constant-operand subset of classic BPF.
// The accumulator starts at zero. Jumps are forward, relative to the following
// instruction, and must remain inside the caller's program. There is no
// index register, scratch memory, indirect load, or computed return action.
type Instruction struct {
	// K is an immediate, byte offset, jump distance, or encoded return action,
	// according to Code. Return data is combined as uint32(Errno)|errno, etc.
	K uint32
	// Code must be one of this package's named operations.
	Code Operation
	// Jt skips this many instructions when a conditional comparison succeeds.
	// It must be zero for nonconditional operations.
	Jt uint8
	// Jf skips this many instructions when a conditional comparison fails.
	// It must be zero for nonconditional operations.
	Jf uint8
}

// Action identifies a Linux SECCOMP_RET_* result. Use uint32 to combine it
// with data in an Instruction's K. Unknown actions and SECCOMP_RET_USER_NOTIF
// are rejected.
type Action uint32

// KillProcess terminates the entire thread group with SIGSYS.
const KillProcess Action = unix.SECCOMP_RET_KILL_PROCESS

// KillThread terminates only the triggering thread. This can corrupt shared
// program state or strand Go runtime locks; prefer KillProcess for termination.
const KillThread Action = unix.SECCOMP_RET_KILL_THREAD

// Trap delivers SIGSYS without executing the syscall; low 16-bit data becomes
// si_errno. Go's signal behavior remains in force; no handler is installed.
const Trap Action = unix.SECCOMP_RET_TRAP

// Errno skips the syscall and returns low 16-bit data as errno, clamped by Linux
// to 4095. Zero data reports success without performing the syscall.
const Errno Action = unix.SECCOMP_RET_ERRNO

// Trace invokes a PTRACE_O_TRACESECCOMP tracer, or returns ENOSYS without one.
// The low 16-bit data is available to the tracer. Tracer management is external.
const Trace Action = unix.SECCOMP_RET_TRACE

// Log permits the syscall and requests audit logging, subject to host policy.
const Log Action = unix.SECCOMP_RET_LOG

// Allow permits the syscall unless a higher-precedence stacked filter refuses it.
const Allow Action = unix.SECCOMP_RET_ALLOW

func validReturn(value uint32) bool {
	action := Action(value & unix.SECCOMP_RET_ACTION_FULL)
	switch action {
	case Errno, Trap, Trace:
		return true
	case KillProcess, KillThread, Log, Allow:
		return value&unix.SECCOMP_RET_DATA == 0
	default:
		return false
	}
}
