//go:build linux

package seccomp_test

import (
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/seccomp"
	"github.com/stretchr/testify/require"
)

func TestComparisonsSelectBothBranches(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		operation seccomp.Operation
		value     uint32
		constant  uint32
		denied    bool
	}{
		{"equal", seccomp.JumpEqual, 7, 7, true},
		{"unequal", seccomp.JumpEqual, 8, 7, false},
		{"greater", seccomp.JumpGreater, 8, 7, true},
		{"not greater", seccomp.JumpGreater, 7, 7, false},
		{"equal boundary", seccomp.JumpGreaterOrEqual, 7, 7, true},
		{"greater boundary", seccomp.JumpGreaterOrEqual, 8, 7, true},
		{"less boundary", seccomp.JumpGreaterOrEqual, 6, 7, false},
		{"bit intersection", seccomp.JumpBitsSet, 3, 2, true},
		{"no bit intersection", seccomp.JumpBitsSet, 1, 2, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if !test.SeccompProcess(t) {
				return
			}
			test.LockThread()
			filter := test.SeccompFilter(t, []seccomp.Instruction{
				{Code: seccomp.LoadWord, K: seccomp.SyscallOffset},
				{Code: seccomp.JumpEqual, K: test.ProcessGroupSyscall, Jf: 3},
				{Code: seccomp.LoadConstant, K: scenario.value},
				{Code: scenario.operation, K: scenario.constant, Jf: 1},
				{Code: seccomp.Return, K: uint32(seccomp.Errno) | uint32(test.ErrPermissionDenied)},
				{Code: seccomp.Return, K: uint32(seccomp.Allow)},
			})

			require.NoError(t, filter.Load(seccomp.Options{Synchronization: seccomp.CallingThread, Privileges: seccomp.SetNoNewPrivileges}))

			_, err := test.ProcessGroup(0)
			if scenario.denied {
				require.ErrorIs(t, err, test.ErrPermissionDenied)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestAndAndJumpPreserveInstructionSemantics(t *testing.T) {
	if !test.SeccompProcess(t) {
		return
	}
	test.LockThread()
	filter := test.SeccompFilter(t, []seccomp.Instruction{
		{Code: seccomp.LoadWord, K: seccomp.SyscallOffset},
		{Code: seccomp.JumpEqual, K: test.ProcessGroupSyscall, Jf: 5},
		{Code: seccomp.LoadConstant, K: 0x1234},
		{Code: seccomp.And, K: 0x00ff},
		{Code: seccomp.JumpEqual, K: 0x34, Jf: 2},
		{Code: seccomp.Jump, K: 2},
		{Code: seccomp.Return, K: uint32(seccomp.Errno) | uint32(test.ErrOperationNotPermitted)},
		{Code: seccomp.Return, K: uint32(seccomp.Allow)},
		{Code: seccomp.Return, K: uint32(seccomp.Errno) | uint32(test.ErrPermissionDenied)},
	})

	require.NoError(t, filter.Load(seccomp.Options{Synchronization: seccomp.CallingThread, Privileges: seccomp.SetNoNewPrivileges}))

	_, err := test.ProcessGroup(0)
	require.ErrorIs(t, err, test.ErrPermissionDenied, "AND must retain the low byte and Jump must skip both intermediate returns")
}

func TestGuardRestoresZeroAccumulator(t *testing.T) {
	if !test.SeccompProcess(t) {
		return
	}
	test.LockThread()
	filter := test.SeccompFilter(t, []seccomp.Instruction{
		{Code: seccomp.JumpEqual, K: 0, Jt: 1},
		{Code: seccomp.Return, K: uint32(seccomp.KillProcess)},
		{Code: seccomp.Return, K: uint32(seccomp.Allow)},
	})

	require.NoError(t, filter.Load(seccomp.Options{Synchronization: seccomp.CallingThread, Privileges: seccomp.SetNoNewPrivileges}))

	_, err := test.ProcessGroup(0)
	require.NoError(t, err)
}
