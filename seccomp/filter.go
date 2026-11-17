//go:build linux

package seccomp

import (
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

// Architecture identifies the audit ABI of the caller-supplied syscall numbers.
// Only the current native amd64 or arm64 ABI is accepted by New.
type Architecture uint32

// AMD64 selects AUDIT_ARCH_X86_64 with x32 syscalls forbidden.
const AMD64 Architecture = unix.AUDIT_ARCH_X86_64

// ARM64 selects AUDIT_ARCH_AARCH64.
const ARM64 Architecture = unix.AUDIT_ARCH_AARCH64

// NativeArchitecture returns the running program's syscall ABI. Other Go
// architectures return zero and an error matching EOPNOTSUPP.
func NativeArchitecture() (Architecture, error) {
	switch runtime.GOARCH {
	case "amd64":
		return AMD64, nil
	case "arm64":
		return ARM64, nil
	default:
		return 0, os.NewSyscallError("seccomp architecture", unix.EOPNOTSUPP)
	}
}

// New validates and copies instructions for the explicit native architecture.
// It makes no syscalls or state changes, returns nil on error, and rejects
// mismatched/unknown architectures before examining instructions. Length must
// be 1..MaxInstructions; the last instruction must Return. Loads must be aligned
// uint32 words inside the 64-byte seccomp_data. Unknown operations, actions,
// ignored nonzero fields/data, and jumps outside the body match os.ErrInvalid.
// Kernel installation can still fail due to policy, resources, or filter trees.
func New(architecture Architecture, instructions []Instruction) (*Filter, error) {
	native, err := NativeArchitecture()
	if err != nil {
		return nil, err
	}

	validLength := len(instructions) > 0 && len(instructions) <= MaxInstructions
	if architecture != native || !validLength {
		return nil, os.NewSyscallError("seccomp program", os.ErrInvalid)
	}
	if instructions[len(instructions)-1].Code != Return {
		return nil, os.NewSyscallError("seccomp program", os.ErrInvalid)
	}

	program := architectureGuard(native)
	for index, instruction := range instructions {
		if !validInstruction(instruction, len(instructions)-index-1) {
			return nil, os.NewSyscallError("seccomp program", os.ErrInvalid)
		}
		program = append(program, unix.SockFilter{
			Code: uint16(instruction.Code), Jt: instruction.Jt, Jf: instruction.Jf, K: instruction.K,
		})
	}
	return &Filter{program: program}, nil
}

// Filter holds an immutable validated program. New copies caller storage;
// subsequent mutations cannot alter the filter. Use through a pointer without
// copying. The zero value owns nothing and cannot be loaded. It acquires no
// descriptors and needs no Close; discarding it cannot remove loaded policy.
// Concurrent Loads may share this value, but callers coordinate kernel policy.
type Filter struct {
	program []unix.SockFilter
}

func architectureGuard(architecture Architecture) []unix.SockFilter {
	program := []unix.SockFilter{
		{Code: uint16(LoadWord), K: ArchitectureOffset},
		{Code: uint16(JumpEqual), K: uint32(architecture), Jt: 1},
		{Code: uint16(Return), K: uint32(KillProcess)},
	}
	if architecture == AMD64 {
		// x86-64 and x32 share an audit architecture; the syscall bit separates them.
		// https://man7.org/linux/man-pages/man2/seccomp.2.html
		program = append(program,
			unix.SockFilter{Code: uint16(LoadWord), K: SyscallOffset},
			unix.SockFilter{Code: uint16(JumpBitsSet), K: 0x40000000, Jf: 1},
			unix.SockFilter{Code: uint16(Return), K: uint32(KillProcess)},
		)
	}
	return append(program, unix.SockFilter{Code: uint16(LoadConstant)})
}

func validInstruction(instruction Instruction, remaining int) bool {
	switch instruction.Code {
	case JumpEqual, JumpGreater, JumpGreaterOrEqual, JumpBitsSet:
		validTrueTarget := int(instruction.Jt) < remaining
		validFalseTarget := int(instruction.Jf) < remaining
		return validTrueTarget && validFalseTarget
	}
	if instruction.Jt|instruction.Jf != 0 {
		return false
	}

	switch instruction.Code {
	case LoadWord:
		withinData := instruction.K < 64
		wordAligned := instruction.K%4 == 0
		return withinData && wordAligned
	case LoadConstant, And:
		return true
	case Jump:
		return instruction.K < uint32(remaining) //nolint:gosec // remaining is bounded by MaxInstructions.
	case Return:
		return validReturn(instruction.K)
	default:
		return false
	}
}
